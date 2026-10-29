// Package media 是媒体优化插件：为存量公开图片批量补生成 WebP 副本。
//
// 转码放在浏览器里做（复用文件模块的 tus 上传 + variantOf 派生），因此插件既不需要
// 读文件存储，也不引入任何图像编码依赖；原图永远保留，服务端按 Accept 优先下发副本。
package media

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/plugin"
)

// PermOptimize 与前端 route.tsx、plugin.yaml 的 menu.perm 一致。
const PermOptimize = "media:optimize"

func init() {
	plugin.Register(plugin.Plugin{
		ID: "media", Name: "媒体优化", Version: "0.1.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

type module struct{}

func (*module) Name() string { return "media" }

func (*module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: newService(d.DB)}
	g := r.Group("/plugins/media")
	g.GET("/stats", core.Perm(PermOptimize), h.Stats)
	g.GET("/images", core.Perm(PermOptimize), h.Images)

	// 视频：浏览器直传外部流媒体（Cloudflare Stream），本机不落原片、不转码。
	g.GET("/video/config", core.Perm(PermOptimize), h.VideoConfig)
	g.PUT("/video/config", core.Perm(PermOptimize), h.SaveVideoConfig)
	g.POST("/video/upload", core.Perm(PermOptimize), h.CreateVideoUpload)
	g.GET("/video/assets", core.Perm(PermOptimize), h.Videos)
	g.POST("/video/assets", core.Perm(PermOptimize), h.SaveVideo)
	g.DELETE("/video/assets/:id", core.Perm(PermOptimize), h.DeleteVideo)
}
