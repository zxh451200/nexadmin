package auth

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestContextWithUser(t *testing.T) {
	ctx := ContextWithUser(context.Background(), 7, "job:export")
	id, name, ok := UserFromContext(ctx)
	if !ok || id != 7 || name != "job:export" {
		t.Fatalf("UserFromContext = %d %q %v", id, name, ok)
	}
	// 绝不能被当作登录态：ClaimsFromContext 仍是 nil，权限/数据范围判断读不到它。
	if cl := ClaimsFromContext(ctx); cl != nil {
		t.Fatalf("ClaimsFromContext 应为 nil：%+v", cl)
	}
	if _, _, ok := UserFromContext(context.Background()); ok {
		t.Fatal("空 context 不应有归属用户")
	}
	if _, _, ok := UserFromContext(ContextWithUser(context.Background(), 0, "")); ok {
		t.Fatal("id 与名称都为空视为没有")
	}
}

func TestUserFromContextIndependentOfClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	SetClaims(c, &Claims{UserID: 5, Username: "alice"})
	ctx := ContextWithUser(c.Request.Context(), 9, "job:x")
	if cl := ClaimsFromContext(ctx); cl == nil || cl.UserID != 5 {
		t.Fatalf("登录态应保持不变：%+v", cl)
	}
	if id, _, ok := UserFromContext(ctx); !ok || id != 9 {
		t.Fatalf("归属用户应独立存在：%d %v", id, ok)
	}
}
