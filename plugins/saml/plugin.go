// Package saml 是企业身份插件的 SAML 2.0 SP 部分：接收企业 IdP 的断言并签发本站登录态。
// 与 modules/sso（本站作为 OIDC Provider）方向相反，互不替代。
package saml

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/plugin"
	"nexadmin/core/tenant"
)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "saml", Name: "SAML 单点登录", Version: "0.6.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct{}

func (*module) Name() string { return "saml" }

var _ plugin.Starter = (*module)(nil)

// Start 启动元数据后台同步：默认每 24 小时一次，saml.metadata_refresh_hours: 0 关闭。
// 同时注册会话跟踪：普通登录、二次验证后的签发、刷新轮换都会回调，IdP 发起的 SLO 才能吊销到当前令牌。
func (*module) Start(ctx context.Context, d *core.Deps) error {
	auth.RegisterSessionTracker(func(ctx context.Context, cl *auth.Claims) {
		recordBindingSession(ctx, d.Cache, cl)
	})
	hours := loadMetadataRefreshHours()
	if hours <= 0 {
		if d.Log != nil {
			d.Log.Info("saml: metadata auto-refresh disabled")
		}
		return nil
	}
	svc := NewService(d.DB, d.Auth, d.Cache).SetLogger(d.Log)
	go func() {
		sweep := func() {
			checked, updated, failures, err := svc.SweepMetadata(tenant.Skip(ctx))
			if err != nil {
				if d.Log != nil {
					d.Log.Warn("saml: metadata refresh failed", zap.Error(err))
				}
				return
			}
			if d.Log != nil {
				// 即使 0 个源也记一条，避免「没跑」和「没有可刷新的源」长得一样。
				d.Log.Info("saml: metadata refresh finished",
					zap.Int("checked", checked), zap.Int("updated", updated), zap.Int("failed", len(failures)))
				for _, f := range failures {
					d.Log.Warn("saml: metadata refresh failed",
						zap.String("sourceId", f.ID.String()), zap.String("name", f.Name), zap.Error(f.Err))
				}
			}
		}
		// 启动先同步一轮（重启后接上欠账），之后按配置间隔。
		sweep()
		t := time.NewTicker(time.Duration(hours) * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweep()
			}
		}
	}()
	return nil
}

func (*module) Register(r *gin.RouterGroup, d *core.Deps) {
	if loadAllowPrivateHosts() { // 只放行，不收回：避免热挂载 / 测试把全局开关改回去
		setAllowPrivateHosts(true)
	}
	s := NewService(d.DB, d.Auth, d.Cache).SetLogger(d.Log)

	admin := r.Group("/plugins/saml")
	admin.GET("/sources", core.Perm("saml:list"), s.listSources)
	admin.POST("/sources", core.Perm("saml:edit"), s.createSource)
	admin.PUT("/sources/:id", core.Perm("saml:edit"), s.updateSource)
	admin.DELETE("/sources/:id", core.Perm("saml:edit"), s.deleteSource)
	admin.PUT("/sources/:id/enabled", core.Perm("saml:edit"), s.setEnabled)
	admin.POST("/sources/:id/test", core.Perm("saml:edit"), s.testSource)
	admin.POST("/sources/:id/refresh", core.Perm("saml:edit"), s.refreshMetadata)
	admin.POST("/sources/:id/rotate-sp-cert", core.Perm("saml:edit"), s.rotateSPCert)
	admin.POST("/sources/:id/promote-sp-cert", core.Perm("saml:edit"), s.promoteSPCert)
	admin.GET("/sources/:id/sp-cert", core.Perm("saml:list"), s.spCert)
	admin.GET("/sources/:id/urls", core.Perm("saml:list"), s.sourceURLs)
	admin.POST("/metadata/inspect", core.Perm("saml:edit"), s.inspectMetadata)
	admin.GET("/sources/:id/logout", s.logout)
	admin.POST("/sources/:id/logout", s.logoutJSON)

	pub := d.Public.Group("/auth/saml")
	pub.GET("/sources", s.publicSources)
	pub.POST("/exchange", s.exchange)
	pub.GET("/:id/metadata", s.metadata)
	pub.GET("/:id/login", s.login)
	pub.POST("/:id/acs", s.acs)
	pub.GET("/:id/slo", s.slo)
	pub.POST("/:id/slo", s.slo)
}
