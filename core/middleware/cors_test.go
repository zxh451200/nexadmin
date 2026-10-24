package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func corsReq(e *gin.Engine, method, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/files/upload", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func corsEngine(dev bool, allowed ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(CORS(dev, allowed...))
	e.Any("/files/upload", func(c *gin.Context) { c.String(200, "ok") })
	return e
}

func TestCORSProductionSameOriginByDefault(t *testing.T) {
	e := corsEngine(false)
	w := corsReq(e, http.MethodGet, "https://evil.example")
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("production without whitelist must not emit CORS headers")
	}
	if w := corsReq(e, http.MethodOptions, "https://evil.example"); w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("preflight from unknown origin must get no CORS headers")
	}
}

func TestCORSWhitelistPreflightAndTus(t *testing.T) {
	e := corsEngine(false, "https://admin.example.com/", "*")
	w := corsReq(e, http.MethodOptions, "https://admin.example.com")
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight want 204, got %d", w.Code)
	}
	h := w.Header()
	if h.Get("Access-Control-Allow-Origin") != "https://admin.example.com" {
		t.Fatalf("origin not echoed: %q", h.Get("Access-Control-Allow-Origin"))
	}
	if h.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("must not send Allow-Credentials")
	}
	if !strings.Contains(h.Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Fatal("PATCH must be allowed")
	}
	for _, name := range []string{"Upload-Offset", "Upload-Length", "Upload-Metadata", "Tus-Resumable", "Upload-Defer-Length", "Upload-Concat"} {
		if !strings.Contains(h.Get("Access-Control-Allow-Headers"), name) {
			t.Errorf("Allow-Headers missing %s", name)
		}
	}
	for _, name := range []string{"Location", "Upload-Offset", "Tus-Resumable"} {
		if !strings.Contains(h.Get("Access-Control-Expose-Headers"), name) {
			t.Errorf("Expose-Headers missing %s", name)
		}
	}
	// 实际请求带 Origin 时也输出（含 Expose），且 "*" 不会被当作白名单项。
	w = corsReq(e, http.MethodPatch, "https://admin.example.com")
	if w.Code != 200 || w.Header().Get("Access-Control-Expose-Headers") == "" {
		t.Fatalf("actual request: code=%d expose=%q", w.Code, w.Header().Get("Access-Control-Expose-Headers"))
	}
	if w := corsReq(e, http.MethodGet, "https://other.example.com"); w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("non-whitelisted origin must not be allowed, even if '*' was configured")
	}
	if w := corsReq(e, http.MethodGet, ""); w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no Origin => no CORS headers")
	}
}

func TestCORSDevAllowsAnyOriginWithoutWildcard(t *testing.T) {
	e := corsEngine(true)
	w := corsReq(e, http.MethodOptions, "http://localhost:5173")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("dev must echo origin, got %q", got)
	}
	if w.Header().Get("Access-Control-Allow-Origin") == "*" {
		t.Fatal("never wildcard")
	}
}

// 基线安全头挂全局；业务 handler 可以覆盖同名头（文件预览的 CSP sandbox 依赖这一点）。
func TestSecurityHeadersDefaultsAndOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(SecurityHeaders())
	e.GET("/plain", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	e.GET("/override", func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.String(http.StatusOK, "ok")
	})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/plain", nil))
	h := w.Header()
	if h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("X-Frame-Options") != "SAMEORIGIN" ||
		h.Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Fatalf("missing baseline security headers: %v", h)
	}
	w = httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/override", nil))
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("handler must be able to override baseline headers, got %q", w.Header().Get("X-Frame-Options"))
	}
}
