package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/middleware"
)

type permEnv struct {
	e    *gin.Engine
	svc  *auth.Service
	prov *fakeProvider
}

// newPermEnv 按框架的装配方式挂载模块：api 带 ParseToken，受保护分组要求登录，Public 即 api。
func newPermEnv(t *testing.T) *permEnv {
	t.Helper()
	initTest(t)
	gin.SetMode(gin.TestMode)
	svc, prov, ch := newAuthService(t)
	prev := auth.Default()
	auth.SetDefault(svc)
	t.Cleanup(func() { auth.SetDefault(prev) })
	e := gin.New()
	api := e.Group("/api/v1")
	api.Use(middleware.ParseToken(svc))
	protected := api.Group("", middleware.RequireLogin())
	d := &core.Deps{Config: &config.Config{}, Cache: ch, Auth: svc, Log: zap.NewNop(), Public: api}
	(&module{}).Register(protected, d)
	return &permEnv{e: e, svc: svc, prov: prov}
}

func (p *permEnv) token(t *testing.T, name string, perms ...string) string {
	t.Helper()
	p.prov.add(1, name, false)
	p.prov.perms[name] = perms
	u, err := p.prov.Profile(context.Background(), p.prov.users[ukey(1, name)].id)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := p.svc.IssuePair(u)
	if err != nil {
		t.Fatal(err)
	}
	return pair.AccessToken
}

func (p *permEnv) do(method, path, token, body string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	p.e.ServeHTTP(w, req)
	return w.Code
}

func adminRoutes(e *gin.Engine) (admin []gin.RouteInfo, public []gin.RouteInfo) {
	for _, r := range e.Routes() {
		switch {
		case strings.HasPrefix(r.Path, "/api/v1/plugins/identity/cas/"):
			public = append(public, r)
		case strings.HasPrefix(r.Path, "/api/v1/plugins/identity/"):
			admin = append(admin, r)
		}
	}
	return
}

func TestEveryAdminRouteIsPermissionGated(t *testing.T) {
	p := newPermEnv(t)
	admin, public := adminRoutes(p.e)
	if len(admin) != 10 {
		t.Fatalf("expected 10 admin routes, got %d: %+v", len(admin), admin)
	}
	if len(public) != 6 {
		t.Fatalf("expected exactly 6 public CAS routes, got %d", len(public))
	}
	none := p.token(t, "nobody")
	onlyList := p.token(t, "viewer", "identity:list")
	editor := p.token(t, "editor", "identity:edit")
	syncer := p.token(t, "syncer", "identity:sync")
	all := p.token(t, "boss", "identity:*")
	for _, r := range admin {
		path := strings.NewReplacer(":id", "bad-id").Replace(r.Path)
		anon := p.do(r.Method, path, "", "{}")
		if anon != http.StatusUnauthorized {
			t.Errorf("%s %s: anonymous got %d, want 401", r.Method, r.Path, anon)
		}
		if c := p.do(r.Method, path, none, "{}"); c != http.StatusForbidden {
			t.Errorf("%s %s: user without permission got %d, want 403", r.Method, r.Path, c)
		}
		// 同步路由（读写同步状态）单列 identity:sync，list 权限不覆盖它。
		isSync := strings.HasSuffix(r.Path, "/sync")
		// 只有 list 权限的人只能走非同步的 GET；只有 edit 权限的人碰不了 list / test / sync
		viewer := p.do(r.Method, path, onlyList, "{}")
		ed, sy := p.do(r.Method, path, editor, "{}"), p.do(r.Method, path, syncer, "{}")
		listOnlyRoute := r.Method == http.MethodGet && !isSync
		if listOnlyRoute && viewer == http.StatusForbidden {
			t.Errorf("%s %s: list permission should be enough", r.Method, r.Path)
		}
		if !listOnlyRoute && viewer != http.StatusForbidden {
			t.Errorf("%s %s: list permission must not allow writes/tests/sync, got %d", r.Method, r.Path, viewer)
		}
		if strings.Contains(r.Path, "test-") && ed != http.StatusForbidden {
			t.Errorf("%s %s: edit permission must not allow tests, got %d", r.Method, r.Path, ed)
		}
		if r.Method == http.MethodGet && ed != http.StatusForbidden {
			t.Errorf("%s %s: edit permission must not allow reads, got %d", r.Method, r.Path, ed)
		}
		if isSync && ed != http.StatusForbidden {
			t.Errorf("%s %s: edit permission must not allow sync, got %d", r.Method, r.Path, ed)
		}
		// identity:sync 只够走同步路由，别的路由一律 403（含只读的 sources 列表）。
		if isSync != (sy != http.StatusForbidden) {
			t.Errorf("%s %s: sync permission handled wrong, got %d", r.Method, r.Path, sy)
		}
		// 权限通过后才进入业务（坏 ID / 坏 JSON 在触库之前就返回 400）
		if r.Path != "/api/v1/plugins/identity/sources" || r.Method == http.MethodPost {
			if c := p.do(r.Method, path, all, "{"); c != http.StatusBadRequest {
				t.Errorf("%s %s: authorized request got %d, want 400 (bad input)", r.Method, r.Path, c)
			}
		}
	}
	// 公开的 CAS 入口不要求登录（无源 / 无租户配置时返回业务错误，但绝不是 401/403）
	for _, r := range public {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			t.Errorf("public route %s %s has unexpected method", r.Method, r.Path)
		}
		if c := p.do(r.Method, r.Path, "", ""); c == http.StatusUnauthorized {
			t.Errorf("%s %s must not require login", r.Method, r.Path)
		}
	}
}
