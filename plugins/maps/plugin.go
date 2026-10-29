// Package maps 是免费基础地图 provider 插件：一家厂商一个适配器（高德 / 百度 / 谷歌），
// 前端按配置注册地图选点组件，逆地理编码统一走本插件代理（服务端 Key 不下发浏览器）。
package maps

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core"
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
	g.GET("/config", core.Perm(PermConfig), h.Config)
	g.PUT("/config", core.Perm(PermConfig), h.SaveConfig)
	g.GET("/regeo", core.Perm(PermConfig), h.Regeo)
}
