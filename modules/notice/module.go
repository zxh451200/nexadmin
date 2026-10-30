// Package notice 是通知公告模块：富文本公告、按全员/角色/部门/用户发布、定时发布与过期、置顶、
// 重要公告登录弹窗、已读未读统计，删除走回收站（软删除）。
package notice

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/modules/internal/session"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "notice" }

// Seed 种入出厂公告（setup.Seeder）。
func (module) Seed(db *gorm.DB) error { return seed(db) }

// Register 注册路由。登录即可用的接口不挂权限点（只读写“自己的”数据）。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := NewHandler(NewService(d.DB))
	touch := session.Middleware(session.Shared(d.Cache, d.Config.Auth.RefreshTTL))
	auth.RegisterPerm("notice:platform") // 发平台公告（全部租户），页面按钮用到

	// 登录用户：弹窗、我的公告、标记已读
	r.GET("/notices/popup", touch, h.Popup)
	r.GET("/notices/mine", h.Mine)
	r.DELETE("/notices/mine", h.ClearMine)
	r.POST("/notices/:id/read", h.MarkRead)

	// 管理端
	r.GET("/notices", core.Perm("notice:list"), h.List)
	r.GET("/notices/:id", core.Perm("notice:list"), h.Get)
	r.GET("/notices/:id/read-stats", core.Perm("notice:list"), h.ReadStats)
	r.POST("/notices", core.Perm("notice:create"), h.Create)
	r.PUT("/notices/:id", core.Perm("notice:update"), h.Update)
	r.POST("/notices/:id/publish", core.Perm("notice:publish"), h.Publish)
	r.POST("/notices/:id/revoke", core.Perm("notice:publish"), h.Revoke)
	r.DELETE("/notices/:id", core.Perm("notice:delete"), h.Delete)
	r.DELETE("/notices", core.Perm("notice:delete"), h.DeleteBatch)
}
