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
		{"home", testView, []string{"为生产级后台系统", "而生的全栈框架", "多租户", "sh scripts/build.sh", "localhost:8080/dashboard"}},
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
		`data-tabs`,
		`id="showcase"`,
		`id="integrations"`,
		`footer-mark`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("layout missing %q", want)
		}
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
