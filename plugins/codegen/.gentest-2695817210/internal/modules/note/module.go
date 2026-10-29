package note

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
)

// 需要在 internal/modules/modules.go 加一行空导入：_ "nexadmin/internal/modules/note"，init 里注册即可，main.go 不用改。
func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "note" }

// Seed 种入菜单与按钮权限（初始化向导 / init 时），幂等。
func (module) Seed(db *gorm.DB) error { return seedMenus(db) }

// SeedOnStartup 每次启动补齐缺失的菜单；已齐全时只有一次查询。
func (module) SeedOnStartup(db *gorm.DB) error { return seedMenus(db) }

// Register 注册路由（/api/v1 下，默认要求登录），权限码 note:list / note:edit / note:delete。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := NewHandler(NewService(d.DB))
	r.GET("/notes", core.Perm("note:list"), h.List)
	r.GET("/notes/:id", core.Perm("note:list"), h.Get)
	r.POST("/notes", core.Perm("note:edit"), h.Create)
	r.PUT("/notes/:id", core.Perm("note:edit"), h.Update)
	r.DELETE("/notes/:id", core.Perm("note:delete"), h.Delete)
	r.DELETE("/notes", core.Perm("note:delete"), h.DeleteBatch)
}
