package website

import (
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"nexadmin/pkg/idgen"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

// 出厂内容版本必须与默认设置一致；新增出厂条目时改 seedVersion 并补迁移。
func TestSeedVersionCurrent(t *testing.T) {
	if got := defaultSetting().SeedVersion; got != seedVersion {
		t.Fatalf("defaultSetting.SeedVersion = %d, seedVersion = %d", got, seedVersion)
	}
	seen := map[string]bool{}
	for _, p := range defaultPosts() {
		if seen[p.Slug] {
			t.Errorf("duplicate factory post slug %q", p.Slug)
		}
		seen[p.Slug] = true
		if p.Published && p.I18nJSON == "" {
			t.Errorf("factory post %q missing English translation", p.Slug)
		}
	}
	if !seen["upgrade-without-fork"] {
		t.Error("third factory post missing")
	}
}

func TestValidateSlug(t *testing.T) {
	for _, s := range []string{"about", "hello-nexadmin", "v0-1-release", "a"} {
		if err := ValidateSlug(s); err != nil {
			t.Errorf("ValidateSlug(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{
		"", "About", "hello_nexadmin", "-lead", "tail-", ".hidden", "a b",
		strings.Repeat("a", 65),
		// 插件自己的路由段必须保留
		"news", "p",
	} {
		if err := ValidateSlug(s); err == nil {
			t.Errorf("ValidateSlug(%q) = nil, want error", s)
		}
	}
	// 内容都挂在 /p/ 与 /news/ 下，后台路径不冲突：about、login-guide 都放行。
	for _, s := range []string{"about", "login-guide", "dashboard-tips"} {
		if err := ValidateSlug(s); err != nil {
			t.Errorf("ValidateSlug(%q) = %v, want nil", s, err)
		}
	}
}

func TestIconFunc(t *testing.T) {
	iconFn := tplFuncs["icon"].(func(string) template.HTML)
	for _, name := range []string{"building", "shield", "key", "plug", "file", "chart", "globe", "puzzle"} {
		if got := string(iconFn(name)); !strings.Contains(got, "<svg") {
			t.Errorf("icon(%q) missing svg", name)
		}
	}
	if got := string(iconFn("nope")); !strings.Contains(got, "circle") {
		t.Error("icon(unknown) should fall back to default circle")
	}
}

// 管理端下拉按 featureIconOrder 渲染名称与 iconPaths 里的路径：两份清单必须一一对应，
// 否则选择器会出现没图的空项，或漏掉可在官网使用的图标。
func TestFeatureIconOrderMatchesPaths(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range featureIconOrder {
		if seen[name] {
			t.Errorf("duplicate icon %q in featureIconOrder", name)
		}
		seen[name] = true
		if _, ok := iconPaths[name]; !ok {
			t.Errorf("featureIconOrder has %q but iconPaths does not", name)
		}
	}
	for name := range iconPaths {
		if !seen[name] {
			t.Errorf("iconPaths has %q but featureIconOrder does not", name)
		}
	}
}

func testView() *siteView {
	now := time.Now()
	post := Post{
		Slug: "hello", Title: "你好 NexAdmin",
		Summary: "第一篇", Content: "<p>正文</p>", Published: true, PublishedAt: &now,
	}
	post.ID = idgen.ID(1)
	return &siteView{
		Lang: DefaultLanguage(), Languages: Languages(),
		SiteName: "NexAdmin", SiteDesc: "以源码交付的后台管理框架",
		HomeTitle:    "为生产级后台系统\n而生的全栈框架",
		HomeSubtitle: "多租户、RBAC、开放平台与插件体系，出厂即备。",
		HomeCTALabel: "开始使用", HomeCTAHref: "#quickstart",
		FooterNote: "MIT", Year: 2026,
		MarketEnabled: true,
		SeoKeywords:   "NexAdmin,后台管理框架",
		BaseURL:       "http://localhost:8080", Path: "/",
		Navs: []Nav{{Label: "能力", Href: "#features", Sort: 10, Enabled: true}},
		Features: []Feature{
			{Icon: "building", Title: "多租户", Description: "共享库 + tenant_id 隔离。", Sort: 10, Enabled: true},
			{Icon: "nope", Title: "未知图标回退", Description: "默认圆点。", Sort: 20, Enabled: true},
		},
		Posts: []Post{post},
	}
}

func TestRenderPages(t *testing.T) {
	cases := []struct {
		page string
		v    func() *siteView
		want []string
	}{
		{"home", testView, []string{"为生产级后台系统", "而生的全栈框架", "多租户", "sh scripts/build.sh", "market/dashboard-dark.jpg"}},
		{"news", testView, []string{"你好 NexAdmin", "hello"}},
		{"post", func() *siteView {
			v := testView()
			v.Post = &v.Posts[0]
			return v
		}, []string{"你好 NexAdmin", "<p>正文</p>"}},
		{"page", func() *siteView {
			v := testView()
			v.Page = &Page{Slug: "about", Title: "关于", Content: "<p>关于内容</p>", Published: true}
			return v
		}, []string{"关于", "<p>关于内容</p>"}},
		{"notfound", testView, []string{"页面不存在"}},
	}
	for _, tc := range cases {
		t.Run(tc.page, func(t *testing.T) {
			tpl, ok := pageTmpl[tc.page]
			if !ok {
				t.Fatalf("template %q missing", tc.page)
			}
			var b strings.Builder
			if err := tpl.ExecuteTemplate(&b, "layout", tc.v()); err != nil {
				t.Fatalf("render %q: %v", tc.page, err)
			}
			out := b.String()
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("render %q missing %q", tc.page, w)
				}
			}
			if !strings.Contains(out, "/website-assets/site.css") {
				t.Errorf("render %q missing stylesheet link", tc.page)
			}
		})
	}
}

func TestHomeProductTour(t *testing.T) {
	for _, lang := range Languages() {
		t.Run(lang.Code, func(t *testing.T) {
			v := testView()
			v.Lang = lang
			v.HomeCTAHref = "/docs/dev-guide"
			var b strings.Builder
			if err := pageTmpl["home"].ExecuteTemplate(&b, "layout", v); err != nil {
				t.Fatal(err)
			}
			out := b.String()
			for _, want := range []string{
				`href="` + v.URL(v.HomeCTAHref) + `"`,
				`class="hero-link" href="#quickstart"`,
				`class="product-facts"`,
				`market/dashboard-dark.jpg`,
				`market/dashboard-light.jpg`,
			} {
				if !strings.Contains(out, want) {
					t.Errorf("home missing %q", want)
				}
			}
			// 首页只留首屏那张真实界面：其余截图已改为文字，避免整页堆图。
			for _, name := range []string{"user", "menu", "siteset", "role", "pluginmarket"} {
				for _, scheme := range []string{"dark", "light"} {
					if strings.Contains(out, "market/"+name+"-"+scheme+".jpg") {
						t.Errorf("%s-%s screenshot should not be on the home page", name, scheme)
					}
				}
			}
			if got := strings.Count(out, "<img "); got != 2 {
				t.Errorf("home images = %d, want 2 (hero dark/light)", got)
			}
			if strings.Contains(out, "home.product.tab.") || strings.Contains(out, "home.editorial.platform.summary") {
				t.Error("home has untranslated labels")
			}
		})
	}
}

// 渲染必须做上下文转义：标题里的 HTML 不能原样进页面。
func TestRenderEscapes(t *testing.T) {
	v := testView()
	v.Page = &Page{Slug: "x", Title: "<script>alert(1)</script>", Content: "<p>ok</p>", Published: true}
	var b strings.Builder
	if err := pageTmpl["page"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "<script>alert(1)") {
		t.Error("title HTML not escaped")
	}
}

// 骨架必须带齐 SEO/分享元信息，并保留移动端入口与无 JS 兜底。
func TestLayoutMetaAndFallbacks(t *testing.T) {
	v := testView()
	var b strings.Builder
	if err := pageTmpl["home"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`rel="canonical" href="http://localhost:8080/"`,
		`property="og:image" content="http://localhost:8080/website-assets/og.png"`,
		`name="twitter:card" content="summary_large_image"`,
		`rel="icon" href="/website-assets/favicon.svg"`,
		`name="keywords" content="NexAdmin,后台管理框架"`,
		`class="nav-toggle"`,
		`aria-controls="mobile-nav"`,
		`id="mobile-nav"`,
		`<noscript>`,
		`class="pillar"`,
		`id="capabilities"`,
		`class="intg-grid"`,
		`class="product-facts"`,
		`<footer`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("layout missing %q", want)
		}
	}

	// 页脚是改版最频繁的区域：断言结构与内容（站名），不绑定某一版的 class 名，
	// 否则每次视觉调整都会把用例打红，反而掩盖真正的结构缺失。
	start := strings.Index(out, "<footer")
	if start < 0 {
		t.Fatal("layout missing footer block")
	}
	rest := out[start:]
	end := strings.Index(rest, "</footer>")
	if end < 0 {
		t.Fatal("footer block not closed")
	}
	footer := rest[:end]
	if !strings.Contains(footer, v.SiteName) {
		t.Errorf("footer missing site name %q", v.SiteName)
	}
	if !strings.Contains(footer, "<nav") {
		t.Error("footer missing link columns")
	}
}

