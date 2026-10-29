package website

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	nethtml "golang.org/x/net/html"

	"nexadmin/docs"
)

// 文档区的多语言契约：译文目录按稳定 Code，不随默认语言或 URL 前缀变化。
// 标题与摘要写在文件的 front-matter 里；中文源文固定为 zh-CN。

var enLang = Language{Code: "en-US", Prefix: "en"}
var jaLang = Language{Code: "ja-JP", Prefix: "ja"}

func allDocItems() []docItem {
	var out []docItem
	for _, g := range docGroups {
		out = append(out, g.Items...)
	}
	return out
}

// TestEveryDocHasEnglishTranslation 校验嵌入与元数据契约，不锁死会持续修订的文案。
func TestEveryDocHasEnglishTranslation(t *testing.T) {
	for _, it := range allDocItems() {
		file := "en/" + it.Slug + ".md"
		raw, err := docs.Files.ReadFile(file)
		if err != nil {
			t.Errorf("embedded English file missing: %v", err)
			continue
		}
		meta, _ := splitFrontMatter(raw)
		if meta["title"] == "" || meta["summary"] == "" {
			t.Errorf("%s needs title and summary front-matter", file)
		}
		d := resolveDoc(it, enLang)
		if d.File != file || d.Language != "en" || d.IsFallback {
			t.Errorf("slug %q: unexpected translation: %+v", it.Slug, d)
		}
		if d.Title != meta["title"] || d.Summary != meta["summary"] {
			t.Errorf("slug %q: metadata must come from the English file: %+v", it.Slug, d)
		}
	}
}

// TestResolveDocUnknownLanguageFallsBack 是这次改造的正靶：
// 没有日语译文时，日语访客必须看到中文原文，而不是「任何非默认语言都拿英文文件」。
func TestResolveDocUnknownLanguageFallsBack(t *testing.T) {
	it, ok := findDoc("deploy")
	if !ok {
		t.Fatal("deploy not in doc manifest")
	}
	source := resolveDoc(it, Language{Code: "zh-CN", Default: true})
	got := resolveDoc(it, jaLang)
	source.IsFallback = true
	if got != source {
		t.Errorf("ja without translations should fall back to marked source: got %+v want %+v", got, source)
	}
	if strings.Contains(got.File, "en/") || strings.Contains(got.Title, "Deployment") {
		t.Errorf("ja leaked English content: %+v", got)
	}
}

func TestResolveDocIndependentOfDefaultAndPrefix(t *testing.T) {
	it, _ := findDoc("config")
	for _, lang := range []Language{
		{Code: "en-US", Default: true},
		{Code: "en-US", Prefix: "english"},
		{Code: "en-US"},
		{Code: "EN-us", Prefix: "read"},
		{Code: "zh-CN", Prefix: "cn"},
		{Code: "zh-CN", Prefix: "en"},
		{Code: "zh-TW"},
		{Code: "ja-JP", Prefix: "en", Default: true},
	} {
		t.Run(lang.Code+"/"+lang.Prefix+"/"+strconv.FormatBool(lang.Default), func(t *testing.T) {
			got := resolveDoc(it, lang)
			wantFile, wantLanguage := it.File, docSourceLanguage
			if strings.EqualFold(lang.Code, "en-US") {
				wantFile, wantLanguage = "en/config.md", "en"
			}
			fallback := lang.Code == "zh-TW" || lang.Code == "ja-JP"
			if got.File != wantFile || got.Language != wantLanguage || got.IsFallback != fallback {
				t.Fatalf("language %+v resolved to %+v", lang, got)
			}
			v := &siteView{Lang: lang}
			for _, group := range v.docNav(it.Slug) {
				for _, item := range group.Items {
					if item.Slug == it.Slug && (item.Title != got.Title || item.URL != lang.Path("/docs/config") || !item.Active) {
						t.Errorf("localized navigation mismatch: %+v", item)
					}
				}
			}
		})
	}
}

