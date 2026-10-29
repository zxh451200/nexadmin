// Package totp 是 NexAdmin 免费基础两步验证(TOTP/MFA)插件:实现 core/auth 的 LoginGate 扩展点。
// 用户在个人设置里绑定验证器(RFC 6238,兼容 Google Authenticator 等),启用后登录要求
// 输入 6 位动态码;步长防重放(core 保证挑战次数/TTL/IP 绑定,本插件保证 TOTP 同码防重放)。
// 只依赖 core/ 与 pkg/,表前缀 plg_totp_,接口路径 /api/v1/plugins/totp/...。
package totp

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/plugin"
)

// 建表由运行时执行 plugins/totp/migrations/<引擎>/*.sql(安装时 CLI 拷贝),插件自己不做迁移。
var _ = plugin.Starter(nil)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "totp", Name: "两步验证", Version: "0.1.0", Edition: plugin.Free,
		Module: &module{},
	})
}

type module struct{}

func (m *module) service(d *core.Deps) *Service { return NewService(d.DB) }

func (*module) Name() string { return "totp" }

// Register 注册个人中心路由(登录即可,不挂权限点——自己的数据自己读写),
// 并把 LoginGate 挂到框架登录链。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := m.service(d)
	h := &handler{svc: svc}

	// 登录链挂门:已绑定用户登录时要求 6 位动态码。
	_ = d.Auth.RegisterLoginGate(&gate{svc: svc})

	g := r.Group("/plugins/totp")
	g.GET("/status", h.status)
	g.POST("/enroll", h.enroll)
	g.POST("/confirm", h.confirm)
	g.POST("/disable", h.disable)
}
