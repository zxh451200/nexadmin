// Package role 是角色模块（按租户隔离）：角色 CRUD、数据权限、分配菜单/按钮权限。
package role

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
)

func init() { core.RegisterBuiltin(Module()) }

// Module 返回角色模块。
func Module() core.Module { return module{} }

type module struct{}

func (module) Name() string { return "role" }

// Seed 实现 setup.Seeder：种入出厂角色（按租户 + code 幂等）。
func (module) Seed(db *gorm.DB) error { return seedRoles(db) }

// Register 注册路由。固定路径写在 /:id 之前。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: &service{db: d.DB}}
	r.GET("/roles", core.Perm("role:list"), h.list)
	r.GET("/roles/options", h.options) // 下拉选项：登录即可，只返回启用角色的 id 与名称
	r.PUT("/roles/sort", core.Perm("role:update"), h.sort)
	r.GET("/roles/:id", core.Perm("role:list"), h.get)
	r.POST("/roles", core.Perm("role:create"), h.create)
	r.PUT("/roles/:id", core.Perm("role:update"), h.update)
	r.DELETE("/roles/:id", core.Perm("role:delete"), h.remove)
	r.GET("/roles/:id/menus", core.Perm("role:list"), h.menus)
	r.PUT("/roles/:id/menus", core.Perm("role:assign"), h.assignMenus)
}
