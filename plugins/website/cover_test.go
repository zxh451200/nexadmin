package website

import (
	"strings"
	"testing"
)

// 封面要同时出现在「新闻列表缩略图」和「og:image」，且 og:image 必须是绝对地址——
// 社交平台不解析相对路径，贴一个 /uploads/x.jpg 上去等于没有图。
func TestPostCoverSurfaces(t *testing.T) {
	v := testView()
	v.Posts[0].CoverURL = "/uploads/cover.jpg"
	var b strings.Builder
	if err := pageTmpl["news"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	if out := b.String(); !strings.Contains(out, `class="post-row-cover"`) || !strings.Contains(out, `src="/uploads/cover.jpg"`) {
		t.Errorf("新闻列表没有渲染封面缩略图")
	}

	v = testView()
	v.Post = &v.Posts[0]
	v.OGImage = "http://localhost:8080/uploads/cover.jpg"
	b.Reset()
	if err := pageTmpl["post"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, `<meta property="og:image" content="http://localhost:8080/uploads/cover.jpg">`) {
		t.Errorf("og:image 没有用文章封面")
	}
	if strings.Contains(out, "og:image:width") {
		t.Errorf("自定义封面不该再声明写死的 1200x630 尺寸")
	}

	// 没有封面：回退站点默认分享图，并带上它真实的尺寸。
	v = testView()
	v.Post = &v.Posts[0]
	b.Reset()
	if err := pageTmpl["post"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	out = b.String()
	if !strings.Contains(out, "http://localhost:8080/website-assets/og.png") || !strings.Contains(out, "og:image:width") {
		t.Errorf("无封面时应回退站点默认分享图")
	}
}

func TestAbsURL(t *testing.T) {
	cases := []struct{ base, in, want string }{
		{"https://x.test", "/uploads/a.jpg", "https://x.test/uploads/a.jpg"},
		{"https://x.test/", "uploads/a.jpg", "https://x.test/uploads/a.jpg"},
		{"https://x.test", "//cdn.test/a.jpg", "//cdn.test/a.jpg"},
		{"https://x.test", "http://cdn.test/a.jpg", "http://cdn.test/a.jpg"},
		{"https://x.test", "", ""},
	}
	for _, c := range cases {
		if got := absURL(c.base, c.in); got != c.want {
			t.Errorf("absURL(%q, %q) = %q, want %q", c.base, c.in, got, c.want)
		}
	}
}
