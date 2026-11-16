package website

import "testing"

// TestProductVideo 覆盖详情页视频的来源识别：直链文件走原生播放器，YouTube / Vimeo /
// Bilibili 走各自嵌入地址；认不出来的（含 m3u8、第三方落地页）返回 nil，模板回退成链接。
// 这块是「后台自由文本 → 塞进 iframe」的边界，白名单错了就是 XSS 或 404，所以逐条钉住。
func TestProductVideo(t *testing.T) {
	cases := []struct {
		name string
		url  string
		kind string
		src  string
	}{
		{"空值", "", "", ""},
		{"空白", "   ", "", ""},
		{"mp4 直链", "https://cdn.example.com/a/b.mp4", "file", "https://cdn.example.com/a/b.mp4"},
		{"站内相对路径", "/website-assets/media/123.webm", "file", "/website-assets/media/123.webm"},
		{"带查询串的 mp4", "https://cdn.example.com/v.mp4?x=1", "file", "https://cdn.example.com/v.mp4?x=1"},
		{"youtube watch", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "embed", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?autoplay=1&rel=0&playsinline=1"},
		{"youtube 短链", "https://youtu.be/dQw4w9WgXcQ?t=3", "embed", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?autoplay=1&rel=0&playsinline=1"},
		{"youtube shorts", "https://www.youtube.com/shorts/dQw4w9WgXcQ", "embed", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?autoplay=1&rel=0&playsinline=1"},
		{"youtube 非法 id", "https://www.youtube.com/watch?v=<script>", "", ""},
		{"vimeo", "https://vimeo.com/123456789", "embed", "https://player.vimeo.com/video/123456789?autoplay=1"},
		{"vimeo 频道", "https://vimeo.com/channels/staffpicks/123456789", "embed", "https://player.vimeo.com/video/123456789?autoplay=1"},
		{"bilibili", "https://www.bilibili.com/video/BV1xx411c7mD", "embed", "https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&autoplay=1&high_quality=1&danmaku=0"},
		{"m3u8 不假装能播", "https://cdn.example.com/live/index.m3u8", "", ""},
		{"第三方落地页", "https://example.com/product/demo.html", "", ""},
		{"裸域名", "https://example.com", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Product{VideoURL: tc.url}
			got := p.Video()
			if tc.kind == "" {
				if got != nil {
					t.Fatalf("VideoURL=%q 期望 nil，得到 %+v", tc.url, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("VideoURL=%q 期望 %s，得到 nil", tc.url, tc.kind)
			}
			if got.Kind != tc.kind || got.Src != tc.src {
				t.Fatalf("VideoURL=%q 得到 %+v，期望 kind=%s src=%s", tc.url, got, tc.kind, tc.src)
			}
		})
	}
}

// TestProductVideoIgnoresForeignHost 单独钉一条：任何非白名单域名都不能被当成嵌入源，
// 哪怕路径长得像播放器（防把任意站塞进 iframe）。
func TestProductVideoIgnoresForeignHost(t *testing.T) {
	for _, u := range []string{
		"https://evil.example/embed/dQw4w9WgXcQ",
		"https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ",
		"javascript:alert(1)",
	} {
		if v := (Product{VideoURL: u}).Video(); v != nil {
			t.Fatalf("VideoURL=%q 不应被识别为可嵌入视频，得到 %+v", u, v)
		}
	}
}
