package file

import (
	"testing"
)

// Accept 协商只认显式 image/webp；q=0 是拒绝，不能当成接受。
func TestAcceptsWebP(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"image/avif,image/webp,image/apng,*/*;q=0.8", true},
		{"image/webp", true},
		{"IMAGE/WEBP;Q=1", true},
		{"image/webp;q=0", false},
		{"image/webp; q=0.000", false},
		{"image/png,image/jpeg", false},
		{"*/*", false},
		{"", false},
	}
	for _, c := range cases {
		if got := acceptsWebP(c.accept); got != c.want {
			t.Errorf("acceptsWebP(%q) = %v, want %v", c.accept, got, c.want)
		}
	}
}

// 图片响应必须声明 Vary: Accept，否则 CDN/浏览器会把原图和副本混为一谈。
func TestServe_ImageSetsVaryAccept(t *testing.T) {
	m := &memStore{data: map[string][]byte{"1/20261028/7.jpg": []byte("jpg")}}
	h := &Handler{svc: &Service{store: &store{blobs: m}}}
	img := File{Name: "a.jpg", StoragePath: "1/20261028/7.jpg", MimeType: "image/jpeg", Category: "image", Size: 3}
	if w := serveReq(h, img, true, "", ""); w.Header().Get("Vary") != "Accept" {
		t.Fatalf("image Vary = %q, want Accept", w.Header().Get("Vary"))
	}
	vid := File{Name: "a.mp4", StoragePath: "1/20261028/9.mp4", MimeType: "video/mp4", Category: "video", Size: 10}
	if w := serveReq(h, vid, true, "", ""); w.Header().Get("Vary") != "" {
		t.Fatalf("video should not set Vary, got %q", w.Header().Get("Vary"))
	}
	if w := serveReq(h, img, true, "", "download=1"); w.Header().Get("Vary") != "" {
		t.Fatalf("download should not negotiate, got Vary %q", w.Header().Get("Vary"))
	}
}
