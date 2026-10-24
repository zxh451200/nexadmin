package openapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestCheckIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "::1", "10.0.0.1", "172.16.5.4", "192.168.1.1", "169.254.169.254", "fe80::1", "fc00::1",
		"0.0.0.0", "::", "100.64.0.1", "198.18.0.1", "224.0.0.1", "255.255.255.255", "240.0.0.1", "::ffff:127.0.0.1", "::ffff:10.1.2.3",
		"64:ff9b::a00:1", "192.0.2.1", "2001:db8::1",
	}
	for _, s := range blocked {
		if err := checkIP(net.ParseIP(s)); !errors.Is(err, errBlockedAddress) {
			t.Errorf("%s should be blocked, got %v", s, err)
		}
	}
	for _, s := range []string{"8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"} {
		if err := checkIP(net.ParseIP(s)); err != nil {
			t.Errorf("%s should be allowed, got %v", s, err)
		}
	}
}

func TestValidateURLStatic(t *testing.T) {
	bad := []string{
		"ftp://example.com/x", "file:///etc/passwd", "gopher://example.com", "javascript:alert(1)", "//example.com/x", "http://",
		"http://user:pw@example.com/", "http://localhost/x", "http://foo.localhost/", "http://svc.internal/", "http://printer.local/",
		"http://127.0.0.1:8080/", "http://[::1]/", "http://169.254.169.254/latest/meta-data/", "http://10.0.0.5/", "http://192.168.0.1/",
		"http://[::ffff:10.0.0.1]/",
	}
	for _, u := range bad {
		if _, err := validateURLStatic(u); err == nil {
			t.Errorf("%q should be rejected", u)
		}
	}
	for _, u := range []string{"https://example.com/hook", "http://8.8.8.8:8080/x"} {
		if _, err := validateURLStatic(u); err != nil {
			t.Errorf("%q should pass static validation: %v", u, err)
		}
	}
}

func fixedLookup(ips ...string) lookupFunc {
	return func(context.Context, string) ([]net.IP, error) {
		out := make([]net.IP, len(ips))
		for i, s := range ips {
			out[i] = net.ParseIP(s)
		}
		return out, nil
	}
}

// 域名解析到内网（含"一个公网 + 一个内网"的混合应答）必须被拒绝。
func TestValidateURLRejectsDNSPointingInternal(t *testing.T) {
	for _, ips := range [][]string{{"10.0.0.8"}, {"127.0.0.1"}, {"169.254.169.254"}, {"8.8.8.8", "192.168.0.9"}, {"::1"}} {
		_, err := validateURL(context.Background(), "https://hooks.example.com/x", fixedLookup(ips...))
		if !errors.Is(err, errBlockedAddress) {
			t.Errorf("lookup %v: want blocked, got %v", ips, err)
		}
	}
	if _, err := validateURL(context.Background(), "https://hooks.example.com/x", fixedLookup("8.8.8.8", "2606:4700:4700::1111")); err != nil {
		t.Errorf("public answers rejected: %v", err)
	}
	if _, err := validateURL(context.Background(), "https://hooks.example.com/x", fixedLookup()); err == nil {
		t.Error("empty answer must be rejected")
	}
}

// 发送路径：DNS 解析到内网 -> 根本不连接。
func TestSenderRefusesInternalResolution(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))

	s := newSender()
	s.lookup = fixedLookup("127.0.0.1") // evil.example 指向本机
	res := s.send(context.Background(), "http://evil.example:"+port+"/", "sec", "ping", "d1", []byte(`{}`))
	if res.ok() || !strings.Contains(res.Err, "not allowed") || hits.Load() != 0 {
		t.Fatalf("res=%+v hits=%d", res, hits.Load())
	}
	// 字面内网地址
	res = s.send(context.Background(), srv.URL, "sec", "ping", "d2", []byte(`{}`))
	if res.ok() || !strings.Contains(res.Err, "not allowed") || hits.Load() != 0 {
		t.Fatalf("literal loopback: res=%+v hits=%d", res, hits.Load())
	}
}

