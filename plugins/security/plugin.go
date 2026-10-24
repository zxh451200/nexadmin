// Package security 是 NexAdmin 官方付费插件「高级安全」：二次验证（TOTP + 一次性恢复码）与登录 IP 黑白名单。
// 遵守设计文档 §19：只依赖 core/ 与 pkg/，表前缀 plg_security_，权限码前缀 security:，
// 接口路径 /api/v1/plugins/security/...。
//
// 与 core/auth 的对接（不修改 core）：在 Register 里通过 d.Auth 注册
//   - RegisterLoginGate：已启用 2FA 的用户，密码登录只拿到 challenge，验证码正确后才签发令牌；
//   - RegisterPreLogin：scope=all 的 IP 黑白名单（deny 优先；白名单非空时仅放行名单内）。
//
// 已知边界：core 没有“注销扩展点”的接口，门和前置检查一旦注册在进程生命周期内一直生效；
// 授权到期后路由会被框架拦成 403，但已注册的门不会撤销（偏安全的一侧：已启用 2FA 的用户仍需二次验证）。
package security

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/plugin"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

var _ plugin.Starter = (*module)(nil)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "security", Name: "高级安全", Version: "1.0.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct {
	once sync.Once
	svc  *Service
	hook sync.Once
}

func (m *module) service(d *core.Deps) *Service {
	m.once.Do(func() { m.svc = newService(&gormStore{db: d.DB}, d.Cache, d.Log) })
	return m.svc
}

// install 向 d.Auth 注册门与前置检查，整个进程只做一次（Register 可能因授权变化被重复调用）。
func (m *module) install(d *core.Deps) {
	s := m.service(d)
	m.hook.Do(func() {
		s.announceBypass()
		s.attachBus(d.Bus) // 规则变更跨实例失效；Bus 不可用时靠 5 秒 TTL 兜底
		if d.Auth == nil {
			return
		}
		a := d.Auth
		s.lookup = func(ctx context.Context, uid idgen.ID) (*auth.User, error) {
			p := a.Provider()
			if p == nil {
				return nil, response.Unavailable("auth.provider_missing")
			}
			return p.Profile(ctx, uid)
		}
		s.reauth = func(ctx context.Context, u User, password string) error {
			v, ok := a.Provider().(auth.PasswordVerifier)
			if !ok {
				return response.Unavailable("auth.provider_missing")
			}
			return v.VerifyPassword(ctx, u.ID, password)
		}
		d.Auth.RegisterPreLogin(s.preLogin)
		d.Auth.RegisterRefreshCheck(s.refreshCheck)
		if err := d.Auth.RegisterLoginGate(&gate{s: s}); err != nil {
			d.Log.Error("security: register login gate failed", zap.Error(err))
		}
	})
}

func (*module) Name() string { return "security" }

// Register 注册路由，并把门与前置检查挂到 d.Auth。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	m.install(d)
	h := &handler{svc: m.service(d)}
	if d.Auth != nil {
		a := d.Auth
		h.profile = func(ctx context.Context, id idgen.ID) (*auth.User, error) {
			p := a.Provider()
			if p == nil {
				return nil, nil
			}
			return p.Profile(ctx, id)
		}
	}
	mount(r, h)
}

// mount 注册全部路由（单测复用）。
func mount(r *gin.RouterGroup, h *handler) {
	g := r.Group("/plugins/security")
	g.GET("/2fa/status", core.Perm("security:self"), h.status)
	g.POST("/2fa/setup", core.Perm("security:self"), h.setup)
	g.POST("/2fa/enable", core.Perm("security:self"), h.enable)
	g.POST("/2fa/disable", core.Perm("security:self"), h.disable)
	g.POST("/2fa/recovery-codes", core.Perm("security:self"), h.regen)
	g.GET("/policy", core.Perm("security:policy"), h.getPolicy)
	g.PUT("/policy", core.Perm("security:policy"), h.putPolicy)
	g.GET("/2fa/users", core.Perm("security:user"), h.listUsers)
	g.POST("/2fa/users/:id/reset", core.Perm("security:reset"), h.resetUser)
	g.GET("/ip-rules", core.Perm("security:ip"), h.listRules)
	g.POST("/ip-rules", core.Perm("security:ip:edit"), h.createRule)
	g.PUT("/ip-rules/:id", core.Perm("security:ip:edit"), h.updateRule)
	g.PUT("/ip-rules/:id/enabled", core.Perm("security:ip:edit"), h.setRuleEnabled)
	g.DELETE("/ip-rules/:id", core.Perm("security:ip:edit"), h.deleteRule)
	g.GET("/logs", core.Perm("security:log"), h.listLogs)
}

// Start 实现 plugin.Starter：确保门与前置检查已注册（不启动后台任务）。
func (m *module) Start(_ context.Context, d *core.Deps) error {
	m.install(d)
	return nil
}
