package website

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"

	"nexadmin/docs"
)

// 官网 /docs 文档区：渲染框架仓库 docs/*.md（经 nexadmin/docs 包内嵌进二进制）。
// 文档是框架区内容，与官网插件解耦——这里只负责目录、渲染与路由。

// DocTocItem 文档页“本页目录”条目（从渲染后的 h2/h3 锚点提取）。
type DocTocItem struct {
	Level int
	Text  string
	Href  string
}

// DocView 当前渲染的文档页。
type DocView struct {
	Slug       string
	Title      string
	Content    template.HTML
	TOC        []DocTocItem
	Language   string // 正文实际语言，缺译时不能继承页面的 lang
	IsFallback bool
}

// docItem 目录清单里的一篇文档；slug 用于 URL（ASCII），file 指向中文源文。
// Title/Summary 是源文的兜底文案；译文按稳定语言 Code 定位并自带 front-matter，
// 不依赖可配置的默认语言或 URL 前缀（见 resolveDoc）。
type docItem struct {
	Slug    string
	File    string
	Title   string
	Summary string
}

// docGroup 目录清单分组。
type docGroup struct {
	Key   string
	Items []docItem
}

// DocItem 侧边栏一篇文档（URL 已带语言前缀，Active 标记当前页）。
type DocItem struct {
	Slug     string
	Title    string
	URL      string
	Active   bool
	Language string
}

// DocGroup 侧边栏分组。
type DocGroup struct {
	Key   string
	Title string
	Items []DocItem
}

// DocCard 是文档首页的一张入口卡片。
type DocCard struct {
	Title    string
	Summary  string
	Group    string
	URL      string
	Language string
}

// DocLink 是上一篇/下一篇。
type DocLink struct {
	Title    string
	URL      string
	Language string
}

// docGroups 文档目录：顺序即侧边栏顺序。新增文档 = docs/ 放 md + 这里加一行。
var docGroups = []docGroup{
	{Key: "start", Items: []docItem{
		{Slug: "quickstart", File: "quickstart.md", Title: "快速开始",
			Summary: "准备开发环境、派生项目并启动服务，完成第一次登录与生产构建。"},
	}},
	{Key: "dev", Items: []docItem{
		{Slug: "dev-guide", File: "二次开发指南.md", Title: "二次开发指南",
			Summary: "从派生项目到升级：框架区与用户区的边界、可用的覆盖点与最小示例。"},
		{Slug: "plugin-dev", File: "plugin-development.md", Title: "插件开发指南",
			Summary: "插件包结构、清单、后端与前端约定，以及打包和安装验证。"},
	}},
	{Key: "ref", Items: []docItem{
		{Slug: "features", File: "功能与权限清单.md", Title: "功能与权限清单",
			Summary: "内置模块、登录方式、权限码与数据范围的完整清单。"},
		{Slug: "config", File: "配置参考.md", Title: "配置参考",
			Summary: "配置文件加载规则与各分区的键、默认值和运行期开关。"},
		{Slug: "deploy", File: "部署与运维指南.md", Title: "部署与运维指南",
			Summary: "构建、单机/容器部署、升级、备份与日常运维。"},
		{Slug: "media", File: "媒体能力接入.md", Title: "媒体能力接入",
			Summary: "图片 WebP、视频托管与嵌入、地图选点的开关、接入方式与排障。"},
	}},
}

// docLocalized 是一篇文档在某个语言下的呈现结果。
type docLocalized struct {
	File       string
	Title      string
	Summary    string
	Language   string
	IsFallback bool
}

const docSourceLanguage = "zh-CN"

var docLanguageCodeRe = regexp.MustCompile(`^[a-z]{2,8}(-[a-z0-9]{1,8})*$`)

// docLanguageDirs 约定：小写完整 Code 优先，再试主语言码，例如 pt-br/ → pt/。
// en-US 因而继续使用 en/；zh-CN 明确使用根目录中文源文。新增语言只需放译文，
// 不用登记 Go 映射，也不随默认语言和 URL Prefix 的修改移动文件。
func docLanguageDirs(code string) []string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == strings.ToLower(docSourceLanguage) || !docLanguageCodeRe.MatchString(code) {
		return nil
	}
	out := []string{code}
	if base, _, ok := strings.Cut(code, "-"); ok {
		out = append(out, base)
	}
	return out
}

