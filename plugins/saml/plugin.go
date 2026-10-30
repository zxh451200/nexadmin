// Package saml 是企业身份插件的 SAML 2.0 SP 部分：接收企业 IdP 的断言并签发本站登录态。
// 与 modules/sso（本站作为 OIDC Provider）方向相反，互不替代。
package saml

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/plugin"
)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "saml", Name: "SAML 单点登录", Version: "0.1.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct{}

func (*module) Name() string { return "saml" }

func (*module) Register(r *gin.RouterGroup, d *core.Deps) {
	s := NewService(d.DB, d.Auth, d.Cache)

	admin := r.Group("/plugins/saml")
	admin.GET("/sources", core.Perm("saml:list"), s.listSources)
	admin.POST("/sources", core.Perm("saml:edit"), s.createSource)
	admin.PUT("/sources/:id", core.Perm("saml:edit"), s.updateSource)
	admin.DELETE("/sources/:id", core.Perm("saml:edit"), s.deleteSource)
	admin.GET("/sources/:id/urls", core.Perm("saml:list"), s.sourceURLs)
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