// 新闻页导航项要标出当前页（Path 与 Href 相等）。
func TestNavCurrent(t *testing.T) {
	v := testView()
	v.Path = "/news"
	v.Route = "/news"
	v.Navs = []Nav{{Label: "动态", Href: "/news", Enabled: true}}
	var b strings.Builder
	if err := pageTmpl["news"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `href="/news" aria-current="page"`) {
		t.Error("current nav item not marked")
	}
}

func TestHomePageTitle(t *testing.T) {
	cases := []struct{ name, home, desc, want string }{
		{"two lines", "为生产级后台系统\n而生的全栈框架", "desc", "NexAdmin — 为生产级后台系统 而生的全栈框架"},
		{"one line", "后台管理框架", "desc", "NexAdmin — 后台管理框架"},
		{"empty title falls back", "", "站点描述", "NexAdmin — 站点描述"},
		{"empty all", "", "", "NexAdmin"},
	}
	for _, c := range cases {
		if got := homePageTitle("NexAdmin", c.home, c.desc); got != c.want {
			t.Errorf("%s: homePageTitle = %q, want %q", c.name, got, c.want)
		}
	}
}

// 市场关闭时，公共骨架里不能再出现任何 /plugins 入口。
func TestMarketToggleTemplate(t *testing.T) {
	v := testView()
	v.MarketEnabled = true
	var b strings.Builder
	if err := pageTmpl["home"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `href="/plugins"`) {
		t.Error("market link missing when enabled")
	}

	v.MarketEnabled = false
	b.Reset()
	if err := pageTmpl["home"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), `href="/plugins"`) {
		t.Error("market link shown when disabled")
	}

	b.Reset()
	if err := pageTmpl["notfound"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), `href="/plugins"`) {
		t.Error("notfound market link shown when disabled")
	}
}

