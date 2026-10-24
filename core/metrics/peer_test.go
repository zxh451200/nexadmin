package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// /metrics 的回环判定只看对端地址：即使引擎信任了本机代理（ClientIP 会采信转发头），伪造的转发头也不能绕过。
func TestMetricsLoopbackUsesPeerAddressNotClientIP(t *testing.T) {
	r := newRegistry()
	call := func(remote string, hdr map[string]string) int {
		e := gin.New()
		_ = e.SetTrustedProxies([]string{"10.0.0.0/8", "127.0.0.1"})
		e.GET("/metrics", r.handler(Options{}))
		req := httptest.NewRequest("GET", "/metrics", nil)
		req.RemoteAddr = remote
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}
	// 可信代理 10.1.2.3 转发了一个“127.0.0.1”的头：对端并非回环 -> 403。
	if c := call("10.1.2.3:1", map[string]string{"X-Real-IP": "127.0.0.1"}); c != http.StatusForbidden {
		t.Fatalf("spoofed loopback via trusted proxy = %d, want 403", c)
	}
	if c := call("10.1.2.3:1", map[string]string{"X-Forwarded-For": "127.0.0.1"}); c != http.StatusForbidden {
		t.Fatalf("spoofed XFF loopback via trusted proxy = %d, want 403", c)
	}
	if c := call("127.0.0.1:1", nil); c != http.StatusOK {
		t.Fatalf("real loopback peer = %d, want 200", c)
	}
}
