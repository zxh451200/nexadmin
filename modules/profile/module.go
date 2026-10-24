package profile

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/middleware"
	"nexadmin/modules/internal/oauthflow"
	"nexadmin/modules/internal/session"
	"nexadmin/modules/internal/smscode"
)

func init() { core.RegisterBuiltin(module{}) }

// 个人中心模块：资料、改密、手机号/第三方账号绑定、登录设备。复用 sys_user，不建表。
type module struct{}

func (module) Name() string { return "profile" }

// Register 注册路由。个人中心只读写“自己的”数据，登录即可，不挂权限点；
// profile:view 仅登记给菜单/角色模块展示。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	auth.RegisterPerm("profile:view")
	store := session.Shared(d.Cache, d.Config.Auth.RefreshTTL)
	h := &Handler{
		svc:      &Service{db: d.DB, cfg: d.Config},
		sms:      smscode.Shared(d.Config, d.Cache, d.Log),
		flow:     oauthflow.Shared(d.Config, d.Cache, d.Log),
		auth:     d.Auth,
		cfg:      d.Config,
		sessions: store,
	}
	g := r.Group("", session.Middleware(store))

	g.GET("/profile", h.Get)
	g.PUT("/profile", h.Update)
	g.PUT("/profile/password", middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "password_change", middleware.ByIPAndPath()), h.ChangePassword)

	g.GET("/profile/bindings", h.Bindings)
	g.POST("/profile/bindings/phone", h.BindPhone)
	g.DELETE("/profile/bindings/phone", h.UnbindPhone)
	g.POST("/profile/bindings/:key", h.BindProvider)
	g.DELETE("/profile/bindings/:key", h.UnbindProvider)

	g.GET("/profile/sessions", h.Sessions)
	g.DELETE("/profile/sessions/:id", h.KickSession)

	// 发短信验证码需要匿名可用（手机号登录也用它）；靠冷却、每日上限和 IP 限流防刷。
	d.Public.POST("/auth/sms-code", middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "sms_code", middleware.ByIPAndPath()), h.SendSMS)
}
