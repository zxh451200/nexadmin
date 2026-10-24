package identity

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
)

// newAuditEngine 返回只挂了几条管理路由的引擎；中间件注入租户 1 与登录用户 admin1（权限校验另有测试覆盖）。
func newAuditEngine(t *testing.T, h *handler) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 1))
		auth.SetClaims(c, &auth.Claims{UserID: 1, TenantID: 1, Username: "admin1"})
	})
	e.POST("/sources", h.create)
	e.PUT("/sources/:id/enabled", h.setEnabled)
	e.POST("/sources/:id/test-login", h.testLogin)
	e.DELETE("/sources/:id", h.remove)
	return e
}

func doJSON(e *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
