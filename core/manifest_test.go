package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

const baseManifest = `{"name":"NexAdmin","short_name":"NexAdmin","start_url":"/","icons":[` +
	`{"src":"/icon-192.png","sizes":"192x192","type":"image/png"},{"src":"/icon-512.png","sizes":"512x512","type":"image/png"},` +
	`{"src":"/icon.svg","sizes":"any","type":"image/svg+xml"}]}`

func manifestEngine(site func(c *gin.Context) (SiteInfo, bool)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	serve := staticHandlerWith(fstest.MapFS{
		"index.html":           {Data: []byte("<html/>")},
		"manifest.webmanifest": {Data: []byte(baseManifest)},
	}, site)
	e := gin.New()
	e.NoRoute(func(c *gin.Context) {
		if !serve(c) {
			c.String(404, "404")
		}
	})
	return e
}

func getManifest(t *testing.T, e *gin.Engine) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/manifest.webmanifest", nil))
	m := map[string]any{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("not json: %v %q", err, w.Body.String())
	}
	return w, m
}

func TestDynamicManifestUsesSiteNameAndBitmapLogo(t *testing.T) {
	e := manifestEngine(func(*gin.Context) (SiteInfo, bool) {
		return SiteInfo{Name: "星河后台管理系统控制台中心", LogoURL: "https://cdn.example.com/logo.PNG?v=2"}, true
	})
	w, m := getManifest(t, e)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/manifest+json" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("headers: %d %v", w.Code, w.Header())
	}
	if m["name"] != "星河后台管理系统控制台中心" || m["short_name"] != "星河后台管理系统控制台中" || m["start_url"] != "/" {
		t.Errorf("name/short_name: %v / %v", m["name"], m["short_name"])
	}
	icons := m["icons"].([]any)
	if len(icons) != 3 { // 两个静态 PNG（SVG 被滤掉）+ logo
		t.Fatalf("icons = %v", icons)
	}
	last := icons[2].(map[string]any)
	if last["src"] != "https://cdn.example.com/logo.PNG?v=2" || last["type"] != "image/png" {
		t.Errorf("logo icon = %v", last)
	}
}

func TestDynamicManifestIgnoresNonBitmapOrUnsafeLogo(t *testing.T) {
	for _, logo := range []string{"", "/logo.svg", "https://x/logo", "javascript:alert(1).png", "data:image/png;base64,AAAA", "//evil.com/a.png", "ftp://x/a.png"} {
		e := manifestEngine(func(*gin.Context) (SiteInfo, bool) { return SiteInfo{Name: "Acme", LogoURL: logo}, true })
		_, m := getManifest(t, e)
		if n := len(m["icons"].([]any)); n != 2 {
			t.Errorf("logo %q 不应成为图标项, icons=%d", logo, n)
		}
		if m["name"] != "Acme" {
			t.Errorf("name = %v", m["name"])
		}
	}
	e := manifestEngine(func(*gin.Context) (SiteInfo, bool) { return SiteInfo{Name: "A", LogoURL: "/uploads/l.webp"}, true })
	if _, m := getManifest(t, e); len(m["icons"].([]any)) != 3 {
		t.Error("站内位图路径应作为额外图标")
	}
}

func TestDynamicManifestFallsBackToStatic(t *testing.T) {
	for name, site := range map[string]func(*gin.Context) (SiteInfo, bool){
		"nil source":  nil,
		"unavailable": func(*gin.Context) (SiteInfo, bool) { return SiteInfo{}, false },
	} {
		w, m := getManifest(t, manifestEngine(site))
		if m["name"] != "NexAdmin" || len(m["icons"].([]any)) != 3 || w.Header().Get("Cache-Control") != "no-cache" ||
			w.Header().Get("Content-Type") != "application/manifest+json" {
			t.Errorf("%s: 应回落静态文件: %v %v", name, m, w.Header())
		}
	}
	// 站点名为空：保留静态名称
	_, m := getManifest(t, manifestEngine(func(*gin.Context) (SiteInfo, bool) { return SiteInfo{}, true }))
	if m["name"] != "NexAdmin" {
		t.Errorf("空站点名应保留静态值: %v", m["name"])
	}
}

func TestDynamicManifestOnlyForManifestPathAndGET(t *testing.T) {
	e := manifestEngine(func(*gin.Context) (SiteInfo, bool) { return SiteInfo{Name: "X"}, true })
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("POST", "/manifest.webmanifest", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("POST 不处理, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Body.String(), "<html/>") {
		t.Error("其它路径不受影响")
	}
}

func TestSetPublicSiteSource(t *testing.T) {
	SetPublicSiteSource(nil)
	if siteSource.Load() != nil {
		t.Fatal("nil should clear")
	}
	SetPublicSiteSource(func(_ context.Context) (SiteInfo, error) { return SiteInfo{}, nil })
	if siteSource.Load() == nil {
		t.Fatal("should set")
	}
	SetPublicSiteSource(nil)
}
