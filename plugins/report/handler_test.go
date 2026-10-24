package report

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func routeCategory(path string) string {
	p := strings.TrimPrefix(path, "/api/v1/plugins/report")
	switch {
	case strings.HasPrefix(p, "/sources"), strings.HasPrefix(p, "/datasets"):
		return "report:dataset"
	case strings.HasPrefix(p, "/chart-options"), strings.HasPrefix(p, "/charts"):
		return "report:chart"
	case strings.HasPrefix(p, "/report-options"), strings.HasPrefix(p, "/reports"):
		return "report:report"
	case strings.HasPrefix(p, "/screen-options"), strings.HasPrefix(p, "/screens"):
		return "report:screen"
	case strings.HasPrefix(p, "/view/"), strings.HasPrefix(p, "/overview"):
		return "report:view"
	}
	return ""
}

func newTestEngine(t *testing.T, cl *auth.Claims) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc, _ := newTestService(t) // sqlmock 无预期：放行到业务层的请求会得到 4xx/5xx，但不会是 401/403
	h := newHandler(svc, nil)
	h.scopeFn = func(*gin.Context, *auth.Claims) (*datascope.Scope, error) { return &datascope.Scope{All: true}, nil }
	h.permFn = func(c *gin.Context, code string) bool { return auth.HasPerm(auth.ClaimsFrom(c).Perms, code) }
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if cl != nil {
			auth.SetClaims(c, cl)
			c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), idgen.ID(7)))
		}
		c.Next()
	})
	registerRoutes(e.Group("/api/v1"), h)
	return e
}

func hit(e *gin.Engine, method, path string) int {
	path = strings.NewReplacer(":chartId", "11", ":key", "k", ":id", "3").Replace(path)
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w.Code
}

func TestEveryRouteRequiresLoginAndItsOwnPermission(t *testing.T) {
	anon := newTestEngine(t, nil)
	routes := anon.Routes()
	if len(routes) < 30 {
		t.Fatalf("only %d routes registered", len(routes))
	}
	for _, rt := range routes {
		if routeCategory(rt.Path) == "" {
			t.Errorf("route %s %s has no known permission category", rt.Method, rt.Path)
			continue
		}
		if code := hit(anon, rt.Method, rt.Path); code != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s -> %d, want 401", rt.Method, rt.Path, code)
		}
	}
	// 只持有某一类权限的开放应用主体：本类路由放行（不是 401/403），其他类一律 403。
	for _, perm := range []string{"report:dataset", "report:chart", "report:report", "report:screen", "report:view"} {
		e := newTestEngine(t, &auth.Claims{UserID: 5, TenantID: 7, Principal: auth.PrincipalApp, Perms: []string{perm}})
		for _, rt := range routes {
			code := hit(e, rt.Method, rt.Path)
			if routeCategory(rt.Path) == perm {
				if code == http.StatusUnauthorized || code == http.StatusForbidden {
					t.Errorf("%s holder: %s %s -> %d, want pass-through", perm, rt.Method, rt.Path, code)
				}
			} else if code != http.StatusForbidden {
				t.Errorf("%s holder: %s %s -> %d, want 403", perm, rt.Method, rt.Path, code)
			}
		}
	}
	// 没有任何 report 权限 -> 全部 403。
	none := newTestEngine(t, &auth.Claims{UserID: 5, TenantID: 7, Principal: auth.PrincipalApp, Perms: []string{"user:list"}})
	for _, rt := range routes {
		if code := hit(none, rt.Method, rt.Path); code != http.StatusForbidden {
			t.Errorf("no report perms: %s %s -> %d, want 403", rt.Method, rt.Path, code)
		}
	}
}

func TestRawSQLIsNotAnInput(t *testing.T) {
	// 请求体里的 sql / query / raw 字段会被忽略（JSON 绑定只认结构化字段），且没有任何路由接受它们。
	e := newTestEngine(t, &auth.Claims{UserID: 5, TenantID: 7, Super: true})
	for _, body := range []string{
		`{"sql":"select * from sys_user"}`,
		`{"name":"x","source":"sys_user","columns":["password"],"sql":"select 1"}`,
		`{"name":"x","source":"plg_cron_job; drop table sys_user","columns":["name"]}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/report/datasets/preview", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %s -> %d %s, want 400", body, w.Code, w.Body.String())
		}
		if strings.Contains(strings.ToLower(w.Body.String()), "select") {
			t.Errorf("response echoes SQL: %s", w.Body.String())
		}
	}
}
