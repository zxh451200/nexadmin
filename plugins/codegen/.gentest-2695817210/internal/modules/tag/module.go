package tag

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
)

// 需要在 internal/modules/modules.go 加一行空导入：_ "nexadmin/internal/modules/tag"，init 里注册即可，main.go 不用改。
func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "tag" }

// Seed 种入菜单与按钮权限（初始化向导 / init 时），幂等。
func (module) Seed(db *gorm.DB) error { return seedMenus(db) }

// SeedOnStartup 每次启动补齐缺失的菜单；已齐全时只有一次查询。
func (module) SeedOnStartup(db *gorm.DB) error { return seedMenus(db) }

// Register 注册路由（/api/v1 下，默认要求登录），权限码 tag:list / tag:edit / tag:delete。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := NewHandler(NewService(d.DB))
	r.GET("/tags", core.Perm("tag:list"), h.List)
	r.GET("/tags/:id", core.Perm("tag:list"), h.Get)
	r.POST("/tags", core.Perm("tag:edit"), h.Create)
	r.PUT("/tags/:id", core.Perm("tag:edit"), h.Update)
	r.DELETE("/tags/:id", core.Perm("tag:delete"), h.Delete)
	r.DELETE("/tags", core.Perm("tag:delete"), h.DeleteBatch)
}
