package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func staticEngine(t *testing.T, withFS bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var serve func(*gin.Context) bool
	if withFS {
		serve = staticHandler(fstest.MapFS{
			"index.html":           {Data: []byte("<html>spa</html>")},
			"assets/app-1a.js":     {Data: []byte("console.log(1)")},
			"favicon.svg":          {Data: []byte("<svg/>")},
			"sw.js":                {Data: []byte("self.skipWaiting()")},
			"manifest.webmanifest": {Data: []byte("{}")},
			"offline.html":         {Data: []byte("<html>offline</html>")},
		})
	} else {
		serve = staticHandler(nil)
	}
	e := gin.New()
	e.GET("/healthz", func(c *gin.Context) { c.String(200, "ok") })
	e.NoRoute(func(c *gin.Context) {
		if serve(c) {
			return
		}
		c.String(404, "json404")
	})
	return e
}

func doReq(e *gin.Engine, method, p string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(method, p, nil))
	return w
}

func TestStaticHandler(t *testing.T) {
	e := staticEngine(t, true)
	cases := []struct {
		method, path string
		code         int
		body, cache  string
	}{
		{"GET", "/", 200, "<html>spa</html>", "no-cache"},
		{"GET", "/index.html", 200, "<html>spa</html>", "no-cache"},
		{"GET", "/sso/authorize", 200, "<html>spa</html>", "no-cache"}, // 前端路由
		{"GET", "/users/1/edit", 200, "<html>spa</html>", "no-cache"},
		{"GET", "/assets/app-1a.js", 200, "console.log(1)", cacheImmutable},
		{"GET", "/favicon.svg", 200, "<svg/>", cacheShort},
		{"GET", "/sw.js", 200, "self.skipWaiting()", "no-cache"}, // 长缓存会导致 SW 永远无法更新
		{"GET", "/manifest.webmanifest", 200, "{}", "no-cache"},
		{"GET", "/offline.html", 200, "<html>offline</html>", "no-cache"},
		{"GET", "/assets/missing.js", 404, "json404", ""},
		{"GET", "/api/v1/nope", 404, "json404", ""},
		{"GET", "/api", 404, "json404", ""},
		{"GET", "/healthz", 200, "ok", ""},
		{"GET", "/healthz/x", 404, "json404", ""},
		{"POST", "/login", 404, "json404", ""},
		{"GET", "/../etc/passwd", 200, "<html>spa</html>", "no-cache"}, // 清理后仍落在 FS 内
	}
	for _, tc := range cases {
		w := doReq(e, tc.method, tc.path)
		if w.Code != tc.code || w.Body.String() != tc.body {
			t.Errorf("%s %s = %d %q，期望 %d %q", tc.method, tc.path, w.Code, w.Body.String(), tc.code, tc.body)
		}
		if tc.cache != "" && w.Header().Get("Cache-Control") != tc.cache {
			t.Errorf("%s %s Cache-Control = %q，期望 %q", tc.method, tc.path, w.Header().Get("Cache-Control"), tc.cache)
		}
	}
	if w := doReq(e, http.MethodHead, "/login"); w.Code != 200 {
		t.Errorf("HEAD /login = %d", w.Code)
	}
}

func TestStaticPWAHeaders(t *testing.T) {
	e := staticEngine(t, true)
	if ct := doReq(e, "GET", "/manifest.webmanifest").Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("manifest Content-Type = %q", ct)
	}
	if ct := doReq(e, "GET", "/sw.js").Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("sw.js Content-Type = %q", ct)
	}
	// 其他根目录文件仍是短缓存，不应被误伤
	if cc := doReq(e, "GET", "/favicon.svg").Header().Get("Cache-Control"); cc != cacheShort {
		t.Errorf("favicon Cache-Control = %q", cc)
	}
	// SW 不得通过 SPA 回退被伪造成 index.html（缺失时应 404，而非返回 HTML）
	serve := staticHandler(fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}})
	e2 := gin.New()
	e2.NoRoute(func(c *gin.Context) {
		if !serve(c) {
			c.String(404, "json404")
		}
	})
	if w := doReq(e2, "GET", "/sw.js"); w.Code != 404 || w.Body.String() != "json404" {
		t.Errorf("缺失的 sw.js 应 404，得到 %d %q", w.Code, w.Body.String())
	}
}

func TestStaticHandlerDisabled(t *testing.T) {
	e := staticEngine(t, false)
	if w := doReq(e, "GET", "/"); w.Code != 404 || w.Body.String() != "json404" {
		t.Errorf("无 FS 时应保持 404，得到 %d %q", w.Code, w.Body.String())
	}
}
