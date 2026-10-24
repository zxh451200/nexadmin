package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func clientIPOf(t *testing.T, trusted []string, remote, xff, realIP string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	ConfigureClientIP(e, trusted, nil)
	var got string
	e.GET("/ip", func(c *gin.Context) { got = c.ClientIP() })
	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	if realIP != "" {
		req.Header.Set("X-Real-IP", realIP)
	}
	e.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// 默认（不配置 trusted_proxies）：不信任任何代理，伪造的转发头无效。
func TestClientIPIgnoresForwardedHeadersByDefault(t *testing.T) {
	if got := clientIPOf(t, nil, "203.0.113.9:4000", "1.2.3.4", "5.6.7.8"); got != "203.0.113.9" {
		t.Fatalf("untrusted peer must win over spoofed headers, got %s", got)
	}
	if got := clientIPOf(t, []string{}, "127.0.0.1:4000", "1.2.3.4", ""); got != "127.0.0.1" {
		t.Fatalf("even a loopback peer is not a trusted proxy by default, got %s", got)
	}
}

// 信任时：对端在 trusted_proxies 内才采信转发头，并取最右一个不在可信列表内的地址。
func TestClientIPTrustedProxyTakesRightmostNonProxy(t *testing.T) {
	trusted := []string{"127.0.0.1", "10.0.0.0/8"}
	// 客户端自己伪造了最左边的 9.9.9.9；nginx 追加了真实地址 198.51.100.7；10.1.1.1 是内部代理。
	if got := clientIPOf(t, trusted, "127.0.0.1:5000", "9.9.9.9, 198.51.100.7, 10.1.1.1", ""); got != "198.51.100.7" {
		t.Fatalf("want rightmost non-proxy 198.51.100.7, got %s", got)
	}
	// 只有 X-Real-IP 时同样可用。
	if got := clientIPOf(t, trusted, "10.2.2.2:5000", "", "198.51.100.8"); got != "198.51.100.8" {
		t.Fatalf("X-Real-IP via trusted proxy, got %s", got)
	}
	// 对端不在可信列表内：转发头照样无效。
	if got := clientIPOf(t, trusted, "203.0.113.9:5000", "198.51.100.7", ""); got != "203.0.113.9" {
		t.Fatalf("untrusted peer must be used as is, got %s", got)
	}
}
