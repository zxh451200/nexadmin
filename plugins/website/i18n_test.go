package website

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/plugin/market"
	pi18n "nexadmin/pkg/i18n"
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
	// 下限只是「扫描没坏」的探针，不是文案数量红线：正则失效时这里会掉到几十。
	if len(used) < 150 {
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
		for _, key := range []string{"packaging", "packagingHint", "packFailed", "copied", "copyManual", "menuOpen", "menuClose", "chatErrSend", "chatErrLoad", "chatClosed", "chatGuest", "chatAgent"} {
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
		// 动态拼接的文档分组名
		"docs.group.dev", "docs.group.ref",
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
		"/admin/login":               "/admin/login",
		"/plugins":                   "/en/plugins",
		"/news":                      "/en/news",
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
	// 默认语言的根路径是语言中立入口，站内链接必须自带 ?lang=，否则会按 cookie 回跳到别的语言。
	zh := &siteView{Lang: def, Route: "/news", Languages: Languages()}
	for in, want := range map[string]string{
		"/":        "/?lang=zh-CN",
		"/news":    "/news",
		"#faq":     "/?lang=zh-CN#faq",
		"/en":      "/en",
		"/en/docs": "/en/docs",
	} {
		if got := zh.URL(in); got != want {
			t.Errorf("zh URL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := zh.Home(); got != "/?lang=zh-CN" {
		t.Errorf("zh Home() = %q", got)
	}
	// 锚点在首页就是同页跳转，不该再经过一次语言入口。
	if got := (&siteView{Lang: def, Route: "/"}).URL("#faq"); got != "#faq" {
		t.Errorf("home anchor URL = %q", got)
	}
	if got := v.URL("/"); got != "/en" {
		t.Errorf("en URL(/) = %q", got)
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

// 访客在读中文页、浏览器却存着旧的英文 cookie（例如之前点过英文建议条）：
// 点「首页」必须留在中文站，而不是被 cookie 回跳到 /en。
func TestStaleEnglishCookieKeepsChineseHomeLinks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	pub := r.Group("")
	pub.Use(LanguageContext(DefaultLanguage()), langNegotiate)
	h := &siteHandler{}
	pub.GET("/", func(c *gin.Context) { h.render(c, "home", http.StatusOK, testView()) })
	pub.GET("/docs", func(c *gin.Context) {
		h.render(c, "docs-index", http.StatusOK, testDocsView(t, DefaultLanguage(), ""))
	})
	en, _ := LanguageByCode("en-US")
	eng := pub.Group("/en")
	eng.Use(LanguageMiddleware(en))
	eng.GET("/docs", func(c *gin.Context) {
		h.render(c, "docs-index", http.StatusOK, testDocsView(t, en, ""))
	})
	get := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	stale := &http.Cookie{Name: langCookie, Value: "en-US"}

	// 中文深链照旧渲染中文，且页内每个首页链接都声明了中文。
	w := get("/docs", stale)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /docs: %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, `href="/"`) {
		t.Error("中文页仍存在语言中立的首页链接，带旧英文 cookie 的访客会在点它时被送去 /en")
	}
	if n := strings.Count(body, `href="/?lang=zh-CN"`); n < 2 {
		t.Errorf("首页链接（导航/品牌/页脚）应显式带 ?lang=zh-CN，只找到 %d 个", n)
	}

	// 点「首页」：语言在 URL 里就直接就地渲染，不再 302 到 "/" 去读 cookie。
	// 原来的 302 依赖 Set-Cookie 写成功；cookie 被拒写/被并发覆盖时，下一条 "/"
	// 会按旧 cookie 把人送去 /en——点中文首页却看到英文就是这么来的。
	w = get("/?lang=zh-CN", stale)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /?lang=zh-CN: %d（不该再回跳）", w.Code)
	}
	if got := w.Header().Get("Content-Language"); got != "zh-CN" {
		t.Errorf("Content-Language = %q, want zh-CN", got)
	}
	if !strings.Contains(w.Body.String(), `lang="zh-CN"`) {
		t.Error("GET /?lang=zh-CN 应就地渲染中文首页")
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), langCookie+"=zh-CN") {
		t.Errorf("切换后应把语言 cookie 改成中文: %q", w.Header().Get("Set-Cookie"))
	}

	// 非默认语言：语言只落在 URL 前缀上，同样不经过 cookie。
	w = get("/?lang=en-US", nil)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/en" {
		t.Errorf("GET /?lang=en-US: %d → %q", w.Code, w.Header().Get("Location"))
	}
	// 带前缀的英文页被点名切回默认语言：回到无前缀路径（不在 /en 下渲染中文）。
	w = get("/en/docs?lang=zh-CN", nil)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/docs" {
		t.Errorf("GET /en/docs?lang=zh-CN: %d → %q", w.Code, w.Header().Get("Location"))
	}

	// 不带 ?lang= 的根路径仍按「记住的语言」回跳：所以首页链接必须显式带语言。
	w = get("/", stale)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/en" {
		t.Errorf("GET / 带旧英文 cookie: %d → %q", w.Code, w.Header().Get("Location"))
	}

	// 回跳后的 "/" 以中文渲染，不再把访客送去英文站。
	w = get("/", &http.Cookie{Name: langCookie, Value: "zh-CN"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `lang="zh-CN"`) {
		t.Errorf("GET / 应渲染中文首页: %d", w.Code)
	}
}

