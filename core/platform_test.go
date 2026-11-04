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

// 平台闸的行为：只认超管或默认租户。两种形态各管一种暴露面——
// RequirePlatform 挡「改别人也在用的那一行」（共享配置表），RequirePlatformFeature 挡
// 「看见别人整批的客户数据」（表里根本没有 tenant_id 的插件后台，读也是泄露）。
// 把任何一个换成 c.Next() 直接放行，第三档就会红。
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
	for _, gate := range []struct {
		name string
		mw   func() gin.HandlerFunc
	}{{"共享配置写入口", RequirePlatform}, {"平台级插件后台", RequirePlatformFeature}} {
		for _, tc := range cases {
			t.Run(gate.name+"/"+tc.name, func(t *testing.T) {
				var reached bool
				r := gin.New()
				g := r.Group("/x", func(c *gin.Context) {
					ctx := tenant.With(c.Request.Context(), tc.tid)
					auth.SetClaims(c, &auth.Claims{UserID: idgen.ID(42), TenantID: tc.tid, Super: tc.super})
					c.Request = c.Request.WithContext(ctx)
				}, gate.mw())
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
}

// 两种闸给被挡者的话必须不是同一句：一句讲「这一行大家共用」，一句讲「这张表不分租户」。
func TestPlatformGatesHaveDistinctCopy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantCaller := func(mw gin.HandlerFunc) string {
		r := gin.New()
		r.POST("/z", func(c *gin.Context) {
			auth.SetClaims(c, &auth.Claims{UserID: idgen.ID(42), TenantID: idgen.ID(7)})
			c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), idgen.ID(7)))
		}, mw)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/z", nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("普通租户调用方应被挡，实际 %d", w.Code)
		}
		return w.Body.String()
	}
	shared := tenantCaller(RequirePlatform())
	feature := tenantCaller(RequirePlatformFeature())
	if shared == feature {
		t.Errorf("两种平台闸回了同一句话：%q（被挡的人分不清是「这行大家共用改不了」还是「这张表不分租户看不到」）", shared)
	}
}
