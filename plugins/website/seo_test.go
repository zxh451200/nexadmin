package website

import (
	"html/template"
	"strings"
	"testing"
)

// 内容级 SEO 元字段的渲染规则：
// seoTitle 是完整 <title> 的覆盖（og/twitter 跟着走）；noindex 要真的落出 robots meta，
// ogImage 覆盖默认分享图。渲染错了不报错，只有爬虫吃亏，必须钉住。
func TestLayoutSeoTitleAndNoindex(t *testing.T) {
	render := func(seal func(*siteView)) string {
		v := testView()
		v.Page = &Page{Slug: "about", Title: "关于", Published: true}
		seal(v)
		var b strings.Builder
		if err := pageTmpl["page"].ExecuteTemplate(&b, "layout", v); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}

	t.Run("默认回退标题", func(t *testing.T) {
		out := render(func(v *siteView) { v.Title = "关于 - NexAdmin" })
		if !strings.Contains(out, "<title>关于 - NexAdmin</title>") {
			t.Errorf("title fallback missing, got %s", head(out))
		}
	})

	t.Run("SeoTitle 覆盖三处标题", func(t *testing.T) {
		v := testView()
		v.Page = &Page{Slug: "about", Title: "关于", SeoTitle: "定制标题", Published: true}
		v.Title, v.SeoTitle = "关于 - NexAdmin", "定制标题 - NexAdmin"
		var b strings.Builder
		if err := pageTmpl["page"].ExecuteTemplate(&b, "layout", v); err != nil {
			t.Fatal(err)
		}
		out := b.String()
		for _, want := range []string{
			"<title>定制标题 - NexAdmin</title>",
			`og:title" content="定制标题 - NexAdmin"`,
			`twitter:title" content="定制标题 - NexAdmin"`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q", want)
			}
		}
	})

	t.Run("Noindex 落出 robots meta", func(t *testing.T) {
		out := render(func(v *siteView) { v.Noindex = true })
		if !strings.Contains(out, `<meta name="robots" content="noindex">`) {
			t.Error("noindex meta missing")
		}
		if !strings.Contains(out, "og:image") {
			t.Error("og tags should still render for noindex pages")
		}
	})

	t.Run("OgImage 覆盖默认分享图", func(t *testing.T) {
		out := render(func(v *siteView) { v.OGImage = "http://localhost:8080/website-assets/media/9" })
		if strings.Contains(out, "/website-assets/og.png") {
			t.Error("default og image should be overridden")
		}
		if !strings.Contains(out, `og:image" content="http://localhost:8080/website-assets/media/9"`) {
			t.Error("custom og image missing")
		}
	})

	t.Run("FaviconURL 覆盖默认图标", func(t *testing.T) {
		out := render(func(v *siteView) {})
		if !strings.Contains(out, `href="/website-assets/favicon.svg"`) {
			t.Error("default favicon fallback missing")
		}
		out = render(func(v *siteView) { v.FaviconURL = "/favicon.png" })
		if strings.Contains(out, "favicon.svg") {
			t.Error("default favicon should be overridden")
		}
		if !strings.Contains(out, `<link rel="icon" href="/favicon.png"`) {
			t.Error("custom favicon missing")
		}
	})

	// HeadCode 原样注入 <head>（统计脚本等可信管理员配置）；空则完全不出现
	t.Run("HeadCode 原样注入", func(t *testing.T) {
		out := render(func(v *siteView) {})
		if strings.Contains(out, "googletagmanager") {
			t.Error("empty head code must render nothing")
		}
		out = render(func(v *siteView) {
			v.HeadCode = template.HTML(`<script async src="https://www.googletagmanager.com/gtag/js?id=G-X"></script>`)
		})
		if !strings.Contains(out, `src="https://www.googletagmanager.com/gtag/js?id=G-X"`) {
			t.Error("head code script missing")
		}
	})
}

// noindex 的内容不进 sitemap：过滤在 SitemapEntries 的 SQL（noindex = false），
// 元数据渲染逻辑由上面的模板用例钉住。
func head(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
