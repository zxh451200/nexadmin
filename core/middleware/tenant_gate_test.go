package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type gateSource map[idgen.ID]*tenant.Info

func (g gateSource) TenantInfo(_ context.Context, id idgen.ID) (*tenant.Info, error) {
	return g[id], nil
}
func (g gateSource) TenantByDomain(context.Context, string) (idgen.ID, bool, error) {
	return 0, false, nil
}

// gateEngine 返回一个带“假登录”的引擎：X-Test-Claims 决定令牌载荷，再走 Tenant 中间件。
func gateEngine(claims func(*http.Request) *auth.Claims) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if cl := claims(c.Request); cl != nil {
			auth.SetClaims(c, cl)
		}
		c.Next()
	}, Tenant(DefaultTenantResolver(nil)))
	e.GET("/api/v1/ping", func(c *gin.Context) { c.String(200, "pong") })
	e.POST("/api/v1/auth/logout", func(c *gin.Context) { c.String(200, "bye") })
	return e
}

func doGate(e *gin.Engine, method, path string) (int, string) {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body.Reason
}

func TestTenantGateExpiredAndSuspended(t *testing.T) {
	now := time.Now()
	tenant.SetSource(gateSource{
		10: {ID: 10, Status: "active", ExpireAt: now.Add(time.Hour)},
		11: {ID: 11, Status: "active", ExpireAt: now.Add(-time.Hour)},
		12: {ID: 12, Status: "active", ExpireAt: now.Add(-time.Hour), GraceDays: 2},
		13: {ID: 13, Status: tenant.StatusSuspended, ExpireAt: now.Add(time.Hour)},
	})
	t.Cleanup(func() { tenant.SetSource(nil) })

	cur := idgen.ID(10)
	super, principal := false, ""
	e := gateEngine(func(*http.Request) *auth.Claims {
		return &auth.Claims{UserID: 1, TenantID: cur, Super: super, Principal: principal}
	})

	if code, _ := doGate(e, "GET", "/api/v1/ping"); code != 200 {
		t.Fatalf("active tenant: %d", code)
	}
	cur = 12
	if code, _ := doGate(e, "GET", "/api/v1/ping"); code != 200 {
		t.Fatalf("grace period must pass: %d", code)
	}
	for _, id := range []idgen.ID{11, 13} {
		cur = id
		if code, reason := doGate(e, "GET", "/api/v1/ping"); code != 403 || reason != "TENANT_EXPIRED" {
			t.Fatalf("tenant %d: got %d %q, want 403 TENANT_EXPIRED", id, code, reason)
		}
	}
	// PAT / 应用签名调用走同一道闸
	cur, principal = 11, auth.PrincipalPAT
	if code, reason := doGate(e, "GET", "/api/v1/ping"); code != 403 || reason != "TENANT_EXPIRED" {
		t.Fatalf("PAT of expired tenant: %d %q", code, reason)
	}
	principal = auth.PrincipalApp
	if code, reason := doGate(e, "GET", "/api/v1/ping"); code != 403 || reason != "TENANT_EXPIRED" {
		t.Fatalf("app call of expired tenant: %d %q", code, reason)
	}
	principal = ""
	// 登出放行，前端才能清理会话
	if code, _ := doGate(e, "POST", "/api/v1/auth/logout"); code != 200 {
		t.Fatalf("logout must pass for expired tenant: %d", code)
	}
	// 平台超管不受影响
	super = true
	if code, _ := doGate(e, "GET", "/api/v1/ping"); code != 200 {
		t.Fatalf("super admin must be exempt: %d", code)
	}
}

func TestTenantGateAnonymousAndDefaultTenant(t *testing.T) {
	tenant.SetSource(gateSource{10: {ID: 10, Status: tenant.StatusSuspended}})
	t.Cleanup(func() { tenant.SetSource(nil) })

	// 匿名请求（登录页等）不受闸门影响，登录时由 auth 层检查
	e := gateEngine(func(*http.Request) *auth.Claims { return nil })
	if code, _ := doGate(e, "GET", "/api/v1/ping"); code != 200 {
		t.Fatalf("anonymous: %d", code)
	}
	// 默认租户没有租户记录：放行
	e = gateEngine(func(*http.Request) *auth.Claims { return &auth.Claims{UserID: 1, TenantID: tenant.DefaultID} })
	if code, _ := doGate(e, "GET", "/api/v1/ping"); code != 200 {
		t.Fatalf("default tenant: %d", code)
	}
}
