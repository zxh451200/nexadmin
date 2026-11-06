// Package maps 是免费基础地图 provider 插件：一家厂商一个适配器（高德 / 百度 / 谷歌），
// 前端按配置注册地图选点组件，逆地理编码统一走本插件代理（服务端 Key 不下发浏览器）。
package maps

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/middleware"
	"nexadmin/core/plugin"
)

// PermConfig 与前端 route.tsx、plugin.yaml 的 menu.perm 一致。
const PermConfig = "maps:config"

func init() {
	plugin.Register(plugin.Plugin{
		ID: "maps", Name: "地图服务", Version: "0.1.0", Edition: plugin.Free,
		Module: &module{},
	})
}

type module struct{}

func (*module) Name() string { return "maps" }

func (*module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: newService(d.DB)}
	g := r.Group("/plugins/maps")
	// 键配置是管理动作：maps:config + 平台级。
	g.GET("/config", core.Perm(PermConfig), h.Config)
	g.PUT("/config", core.Perm(PermConfig), core.RequirePlatform(), h.SaveConfig)
	// 地理编码 / POI / 逆地理是所有表单用户的基础能力（选点、搜索、回填地址）：
	// 登录即可用，不要求 maps:config——买回去配好 Key 就能到处挂 LocationField，不用改权限。
	login := middleware.RequireLogin()
	g.GET("/regeo", login, h.Regeo)
	g.GET("/geocode", login, h.Geocode)
	g.GET("/poi", login, h.Poi)
}
