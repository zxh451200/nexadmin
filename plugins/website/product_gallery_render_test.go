package website

import (
	"strings"
	"testing"
)

// TestProductTemplateGallery 渲染真实的 product 模板（跳过 layout，只跑 content），
// 钉住详情页图区的三件事：
//  1. 主图包在 .pd-stage 里（放大镜与视频播放器都挂在这个容器上）
//  2. 填了可内嵌的视频才出视频缩略图，且 kind/src 带在 data-* 上给 site.js 用
//  3. 认不出来的视频地址不出缩略图，退回「新窗口打开」链接
//
// 模板与脚本是两份文件、靠 data-* 契约对接，这类改动最容易一边改一边忘，所以用渲染结果兜住。
func TestProductTemplateGallery(t *testing.T) {
	tpl, ok := themePackFor("")["product"]
	if !ok {
		t.Fatal("product 模板缺失")
	}
	render := func(p *Product) string {
		v := &siteView{
			SiteName: "Solaris",
			Lang:     Language{Code: "zh-CN"},
			Product:  p,
		}
		var b strings.Builder
		if err := tpl.ExecuteTemplate(&b, "content", v); err != nil {
			t.Fatalf("渲染失败: %v", err)
		}
		return b.String()
	}

	base := Product{Slug: "demo", Title: "Demo", CoverURL: "/c.webp", Currency: "USD"}

	html := render(&base)
	if !strings.Contains(html, `class="pd-stage" data-pd-stage`) {
		t.Errorf("主图没包进 .pd-stage：放大镜与视频都挂不上去")
	}
	if !strings.Contains(html, `pd-zoom-tip`) {
		t.Errorf("缺少「点击看大图」提示")
	}
	// 悬停放大由 site.js 的 initGalleryZoom 接管，模板里只需要一个舞台容器
	if !strings.Contains(html, `data-pd-stage`) {
		t.Errorf("缺少放大挂载用的舞台容器")
	}
	if strings.Contains(html, "pd-thumb-video") {
		t.Errorf("没有视频时不应渲染视频缩略图")
	}

	// mp4 直链：出视频缩略图，kind=file
	p := base
	p.VideoURL = "https://cdn.example.com/demo.mp4"
	html = render(&p)
	if !strings.Contains(html, `pd-thumb-video`) || !strings.Contains(html, `data-video-kind="file"`) {
		t.Errorf("mp4 直链应渲染 file 类型的视频缩略图，实际：%s", excerpt(html, "pd-gallery"))
	}
	if !strings.Contains(html, `data-video-src="https://cdn.example.com/demo.mp4"`) {
		t.Errorf("视频地址没带到 data-video-src 上")
	}

	// YouTube：出视频缩略图，kind=embed，且地址换成无 cookie 嵌入页
	p.VideoURL = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	html = render(&p)
	if !strings.Contains(html, `data-video-kind="embed"`) || !strings.Contains(html, "youtube-nocookie.com/embed/dQw4w9WgXcQ") {
		t.Errorf("YouTube 应渲染嵌入播放器缩略图，实际：%s", excerpt(html, "pd-gallery"))
	}

	// 认不出来的地址：不出缩略图，保留「新窗口打开」的链接
	p.VideoURL = "https://example.com/landing.html"
	html = render(&p)
	if strings.Contains(html, "pd-thumb-video") {
		t.Errorf("非白名单地址不应渲染视频缩略图")
	}
	if !strings.Contains(html, "pd-media-tabs") {
		t.Errorf("非白名单地址应保留「观看产品视频」链接兜底")
	}
}

func excerpt(s, anchor string) string {
	i := strings.Index(s, anchor)
	if i < 0 {
		return "(未找到 " + anchor + ")"
	}
	end := i + 400
	if end > len(s) {
		end = len(s)
	}
	return s[i:end]
}
