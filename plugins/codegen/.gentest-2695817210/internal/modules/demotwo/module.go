package demotwo

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
)

// 需要在 internal/modules/modules.go 加一行空导入：_ "nexadmin/internal/modules/demotwo"，init 里注册即可，main.go 不用改。
func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "demotwo" }

// Seed 种入菜单与按钮权限（初始化向导 / init 时），幂等。
func (module) Seed(db *gorm.DB) error { return seedMenus(db) }

// SeedOnStartup 每次启动补齐缺失的菜单；已齐全时只有一次查询。
func (module) SeedOnStartup(db *gorm.DB) error { return seedMenus(db) }

// Register 注册路由（/api/v1 下，默认要求登录），权限码 demotwo:list / demotwo:edit / demotwo:delete。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := NewHandler(NewService(d.DB))
	r.GET("/demotwos", core.Perm("demotwo:list"), h.List)
	r.GET("/demotwos/:id", core.Perm("demotwo:list"), h.Get)
	r.POST("/demotwos", core.Perm("demotwo:edit"), h.Create)
	r.PUT("/demotwos/:id", core.Perm("demotwo:edit"), h.Update)
	r.DELETE("/demotwos/:id", core.Perm("demotwo:delete"), h.Delete)
	r.DELETE("/demotwos", core.Perm("demotwo:delete"), h.DeleteBatch)
}
