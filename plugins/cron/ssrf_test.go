package cron

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckIPBlocksInternal(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1", "10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"169.254.169.254", "fe80::1", "fc00::1", "fd12::1", "0.0.0.0", "::", "224.0.0.1", "ff02::1",
		"100.64.0.1", "198.18.0.1", "240.0.0.1", "255.255.255.255", "::ffff:10.0.0.1", "::ffff:127.0.0.1",
		"64:ff9b::a00:1", "192.0.2.1",
	}
	for _, s := range blocked {
		if err := checkIP(net.ParseIP(s)); !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("checkIP(%s) = %v, want ErrBlockedAddress", s, err)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111", "172.32.0.1", "11.0.0.1"}
	for _, s := range allowed {
		if err := checkIP(net.ParseIP(s)); err != nil {
			t.Errorf("checkIP(%s) = %v, want nil", s, err)
		}
	}
}

func TestValidateURLStatic(t *testing.T) {
	bad := []string{
		"", "ftp://example.com/x", "file:///etc/passwd", "gopher://example.com", "javascript:alert(1)",
		"http://localhost/x", "http://LOCALHOST:8080", "http://a.localhost", "http://svc.internal/x",
		"http://127.0.0.1:6379", "http://[::1]/", "http://10.1.2.3/", "http://169.254.169.254/latest/meta-data/",
		"http://[::ffff:192.168.0.1]/", "http://user:pass@example.com/", "http:///nohost",
	}
	for _, u := range bad {
		if _, err := validateURLStatic(u); err == nil {
			t.Errorf("validateURLStatic(%q) should fail", u)
		}
	}
	for _, u := range []string{"https://example.com/hook?a=1", "http://8.8.8.8/x", "https://[2606:4700:4700::1111]/"} {
		if _, err := validateURLStatic(u); err != nil {
			t.Errorf("validateURLStatic(%q): %v", u, err)
		}
	}
}

func TestValidateURLResolvesAllAddresses(t *testing.T) {
	mk := func(ips ...string) lookupFunc {
		return func(context.Context, string) ([]net.IP, error) {
			out := make([]net.IP, len(ips))
			for i, s := range ips {
				out[i] = net.ParseIP(s)
			}
			return out, nil
		}
	}
	ctx := context.Background()
	if _, err := validateURL(ctx, "https://evil.example.com/", mk("10.0.0.5")); !errors.Is(err, ErrBlockedAddress) {
		t.Errorf("domain resolving to private ip should be blocked, got %v", err)
	}
	// 只要有一个解析结果是内网就拒绝（防止 DNS 返回多条记录混入内网地址）
	if _, err := validateURL(ctx, "https://mixed.example.com/", mk("8.8.8.8", "127.0.0.1")); !errors.Is(err, ErrBlockedAddress) {
		t.Errorf("mixed answers should be blocked, got %v", err)
	}
	if _, err := validateURL(ctx, "https://ok.example.com/", mk("8.8.8.8", "1.1.1.1")); err != nil {
		t.Errorf("public domain should pass: %v", err)
	}
	if _, err := validateURL(ctx, "https://none.example.com/", mk()); err == nil {
		t.Error("empty resolution should fail")
	}
}

// 连接层兜底：即使前面的校验被绕过（这里直接跳过 validateURL），Dialer.Control 也会拒绝回环地址。
func TestSafeClientBlocksLoopbackAtDial(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hit = true }))
	defer srv.Close()
	_, err := newSafeClient(httpDefaultTimeout).Get(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected blocked dial, got %v", err)
	}
	if hit {
		t.Fatal("request must not reach the loopback server")
	}
}

func TestRunHTTPRejectsSSRF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("secret")) }))
	defer srv.Close()
	for _, u := range []string{srv.URL, "http://169.254.169.254/latest/meta-data/", "http://localhost:8080/", "file:///etc/passwd"} {
		out, err := runHTTP(context.Background(), nil, `{"url":"`+u+`"}`)
		if err == nil || strings.Contains(out, "secret") {
			t.Errorf("runHTTP(%s) should be rejected, out=%q err=%v", u, out, err)
		}
	}
}

func TestHTTPParams(t *testing.T) {
	for _, p := range []string{
		`{"url":"https://example.com","method":"PUT"}`, `{"url":"https://example.com","timeoutSec":99}`,
		`{"url":"https://example.com","extra":1}`, `{"method":"GET"}`, `not json`,
	} {
		if err := validateHTTPParams(p); err == nil {
			t.Errorf("validateHTTPParams(%s) should fail", p)
		}
	}
	if err := validateHTTPParams(`{"url":"https://example.com/ping","method":"post","body":"{}","timeoutSec":5}`); err != nil {
		t.Errorf("valid params rejected: %v", err)
	}
}

func TestCleanAndLogParams(t *testing.T) {
	if err := validateCleanParams(``); err != nil {
		t.Errorf("empty clean params should default: %v", err)
	}
	for _, p := range []string{`{"days":0.5}`, `{"days":-1}`, `{"days":5000}`, `{"targets":["user"]}`} {
		if err := validateCleanParams(p); err == nil {
			t.Errorf("validateCleanParams(%s) should fail", p)
		}
	}
	if err := validateLogParams(`{"message":"hi"}`); err != nil {
		t.Error(err)
	}
}

func TestRegistry(t *testing.T) {
	for _, n := range []string{"http", "clean_logs", "log"} {
		if _, ok := lookupHandler(n); !ok {
			t.Errorf("builtin handler %q missing", n)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("duplicate registration should panic")
		}
	}()
	RegisterHandler(HandlerSpec{Name: "log", Run: runLog})
}
