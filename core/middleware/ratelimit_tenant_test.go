package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func tenantHit(e *gin.Engine, tid string) int {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if tid != "" {
		req.Header.Set("X-Tenant", tid)
	}
	req.RemoteAddr = "1.1.1.1:1"
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w.Code
}

// TestRateLimitByTenant 验证按租户分桶：同一 IP 下，A 租户超限不影响 B 租户。
func TestRateLimitByTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := memCache(t)
	e := gin.New()
	e.Use(func(gc *gin.Context) {
		id, err := idgen.Parse(gc.GetHeader("X-Tenant"))
		if err == nil {
			gc.Request = gc.Request.WithContext(tenant.With(gc.Request.Context(), id))
		}
		gc.Next()
	})
	e.GET("/x", RateLimit(c, zap.NewNop(), "tenant-api", ByTenant(), 2, time.Minute), func(gc *gin.Context) { gc.String(200, "ok") })

	for i := 0; i < 2; i++ {
		if code := tenantHit(e, "1001"); code != 200 {
			t.Fatalf("tenant 1001 request %d: got %d", i+1, code)
		}
	}
	if code := tenantHit(e, "1001"); code != http.StatusTooManyRequests {
		t.Fatalf("tenant 1001 third request: want 429, got %d", code)
	}
	if code := tenantHit(e, "1002"); code != 200 {
		t.Fatalf("other tenant must have its own bucket, got %d", code)
	}
	// 没有租户上下文时必须放行（ByTenant 返回空 key，RateLimit 对空 key 直通）。
	if code := tenantHit(e, ""); code != 200 {
		t.Fatalf("request without tenant: got %d", code)
	}
}
