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
	"nexadmin/core/tenant"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "openapi" }

func (module) TenantTables() []tenant.Table {
	return []tenant.Table{
		{Name: "sys_pat", Column: "tenant_id"},
		{Name: "sys_openapi_app", Column: "tenant_id"},
		{Name: "sys_webhook", Column: "tenant_id"},
		{Name: "sys_webhook_delivery", Column: "tenant_id"},
	}
}

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
	// tokenCreate 不进操作日志：响应带一次性明文令牌，而操作日志中间件会把响应原文入库
	// （只对请求参数打码，见 modules/log/middleware.go），挂了反而把令牌泄进 sys_oper_log；撤销照常记录。
	r.GET("/profile/tokens", h.tokenList)
	r.POST("/profile/tokens", patCreate, h.tokenCreate)
	r.DELETE("/profile/tokens/:id", d.OperLogMiddleware("openapi", "delete"), h.tokenDelete)
	// 批量撤销与单条同作用域（只动自己的令牌），同样不挂权限码
	r.DELETE("/profile/tokens", d.OperLogMiddleware("openapi", "delete"), h.tokenDeleteMany)

	registerDocs(r, d)

	r.GET("/openapi/events", core.Perm("openapi:list"), func(c *gin.Context) { response.OK(c, event.Catalog()) })

	// create / reset-secret 同 /sso/apps 的取舍：响应带一次性明文密钥，不进操作日志。
	r.GET("/openapi/apps", core.Perm("openapi:list"), h.appList)
	r.POST("/openapi/apps", core.Perm("openapi:edit"), h.appCreate)
	r.PUT("/openapi/apps/:id", core.Perm("openapi:edit"), d.OperLogMiddleware("openapi", "update"), h.appUpdate)
	r.DELETE("/openapi/apps/:id", core.Perm("openapi:edit"), d.OperLogMiddleware("openapi", "delete"), h.appRemove)
	r.DELETE("/openapi/apps", core.Perm("openapi:edit"), d.OperLogMiddleware("openapi", "delete"), h.appRemoveMany) // 批量：?ids=1,2,3
	r.PUT("/openapi/apps/:id/enabled", core.Perm("openapi:edit"), d.OperLogMiddleware("openapi", "update"), h.appSetEnabled)
	r.POST("/openapi/apps/:id/reset-secret", core.Perm("openapi:edit"), h.appResetSecret)

	// create / reset-secret 同上；test 只发一次 ping 探测、不写投递记录（见 hookTest），均不进操作日志。
	r.GET("/openapi/webhooks", core.Perm("openapi:list"), h.hookList)
	r.POST("/openapi/webhooks", core.Perm("openapi:edit"), h.hookCreate)
	r.PUT("/openapi/webhooks/:id", core.Perm("openapi:edit"), d.OperLogMiddleware("openapi", "update"), h.hookUpdate)
	r.DELETE("/openapi/webhooks/:id", core.Perm("openapi:edit"), d.OperLogMiddleware("openapi", "delete"), h.hookRemove)
	r.DELETE("/openapi/webhooks", core.Perm("openapi:edit"), d.OperLogMiddleware("openapi", "delete"), h.hookRemoveMany) // 批量：?ids=1,2,3
	r.PUT("/openapi/webhooks/:id/enabled", core.Perm("openapi:edit"), d.OperLogMiddleware("openapi", "update"), h.hookSetEnabled)
	r.POST("/openapi/webhooks/:id/reset-secret", core.Perm("openapi:edit"), h.hookResetSecret)
	r.POST("/openapi/webhooks/:id/test", core.Perm("openapi:edit"), h.hookTest)

	r.GET("/openapi/deliveries", core.Perm("openapi:list"), h.deliveryList)
	r.POST("/openapi/deliveries/:id/retry", core.Perm("openapi:edit"), d.OperLogMiddleware("openapi", "retry"), h.deliveryRetry)
}
