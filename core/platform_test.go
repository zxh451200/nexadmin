package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 平台共享表（表里没有 tenant_id，一行服务所有租户）的写入口只认平台级调用方：
// 权限点只说明「能用这个模块」，说明不了「能改别人也在用的那一行」。
// 把 RequirePlatform 换成 c.Next() 直接放行，第三档就会红。
func TestRequirePlatformAdmitsOnlyPlatformCallers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name   string
		super  bool
		tid    idgen.ID
		wantOK bool
	}{
		{"超管在租户上下文里", true, 7, true},
		{"默认租户（平台本身）", false, tenant.DefaultID, true},
		{"普通租户管理员", false, 7, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			r := gin.New()
			g := r.Group("/x", func(c *gin.Context) {
				ctx := tenant.With(c.Request.Context(), tc.tid)
				auth.SetClaims(c, &auth.Claims{UserID: idgen.ID(42), TenantID: tc.tid, Super: tc.super})
				c.Request = c.Request.WithContext(ctx)
			}, RequirePlatform())
			g.POST("", func(c *gin.Context) { reached = true; c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))
			want := http.StatusForbidden
			if tc.wantOK {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("状态码 = %d, want %d（ok=%v）", w.Code, want, tc.wantOK)
			}
			if reached != tc.wantOK {
				t.Errorf("业务处理函数被调到 = %v, want %v", reached, tc.wantOK)
			}
		})
	}
}
