package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func demoEngine(extra ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	reg := NewDemoRegistry(DemoBuiltinAllow("/api/v1")...)
	reg.Allow(extra...)
	api := e.Group("/api/v1")
	api.Use(DemoMode(reg))
	api.Any("/system/users", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	api.Any("/auth/login", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	api.Any("/auth/refresh", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	api.Any("/auth/logout", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	api.Any("/ws/ticket", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	api.Any("/pay/notify", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	api.Any("/pay/callback", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	api.Any("/pay/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return e
}

func demoReq(e *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestDemoModeAllowsReadOnlyAndBuiltin(t *testing.T) {
	e := demoEngine()
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/v1/system/users", 200},
		{http.MethodHead, "/api/v1/system/users", 200},
		{http.MethodOptions, "/api/v1/system/users", 200},
		{http.MethodPost, "/api/v1/auth/login", 200},
		{http.MethodPost, "/api/v1/auth/refresh", 200},
		{http.MethodPost, "/api/v1/auth/logout", 200},
		{http.MethodPost, "/api/v1/ws/ticket", 200},
		{http.MethodPost, "/api/v1/system/users", 403},
		{http.MethodPut, "/api/v1/system/users", 403},
		{http.MethodDelete, "/api/v1/system/users", 403},
		{http.MethodPatch, "/api/v1/system/users", 403},
	} {
		if w := demoReq(e, tc.method, tc.path); w.Code != tc.want {
			t.Errorf("%s %s: want %d, got %d", tc.method, tc.path, tc.want, w.Code)
		}
	}
}

// 尾部斜杠在注册表层面归一（HTTP 层 gin 对带斜杠路径先做 307 重定向，请求到不了中间件）。
func TestDemoRegistryTrailingSlash(t *testing.T) {
	reg := NewDemoRegistry("POST /api/v1/auth/login")
	if !reg.Allowed(http.MethodPost, "/api/v1/auth/login/") {
		t.Fatal("trailing slash must be normalized away")
	}
}

func TestDemoModeConfiguredAllow(t *testing.T) {
	// 方法大小写不敏感、支持尾部 * 前缀；格式不对的条目忽略且不 panic。
	e := demoEngine("post /api/v1/pay/notify", "DELETE /api/v1/pay/*", "nonsense", "", "POST")
	if w := demoReq(e, http.MethodPost, "/api/v1/pay/notify"); w.Code != 200 {
		t.Errorf("exact allow entry: want 200, got %d", w.Code)
	}
	if w := demoReq(e, http.MethodDelete, "/api/v1/pay/callback"); w.Code != 200 {
		t.Errorf("prefix allow entry: want 200, got %d", w.Code)
	}
	if w := demoReq(e, http.MethodPost, "/api/v1/system/users"); w.Code != 403 {
		t.Errorf("allow entries must not open anything else: want 403, got %d", w.Code)
	}
	if w := demoReq(e, http.MethodPost, "/api/v1/pay/"); w.Code != 403 {
		t.Errorf("prefix entry must not match the prefix path itself: want 403, got %d", w.Code)
	}
}

func TestDemoRegistryNilSafety(t *testing.T) {
	var r *DemoRegistry
	r.Allow("POST /api/v1/x") // nil 接收者不 panic
	if r.Allowed(http.MethodPost, "/api/v1/x") {
		t.Fatal("nil registry must allow nothing")
	}
	if DemoMode(nil) == nil {
		t.Fatal("DemoMode must accept a nil registry (403 everything writable)")
	}
}

// 内建白名单必须恰好是四个公开写接口：演示账号改不了密码，也就不需要重置兜底。
func TestDemoBuiltinAllow(t *testing.T) {
	got := DemoBuiltinAllow("/api/v1")
	want := []string{
		"POST /api/v1/auth/login",
		"POST /api/v1/auth/refresh",
		"POST /api/v1/auth/logout",
		"POST /api/v1/ws/ticket",
	}
	if len(got) != len(want) {
		t.Fatalf("builtin allow count: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("builtin[%d]: want %q, got %q", i, want[i], got[i])
		}
	}
}
