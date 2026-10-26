package backup

import (
	"context"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/modules/about"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "backup" }

// Register 注册路由并启动定时检查。恢复不提供网页入口，见 `nexadmin-server restore`。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := NewService(d.DB, d.Log, d.Config, about.Version)
	routes(r, NewHandler(svc))
	// 模块没有关闭钩子，goroutine 随进程退出（与 message 模块一致）。
	go svc.Loop(context.Background())
}

// routes 注册接口：都要求超管 + 对应权限点；写操作进操作日志，下载单独记录。
func routes(r gin.IRoutes, h *Handler) {
	guard := superOnly()
	r.GET("/backups", core.Perm("backup:list"), guard, h.List)
	r.GET("/backups/status", core.Perm("backup:list"), guard, h.Status)
	r.GET("/backups/config", core.Perm("backup:list"), guard, h.GetConfig)
	r.PUT("/backups/config", core.Perm("backup:edit"), guard, audit("update"), h.PutConfig)
	r.POST("/backups", core.Perm("backup:edit"), guard, audit("create"), h.Create)
	r.GET("/backups/:id/download", core.Perm("backup:download"), guard, h.Download)
	r.DELETE("/backups/:id", core.Perm("backup:edit"), guard, audit("delete"), h.Delete)
}
