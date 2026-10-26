package website

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"time"

	"nexadmin/core/plugin/market"
)

var templateKeyRe = regexp.MustCompile(`\.TF? "([^"]+)"`)

// 模板里出现的每个 key 都必须在每种语言里有非空译文；新增语言时这条测试会兜底。
func TestLocaleDictionariesCoverTemplates(t *testing.T) {
	used := map[string]bool{}
	err := fs.WalkDir(tplFS, "tpl", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := tplFS.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range templateKeyRe.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(used) < 200 {
		t.Fatalf("只扫描到 %d 个模板 key，扫描规则可能失效", len(used))
	}
	for _, l := range Languages() {
		d := dicts[l.Code]
		if len(d) == 0 {
			t.Fatalf("语言 %s 没有字典", l.Code)
		}
		for key := range used {
			if strings.TrimSpace(d[key]) == "" {
				t.Errorf("语言 %s 缺少模板文案 %q", l.Code, key)
			}
		}
		// 脚本文案不在模板里，单独校验。
		for _, key := range []string{"packaging", "packagingHint", "packFailed", "copied", "copyManual", "menuOpen", "menuClose"} {
			if strings.TrimSpace(d["site.js."+key]) == "" {
				t.Errorf("语言 %s 缺少脚本文案 site.js.%s", l.Code, key)
			}
		}
	}
	// 动态拼出来的分类与占位价也要有译文。
	for _, key := range []string{
		"market.category.ai", "market.category.security", "market.category.devops",
		"market.category.business", "market.category.content",
		"market.priceTbd", "market.priceFree", "market.all", "market.free", "market.pro",
		"error.msg", "error.titleTag", "market.metaDesc", "market.titleTag",
		"news.pageDesc", "news.pageTitleTag", "plugin.rateInvalid", "plugin.rateFailed",
	} {
		for _, l := range Languages() {
			if strings.TrimSpace(dicts[l.Code][key]) == "" {
				t.Errorf("语言 %s 缺少动态文案 %q", l.Code, key)
			}
		}
	}
}

func TestNegotiateAcceptLanguage(t *testing.T) {
	en, _ := LanguageByCode("en-US")
	def := DefaultLanguage()
	cases := []struct {
		header string
		want   string
		ok     bool
	}{
		{"en-US,en;q=0.9", "en-US", true},
		{"en-GB,en;q=0.9", "en-US", true},
		{"zh-CN,zh;q=0.9,en;q=0.8", def.Code, true},
		{"fr-FR,de;q=0.8", "", false},
		{"en;q=0,zh;q=0.5", def.Code, true},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := negotiateAcceptLanguage(c.header)
		if ok != c.ok || got.Code != c.want {
			t.Errorf("negotiate(%q) = %q,%v want %q,%v", c.header, got.Code, ok, c.want, c.ok)
		}
	}
	if en.Code != "en-US" {
		t.Fatal("fixture")
	}
}

// 浏览器偏好英文、且没有语言 cookie 时，默认首页给出切换提示（不重定向，SEO 安全）。
func TestSuggestBannerRender(t *testing.T) {
	en, _ := LanguageByCode("en-US")
	v := testView()
	v.Suggest, v.SuggestLang = true, en
	var b strings.Builder
	if err := pageTmpl["home"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`class="lang-banner"`, `data-lang-banner-close`, `href="/en"`,
		"本页也提供 English 版", "查看 English 版", `aria-label="关闭"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("suggestion banner missing %q", want)
		}
	}
	v.Suggest = false
	b.Reset()
	if err := pageTmpl["home"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "lang-banner") {
		t.Error("banner should disappear when Suggest is false")
	}
}

func TestLanguageLookupAndPaths(t *testing.T) {
	def := DefaultLanguage()
	if def.Prefix != "" || !def.Default {
		t.Fatalf("默认语言必须无前缀: %+v", def)
	}
	for _, alias := range []string{"en-US", "en", "EN-us", "en-GB"} {
		l, ok := LanguageByCode(alias)
		if !ok || l.Code != "en-US" {
			t.Errorf("LanguageByCode(%q) = %+v, %v", alias, l, ok)
		}
	}
	if _, ok := LanguageByCode("fr-FR"); ok {
		t.Error("未配置语言不应命中")
	}

	cases := []struct{ code, in, want string }{
		{"zh-CN", "/", "/"},
		{"zh-CN", "/plugins", "/plugins"},
		{"en-US", "/", "/en"},
		{"en-US", "/plugins", "/en/plugins"},
		{"en-US", "/plugins?q=x&edition=pro", "/en/plugins?q=x&edition=pro"},
	}
	for _, c := range cases {
		l, _ := LanguageByCode(c.code)
		if got := l.Path(c.in); got != c.want {
			t.Errorf("%s.Path(%q) = %q, want %q", c.code, c.in, got, c.want)
		}
	}
	for in, want := range map[string]string{
		"/en": "/", "/en/": "/", "/en/plugins": "/plugins", "/en/news/x": "/news/x",
		"/enterprise": "/enterprise", "/plugins": "/plugins",
	} {
		if got := stripLangPrefix(in); got != want {
			t.Errorf("stripLangPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestViewURLAndSwitch(t *testing.T) {
	en, _ := LanguageByCode("en-US")
	def := DefaultLanguage()
	v := &siteView{Lang: en, Route: "/plugins", Query: "q=cron", Languages: Languages()}

	for in, want := range map[string]string{
		"#features":                  "/en#features",
		"/plugins":                   "/en/plugins",
		"/news":                      "/en/news",
		"/login":                     "/login",
		"/website-assets/site.css":   "/website-assets/site.css",
		"/plugins/cron/download":     "/plugins/cron/download",
		"/plugins/cron/download.sig": "/plugins/cron/download.sig",
		"/plugins/index.json":        "/plugins/index.json",
		"https://example.com/docs":   "https://example.com/docs",
	} {
		if got := v.URL(in); got != want {
			t.Errorf("URL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := v.Alternate(def); got != "/plugins" {
		t.Errorf("Alternate(default) = %q", got)
	}
	if got := v.Alternate(en); got != "/en/plugins" {
		t.Errorf("Alternate(en) = %q", got)
	}
	if got := v.Switch(def); got != "/plugins?lang=zh-CN&q=cron" {
		t.Errorf("Switch(default) = %q", got)
	}
	if got := v.Switch(en); got != "/en/plugins?q=cron" {
		t.Errorf("Switch(en) = %q", got)
	}
}

func TestNormalizeI18nJSON(t *testing.T) {
	raw := `{
		"en-US": {"title":"  English title  ","content":"<p>hi</p><script>alert(1)</script>","unknown":"x"},
		"fr-FR": {"title":"bonjour"},
		"zh-CN": {"title":"不应覆盖默认语言"}
	}`
	out, err := normalizeI18nJSON(raw, map[string]i18nField{
		"title":   {Max: 20},
		"content": {Sanitize: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"en-US"`, `"title":"English title"`, `"content"`} {
		if !strings.Contains(out, want) {
			t.Errorf("normalized missing %s: %s", want, out)
		}
	}
	for _, bad := range []string{"fr-FR", "unknown", "zh-CN", "<script"} {
		if strings.Contains(out, bad) {
			t.Errorf("normalized kept %q: %s", bad, out)
		}
	}
	if _, err := normalizeI18nJSON("{not json", postI18nFields); err == nil {
		t.Error("invalid JSON should fail")
	}
	if out, err := normalizeI18nJSON("", postI18nFields); err != nil || out != "" {
		t.Errorf("empty should stay empty: %q %v", out, err)
	}
}

func TestLocalizeEntryAndPrice(t *testing.T) {
	e := market.Entry{
		ID: "cron", Name: "定时任务", Description: "中文简介", Price: "待定",
		I18n: map[string]market.EntryText{
			"en-US": {Name: "Scheduled Jobs", Description: "English copy"},
		},
	}
	en := localizeEntry(e, "en-US")
	if en.Name != "Scheduled Jobs" || en.Description != "English copy" {
		t.Errorf("localized entry = %+v", en)
	}
	if zh := localizeEntry(e, "zh-CN"); zh.Name != "定时任务" {
		t.Errorf("zh should fall back to source: %+v", zh)
	}
	if got := marketPriceLabel("en-US", "待定"); got != "TBD" {
		t.Errorf("price = %q", got)
	}
	if got := marketPriceLabel("en-US", "¥299"); got != "¥299" {
		t.Errorf("free-form price changed: %q", got)
	}
}

// 英文页面：静态文案、内容、导航、canonical、hreflang 与语言切换都要切到英文。
func TestRenderEnglishHome(t *testing.T) {
	en, _ := LanguageByCode("en-US")
	v := testView()
	v.Lang = en
	v.Route = "/"
	v.Path = "/en"
	v.Navs = []Nav{{Label: "Capabilities", Href: "#features", Enabled: true}}
	v.Features = []Feature{{Icon: "building", Title: "Multi-tenancy", Description: "Tenant isolation at the data layer.", Enabled: true}}
	v.SiteDesc = "A source-delivered admin framework."
	v.HomeTitle = "The full-stack framework\nbuilt for production admin"
	v.HomeSubtitle = "Everything included."
	v.HomeCTALabel = "Get started"
	v.FooterNote = "MIT"
	v.Posts = nil

	var b strings.Builder
	if err := pageTmpl["home"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`<html lang="en">`,
		`hreflang="en" href="http://localhost:8080/en"`,
		`hreflang="zh-CN" href="http://localhost:8080/"`,
		`hreflang="x-default" href="http://localhost:8080/"`,
		`property="og:url" content="http://localhost:8080/en"`,
		`rel="canonical" href="http://localhost:8080/en"`,
		`property="og:locale" content="en_US"`,
		"Ready on day one, not next quarter",
		"Capabilities",
		"Skip to main content",
		"Plugin marketplace",
		`class="lang-switch"`,
		`href="/?lang=zh-CN"`,
		"Multi-tenancy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("English home missing %q", want)
		}
	}
	for _, zh := range []string{"装好就有，不用等排期", "跳到主要内容", "主导航"} {
		if strings.Contains(out, zh) {
			t.Errorf("English home still contains Chinese static copy %q", zh)
		}
	}
}

func TestFormatDateByLanguage(t *testing.T) {
	en, _ := LanguageByCode("en-US")
	day := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if got := formatDate(en, day); got != "Oct 26, 2026" {
		t.Errorf("en date = %q", got)
	}
	if got := formatDate(DefaultLanguage(), day); got != "2026-10-26" {
		t.Errorf("zh date = %q", got)
	}
	if got := formatDate(en, "not a time"); got != "" {
		t.Errorf("invalid date = %q", got)
	}
}
