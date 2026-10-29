// Package ldap 是 NexAdmin 免费基础 LDAP/AD 登录适配插件:实现 core/auth 的 ExternalAuthenticator
// 扩展点(本地密码失败后按顺序尝试的外部校验器)。配置存 plg_ldap_config(平台级,
// 密码经 core/secret 加密),管理端可编辑与连通性测试;身份映射/自动建号由框架的用户模块负责。
// 只依赖 core/ 与 pkg/(+ go-ldap,MIT),表前缀 plg_ldap_,权限码前缀 ldap:。
package ldap

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/plugin"
)

// 建表由运行时执行 plugins/ldap/migrations/<引擎>/*.sql(安装时 CLI 拷贝),插件自己不做迁移。
var _ = plugin.Starter(nil)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "ldap", Name: "LDAP/AD 登录", Version: "0.1.0", Edition: plugin.Free,
		Module: &module{},
	})
}

// zapLog 把 *zap.Logger 适配成 Service 需要的最小日志面。
type zapLog struct{ l *zap.Logger }

func (z zapLog) Warn(msg string, kv ...any)  { z.l.Sugar().Warnw(msg, kv...) }
func (z zapLog) Error(msg string, kv ...any) { z.l.Sugar().Errorw(msg, kv...) }

type module struct{}

func (m *module) service(d *core.Deps) *Service {
	return NewService(d.DB, zapLog{orDefault(d)})
}

func orDefault(d *core.Deps) *zap.Logger {
	if d.Log == nil {
		return zap.NewNop()
	}
	return d.Log
}

func (*module) Name() string { return "ldap" }

// Register 注册路由,并把 authenticator 挂到框架登录链(本地密码失败后尝试 LDAP)。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := NewService(d.DB, zapLog{orDefault(d)})
	h := &handler{svc: svc}

	// 本地凭据失败后按注册顺序尝试;登录日志 login_type = "ldap"。
	_ = d.Auth.RegisterExternalAuthenticator(&authenticator{svc: svc})

	g := r.Group("/plugins/ldap")
	g.GET("/config", core.Perm("ldap:list"), h.config)
	g.PUT("/config", core.Perm("ldap:edit"), h.configSave)
	g.POST("/test", core.Perm("ldap:edit"), h.test)
}

var _ = auth.RegisteredPerms