// resolveDoc 按稳定语言代码定位文件；缺译时显式标记中文源文回退。
// 元数据取实际文件的 front-matter，缺省键沿用清单中的兜底值。
func resolveDoc(it docItem, lang Language) docLocalized {
	out := docLocalized{
		File: it.File, Title: it.Title, Summary: it.Summary, Language: docSourceLanguage,
		IsFallback: lang.Code != "" && !strings.EqualFold(strings.TrimSpace(lang.Code), docSourceLanguage),
	}
	for _, dir := range docLanguageDirs(lang.Code) {
		file := dir + "/" + it.Slug + ".md"
		if _, ok := docMetaFor(file); ok {
			out.File, out.Language, out.IsFallback = file, dir, false
			break
		}
	}
	meta, _ := docMetaFor(out.File)
	if meta["title"] != "" {
		out.Title = meta["title"]
	}
	if meta["summary"] != "" {
		out.Summary = meta["summary"]
	}
	return out
}

// splitFrontMatter 剥出文件开头的 --- 块并解析成 key: value。
// 只支持一行一个标量，按**第一个**冒号切（值里可以再含冒号）；
// 文档只需要 title / summary 两个键，不值得为此引入 YAML 解析。
// 没有闭合的 --- 时原样返回，绝不把正文吃掉。
func splitFrontMatter(src []byte) (map[string]string, []byte) {
	text := strings.TrimPrefix(string(src), "\ufeff")
	first, rest, found := strings.Cut(text, "\n")
	if !found || strings.TrimSpace(first) != "---" {
		return nil, src
	}
	lines := strings.Split(rest, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "---" {
			continue
		}
		meta := map[string]string{}
		for _, kv := range lines[:i] {
			k, v, hasV := strings.Cut(kv, ":")
			if k = strings.TrimSpace(k); hasV && k != "" {
				meta[k] = scalarValue(v)
			}
		}
		return meta, []byte(strings.Join(lines[i+1:], "\n"))
	}
	return nil, src // 没有闭合的 ---：原样交给 markdown，绝不吞正文
}

// scalarValue 取一个 front-matter 标量的值：去首尾空白，并脱掉成对的引号。
// 摘要里常有冒号，按 YAML 习惯写的人会顺手加引号；不脱就会把引号渲染进标题。
func scalarValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 2 {
		return v
	}
	if q := v[0]; (q == '"' || q == '\'') && v[len(v)-1] == q {
		return v[1 : len(v)-1]
	}
	return v
}

// docMetaFor 读一篇文档的 front-matter；ok=false 表示该文件不存在（这门语言还没有译文）。
// 内嵌文件集在运行期不变，所以连「不存在」一起缓存。
func docMetaFor(file string) (map[string]string, bool) {
	if v, ok := docMetaCache.Load(file); ok {
		e := v.(docMetaEntry)
		return e.meta, e.found
	}
	b, err := docs.Files.ReadFile(file)
	e := docMetaEntry{found: err == nil}
	if err == nil {
		e.meta, _ = splitFrontMatter(b)
	}
	docMetaCache.Store(file, e)
	return e.meta, e.found
}

type docMetaEntry struct {
	meta  map[string]string
	found bool
}

var docMetaCache sync.Map // file -> docMetaEntry

// docCards 生成文档首页卡片（保持目录顺序）。
func (v *siteView) docCards() []DocCard {
	out := make([]DocCard, 0, 8)
	for _, g := range docGroups {
		for _, it := range g.Items {
			d := resolveDoc(it, v.Lang)
			out = append(out, DocCard{
				Title:    d.Title,
				Summary:  d.Summary,
				Group:    v.T("docs.group." + g.Key),
				URL:      v.URL("/docs/" + it.Slug),
				Language: d.Language,
			})
		}
	}
	return out
}

// docNeighbors 返回当前文档的上一篇/下一篇（跨分组按目录顺序）。
func (v *siteView) docNeighbors(current string) (*DocLink, *DocLink) {
	var flat []docItem
	for _, g := range docGroups {
		flat = append(flat, g.Items...)
	}
	for i, it := range flat {
		if it.Slug != current {
			continue
		}
		var prev, next *DocLink
		link := func(item docItem) *DocLink {
			d := resolveDoc(item, v.Lang)
			return &DocLink{Title: d.Title, URL: v.URL("/docs/" + item.Slug), Language: d.Language}
		}
		if i > 0 {
			prev = link(flat[i-1])
		}
		if i+1 < len(flat) {
			next = link(flat[i+1])
		}
		return prev, next
	}
	return nil, nil
}

