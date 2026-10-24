// Package menu 是菜单模块（平台级，不分租户）：菜单树、侧边栏菜单（按角色过滤）、菜单 CRUD 与排序。
package menu

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
)

func init() { core.RegisterBuiltin(Module()) }

// Module 返回菜单模块。
func Module() core.Module { return module{} }

type module struct{}

func (module) Name() string { return "menu" }

// Seed 实现 setup.Seeder：种入出厂菜单（按 code 幂等）。
func (module) Seed(db *gorm.DB) error { return seedMenus(db) }

// SeedOnStartup 实现 setup.StartupSeeder：每次启动补上新版本新增的出厂菜单（存量库升级后重启即可见）。
func (module) SeedOnStartup(db *gorm.DB) error { return seedMenus(db) }

// Register 注册路由。固定路径写在 /:id 之前。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: &service{db: d.DB}}
	// 菜单树被菜单页与角色“分配菜单”共用，所以三个权限满足任一即可。
	r.GET("/menus/tree", anyPerm("menu:list", "role:list", "role:assign"), h.tree)
	r.GET("/menus/mine", h.mine) // 侧边栏：登录即可，内容按当前用户权限过滤
	r.PUT("/menus/sort", core.Perm("menu:update"), h.sort)
	r.PUT("/menus/:id/move", core.Perm("menu:update"), h.move)
	r.GET("/menus/:id", core.Perm("menu:list"), h.get)
	r.POST("/menus", core.Perm("menu:create"), h.create)
	r.PUT("/menus/:id", core.Perm("menu:update"), h.update)
	r.DELETE("/menus/:id", core.Perm("menu:delete"), h.remove)
}
