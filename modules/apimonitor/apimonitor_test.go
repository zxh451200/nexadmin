package apimonitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
)

func newEngine(cl *auth.Claims) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/api/v1")
	if cl != nil {
		g.Use(func(c *gin.Context) { auth.SetClaims(c, cl) })
	}
	module{}.Register(g, &core.Deps{Public: e.Group("/api/v1/public")})
	return e
}

func call(e *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestRequiresPermission(t *testing.T) {
	if w := call(newEngine(nil), "/api/v1/monitor/api"); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录应 401，得到 %d", w.Code)
	}
	if w := call(newEngine(&auth.Claims{Principal: auth.PrincipalApp}), "/api/v1/monitor/api"); w.Code != http.StatusForbidden {
		t.Errorf("无 apiMonitor:list 应 403，得到 %d", w.Code)
	}
	if w := call(newEngine(&auth.Claims{Principal: auth.PrincipalApp, Perms: []string{PermList}}), "/api/v1/monitor/api"); w.Code != http.StatusOK {
		t.Errorf("有权限应 200，得到 %d", w.Code)
	}
}

func TestSnapshotShapeAndSelfCounting(t *testing.T) {
	e := newEngine(&auth.Claims{Super: true})
	first := call(e, "/api/v1/monitor/api")
	if first.Code != http.StatusOK {
		t.Fatalf("code=%d", first.Code)
	}
	// 没有任何数据时 apis 也必须是 []，前端直接 map 不能遇到 null
	if strings.Contains(first.Body.String(), `"apis":null`) {
		t.Fatalf("apis 不能为 null: %s", first.Body.String())
	}
	second := call(e, "/api/v1/monitor/api")
	var body struct {
		Data snapshot `json:"data"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.TS == 0 {
		t.Error("应带时间戳")
	}
	found := false
	for _, a := range body.Data.APIs {
		if a.Method == http.MethodGet && a.Path == "/monitor/api" { // 路由模板，已去掉 /api/v1 前缀
			found = true
		}
	}
	if !found {
		t.Errorf("安装的统计中间件应记录本接口自身: %+v", body.Data.APIs)
	}
}