func TestSplitFrontMatter(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantKV  map[string]string
		wantRes string
	}{
		{"basic", "---\ntitle: T\nsummary: S\n---\nbody", map[string]string{"title": "T", "summary": "S"}, "body"},
		{"colon in value", "---\nsummary: a: b\n---\nx", map[string]string{"summary": "a: b"}, "x"},
		{"double quoted", "---\ntitle: \"T\"\n---\nb", map[string]string{"title": "T"}, "b"},
		{"single quoted", "---\ntitle: 'T'\n---\nb", map[string]string{"title": "T"}, "b"},
		{"unbalanced quote stays", "---\ntitle: \"T\n---\nb", map[string]string{"title": `"T`}, "b"},
		{"lone quote stays", "---\ntitle: \"\n---\nb", map[string]string{"title": `"`}, "b"},
		{"blank value", "---\ntitle:\n---\nb", map[string]string{"title": ""}, "b"},
		{"unclosed stays intact", "---\ntitle: T\nno fence here", nil, "---\ntitle: T\nno fence here"},
		{"no fence", "# H1\nbody", nil, "# H1\nbody"},
		{"empty block", "---\n---\nbody", map[string]string{}, "body"},
	}
	for _, c := range cases {
		meta, rest := splitFrontMatter([]byte(c.in))
		if got := string(rest); got != c.wantRes {
			t.Errorf("%s: rest = %q, want %q", c.name, got, c.wantRes)
		}
		if c.wantKV == nil {
			if meta != nil {
				t.Errorf("%s: meta = %v, want nil", c.name, meta)
			}
			continue
		}
		if len(meta) != len(c.wantKV) {
			t.Errorf("%s: meta = %v, want %v", c.name, meta, c.wantKV)
			continue
		}
		for k, v := range c.wantKV {
			if meta[k] != v {
				t.Errorf("%s: meta[%q] = %q, want %q", c.name, k, meta[k], v)
			}
		}
	}
}

// TestRenderedDocsHideFrontMatter 渲染结果里不能出现 front-matter 的摘要原文——
// 漏剥时 goldmark 会把「--- 键值 ---」当成 setext 标题渲染，正文顶部多出一个假 h2。
// 断言用整条摘要句：正文不会原样复述它，所以既敏感又不会误报。
func TestRenderedDocsHideFrontMatter(t *testing.T) {
	langs := []Language{{Code: "zh-CN", Default: true}, enLang}
	for _, it := range allDocItems() {
		for _, lang := range langs {
			d := resolveDoc(it, lang)
			html, err := renderDocHTML(d.File)
			if err != nil {
				t.Fatalf("%s: %v", d.File, err)
			}
			if d.Summary != "" && strings.Contains(string(html), d.Summary) {
				t.Errorf("%s leaked its front-matter summary into the body", d.File)
			}
		}
	}
}

func TestDocManifestAndQuickstart(t *testing.T) {
	if docGroups[0].Key != "start" || firstDoc().Slug != "quickstart" {
		t.Fatal("quickstart must be the first document in Getting started")
	}
	validSlug := regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	seen := map[string]bool{}
	for _, item := range allDocItems() {
		if seen[item.Slug] || !validSlug.MatchString(item.Slug) {
			t.Errorf("invalid or duplicate document slug: %q", item.Slug)
		}
		seen[item.Slug] = true
		for _, file := range []string{item.File, "en/" + item.Slug + ".md"} {
			if raw, err := docs.Files.ReadFile(file); err != nil || len(raw) == 0 {
				t.Errorf("document must exist in embedded files: %s: %v", file, err)
			}
		}
	}
	for _, code := range []string{"zh-CN", "en-US"} {
		dict, err := readDict(code)
		if err != nil {
			t.Fatal(err)
		}
		for _, group := range docGroups {
			if dict["docs.group."+group.Key] == "" {
				t.Errorf("missing %s group translation: %s", code, group.Key)
			}
		}
	}
	v := &siteView{Lang: enLang}
	prev, next := v.docNeighbors("quickstart")
	if prev != nil || next == nil || next.URL != "/en/docs/dev-guide" {
		t.Fatalf("quickstart neighbors: prev=%+v next=%+v", prev, next)
	}
	if cards := v.docCards(); len(cards) != len(seen) || cards[0].URL != "/en/docs/quickstart" {
		t.Fatalf("quickstart missing from index cards: %+v", cards)
	}
}

