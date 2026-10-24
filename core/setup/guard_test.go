package setup

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func guardCode(t *testing.T, r *Runner, remote string, hdr map[string]string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	_ = e.SetTrustedProxies([]string{"127.0.0.1"}) // 即使引擎信任本机代理，向导的“本机”判定也只看对端地址
	g := e.Group("/setup")
	g.Use(r.guard)
	g.GET("/env", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/setup/env", nil)
	req.RemoteAddr = remote
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w.Code
}

func TestSetupGuardLocalPeerWithoutProxyHeaders(t *testing.T) {
	r := testRunner(t)
	if c := guardCode(t, r, "127.0.0.1:5000", nil); c != http.StatusOK {
		t.Fatalf("direct loopback = %d, want 200", c)
	}
	if c := guardCode(t, r, "[::1]:5000", nil); c != http.StatusOK {
		t.Fatalf("direct ::1 = %d, want 200", c)
	}
	if c := guardCode(t, r, "203.0.113.9:5000", nil); c != http.StatusForbidden {
		t.Fatalf("remote peer = %d, want 403", c)
	}
}

// 同机 nginx 反代：对端是 127.0.0.1，但带转发头 -> 不能当作本机，默认拒绝。
func TestSetupGuardProxiedRequestIsNotLocal(t *testing.T) {
	r := testRunner(t)
	for _, h := range []map[string]string{
		{"X-Forwarded-For": "198.51.100.7"},
		{"X-Real-IP": "198.51.100.7"},
		{"Forwarded": "for=198.51.100.7"},
		{"X-Forwarded-For": "127.0.0.1"}, // 即使转发头声称是本机也不放行
	} {
		if c := guardCode(t, r, "127.0.0.1:5000", h); c != http.StatusForbidden {
			t.Fatalf("proxied request %v = %d, want 403", h, c)
		}
	}
}

// 允许远程后，经代理的请求必须带正确令牌。
func TestSetupGuardProxiedWithAllowRemoteNeedsToken(t *testing.T) {
	r := testRunner(t)
	r.Cfg.Setup.AllowRemote = true
	r.Token = "tok123"
	h := map[string]string{"X-Forwarded-For": "198.51.100.7"}
	if c := guardCode(t, r, "127.0.0.1:5000", h); c != http.StatusForbidden {
		t.Fatalf("no token = %d, want 403", c)
	}
	h["X-Setup-Token"] = "wrong"
	if c := guardCode(t, r, "127.0.0.1:5000", h); c != http.StatusForbidden {
		t.Fatalf("wrong token = %d, want 403", c)
	}
	h["X-Setup-Token"] = "tok123"
	if c := guardCode(t, r, "127.0.0.1:5000", h); c != http.StatusOK {
		t.Fatalf("right token = %d, want 200", c)
	}
}
