package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 反向代理部署：对端是可信代理时，限流按转发头里的真实客户端 IP 计数，而不是所有人共用代理 IP。
func TestRateLimitCountsPerRealClientIPBehindTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := memCache(t)
	e := gin.New()
	_ = e.SetTrustedProxies([]string{"127.0.0.1"})
	e.GET("/a", RateLimit(c, zap.NewNop(), "t", ByIP(), 2, time.Minute), func(c *gin.Context) { c.String(200, "ok") })

	hit := func(xff string) int {
		req := httptest.NewRequest(http.MethodGet, "/a", nil)
		req.RemoteAddr = "127.0.0.1:9000" // nginx
		req.Header.Set("X-Forwarded-For", xff)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}
	for i := 0; i < 2; i++ {
		if code := hit("198.51.100.1"); code != 200 {
			t.Fatalf("client A request %d = %d", i+1, code)
		}
	}
	if code := hit("198.51.100.1"); code != http.StatusTooManyRequests {
		t.Fatalf("client A third request = %d, want 429", code)
	}
	if code := hit("198.51.100.2"); code != 200 {
		t.Fatalf("client B must have its own counter, got %d", code)
	}
}

// 不信任代理（默认）：伪造 X-Forwarded-For 不能换取新的限流额度。
func TestRateLimitSpoofedForwardedForDoesNotResetCounter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := memCache(t)
	e := gin.New()
	_ = e.SetTrustedProxies(nil)
	e.GET("/a", RateLimit(c, zap.NewNop(), "t", ByIP(), 2, time.Minute), func(c *gin.Context) { c.String(200, "ok") })
	last := 0
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodGet, "/a", nil)
		req.RemoteAddr = "203.0.113.9:9000"
		req.Header.Set("X-Forwarded-For", "10.0.0."+string(rune('1'+i)))
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		last = w.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("rotating spoofed XFF must not bypass the limit, last = %d", last)
	}
}
