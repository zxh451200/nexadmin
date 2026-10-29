package demo

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
)

// 需要在 internal/modules/modules.go 加一行空导入：_ "nexadmin/internal/modules/demo"，init 里注册即可，main.go 不用改。
func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "demo" }

// Seed 种入菜单与按钮权限（初始化向导 / init 时），幂等。
func (module) Seed(db *gorm.DB) error { return seedMenus(db) }

// SeedOnStartup 每次启动补齐缺失的菜单；已齐全时只有一次查询。
func (module) SeedOnStartup(db *gorm.DB) error { return seedMenus(db) }

// Register 注册路由（/api/v1 下，默认要求登录），权限码 demo:list / demo:edit / demo:delete。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := NewHandler(NewService(d.DB))
	r.GET("/demos", core.Perm("demo:list"), h.List)
	r.GET("/demos/:id", core.Perm("demo:list"), h.Get)
	r.POST("/demos", core.Perm("demo:edit"), h.Create)
	r.PUT("/demos/:id", core.Perm("demo:edit"), h.Update)
	r.DELETE("/demos/:id", core.Perm("demo:delete"), h.Delete)
	r.DELETE("/demos", core.Perm("demo:delete"), h.DeleteBatch)
}
