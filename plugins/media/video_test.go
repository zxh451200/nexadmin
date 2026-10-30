package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 假 Cloudflare 上游：断言请求形状，返回一次性直传地址。
func TestCloudflareCreateUpload(t *testing.T) {
	var gotAuth, gotPath, gotName string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		var body struct {
			Meta struct {
				Name string `json:"name"`
			} `json:"meta"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotName = body.Meta.Name
		_, _ = w.Write([]byte(`{"success":true,"result":{"uploadURL":"https://upload.example/tus/abc","uid":"vid-12345678"}}`))
	}))
	defer ts.Close()

	c := &cloudflareStream{hc: ts.Client(), baseURL: ts.URL}
	cfg := VideoConfig{Enabled: true, Provider: ProviderCloudflare, KeyA: "acc-1", KeyB: "tok", KeyC: "demo"}
	url, headers, id, err := c.CreateUpload(context.Background(), cfg, "demo.mp4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://upload.example/tus/abc" || id != "vid-12345678" || len(headers) != 0 {
		t.Fatalf("got url=%q id=%q headers=%v", url, id, headers)
	}
	if gotAuth != "Bearer tok" || gotPath != "/accounts/acc-1/stream/direct_upload" || gotName != "demo.mp4" {
		t.Fatalf("request shape: auth=%q path=%q name=%q", gotAuth, gotPath, gotName)
	}
	if p := c.PlaybackURL(cfg, id); p != "https://customer-demo.cloudflarestream.com/vid-12345678/iframe" {
		t.Fatalf("playback url = %q", p)
	}
}

// 假 Bunny 上游：建视频拿 guid，返回带签名的 tus 参数；签名必须等于本地按公式算出来的值。
func TestBunnyCreateUpload(t *testing.T) {
	var gotKey, gotPath, gotTitle string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("AccessKey")
		gotPath = r.URL.Path
		var body struct {
			Title string `json:"title"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle = body.Title
		_, _ = w.Write([]byte(`{"success":true,"guid":"guid-12345678"}`))
	}))
	defer ts.Close()

	fixed := time.Unix(1800000000, 0)
	c := &bunnyStream{hc: ts.Client(), baseURL: ts.URL, now: func() time.Time { return fixed }}
	cfg := VideoConfig{Enabled: true, Provider: ProviderBunny, KeyA: "lib-1", KeyB: "api-key", KeyC: "pull-zone"}
	url, headers, id, err := c.CreateUpload(context.Background(), cfg, "demo.mp4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "api-key" || gotPath != "/library/lib-1/videos" || gotTitle != "demo.mp4" {
		t.Fatalf("request shape: key=%q path=%q title=%q", gotKey, gotPath, gotTitle)
	}
	if url != ts.URL+"/tusupload" || id != "guid-12345678" {
		t.Fatalf("got url=%q id=%q", url, id)
	}
	expire := strconv.FormatInt(fixed.Add(2*time.Hour).Unix(), 10)
	sum := sha256.Sum256([]byte("lib-1" + "api-key" + expire + "guid-12345678"))
	want := hex.EncodeToString(sum[:])
	if headers["AuthorizationSignature"] != want || headers["AuthorizationExpire"] != expire ||
		headers["VideoId"] != "guid-12345678" || headers["LibraryId"] != "lib-1" {
		t.Fatalf("signature headers = %v", headers)
	}
	if p := c.PlaybackURL(cfg, id); p != "https://pull-zone.b-cdn.net/guid-12345678/playlist.m3u8" {
		t.Fatalf("playback = %q", p)
	}
	if d := c.DownloadURL(cfg, id); d != "https://pull-zone.b-cdn.net/guid-12345678/play_720p.mp4" {
		t.Fatalf("download = %q", d)
	}
}

// 上游报错要透出可读原因，而不是「未知错误」。
func TestCreateUploadErrors(t *testing.T) {
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"message":"Invalid API Token"}]}`))
	}))
	defer cf.Close()
	_, _, _, err := (&cloudflareStream{hc: cf.Client(), baseURL: cf.URL}).CreateUpload(context.Background(), VideoConfig{KeyA: "a", KeyB: "t"}, "x.mp4")
	if err == nil || !strings.Contains(err.Error(), "Invalid API Token") {
		t.Fatalf("cf err: %v", err)
	}
	if _, _, _, err := (&cloudflareStream{}).CreateUpload(context.Background(), VideoConfig{KeyA: "a"}, "x.mp4"); err == nil {
		t.Fatal("missing token must fail fast")
	}

	bunny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Library not found"}`))
	}))
	defer bunny.Close()
	_, _, _, err = (&bunnyStream{hc: bunny.Client(), baseURL: bunny.URL, now: time.Now}).CreateUpload(context.Background(), VideoConfig{KeyA: "l", KeyB: "k", KeyC: "z"}, "x.mp4")
	if err == nil || !strings.Contains(err.Error(), "Library not found") {
		t.Fatalf("bunny err: %v", err)
	}
}

// videoId 只允许厂商 ID 的字符集，防止把任意串拼进播放地址。
func TestVideoIDPattern(t *testing.T) {
	for _, ok := range []string{"vid-12345678", "abcdefgh12345678", "a_b-c_12345678", "guid-12345678"} {
		if !videoIDPattern.MatchString(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "short", "has space here", "../etc/passwd", "a/b/c/d/e/f/g/h"} {
		if videoIDPattern.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// providerFor 只认已实现的厂商。
func TestProviderFor(t *testing.T) {
	for _, name := range []string{ProviderCloudflare, ProviderBunny} {
		if p, err := providerFor(name); err != nil || p.Name() != name {
			t.Errorf("providerFor(%q) = %v, %v", name, p, err)
		}
	}
	if _, err := providerFor("mux"); err == nil {
		t.Error("unknown provider should fail")
	}
}

// 阿里云/腾讯云：只登记已有资产，播放地址按「播放域名 + 资产 id」推导。
func TestRegisterOnlyProviders(t *testing.T) {
	cases := []struct{ name, domain, id, play string }{
		{ProviderAliyun, "https://vod.example.com", "vid-12345678", "https://vod.example.com/vid-12345678/index.m3u8"},
		{ProviderTencent, "https://play.example.com/", "file-12345678", "https://play.example.com/file-12345678.m3u8"},
	}
	for _, c := range cases {
		p, err := providerFor(c.name)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if p.SupportsUpload() {
			t.Errorf("%s should be register-only", c.name)
		}
		cfg := VideoConfig{Enabled: true, Provider: c.name, KeyA: c.domain}
		if got := p.PlaybackURL(cfg, c.id); got != c.play {
			t.Errorf("%s playback = %q, want %q", c.name, got, c.play)
		}
		if _, _, _, err := p.CreateUpload(context.Background(), cfg, "x.mp4"); err == nil {
			t.Errorf("%s must reject direct upload", c.name)
		}
		if got := p.DownloadURL(cfg, c.id); got != "" {
			t.Errorf("%s download must be empty (rules vary by template), got %q", c.name, got)
		}
	}
}
