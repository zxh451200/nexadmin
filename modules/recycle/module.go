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

// Register 注册路由。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &Handler{svc: &Service{db: d.DB}}
	r.GET("/recycle", core.Perm("recycle:list"), h.List)
	r.POST("/recycle/:id/restore", core.Perm("recycle:restore"), h.Restore)
	r.DELETE("/recycle/:id", core.Perm("recycle:delete"), h.Purge)
}
