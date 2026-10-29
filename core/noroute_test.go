package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIsAdminPath(t *testing.T) {
	cases := map[string]bool{
		"/admin":           true,
		"/admin/":          true,
		"/admin/login":     true,
		"/administrator":   false,
		"/":                false,
		"/login":           false,
		"/adminx/whatever": false,
	}
	for p, want := range cases {
		if got := IsAdminPath(p); got != want {
			t.Errorf("IsAdminPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestLegacyAdminRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		path, want, query string
	}{
		{"/login", "/admin/login", ""},
		{"/login/oauth/github", "/admin/login/oauth/github", ""},
		{"/setup", "/admin/setup", ""},
		{"/sso/authorize", "/admin/sso/authorize", "client_id=abc"},
		{"/mcp/confirmations/42", "/admin/mcp/confirmations/42", ""},
		{"/users", "", ""},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		url := tc.path
		if tc.query != "" {
			url += "?" + tc.query
		}
		c.Request = httptest.NewRequest(http.MethodGet, url, nil)
		handled := redirectLegacyAdmin(c)
		if tc.want == "" {
			if handled {
				t.Errorf("%s 不应被当作后台旧入口", tc.path)
			}
			continue
		}
		if !handled || w.Code != http.StatusMovedPermanently {
			t.Fatalf("%s: handled=%v code=%d", tc.path, handled, w.Code)
		}
		want := tc.want
		if tc.query != "" {
			want += "?" + tc.query
		}
		if got := w.Header().Get("Location"); got != want {
			t.Errorf("%s Location = %q, want %q", tc.path, got, want)
		}
	}

	// 非 GET 不改写，交给 NoRoute 的 JSON 404。
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/login", nil)
	if redirectLegacyAdmin(c) {
		t.Error("POST /login 不应跳转")
	}
}

func TestSetSiteNotFoundHandler(t *testing.T) {
	SetSiteNotFoundHandler(nil)
	if siteNotFound.Load() != nil {
		t.Fatal("nil should clear")
	}
	SetSiteNotFoundHandler(func(*gin.Context) bool { return true })
	if siteNotFound.Load() == nil {
		t.Fatal("handler should be stored")
	}
	SetSiteNotFoundHandler(nil)
}