func TestDocLanguageDirectories(t *testing.T) {
	for code, want := range map[string][]string{
		"en-US": {"en-us", "en"}, "pt-BR": {"pt-br", "pt"},
		"ja-JP": {"ja-jp", "ja"}, "zh-TW": {"zh-tw", "zh"}, "fr": {"fr"},
		"zh-CN": nil, "": nil, "../en": nil, "en/us": nil,
	} {
		if got := docLanguageDirs(code); !reflect.DeepEqual(got, want) {
			t.Errorf("directories for %s: got %v want %v", code, got, want)
		}
	}
	// 用缓存模拟新增译文，不往仓库写测试 Markdown，也不要求某语言已出厂。
	item := docItem{Slug: "__language-test", File: "__source-test.md", Title: "原文"}
	files := []string{"pt-br/__language-test.md", "pt/__language-test.md"}
	for _, file := range files {
		docMetaCache.Store(file, docMetaEntry{found: true, meta: map[string]string{"title": file}})
		t.Cleanup(func() { docMetaCache.Delete(file) })
	}
	lang := Language{Code: "pt-BR", Prefix: "portuguese", Default: true}
	if got := resolveDoc(item, lang); got.File != files[0] || got.Language != "pt-br" || got.IsFallback {
		t.Fatalf("full code must win: %+v", got)
	}
	docMetaCache.Store(files[0], docMetaEntry{found: false})
	if got := resolveDoc(item, lang); got.File != files[1] || got.Language != "pt" || got.IsFallback {
		t.Fatalf("base language should be supported: %+v", got)
	}
}

func TestDocPlainText(t *testing.T) {
	got := docPlainText(`<h2>Body &amp; examples</h2><p>A <code>keyword</code>.</p><pre><span>docker</span><span>-compose</span></pre><table><tr><td>one</td><td>two</td></tr></table><p>&lt;script&gt;safe&lt;/script&gt;</p>`)
	want := "Body & examples A keyword. docker-compose one two <script>safe</script>"
	if got != want {
		t.Fatalf("plain text = %q, want %q", got, want)
	}
}

func TestDocSearchIndexFullTextAndLanguage(t *testing.T) {
	for _, lang := range []Language{enLang, {Code: "en-US", Default: true}, {Code: "en-US", Prefix: "english"}, {Code: "zh-CN", Prefix: "cn"}, jaLang} {
		v := &siteView{Lang: lang}
		raw, err := v.DocSearchIndexJSON()
		if err != nil {
			t.Fatal(err)
		}
		var entries []docSearchEntry
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			t.Fatal(err)
		}
		items := allDocItems()
		if len(entries) != len(items) {
			t.Fatalf("index entries = %d, want %d", len(entries), len(items))
		}
		for i, entry := range entries {
			d := resolveDoc(items[i], lang)
			content, err := renderDocHTML(d.File)
			if err != nil {
				t.Fatal(err)
			}
			if entry.Text != docPlainText(string(content)) || len(entry.Text) < 500 {
				t.Errorf("%s/%s must index the full body", lang.Code, items[i].Slug)
			}
			if entry.URL != lang.Path("/docs/"+items[i].Slug) || entry.Title != d.Title || entry.Language != d.Language || entry.IsFallback != d.IsFallback {
				t.Errorf("index language/link mismatch: %+v", entry)
			}
		}
		// 命令仅出现在正文，旧的目录/摘要过滤搜不到它。
		if !strings.Contains(entries[0].Text, "go run ./cmd/nexadmin new myapp") {
			t.Error("quickstart body-only command missing from the index")
		}
	}
}

