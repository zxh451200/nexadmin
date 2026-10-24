package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
)

type hotModule struct{ started chan struct{} }

func (hotModule) Name() string { return "hot" }
func (hotModule) Register(r *gin.RouterGroup, _ *core.Deps) {
	r.GET("/plugins/cron/jobs/:id", func(c *gin.Context) {
		u, _ := c.Get("user")
		c.String(http.StatusOK, "job=%s user=%v", c.Param("id"), u)
	})
}
func (h hotModule) Start(context.Context, *core.Deps) error { close(h.started); return nil }

// 热挂载的转发：主引擎中间件设置的 Keys 要能传到子引擎，路径参数可用，未知路径 404，
// 且与同级的静态授权路由、/plugins 列表路由共存不冲突。
func TestHotMountDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hm := hotModule{started: make(chan struct{})}
	m := newManager(nil, Options{})
	e := &entry{p: Plugin{ID: "cron", Edition: Pro, Module: hm}}
	m.entries["cron"] = e
	d := &core.Deps{Log: zap.NewNop()}
	if err := m.buildSub(e, d); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hm.started:
	default:
		t.Fatal("Starter was not called")
	}

	root := gin.New()
	api := root.Group("/api/v1", func(c *gin.Context) { c.Set("user", "alice"); c.Next() })
	api.GET("/plugins", func(c *gin.Context) { c.String(200, "list") })
	api.POST("/plugins/other/license", func(c *gin.Context) { c.String(200, "other-license") })
	api.Any("/plugins/cron/*rest", func(c *gin.Context) { m.Dispatch(c, "cron") })

	do := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		root.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	if w := do("GET", "/api/v1/plugins/cron/jobs/42"); w.Code != 200 || w.Body.String() != "job=42 user=alice" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	if w := do("GET", "/api/v1/plugins/cron/nope"); w.Code != 404 {
		t.Fatalf("want 404, got %d", w.Code)
	}
	if w := do("GET", "/api/v1/plugins"); w.Body.String() != "list" {
		t.Fatalf("got %q", w.Body.String())
	}
	if w := do("POST", "/api/v1/plugins/other/license"); w.Body.String() != "other-license" {
		t.Fatalf("got %q", w.Body.String())
	}
	// 没有挂载子引擎的插件：404，不 panic。
	m.entries["x"] = &entry{p: Plugin{ID: "x"}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	m.Dispatch(c, "x")
	if w.Code != 404 {
		t.Fatalf("want 404, got %d", w.Code)
	}
}
