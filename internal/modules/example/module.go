// Package example 演示业务模块的注册约定，与 nexadmin add module 生成的模块一致：在自己包的 init() 里
// core.RegisterBuiltin，再由 internal/modules/modules.go 标记行之间的空导入引入。
// 要删除示例：删除本目录，并删掉 modules.go 里 example 那一行空导入（Go 不允许悬空导入）。
package example

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "example" }

// Register 的参数 r 对应 /api/v1，默认要求登录；d.Public 是匿名路由组。
// 真实模块通常是：h := NewHandler(NewService(d.DB))，再 r.GET("/articles", core.Perm("article:list"), h.List)。
// 模型嵌入 database.TenantModel 即可自动带租户隔离和雪花主键。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	d.Public.GET("/example/hello", func(c *gin.Context) { // 匿名：GET /api/v1/example/hello
		response.OK(c, gin.H{"hello": "nexadmin"})
	})
	r.GET("/example/ping", core.Perm("example:ping"), func(c *gin.Context) { // 需登录 + 权限点 example:ping
		tid, _ := tenant.From(c.Request.Context())
		response.OK(c, gin.H{"pong": true, "tenantId": tid})
	})
}
