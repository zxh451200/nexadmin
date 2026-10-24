package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	coreai "nexadmin/core/ai"
	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/i18n"
	"nexadmin/core/middleware"
	"nexadmin/pkg/idgen"
)

const key = "sk-module-SECRET-987"

// permProvider 是测试用的 auth.Provider：按用户 ID 返回固定权限。
type permProvider struct{ perms map[idgen.ID][]string }

func (permProvider) Login(context.Context, auth.LoginRequest) (*auth.User, error) { return nil, nil }
func (p permProvider) Profile(_ context.Context, id idgen.ID) (*auth.User, error) {
	return &auth.User{ID: id, Perms: p.perms[id]}, nil
}

// installAuth 把测试 Provider 装到 auth.Default()，测试结束还原。
func installAuth(t *testing.T, perms map[idgen.ID][]string) {
	t.Helper()
	svc, err := auth.NewService(&config.Config{Auth: config.Auth{JWTSecret: strings.Repeat("s", 32)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetProvider(permProvider{perms})
	prev := auth.Default()
	auth.SetDefault(svc)
	t.Cleanup(func() { auth.SetDefault(prev) })
}

func engine(t *testing.T, cfg coreai.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) { auth.SetClaims(c, &auth.Claims{UserID: 5, TenantID: 1, Super: true}) })
	gw := coreai.NewGateway(func(context.Context) (coreai.Config, error) { return cfg, nil }, nil, time.UTC)
	h := &handler{gw: gw}
	g := e.Group("/ai")
	g.GET("/status", h.status)
	g.POST("/filter", h.filter)
	g.POST("/translate", h.translate)
	return e
}

func do(e *gin.Engine, method, path, body string) (int, map[string]any, string) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w, r)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m, w.Body.String()
}

func TestDisabledReturnsReason(t *testing.T) {
	e := engine(t, coreai.Config{Enabled: false, BaseURL: "https://x.example.com/v1", APIKey: key, Model: "m"})
	code, m, raw := do(e, "GET", "/ai/status", "")
	if code != 200 || m["data"].(map[string]any)["enabled"] != false {
		t.Fatalf("status: %d %s", code, raw)
	}
	for _, p := range []struct{ path, body string }{
		{"/ai/filter", `{"scope":"user","text":"x"}`},
		{"/ai/translate", `{"text":"a","from":"en","to":"zh","format":"text"}`},
	} {
		code, m, raw := do(e, "POST", p.path, p.body)
		if code != 503 || m["reason"] != "AI_DISABLED" || m["code"] != float64(503) {
			t.Fatalf("%s: %d %s", p.path, code, raw)
		}
		if strings.Contains(raw, key) {
			t.Fatal("key leaked")
		}
	}
}

func TestStatusNeverReturnsKey(t *testing.T) {
	up := httptest.NewServer(http.NotFoundHandler())
	defer up.Close()
	e := engine(t, coreai.Config{Enabled: true, AllowPrivate: true, BaseURL: up.URL, APIKey: key, Model: "qwen-plus"})
	code, m, raw := do(e, "GET", "/ai/status", "")
	d, _ := m["data"].(map[string]any)
	if code != 200 || d["enabled"] != true || d["model"] != "qwen-plus" || len(d) != 2 || strings.Contains(raw, key) || strings.Contains(raw, up.URL) {
		t.Fatalf("status: %d %s", code, raw)
	}
}

