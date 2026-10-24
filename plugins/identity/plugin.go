// Package identity 是 NexAdmin 官方付费插件「身份源」：按租户独立配置的 LDAP/AD 登录与 CAS 单点登录。
// 遵守插件约定：只依赖 core/ 与 pkg/，表前缀 plg_identity_，权限码前缀 identity:，
// 接口路径 /api/v1/plugins/identity/...。
//
//   - LDAP：通过 d.Auth.RegisterExternalAuthenticator 接入密码登录（本地密码失败后尝试），
//     客户端是标准库自写的最小 LDAPv3（bind / search / StartTLS），没有引入第三方依赖。
//   - CAS：登录页只能展示框架内置的第三方 provider，所以插件提供独立入口
//     /api/v1/plugins/identity/cas/login，由 d.Auth.LoginAs 签发登录态；入口链接显示在插件页。
//
// 安全说明（修订）：
//   - SSRF：LDAP 地址与 CAS serverUrl 默认拒绝回环 / 私网 / 链路本地 / 云元数据地址（解析后校验，并在 Dialer.Control 对实际 IP 再校验）；
//     内网部署用配置 identity.allow_private_hosts（或 NEXADMIN_IDENTITY_ALLOW_PRIVATE_HOSTS=true）显式放行，见 ssrf.go。
//   - matchExisting（外部账号按用户名绑定已有本地账号）需要单独权限 identity:match；用户模块永不绑定超管与持有内置角色的账号。
//   - 绑定键带 source id：目录作用域 ldap-<id>，登录名主体 ldapu-<id> / cas-<id>；升级前的旧键 "ldap" / "cas"
//     由用户模块在下一次登录时迁移（先到先得，之后该绑定只属于迁移它的源）。
//   - 目录同步用 RFC 2696 分页（上限 5000 条，超出整体失败）；deactivateMissing（默认关）只停用由该目录绑定过的账号，
//     目录返回数不到已绑定数一半时中止。已停用账号再次出现在目录里不会自动恢复（状态属于本地策略）。
//   - 公开的 CAS 接口始终有进程内兜底限流（按 IP），有 Cache 时再叠加 core 的限流；SLO 只撤销本系统登录时登记过的 ticket，
//     未知 index 不写缓存。残余风险：CAS 在回调完成前就发来的 SLO 会被忽略（该会话随后仍可登录），需依赖令牌自身 TTL。
//
// 已知限制：登录扩展点没有“注销”能力，授权到期后 LDAP 校验器在重启前仍然注册（重启后 Start 不会再被调用）；
// CAS 与管理接口则立即被框架的授权闸门拦截。
package identity

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/job"
	"nexadmin/core/middleware"
	"nexadmin/core/plugin"
)

var _ plugin.Starter = (*module)(nil)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "identity", Name: "身份源", Version: "1.0.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct {
	once sync.Once
	svc  *Service

	startOnce sync.Once
}

func (m *module) service(d *core.Deps) *Service {
	m.once.Do(func() {
		s := NewService(gormStore{db: d.DB}, d.Log)
		s.audit = dbAuditor(d.DB, d.Log)
		s.cache, s.auth = d.Cache, d.Auth
		m.svc = s
	})
	return m.svc
}

func (*module) Name() string { return "identity" }

// Register 注册路由。管理接口都带 core.Perm；CAS 登录入口是公开接口（d.Public），按 IP 限流。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	if loadAllowPrivateHosts() { // 只放行，不收回：避免热挂载 / 测试把全局开关改回去
		setAllowPrivateHosts(true)
	}
	svc := m.service(d)
	h := &handler{svc: svc}
	g := r.Group("/plugins/identity")
	g.GET("/sources", core.Perm("identity:list"), h.list)
	g.GET("/sources/:id", core.Perm("identity:list"), h.get)
	g.POST("/sources", core.Perm("identity:edit"), h.create)
	g.PUT("/sources/:id", core.Perm("identity:edit"), h.update)
	g.PUT("/sources/:id/enabled", core.Perm("identity:edit"), h.setEnabled)
	g.DELETE("/sources/:id", core.Perm("identity:edit"), h.remove)
	g.POST("/sources/:id/test-connection", core.Perm("identity:test"), h.testConnection)
	g.POST("/sources/:id/test-login", core.Perm("identity:test"), h.testLogin)
	g.POST("/sources/:id/sync", core.Perm("identity:sync"), h.syncNow)
	g.GET("/sources/:id/sync", core.Perm("identity:sync"), h.syncStatus)
	job.Register(directoryJobType, svc.runDirectorySync)

	if d.Public == nil {
		return
	}
	ch := &casHandler{svc: svc}
	pub := d.Public.Group("/plugins/identity/cas")
	pub.Use(limitByIP(newLocalLimiter(600, time.Minute))) // 兜底：无论 Config / Cache 是否装配都生效
	if d.Config != nil && d.Cache != nil {
		pub.Use(middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "login", middleware.ByIPAndPath()))
	}
	// SLO 是无登录态的 POST：单独再限一道（CAS 服务器出口 IP 固定，阈值给足突发注销）。
	sloLimit := []gin.HandlerFunc{limitByIP(newLocalLimiter(600, time.Minute))}
	if d.Cache != nil {
		sloLimit = append(sloLimit, middleware.RateLimit(d.Cache, d.Log, "identity-slo", middleware.ByIP(), 600, time.Minute))
	}
	pub.GET("/login", ch.login)
	pub.GET("/callback", ch.callback)
	pub.POST("/mfa/exchange", ch.exchangeMFA)
	// CAS posts SLO to the service callback URL, including source/state.
	pub.POST("/callback", append(append([]gin.HandlerFunc{}, sloLimit...), ch.slo)...)
	pub.POST("/slo", append(append([]gin.HandlerFunc{}, sloLimit...), ch.slo)...)
	pub.GET("/logout", ch.logout)
}

// Start 实现 plugin.Starter：把 LDAP 校验器注册到 core/auth。非阻塞。
func (m *module) Start(_ context.Context, d *core.Deps) error {
	svc := m.service(d)
	var err error
	m.startOnce.Do(func() {
		err = d.Auth.RegisterExternalAuthenticator(&ldapAuthenticator{svc: svc, dial: svc.dial, log: d.Log})
	})
	return err
}