func findDoc(slug string) (docItem, bool) {
	for _, g := range docGroups {
		for _, it := range g.Items {
			if it.Slug == slug {
				return it, true
			}
		}
	}
	return docItem{}, false
}

// firstDoc 文档区入口默认打开的一篇。
func firstDoc() docItem { return docGroups[0].Items[0] }

// GFM 扩展：表格/删除线/任务列表/自动链接（文档里大量使用表格）。
// WithAutoHeadingID：为标题生成锚点 id，供“本页目录”跳转。
// 代码高亮用 github-dark 内联样式：正文代码块两套主题下都固定深底（配色由 chroma 内联输出，无需额外 CSS）。
var mdRenderer = goldmark.New(
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithExtensions(
		extension.GFM,
		highlighting.NewHighlighting(highlighting.WithStyle("github-dark")),
	),
)

// tocRe 从渲染 HTML 提取 h2/h3 锚点（goldmark 自动生成 heading id）。
var tocRe = regexp.MustCompile(`<h([23]) id="([^"]+)"[^>]*>(.*?)</h[23]>`)
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func extractTOC(s string) []DocTocItem {
	out := []DocTocItem{}
	for _, m := range tocRe.FindAllStringSubmatch(s, -1) {
		lvl := 0
		_, _ = fmt.Sscanf(m[1], "%d", &lvl)
		// 先反转义再去标签：标题原文交回模板后再转义一次，避免 & 显示成 &amp;
		text := html.UnescapeString(htmlTagRe.ReplaceAllString(m[3], ""))
		out = append(out, DocTocItem{Level: lvl, Text: text, Href: "#" + m[2]})
	}
	return out
}

// docHTMLCache 缓存渲染结果：文档内容编译期固定，渲染一次终身使用。
var docHTMLCache sync.Map // file -> template.HTML

func renderDocHTML(file string) (template.HTML, error) {
	if v, ok := docHTMLCache.Load(file); ok {
		return v.(template.HTML), nil
	}
	b, err := docs.Files.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("docs: %w", err)
	}
	var buf bytes.Buffer
	// front-matter 只是给目录用的元数据，不能进正文渲染。
	_, body := splitFrontMatter(b)
	node := mdRenderer.Parser().Parse(text.NewReader(body))
	_ = ast.Walk(node, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if link, ok := node.(*ast.Link); ok && entering {
			link.Destination = []byte(docLinkDestination(file, string(link.Destination)))
		}
		return ast.WalkContinue, nil
	})
	if err := mdRenderer.Renderer().Render(&buf, body, node); err != nil {
		return "", fmt.Errorf("docs markdown: %w", err)
	}
	// 正文标题交给页面模板渲染（docs.tmpl 用 manifest 的标题 + 站点样式），去掉 md 里自己的 h1。
	s := buf.String()
	if i := strings.Index(s, "<h1"); i >= 0 {
		if j := strings.Index(s[i:], "</h1>"); j >= 0 {
			s = s[:i] + s[i+j+len("</h1>"):]
		}
	}
	out := template.HTML(s)
	docHTMLCache.Store(file, out)
	return out, nil
}

// docNav 组装侧边栏：组名走 i18n，链接带语言前缀，标题来自实际语言文件。
func (v *siteView) docNav(current string) []DocGroup {
	out := make([]DocGroup, 0, len(docGroups))
	for _, g := range docGroups {
		items := make([]DocItem, 0, len(g.Items))
		for _, it := range g.Items {
			d := resolveDoc(it, v.Lang)
			items = append(items, DocItem{
				Slug:     it.Slug,
				Title:    d.Title,
				URL:      v.URL("/docs/" + it.Slug),
				Active:   it.Slug == current,
				Language: d.Language,
			})
		}
		out = append(out, DocGroup{Key: g.Key, Title: v.T("docs.group." + g.Key), Items: items})
	}
	return out
}