func TestTranslateAndUpstreamFailure(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "fail") {
			http.Error(w, "upstream blew up: "+key, 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"items":["你好 ⟦0⟧"]}`}, "finish_reason": "stop"}}})
	}))
	defer up.Close()
	ok := engine(t, coreai.Config{Enabled: true, AllowPrivate: true, BaseURL: up.URL, APIKey: key, Model: "m"})
	code, m, raw := do(ok, "POST", "/ai/translate", `{"text":"Hello {name}","from":"en","to":"zh","format":"text"}`)
	if code != 200 || m["data"].(map[string]any)["text"] != "你好 {name}" {
		t.Fatalf("translate: %d %s", code, raw)
	}
	bad := engine(t, coreai.Config{Enabled: true, AllowPrivate: true, BaseURL: up.URL + "/fail", APIKey: key, Model: "m"})
	code, m, raw = do(bad, "POST", "/ai/filter", `{"scope":"user","text":"x"}`)
	if code != 502 || m["reason"] != nil || strings.Contains(raw, key) || strings.Contains(raw, "blew up") {
		t.Fatalf("upstream failure: %d %s", code, raw)
	}
	if code, _, _ := do(ok, "POST", "/ai/translate", `{"text":"a","from":"en","to":"zh","format":"pdf"}`); code != 400 {
		t.Fatalf("bad format code = %d", code)
	}
}

func tenantEngine(t *testing.T, cfg coreai.Config, claims auth.Claims, p *coreai.TenantPolicy) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	if b := i18n.Default(); b != nil {
		e.Use(middleware.I18n(b))
	}
	e.Use(func(c *gin.Context) { auth.SetClaims(c, &claims) })
	gw := coreai.NewGateway(func(context.Context) (coreai.Config, error) { return cfg, nil }, nil, time.UTC)
	gw.Policy = func(context.Context, coreai.Caller) (*coreai.TenantPolicy, error) { return p, nil }
	gw.Quota = coreai.NewMemoryCounter()
	h := &handler{gw: gw}
	g := e.Group("/ai")
	g.GET("/status", h.status)
	g.POST("/translate", h.translate)
	g.POST("/write", h.write)
	return e
}

func okUpstream(t *testing.T, content string) coreai.Config {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}, "finish_reason": "stop"}}})
	}))
	t.Cleanup(up.Close)
	return coreai.Config{Enabled: true, AllowPrivate: true, BaseURL: up.URL, APIKey: key, Model: "m"}
}

func TestWriteEndpoint(t *testing.T) {
	cfg := okUpstream(t, `{"text":"各位同事：明晚维护。"}`)
	e := tenantEngine(t, cfg, auth.Claims{UserID: 5, TenantID: 1, Super: true}, nil)
	code, m, raw := do(e, "POST", "/ai/write", `{"mode":"write","text":"明晚维护","tone":"formal","format":"text"}`)
	if code != 200 || m["data"].(map[string]any)["text"] != "各位同事：明晚维护。" {
		t.Fatalf("write: %d %s", code, raw)
	}
	for _, body := range []string{`{"mode":"x","text":"a"}`, `{"mode":"write","text":""}`, `{"mode":"write","text":"a","tone":"rude"}`} {
		if code, _, raw := do(e, "POST", "/ai/write", body); code != 400 {
			t.Fatalf("%s: %d %s", body, code, raw)
		}
	}
}

func TestTenantSwitchAndQuotaErrorCodes(t *testing.T) {
	b, err := i18n.New([]config.Language{{Code: "zh-CN", Default: true}, {Code: "en-US"}}, "zh-CN", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := i18n.Default()
	i18n.SetDefault(b)
	t.Cleanup(func() { i18n.SetDefault(prev) })
	installAuth(t, map[idgen.ID][]string{5: {"notice:update"}, 1: {"notice:create"}})
	cfg := okUpstream(t, `{"items":["你好"]}`)
	tr := `{"text":"hello","from":"en","to":"zh","format":"text"}`
	tenantUser := auth.Claims{UserID: 5, TenantID: 9}

	off := tenantEngine(t, cfg, tenantUser, &coreai.TenantPolicy{Disabled: true})
	code, m, raw := do(off, "POST", "/ai/translate", tr)
	if code != 403 || m["reason"] != "TENANT_AI_DISABLED" || m["message"] != "当前租户已被平台关闭 AI 功能，请联系平台管理员" || strings.Contains(raw, key) {
		t.Fatalf("disabled: %d %s", code, raw)
	}
	// 被关闭的租户 /ai/status 返回未启用，前端隐藏入口
	if code, m, raw := do(off, "GET", "/ai/status", ""); code != 200 || m["data"].(map[string]any)["enabled"] != false {
		t.Fatalf("status: %d %s", code, raw)
	}

	lim := tenantEngine(t, cfg, tenantUser, &coreai.TenantPolicy{DailyQuota: 2})
	for i := 0; i < 2; i++ {
		if code, _, raw := do(lim, "POST", "/ai/translate", tr); code != 200 {
			t.Fatalf("call %d: %d %s", i+1, code, raw)
		}
	}
	code, m, raw = do(lim, "POST", "/ai/translate", tr)
	if code != 429 || m["reason"] != "TENANT_QUOTA_AI" || m["message"] != "今日 AI 调用次数已达租户上限（2 次），请明天再试或联系平台管理员" {
		t.Fatalf("over quota: %d %s", code, raw)
	}
	if code, m, _ := do(lim, "POST", "/ai/write", `{"mode":"write","text":"x"}`); code != 429 || m["reason"] != "TENANT_QUOTA_AI" {
		t.Fatalf("write shares the quota: %d", code)
	}

	// 超管 / 平台租户不受限
	for _, cl := range []auth.Claims{{UserID: 1, TenantID: 9, Super: true}, {UserID: 1, TenantID: 1}} {
		e := tenantEngine(t, cfg, cl, &coreai.TenantPolicy{Disabled: true, DailyQuota: 1})
		for i := 0; i < 3; i++ {
			if code, _, raw := do(e, "POST", "/ai/translate", tr); code != 200 {
				t.Fatalf("%+v call %d: %d %s", cl, i+1, code, raw)
			}
		}
	}
}

func TestWriteAndTranslateRequireBusinessPermission(t *testing.T) {
	installAuth(t, map[idgen.ID][]string{
		10: nil,                       // 只有登录，没有任何业务权限
		11: {"notice:update"},         // 公告编辑
		12: {"dict:create"},           // 字典新增：能翻译，不能写公告
		13: {"user:list", "notice:*"}, // 通配符
	})
	cfg := okUpstream(t, `{"text":"ok"}`)
	write := `{"mode":"write","text":"x"}`
	trans := `{"text":"hello","from":"en","to":"zh","format":"text"}`
	cases := []struct {
		uid         int64
		write, tran int
	}{{10, 403, 403}, {11, 200, 502}, {12, 403, 502}, {13, 200, 502}}
	for _, tc := range cases {
		e := tenantEngine(t, cfg, auth.Claims{UserID: idgen.ID(tc.uid), TenantID: 9}, nil)
		if code, _, raw := do(e, "POST", "/ai/write", write); code != tc.write {
			t.Errorf("user %d write = %d %s, want %d", tc.uid, code, raw, tc.write)
		}
		// translate 的上游返回的是 write 格式的 JSON，翻译解析失败 => 502；这里只关心有没有被权限层拦成 403。
		if code, _, raw := do(e, "POST", "/ai/translate", trans); code != tc.tran {
			t.Errorf("user %d translate = %d %s, want %d", tc.uid, code, raw, tc.tran)
		}
	}
	// 没有权限时不得消耗配额：被拒的请求不应触达网关。
	quota := tenantEngine(t, cfg, auth.Claims{UserID: 10, TenantID: 9}, &coreai.TenantPolicy{DailyQuota: 1})
	for i := 0; i < 3; i++ {
		if code, _, _ := do(quota, "POST", "/ai/write", write); code != 403 {
			t.Fatalf("denied call %d = %d", i, code)
		}
	}
	ok := tenantEngine(t, cfg, auth.Claims{UserID: 11, TenantID: 9}, &coreai.TenantPolicy{DailyQuota: 1})
	if code, _, raw := do(ok, "POST", "/ai/write", write); code != 200 {
		t.Fatalf("first allowed call after denied ones: %d %s", code, raw)
	}
}