func testDocsView(t *testing.T, lang Language, slug string) *siteView {
	t.Helper()
	v := testView()
	v.Lang, v.DocsEnabled = lang, true
	v.DocNav, v.DocCards = v.docNav(slug), v.docCards()
	v.Route, v.Path = "/docs", lang.Path("/docs")
	v.Title = v.T("docs.index.title")
	if slug == "" {
		return v
	}
	item, ok := findDoc(slug)
	if !ok {
		t.Fatalf("missing fixture doc %s", slug)
	}
	d := resolveDoc(item, lang)
	content, err := renderDocHTML(d.File)
	if err != nil {
		t.Fatal(err)
	}
	v.Doc = &DocView{Slug: slug, Title: d.Title, Content: content, TOC: extractTOC(string(content)), Language: d.Language, IsFallback: d.IsFallback}
	v.DocPrev, v.DocNext = v.docNeighbors(slug)
	v.Route, v.Path = "/docs/"+slug, lang.Path("/docs/"+slug)
	v.Title, v.Desc = d.Title, d.Summary
	return v
}

// 只替换索引方法，仍用真实模板，验证恶意文本不能闭合 hidden 元素或变成脚本。
type docSearchTemplateView struct {
	*siteView
	index string
}

func (v docSearchTemplateView) DocSearchIndexJSON() string { return v.index }

func renderedDocIndex(t *testing.T, page string) []docSearchEntry {
	t.Helper()
	root, err := nethtml.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	var index *nethtml.Node
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		for _, attr := range n.Attr {
			if attr.Key == "id" && attr.Val == "docs-search-index" {
				index = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if index == nil {
		t.Fatal("search index missing from rendered page")
	}
	var text strings.Builder
	for c := index.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != nethtml.TextNode {
			t.Fatalf("index must contain text only, found %v/%s", c.Type, c.Data)
		}
		text.WriteString(c.Data)
	}
	var entries []docSearchEntry
	if err := json.Unmarshal([]byte(text.String()), &entries); err != nil {
		t.Fatalf("rendered index cannot be decoded: %v", err)
	}
	return entries
}

func TestDocLinksUsePublicSlugs(t *testing.T) {
	for _, tt := range []struct{ file, href, want string }{
		{"quickstart.md", "二次开发指南.md#3", "dev-guide#3"},
		{"plugin-development.md", "quickstart.md", "quickstart"},
		{"en/quickstart.md", "plugin-dev.md?from=start#install", "plugin-dev?from=start#install"},
		{"en/plugin-dev.md", "../二次开发指南.md", "dev-guide"},
		{"quickstart.md", "https://example.com/quickstart.md", "https://example.com/quickstart.md"},
		{"quickstart.md", "../core/README.md", "../core/README.md"},
		{"quickstart.md", "/docs/config", "/docs/config"},
		{"quickstart.md", "#install", "#install"},
	} {
		if got := docLinkDestination(tt.file, tt.href); got != tt.want {
			t.Errorf("%s: %s => %s, want %s", tt.file, tt.href, got, tt.want)
		}
	}
	for _, file := range []string{"quickstart.md", "en/quickstart.md"} {
		page, err := renderDocHTML(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(page), `href="plugin-dev"`) {
			t.Errorf("%s lacks a working plugin guide link", file)
		}
	}
}

func TestDocSearchIndexTemplateHTMLSafety(t *testing.T) {
	attack := `</div></script><img id="doc-xss" src=x onerror=alert(1)><script>alert(1)</script>&"'` + "\u2028\u2029"
	want := []docSearchEntry{{Title: attack, Summary: attack, Text: attack, URL: "/docs/quickstart", Language: "en"}}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docs", "docs-index"} {
		var b strings.Builder
		v := docSearchTemplateView{testDocsView(t, enLang, "quickstart"), string(raw)}
		if err := pageTmpl[name].ExecuteTemplate(&b, "layout", v); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), `<img id="doc-xss"`) || strings.Contains(b.String(), "ZgotmplZ") {
			t.Fatal("unsafe or invalid template output")
		}
		if got := renderedDocIndex(t, b.String()); !reflect.DeepEqual(got, want) {
			t.Fatalf("index did not round-trip safely: %+v", got)
		}
	}
}

