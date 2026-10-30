package website

import (
	"strings"
	"testing"
)

// 社交平台清单与图标表必须一一对应：下拉里选得到、页脚就画得出来。
func TestSocialPlatformsMatchIcons(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range socialPlatforms {
		if seen[p] {
			t.Errorf("duplicate platform %q", p)
		}
		seen[p] = true
		if _, ok := socialIconPaths[p]; !ok {
			t.Errorf("socialPlatforms has %q but socialIconPaths does not", p)
		}
	}
	for p := range socialIconPaths {
		if !seen[p] {
			t.Errorf("socialIconPaths has %q but socialPlatforms does not", p)
		}
	}
}

func TestNormalizeSocials(t *testing.T) {
	got, err := normalizeSocials([]SocialLink{
		{Platform: "github", URL: " https://github.com/nexadmin "},
		{Platform: "", URL: "   "}, // 空行忽略
		{Platform: "mail", URL: "mailto:team@example.com"},
	})
	if err != nil {
		t.Fatalf("normalizeSocials: %v", err)
	}
	want := `[{"platform":"github","url":"https://github.com/nexadmin"},{"platform":"mail","url":"mailto:team@example.com"}]`
	if got != want {
		t.Fatalf("normalizeSocials = %s, want %s", got, want)
	}

	for _, bad := range [][]SocialLink{
		{{Platform: "myspace", URL: "https://example.com"}},
		{{Platform: "github", URL: "javascript:alert(1)"}},
		{{Platform: "github", URL: "github.com/nexadmin"}},
	} {
		if _, err := normalizeSocials(bad); err == nil {
			t.Errorf("normalizeSocials(%v) = nil error", bad)
		}
	}
}

func TestNormalizeFriendLinks(t *testing.T) {
	got, err := normalizeFriendLinks([]FriendLink{
		{Name: " Vite ", URL: "https://vite.dev"},
		{Name: "", URL: ""},
		{Name: "文档", URL: "/docs"},
	})
	if err != nil {
		t.Fatalf("normalizeFriendLinks: %v", err)
	}
	want := `[{"name":"Vite","url":"https://vite.dev"},{"name":"文档","url":"/docs"}]`
	if got != want {
		t.Fatalf("normalizeFriendLinks = %s, want %s", got, want)
	}
	for _, bad := range [][]FriendLink{
		{{Name: "", URL: "https://example.com"}},
		{{Name: "Vite", URL: "vite.dev"}},
	} {
		if _, err := normalizeFriendLinks(bad); err == nil {
			t.Errorf("normalizeFriendLinks(%v) = nil error", bad)
		}
	}
}

// 库里的 JSON 被手改坏时页脚不能整页挂掉：当作空列表。
func TestParseFooterLinksTolerant(t *testing.T) {
	if got := parseSocials("{oops"); got != nil {
		t.Errorf("parseSocials(bad) = %v, want nil", got)
	}
	if got := parseFriendLinks(""); got != nil {
		t.Errorf("parseFriendLinks(empty) = %v, want nil", got)
	}
}

func renderPage(t *testing.T, name string, v *siteView) string {
	t.Helper()
	tpl, ok := pageTmpl[name]
	if !ok {
		t.Fatalf("template %q missing", name)
	}
	var b strings.Builder
	if err := tpl.ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	return b.String()
}

// 页脚四个块（电话 / 社交 / 二维码 / 友情链接）配了就渲染，没配就不出现。
func TestFooterContactBlocks(t *testing.T) {
	v := &siteView{
		Lang: DefaultLanguage(), Languages: Languages(),
		SiteName: "NexAdmin", BaseURL: "http://localhost", Path: "/", Route: "/",
		ContactEmail: "team@example.com", ContactPhone: "010-88886666",
		QRCodeURL: "/website-assets/media/1", QRCodeCaption: "微信公众号",
		Socials:     []SocialLink{{Platform: "github", URL: "https://github.com/nexadmin"}},
		FriendLinks: []FriendLink{{Name: "Vite", URL: "https://vite.dev"}},
	}
	html := renderPage(t, "home", v)
	for _, want := range []string{
		`href="tel:010-88886666"`,
		`class="footer-social"`,
		`aria-label="GitHub"`,
		`class="footer-qr"`,
		`微信公众号`,
		`href="https://vite.dev"`,
		`href="/contact"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("footer missing %q", want)
		}
	}

	empty := &siteView{Lang: DefaultLanguage(), Languages: Languages(), SiteName: "NexAdmin", Route: "/"}
	plain := renderPage(t, "home", empty)
	for _, notWant := range []string{`class="footer-social"`, `class="footer-qr"`} {
		if strings.Contains(plain, notWant) {
			t.Errorf("empty footer should not contain %q", notWant)
		}
	}
	// 联系页是站点自带页面：没配联系方式也保留入口。
	if !strings.Contains(plain, `href="/contact"`) {
		t.Error("footer should always link to /contact")
	}
}

// 联系页：表单指向当前语言的 /contact，无 JS 回跳时可显示成功/失败提示。
func TestContactPageRenders(t *testing.T) {
	v := &siteView{
		Lang: DefaultLanguage(), Languages: Languages(),
		SiteName: "NexAdmin", Route: "/contact", BaseURL: "http://localhost",
		ContactEmail: "team@example.com", ContactPhone: "010-88886666",
	}
	html := renderPage(t, "contact", v)
	for _, want := range []string{
		`data-contact-form`, `action="/contact"`, `name="email"`, `name="content"`,
		`name="company"`, `href="mailto:team@example.com"`, `href="tel:010-88886666"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("contact page missing %q", want)
		}
	}

	v.ContactSent = true
	if html := renderPage(t, "contact", v); !strings.Contains(html, "contact-status-ok") {
		t.Error("sent banner missing")
	}
	v.ContactSent = false
	v.ContactError = contactErrorText(v, "email")
	if html := renderPage(t, "contact", v); !strings.Contains(html, "contact-status-err") || !strings.Contains(html, v.ContactError) {
		t.Error("error banner missing")
	}
}

// /contact 必须被导航校验当作站内页面放行，否则出厂导航会被公开站丢掉。
func TestContactHrefValid(t *testing.T) {
	for _, href := range []string{"/contact", "/en/contact", "/contact?sent=1"} {
		if !navHrefValid(href) {
			t.Errorf("navHrefValid(%q) = false", href)
		}
	}
}
