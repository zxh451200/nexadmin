package user

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/config"
)

func httpEnv(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := newEnv(t)
	e.d.Config.Auth.Providers = map[string]any{
		"github": map[string]any{"client_id": "cid", "client_secret": "sec", "redirect_uris": []any{"https://admin.example.com/login/oauth/github"}},
		"google": map[string]any{"client_id": "cid"}, // 不完整：不应出现
	}
	r := gin.New()
	api := r.Group("/api/v1")
	e.d.Public = api
	(&module{}).registerOAuth(api, e.d) // 路由冲突会在这里 panic
	return r
}

func do(r http.Handler, method, path, body string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m
}

func TestProvidersOnlyListsConfigured(t *testing.T) {
	r := httpEnv(t)
	code, body := do(r, "GET", "/api/v1/auth/providers", "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	list, _ := body["data"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["key"] != "github" {
		t.Fatalf("providers = %v", body)
	}
}

func TestStartValidatesProviderAndRedirect(t *testing.T) {
	r := httpEnv(t)
	cb := url.QueryEscape("https://admin.example.com/login/oauth/github")
	if code, _ := do(r, "GET", "/api/v1/auth/oauth/google/start?redirect="+cb, ""); code != 404 {
		t.Errorf("incomplete provider: %d", code)
	}
	if code, _ := do(r, "GET", "/api/v1/auth/oauth/nope/start?redirect="+cb, ""); code != 404 {
		t.Errorf("unknown provider: %d", code)
	}
	for _, bad := range []string{"", url.QueryEscape("https://evil.example.com/cb"), url.QueryEscape("https://admin.example.com/login/oauth/github/x")} {
		if code, _ := do(r, "GET", "/api/v1/auth/oauth/github/start?redirect="+bad, ""); code != 400 {
			t.Errorf("redirect %q: %d", bad, code)
		}
	}
	code, body := do(r, "GET", "/api/v1/auth/oauth/github/start?redirect="+cb, "")
	if code != 200 {
		t.Fatalf("status %d: %v", code, body)
	}
	raw := body["data"].(map[string]any)["authUrl"].(string)
	u, _ := url.Parse(raw)
	if u.Host != "github.com" || u.Query().Get("state") == "" || u.Query().Get("client_id") != "cid" ||
		u.Query().Get("redirect_uri") != "https://admin.example.com/login/oauth/github" {
		t.Fatalf("authUrl = %s", raw)
	}
}

// 回调失败一律同一条提示：不论是 state 不存在、重放，还是 provider 不存在。
func TestCallbackFailuresAreUniform(t *testing.T) {
	r := httpEnv(t)
	cb := url.QueryEscape("https://admin.example.com/login/oauth/github")
	_, started := do(r, "GET", "/api/v1/auth/oauth/github/start?redirect="+cb, "")
	u, _ := url.Parse(started["data"].(map[string]any)["authUrl"].(string))
	state := u.Query().Get("state")

	var msgs []string
	for _, c := range []struct{ path, body string }{
		{"/api/v1/auth/oauth/github/callback", `{"code":"c","state":"does-not-exist"}`},
		{"/api/v1/auth/oauth/nope/callback", `{"code":"c","state":"does-not-exist"}`},
		{"/api/v1/auth/oauth/google/callback", `{"code":"c","state":"` + state + `"}`}, // state 属于 github
	} {
		code, body := do(r, "POST", c.path, c.body)
		if code != 400 {
			t.Fatalf("%s: status %d", c.path, code)
		}
		msgs = append(msgs, body["message"].(string))
	}
	for _, m := range msgs {
		if m != msgs[0] {
			t.Fatalf("failure messages must be identical, got %v", msgs)
		}
	}
	// state 被上面第三条请求消费（取出即作废）：同一个 state 再用于正确的 provider 也不行
	code, body := do(r, "POST", "/api/v1/auth/oauth/github/callback", `{"code":"c","state":"`+state+`"}`)
	if code != 400 || body["message"] != msgs[0] {
		t.Fatalf("replayed state: %d %v", code, body)
	}
}

func TestBindWithInvalidTokenFails(t *testing.T) {
	r := httpEnv(t)
	code, _ := do(r, "POST", "/api/v1/auth/oauth/bind", `{"bindToken":"nope","username":"alice","password":"x"}`)
	if code != 400 {
		t.Fatalf("status %d", code)
	}
}

var _ = config.Config{}