func TestDocSourceLanguageMarkup(t *testing.T) {
	// 模拟英文缺译，界面依旧英文，正文/目录标题必须标记中文，不误报已翻译。
	item := docItem{Slug: "__missing-translation", File: "配置参考.md", Title: "配置参考"}
	d := resolveDoc(item, enLang)
	v := testDocsView(t, enLang, "config")
	content, err := renderDocHTML(d.File)
	if err != nil {
		t.Fatal(err)
	}
	v.Doc = &DocView{Slug: item.Slug, Title: d.Title, Content: content, TOC: extractTOC(string(content)), Language: d.Language, IsFallback: d.IsFallback}
	var b strings.Builder
	if err := pageTmpl["docs"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`class="docs-content" lang="zh-CN"`, v.T("docs.sourceFallback"), `data-docs-disclosure="760"`, `data-docs-disclosure="1200"`, `aria-controls="docs-search-results"`} {
		if !strings.Contains(b.String(), text) {
			t.Errorf("missing language/accessibility markup: %s", text)
		}
	}
	for _, lang := range []Language{{Code: "en-US", Default: true}, {Code: "zh-CN", Prefix: "cn"}} {
		v := testDocsView(t, lang, "config")
		b.Reset()
		if err := pageTmpl["docs"].ExecuteTemplate(&b, "layout", v); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), `class="docs-content" lang="`+v.Doc.Language+`"`) || strings.Contains(b.String(), `class="docs-source-note"`) {
			t.Errorf("available document should have its own language and no fallback notice: %+v", lang)
		}
	}
}

// 复用 testView 和真实页面模板/资源，避开 SiteData 的数据库依赖。
func docsPreviewHandler(t *testing.T) http.Handler {
	t.Helper()
	r := gin.New()
	h := &siteHandler{}
	r.GET("/website-assets/*path", h.asset)
	for _, lang := range Languages() {
		for _, slug := range append([]string{""}, docSlugs()...) {
			name := "docs"
			if slug == "" {
				name = "docs-index"
			}
			view := testDocsView(t, lang, slug)
			r.GET(view.Path, func(c *gin.Context) {
				v := *view // render 会设置请求级元数据，不能并发修改共享 fixture
				h.render(c, name, http.StatusOK, &v)
			})
		}
	}
	return r
}

func docSlugs() []string {
	var slugs []string
	for _, item := range allDocItems() {
		slugs = append(slugs, item.Slug)
	}
	return slugs
}

func TestDocsHTTPPreview(t *testing.T) {
	handler := docsPreviewHandler(t)
	for _, lang := range Languages() {
		for _, slug := range append([]string{""}, docSlugs()...) {
			path := lang.Path("/docs")
			if slug != "" {
				path += "/" + slug
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("GET %s: %d: %s", path, w.Code, w.Body.String())
			}
			if got := renderedDocIndex(t, w.Body.String()); len(got) != len(allDocItems()) {
				t.Errorf("GET %s: incomplete full-text index", path)
			}
		}
	}
}

// 手工预览（仅回环地址，无数据库）：设置 NEXADMIN_DOCS_PREVIEW_PORT 后单独运行本测试，
// 使用 -timeout=0 保持运行；停止测试进程即可关闭服务。
func TestDocsPreview(t *testing.T) {
	port := os.Getenv("NEXADMIN_DOCS_PREVIEW_PORT")
	if port == "" {
		t.Skip("set NEXADMIN_DOCS_PREVIEW_PORT for an opt-in browser preview")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		t.Fatal("invalid preview port")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(docsPreviewHandler(t))
	_ = srv.Listener.Close()
	srv.Listener = listener
	srv.Start()
	defer srv.Close()
	t.Logf("Docs preview: %s/docs and %s/en/docs", srv.URL, srv.URL)
	select {}
}
