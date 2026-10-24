package audit

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
)

// 操作人优先级：显式 WithOperator > core 放进 request context 的登录用户 > 系统。
func TestOperatorFromFallsBackToClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	auth.SetClaims(c, &auth.Claims{UserID: 7, Username: "bob"})

	if id, name := operatorFrom(c.Request.Context()); id != 7 || name != "bob" {
		t.Fatalf("应取到登录用户，得到 %d %q", id, name)
	}
	explicit := WithOperator(c.Request.Context(), 9, "job")
	if id, name := operatorFrom(explicit); id != 9 || name != "job" {
		t.Fatalf("显式操作人应优先，得到 %d %q", id, name)
	}
	if id, name := operatorFrom(context.Background()); id != 0 || name != SystemOperator {
		t.Fatalf("没有用户应记为系统，得到 %d %q", id, name)
	}
}

// 后台任务没有登录态：ContextWithUser 的归属用户作为最后的回落，且排在 Claims 与显式 WithOperator 之后。
func TestOperatorFromFallsBackToUserFromContext(t *testing.T) {
	jobCtx := auth.ContextWithUser(context.Background(), 11, "job:export")
	if id, name := operatorFrom(jobCtx); id != 11 || name != "job:export" {
		t.Fatalf("应回落到归属用户，得到 %d %q", id, name)
	}
	if id, name := operatorFrom(WithOperator(jobCtx, 9, "explicit")); id != 9 || name != "explicit" {
		t.Fatalf("显式操作人应优先，得到 %d %q", id, name)
	}
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	auth.SetClaims(c, &auth.Claims{UserID: 7, Username: "bob"})
	both := auth.ContextWithUser(c.Request.Context(), 11, "job:export")
	if id, name := operatorFrom(both); id != 7 || name != "bob" {
		t.Fatalf("Claims 应优先于归属用户，得到 %d %q", id, name)
	}
}
