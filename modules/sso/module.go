package sso

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/middleware"
)

// tokenLimiter 创建 /oauth/token 的失败计数器：ratelimit.token（默认每分钟 10 次，同 IP 或 IP+client），计数在 core/cache。
func tokenLimiter(d *core.Deps) *middleware.Limiter {
	r := d.Config.RateLimit.Rule("token")
	return middleware.NewLimiter(d.Cache, d.Log, "sso-token", r.Limit, r.Window)
}

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "sso" }

// configuredIssuer 读取 sso.issuer（default.yaml / config.yaml / 环境变量 NEXADMIN_SSO_ISSUER）。
// core/config 没有 sso 段且不能改 core，所以这里单独读一次。
func configuredIssuer(cfg *config.Config) string {
	v := viper.New()
	v.SetConfigType("yaml")
	for _, p := range []string{cfg.DefaultConfigPath, cfg.UserConfigPath} {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			v.SetConfigFile(p)
			_ = v.MergeInConfig()
		}
	}
	v.SetEnvPrefix("NEXADMIN")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	return strings.TrimRight(strings.TrimSpace(v.GetString("sso.issuer")), "/")
}

// Register 注册路由。管理接口要 sso:list / sso:edit；应用中心与授权确认只需登录；
// 协议端点（jwks / token / userinfo / logout / discovery）是公开的，靠 client 凭据或 Bearer 保护。
// 注意：core 只给模块 /api/v1 分组，/.well-known/openid-configuration 挂不到站点根路径。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	st := &gormStore{db: d.DB}
	p := &provider{st: st, cache: d.Cache, accessTTL: d.Config.Auth.AccessTTL, refreshTTL: d.Config.Auth.RefreshTTL, now: time.Now}
	if ks, err := loadKeyStore(filepath.Join(d.Config.DataDir, "sso-keys")); err != nil {
		d.Log.Error("sso: signing keys unavailable, protocol endpoints disabled", zap.Error(err))
	} else {
		p.keys = ks
	}
	h := &handler{p: p, db: d.DB, log: d.Log, issuer: configuredIssuer(d.Config), lim: tokenLimiter(d), org: &orgGorm{db: d.DB}}
	if err := ensureOrgColumn(d.DB); err != nil {
		d.Log.Error("sso: add allow_org_read column failed, org sync stays unavailable", zap.Error(err))
	}
	auth.RegisterPerm("sso:portal")

	r.GET("/sso/keys", core.Perm("sso:list"), h.keysList)
	r.POST("/sso/keys/rotate", core.Perm("sso:edit"), h.keysRotate)
	r.GET("/sso/apps", core.Perm("sso:list"), h.list)
	r.POST("/sso/apps", core.Perm("sso:edit"), h.create)
	r.PUT("/sso/apps/:id", core.Perm("sso:edit"), h.update)
	r.DELETE("/sso/apps/:id", core.Perm("sso:edit"), h.remove)
	r.PUT("/sso/apps/:id/enabled", core.Perm("sso:edit"), h.setEnabled)
	r.POST("/sso/apps/:id/reset-secret", core.Perm("sso:edit"), h.resetSecret)
	r.GET("/sso/portal", h.portal)

	r.GET("/oauth/authorize/info", h.authorizeInfo)
	r.POST("/oauth/authorize", h.authorize)

	pub := d.Public.Group("/oauth")
	pub.GET("/.well-known/openid-configuration", h.discovery)
	pub.GET("/jwks", h.jwks)
	pub.POST("/token", h.token)
	pub.GET("/userinfo", h.userinfo)
	pub.POST("/userinfo", h.userinfo)
	pub.GET("/logout", h.logout)
	pub.GET("/org/users", h.orgUsersHandler)
	pub.GET("/org/depts", h.orgDeptsHandler)
	if d.Root != nil {
		d.Root.GET("/.well-known/openid-configuration", h.discovery)
	}
}
