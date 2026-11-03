package recycle

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core"
)

func init() { core.RegisterBuiltin(module{}) }

// 回收站模块：展示 user / notice 的软删除记录，支持还原与彻底删除。
// 软删除本身由源模块完成，并通过 modules/internal/recbin 登记 sys_recycle。
type module struct{}

func (module) Name() string { return "recycle" }

// Register 注册路由，并挂上每日孤儿清理循环
// （与 message 的清理循环同口径：OnClose 关闭 stop 通道，优雅关闭时退出）。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &Handler{svc: &Service{db: d.DB, log: d.Log}}
	r.GET("/recycle", core.Perm("recycle:list"), h.List)
	r.POST("/recycle/restore", core.Perm("recycle:restore"), h.RestoreBatch)
	r.POST("/recycle/:id/restore", core.Perm("recycle:restore"), h.Restore)
	r.DELETE("/recycle", core.Perm("recycle:delete"), h.PurgeBatch)
	r.DELETE("/recycle/:id", core.Perm("recycle:delete"), h.Purge)

	stop := make(chan struct{})
	if d.OnClose != nil {
		d.OnClose(func() { close(stop) })
	}
	go h.svc.sweepLoop(stop)
}
