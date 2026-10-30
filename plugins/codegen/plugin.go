// Package codegen 是 NexAdmin 官方免费插件「代码生成器」：读取当前库的业务表结构，
// 或按向导里的表定义，在内存里渲染 Go 模块 + 前端页面（新建表模式额外附带三引擎迁移），
// 预览或打包为 zip 下载。无状态、不建表、不写磁盘。遵守插件约定：只依赖 core/ 与 pkg/，
// 权限码前缀 codegen:，接口路径 /api/v1/plugins/codegen/...。
package codegen

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/plugin"
)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "codegen", Name: "代码生成器", Version: "1.1.0", Edition: plugin.Free,
		Module: &module{},
	})
}

type module struct{}

func (*module) Name() string { return "codegen" }

// Register 注册路由。每个接口都带 core.Perm。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: NewService(NewGormReader(d.DB))}
	g := r.Group("/plugins/codegen")
	g.GET("/tables", core.Perm("codegen:list"), h.tables)
	g.GET("/tables/:name/columns", core.Perm("codegen:list"), h.columns)
	g.POST("/schema", core.Perm("codegen:list"), h.schema)
	g.POST("/preview", core.Perm("codegen:list"), h.preview)
	g.POST("/download", core.Perm("codegen:download"), h.download)
}