// DNS rebinding：校验时看起来是公网，连接时实际落到内网 -> 由 Dialer.Control 兜底。
func TestDialControlBlocksRebinding(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()

	// 跳过第 2 层（解析校验），模拟"校验后域名被改指向内网"，只靠连接时校验。
	s := &sender{client: newClient(2*time.Second, safeDialControl), now: time.Now}
	res := s.send(context.Background(), srv.URL, "sec", "ping", "d1", []byte(`{}`))
	if res.ok() || !strings.Contains(res.Err, "not allowed") || hits.Load() != 0 {
		t.Fatalf("res=%+v hits=%d", res, hits.Load())
	}
	for _, addr := range []string{"127.0.0.1:80", "10.0.0.1:443", "169.254.169.254:80", "[::1]:80", "[::ffff:192.168.1.1]:80", "100.64.0.1:80"} {
		if err := safeDialControl("tcp", addr, nil); !errors.Is(err, errBlockedAddress) {
			t.Errorf("dial %s should be blocked, got %v", addr, err)
		}
	}
	if err := safeDialControl("tcp", "8.8.8.8:443", nil); err != nil {
		t.Errorf("public dial rejected: %v", err)
	}
	if err := safeDialControl("tcp", "example.com:443", nil); err == nil {
		t.Error("non-IP dial address must be rejected")
	}
}

// 重定向：即使第一跳被放行，也不会跟随 302 到内网地址。
func TestRedirectToInternalIsNotFollowed(t *testing.T) {
	var internalHits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { internalHits.Add(1) }))
	defer internal.Close()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/admin", http.StatusFound)
	}))
	defer front.Close()

	frontAddr := strings.TrimPrefix(front.URL, "http://")
	allowFront := func(network, address string, c syscall.RawConn) error {
		if address == frontAddr {
			return nil // 测试里放行"第一跳"，用来证明拦截来自"不跟随重定向"
		}
		return safeDialControl(network, address, c)
	}
	s := &sender{client: newClient(2*time.Second, allowFront), now: time.Now}
	res := s.send(context.Background(), front.URL, "sec", "ping", "d1", []byte(`{}`))
	if res.ok() || res.Status != http.StatusFound || internalHits.Load() != 0 {
		t.Fatalf("res=%+v internalHits=%d", res, internalHits.Load())
	}

	// 对照：即便客户端放行了所有地址，CheckRedirect 也不会发起第二次请求。
	s2 := &sender{client: newClient(2*time.Second, func(string, string, syscall.RawConn) error { return nil }), now: time.Now}
	res = s2.send(context.Background(), front.URL, "sec", "ping", "d2", []byte(`{}`))
	if res.Status != http.StatusFound || internalHits.Load() != 0 {
		t.Fatalf("redirect was followed: res=%+v internalHits=%d", res, internalHits.Load())
	}
}

func TestSaveRejectsUnsafeWebhookURLs(t *testing.T) {
	f := hookForm{Name: "x", URL: "http://169.254.169.254/latest", Events: []string{"user.created"}}
	if e := f.normalize(); e == nil {
		t.Fatal("metadata address must be rejected at save time")
	}
	f = hookForm{Name: "x", URL: "https://example.com/hook", Events: []string{"user.created", "user.created"}}
	if e := f.normalize(); e != nil || len(f.Events) != 1 {
		t.Fatalf("valid form rejected: %v %v", e, f.Events)
	}
	f = hookForm{Name: "x", URL: "https://example.com/hook", Events: []string{"nope.event"}}
	if e := f.normalize(); e == nil {
		t.Fatal("unknown event must be rejected")
	}
	f = hookForm{Name: "x", URL: "https://example.com/hook"}
	if e := f.normalize(); e == nil {
		t.Fatal("empty events must be rejected")
	}
}
