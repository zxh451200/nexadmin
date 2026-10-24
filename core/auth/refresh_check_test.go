package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestInfoDirectLoopback(t *testing.T) {
	mk := func(remote string, hdr map[string]string) RequestInfo {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return NewRequestInfo(r, "9.9.9.9")
	}
	cases := []struct {
		name   string
		remote string
		hdr    map[string]string
		want   bool
	}{
		{"direct v4 loopback", "127.0.0.1:5000", nil, true},
		{"direct v6 loopback", "[::1]:5000", nil, true},
		{"non-loopback peer with spoofed XFF 127.0.0.1", "203.0.113.9:5000", map[string]string{"X-Forwarded-For": "127.0.0.1"}, false},
		{"non-loopback peer with spoofed X-Real-IP", "203.0.113.9:5000", map[string]string{"X-Real-IP": "127.0.0.1"}, false},
		{"local proxy forwarding a remote client", "127.0.0.1:5000", map[string]string{"X-Forwarded-For": "203.0.113.9"}, false},
		{"local with Forwarded header", "127.0.0.1:5000", map[string]string{"Forwarded": "for=203.0.113.9"}, false},
		{"plain remote", "203.0.113.9:5000", nil, false},
	}
	for _, c := range cases {
		if got := mk(c.remote, c.hdr).DirectLoopback(); got != c.want {
			t.Errorf("%s: DirectLoopback=%v want %v", c.name, got, c.want)
		}
	}
}

func TestRefreshCheckDeniesAndRevokes(t *testing.T) {
	x := newExtEnv(t)
	denied := false
	var seen RequestInfo
	x.s.RegisterRefreshCheck(func(_ context.Context, u *User, ri RequestInfo) error {
		seen = ri
		if denied {
			return errors.New("ip not allowed")
		}
		return nil
	})
	code, r := x.login("1.1.1.1")
	if code != 200 {
		t.Fatalf("login: %d %+v", code, r)
	}
	rt1, _ := r.Data["refreshToken"].(string)
	// 放行:正常换发,且检查函数看到的是对端地址
	code, r = x.post("/refresh", map[string]string{"refreshToken": rt1}, "1.1.1.1")
	if code != 200 {
		t.Fatalf("allowed refresh: %d %+v", code, r)
	}
	if seen.Peer != "1.1.1.1" || seen.IP != "1.1.1.1" {
		t.Fatalf("request info not propagated: %+v", seen)
	}
	rt2, _ := r.Data["refreshToken"].(string)
	// 拒绝:403,并且该刷新令牌被吊销(即使之后规则放开也不能再用)
	denied = true
	if code, _ = x.post("/refresh", map[string]string{"refreshToken": rt2}, "6.6.6.6"); code != http.StatusForbidden {
		t.Fatalf("denied refresh must be 403, got %d", code)
	}
	denied = false
	if code, _ = x.post("/refresh", map[string]string{"refreshToken": rt2}, "1.1.1.1"); code != http.StatusUnauthorized {
		t.Fatalf("refresh token must be revoked after a denied refresh, got %d", code)
	}
}

func TestRefreshCheckNotCalledWithoutRequestInfo(t *testing.T) {
	x := newExtEnv(t)
	called := false
	x.s.RegisterRefreshCheck(func(context.Context, *User, RequestInfo) error { called = true; return errors.New("no") })
	code, r := x.login("1.1.1.1")
	if code != 200 {
		t.Fatalf("login: %d", code)
	}
	rt, _ := r.Data["refreshToken"].(string)
	// 直接调 Service.Refresh(没有 HTTP 来源):不触发复检
	if _, err := x.s.Refresh(context.Background(), rt); err != nil {
		t.Fatalf("internal refresh: %v", err)
	}
	if called {
		t.Fatal("refresh check must only run for requests that carry RequestInfo")
	}
}
