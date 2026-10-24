package openapi

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/event"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "openapi" }

var startOnce sync.Once

// Register 注册路由。
//   - 个人访问令牌：登录用户自助，不挂权限码（令牌本身不能调用这些接口，见 core/openapi）；
//   - 开放应用 / 事件目录 / Webhook / 投递记录：openapi:list（读）、openapi:edit（写）。
//
// 同时启动 Webhook 投递 worker 并订阅事件总线（进程内只启动一次）。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{db: d.DB, log: d.Log, snd: newSender()}
	st := gormStore{db: d.DB}
	h.wk = newWorker(st, h.snd, d.Log)

	startOnce.Do(func() {
		event.Default().SetLogger(d.Log)
		event.Subscribe(&dispatcher{st: st, log: d.Log, wake: h.wk.wake, now: time.Now})
		go h.wk.run(context.Background())
	})

	patCreate := middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "pat_create", middleware.ByUser())
	r.GET("/profile/tokens", h.tokenList)
	r.POST("/profile/tokens", patCreate, h.tokenCreate)
	r.DELETE("/profile/tokens/:id", h.tokenDelete)

	registerDocs(r, d)

	r.GET("/openapi/events", core.Perm("openapi:list"), func(c *gin.Context) { response.OK(c, event.Catalog()) })

	r.GET("/openapi/apps", core.Perm("openapi:list"), h.appList)
	r.POST("/openapi/apps", core.Perm("openapi:edit"), h.appCreate)
	r.PUT("/openapi/apps/:id", core.Perm("openapi:edit"), h.appUpdate)
	r.DELETE("/openapi/apps/:id", core.Perm("openapi:edit"), h.appRemove)
	r.PUT("/openapi/apps/:id/enabled", core.Perm("openapi:edit"), h.appSetEnabled)
	r.POST("/openapi/apps/:id/reset-secret", core.Perm("openapi:edit"), h.appResetSecret)

	r.GET("/openapi/webhooks", core.Perm("openapi:list"), h.hookList)
	r.POST("/openapi/webhooks", core.Perm("openapi:edit"), h.hookCreate)
	r.PUT("/openapi/webhooks/:id", core.Perm("openapi:edit"), h.hookUpdate)
	r.DELETE("/openapi/webhooks/:id", core.Perm("openapi:edit"), h.hookRemove)
	r.PUT("/openapi/webhooks/:id/enabled", core.Perm("openapi:edit"), h.hookSetEnabled)
	r.POST("/openapi/webhooks/:id/reset-secret", core.Perm("openapi:edit"), h.hookResetSecret)
	r.POST("/openapi/webhooks/:id/test", core.Perm("openapi:edit"), h.hookTest)

	r.GET("/openapi/deliveries", core.Perm("openapi:list"), h.deliveryList)
	r.POST("/openapi/deliveries/:id/retry", core.Perm("openapi:edit"), h.deliveryRetry)
}