// pkg/i18n 的算法自己有用例（pkg/i18n/i18n_test.go）。这里只测官网这一侧的接缝：
// siteLocales 适配得不对，译文会被整批丢掉或存成错误的语言键，而 pkg 的测试照样全绿。
func TestSiteLocalesAdapter(t *testing.T) {
	l, ok := (siteLocales{}).Resolve("en-US")
	if !ok || l.Code != "en-US" || l.Default {
		t.Errorf("Resolve(en-US) = %+v %v", l, ok)
	}
	// 语基别名必须可解析：前端传 "en" 时译文不该被当成未知语言丢掉。
	if l, ok := (siteLocales{}).Resolve("en"); !ok || l.Code != "en-US" {
		t.Errorf("Resolve(en) 未命中 en-US: %+v %v", l, ok)
	}
	if l, ok := (siteLocales{}).Resolve("zh-CN"); !ok || !l.Default {
		t.Errorf("Resolve(zh-CN) 应命中且为默认语言: %+v %v", l, ok)
	}
	if _, ok := (siteLocales{}).Resolve("kk-KK"); ok {
		t.Error("未配置的语言不该命中")
	}
	all := (siteLocales{}).All()
	if len(all) != len(Languages()) {
		t.Errorf("All() 条数 = %d, want %d", len(all), len(Languages()))
	}
	def := 0
	for _, x := range all {
		if x.Default {
			def++
		}
	}
	if def != 1 {
		t.Errorf("All() 里默认语言有 %d 个, want 1", def)
	}
}

// 端到端接缝：通过真实适配器走一遍 pkg 的归一化，语基键要被规范成清单里的码。
func TestNormalizeThroughSiteLocales(t *testing.T) {
	out, err := pi18n.Normalize(`{"en":{"title":"T","summary":"  "},"zh-CN":{"title":"不应存"},"xx-YY":{"title":"x"}}`,
		siteLocales{}, pi18n.Fields{"title": {Max: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"en-US"`) {
		t.Errorf("语基键未规范成清单码: %s", out)
	}
	for _, bad := range []string{"zh-CN", "xx-YY", "不应存", "summary"} {
		if strings.Contains(out, bad) {
			t.Errorf("结果里不该有 %q: %s", bad, out)
		}
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
		"The shared core,",
		"built in.",
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
	for _, zh := range []string{"公共部分", "出厂即具备。", "跳到主要内容", "主导航"} {
		if strings.Contains(out, zh) {
			t.Errorf("English home still contains Chinese static copy %q", zh)
		}
	}
}

func TestPluralTranslations(t *testing.T) {
	en, _ := LanguageByCode("en-US")
	v := &siteView{Lang: en}
	cases := []struct {
		n    any
		key  string
		args []any
		want string
	}{
		{1, "market.count", []any{1}, "1 plugin"},
		{3, "market.count", []any{3}, "3 plugins"},
		{1, "market.matched", []any{1}, "1 match"},
		{2, "market.matched", []any{2}, "2 matches"},
		{1, "market.downloads", []any{"1"}, "1 download"},
		{5, "market.downloads", []any{"5"}, "5 downloads"},
		{1, "plugin.downloads", []any{"1"}, "1 download"},
		{1, "plugin.ratingSummary", []any{"5.0", 1}, "5.0 out of 5 from 1 rating."},
		{2, "plugin.ratingSummary", []any{"4.5", 2}, "4.5 out of 5 from 2 ratings."},
		{1, "plugin.ratingMine", []any{1}, "You rated 1 star. You can rate again."},
		{4, "plugin.ratingMine", []any{4}, "You rated 4 stars. You can rate again."},
		{1, "plugin.starAria", []any{1}, "1 star"},
		{5, "plugin.starAria", []any{5}, "5 stars"},
	}
	for _, c := range cases {
		if got := v.TN(c.n, c.key, c.args...); got != c.want {
			t.Errorf("TN(%v,%q) = %q, want %q", c.n, c.key, got, c.want)
		}
	}
	zh := &siteView{Lang: DefaultLanguage()}
	if got := zh.TN(1, "market.count", 1); got != "1 个插件" {
		t.Errorf("zh TN = %q", got)
	}
	// 每种语言都要有单复数两套键。
	for _, l := range Languages() {
		for _, key := range []string{
			"market.count", "market.matched", "market.downloads", "plugin.downloads",
			"plugin.ratingSummary", "plugin.ratingMine", "plugin.starAria",
		} {
			for _, suffix := range []string{".one", ".other"} {
				if strings.TrimSpace(dicts[l.Code][key+suffix]) == "" {
					t.Errorf("语言 %s 缺少 %s%s", l.Code, key, suffix)
				}
			}
		}
		if strings.TrimSpace(dicts[l.Code]["plugin.downloadsNone"]) == "" {
			t.Errorf("语言 %s 缺少 plugin.downloadsNone", l.Code)
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