func docLinkDestination(file, destination string) string {
	u, err := url.Parse(destination)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "/") || !strings.HasSuffix(u.Path, ".md") {
		return destination
	}
	target := path.Clean(path.Join(path.Dir(file), u.Path))
	dir, base := path.Dir(target), path.Base(target)
	if dir != "." && !docLanguageCodeRe.MatchString(dir) {
		return destination
	}
	for _, group := range docGroups {
		for _, item := range group.Items {
			if target == item.File || dir != "." && base == item.Slug+".md" {
				u.Path, u.RawPath = item.Slug, ""
				return u.String()
			}
		}
	}
	return destination
}

// docSearchEntry 是当前语言下的整篇正文索引，只包含已公开登记的文档。
type docSearchEntry struct {
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Text       string `json:"text"`
	URL        string `json:"url"`
	Language   string `json:"language"`
	IsFallback bool   `json:"isFallback"`
}

// 块间补空白，行内标签直接移除：高亮代码的多个 span 不能拆散一个搜索词。
var docBlockTagRe = regexp.MustCompile(`(?i)</(?:p|h[1-6]|li|td|th|tr|pre|blockquote|div)>|<br\s*/?>`)
var docTextCache sync.Map // file -> string

func docPlainText(s string) string {
	s = docBlockTagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(htmlTagRe.ReplaceAllString(s, ""))
	return strings.Join(strings.Fields(s), " ")
}

func (v *siteView) docSearchEntries() ([]docSearchEntry, error) {
	out := make([]docSearchEntry, 0, 8)
	for _, group := range docGroups {
		for _, it := range group.Items {
			d := resolveDoc(it, v.Lang)
			text, ok := docTextCache.Load(d.File)
			if !ok {
				content, err := renderDocHTML(d.File)
				if err != nil {
					return nil, err
				}
				text = docPlainText(string(content))
				docTextCache.Store(d.File, text)
			}
			out = append(out, docSearchEntry{
				Title: d.Title, Summary: d.Summary, Text: text.(string),
				URL: v.URL("/docs/" + it.Slug), Language: d.Language, IsFallback: d.IsFallback,
			})
		}
	}
	return out, nil
}

// DocSearchIndexJSON 在现有文档页面中内嵌索引，不增加接口或外部搜索服务。
// 返回普通 string，由 html/template 转义进 hidden 元素，site.js 只读 textContent；
// 不使用 safeHTML/template.JS，也没有可执行的内联脚本或 CSP 放宽。
func (v *siteView) DocSearchIndexJSON() (string, error) {
	entries, err := v.docSearchEntries()
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(entries)
	return string(b), err
}

// GET|HEAD /docs —— 文档首页（分组卡片）。
func (h *siteHandler) docsIndex(c *gin.Context) {
	lang := langOf(c)
	v, err := h.baseView(c.Request.Context(), lang.Code, 3)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.DocNav = v.docNav("")
	v.DocCards = v.docCards()
	v.Title = v.T("docs.index.title") + " - " + v.SiteName
	v.Desc = v.T("docs.desc")
	h.render(c, "docs-index", http.StatusOK, v)
}

// GET|HEAD /docs/:slug —— 渲染一篇框架文档。
func (h *siteHandler) docPage(c *gin.Context) {
	item, ok := findDoc(c.Param("slug"))
	if !ok {
		v, err := h.baseView(c.Request.Context(), langOf(c).Code, 3)
		if err != nil {
			c.String(http.StatusInternalServerError, "site unavailable")
			return
		}
		h.notFound(c, v)
		return
	}
	lang := langOf(c)
	doc := resolveDoc(item, lang)
	content, err := renderDocHTML(doc.File)
	if err != nil {
		c.String(http.StatusInternalServerError, "docs unavailable")
		return
	}
	v, err := h.baseView(c.Request.Context(), lang.Code, 3)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.DocsEnabled = true
	v.DocNav = v.docNav(item.Slug)
	v.Doc = &DocView{
		Slug: item.Slug, Title: doc.Title, Content: content, TOC: extractTOC(string(content)),
		Language: doc.Language, IsFallback: doc.IsFallback,
	}
	v.DocPrev, v.DocNext = v.docNeighbors(item.Slug)
	v.Title = doc.Title + " - " + v.SiteName
	v.Desc = doc.Summary
	h.render(c, "docs", http.StatusOK, v)
}