func TestXMLEscape(t *testing.T) {
	got := xmlEscape("http://a&b.example/x<y>")
	want := "http://a&amp;b.example/x&lt;y&gt;"
	if got != want {
		t.Errorf("xmlEscape = %q, want %q", got, want)
	}
}

// 保存媒体按内容嗅探判定类型：扩展名骗不过；非法内容要拒。
func TestMediaValidation(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13}
	if mime := http.DetectContentType(png); !mediaMimes[mime] {
		t.Errorf("png detected as %q", mime)
	}
	if mediaMimes[http.DetectContentType([]byte("<svg onload=alert(1)>"))] {
		t.Error("svg/html content should not be accepted as image")
	}
	if mediaMimes[http.DetectContentType([]byte("plain text, not an image"))] {
		t.Error("text content should not be accepted as image")
	}
}

// 公共导航只能指向官网路由、站内锚点或完整外部地址；裸路径与后台路由要拒，
// 否则浏览器把 "xx" 拼成 /xx，服务端 SPA 回退会把后台外壳返回给访客。
func TestNavHrefValid(t *testing.T) {
	cases := []struct {
		href string
		want bool
	}{
		{"/", true},
		{"/en", true},
		{"/news", true},
		{"/news/hello", true},
		{"/plugins", true},
		{"/plugins/cron", true},
		{"/docs", true},
		{"/docs/dev-guide", true},
		{"/p/about", true},
		{"/en/docs/dev-guide", true},
		{"/news?page=2", true},
		{"/docs/dev-guide#toc", true},
		{"#features", true},
		{"https://linear.app", true},
		{"mailto:hi@example.com", true},
		{"xx", false},
		{"news", false},
		{"/users", false},
		{"/login", false},
		{"/p/", false},
		{"/plugins-admin", false},
		{"javascript:alert(1)", false},
		{"", false},
	}
	for _, c := range cases {
		if got := navHrefValid(c.href); got != c.want {
			t.Errorf("navHrefValid(%q) = %v, want %v", c.href, got, c.want)
		}
	}
}

// 导航里的 /p/{slug} 要能取出 slug；语言前缀与查询串不影响。
func TestNavPageSlug(t *testing.T) {
	cases := []struct {
		href string
		slug string
		ok   bool
	}{
		{"/p/about", "about", true},
		{"/en/p/about", "about", true},
		{"/p/about?from=nav", "about", true},
		{"/p/about#top", "about", true},
		{"/news", "", false},
		{"/p/", "", false},
		{"https://x.example/p/about", "", false},
	}
	for _, c := range cases {
		slug, ok := navPageSlug(c.href)
		if slug != c.slug || ok != c.ok {
			t.Errorf("navPageSlug(%q) = (%q,%v), want (%q,%v)", c.href, slug, ok, c.slug, c.ok)
		}
	}
}
