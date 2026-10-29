package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 假 Cloudflare Stream 上游：断言请求形状，返回一次性直传地址。
func TestCloudflareCreateDirectUpload(t *testing.T) {
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
	cfg := VideoConfig{Enabled: true, AccountID: "acc-1", Token: "tok", Subdomain: "demo"}
	url, id, err := c.CreateDirectUpload(context.Background(), cfg, "demo.mp4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://upload.example/tus/abc" || id != "vid-12345678" {
		t.Fatalf("got url=%q id=%q", url, id)
	}
	if gotAuth != "Bearer tok" || gotPath != "/accounts/acc-1/stream/direct_upload" || gotName != "demo.mp4" {
		t.Fatalf("request shape: auth=%q path=%q name=%q", gotAuth, gotPath, gotName)
	}
	if p := c.PlaybackURL(cfg, id); p != "https://customer-demo.cloudflarestream.com/vid-12345678/iframe" {
		t.Fatalf("playback url = %q", p)
	}
}

// 上游报错要透出可读原因，而不是「未知错误」。
func TestCloudflareCreateDirectUploadError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"message":"Invalid API Token"}]}`))
	}))
	defer ts.Close()
	c := &cloudflareStream{hc: ts.Client(), baseURL: ts.URL}
	_, _, err := c.CreateDirectUpload(context.Background(), VideoConfig{AccountID: "a", Token: "t"}, "x.mp4")
	if err == nil || !strings.Contains(err.Error(), "Invalid API Token") {
		t.Fatalf("want upstream message, got %v", err)
	}
	if _, _, err := c.CreateDirectUpload(context.Background(), VideoConfig{AccountID: "a"}, "x.mp4"); err == nil {
		t.Fatal("missing token must fail fast")
	}
}

// videoId 只允许厂商 ID 的字符集，防止把任意串拼进播放地址。
func TestVideoIDPattern(t *testing.T) {
	for _, ok := range []string{"vid-12345678", "abcdefgh12345678", "a_b-c_12345678"} {
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
