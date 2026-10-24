// Package region 提供省市区接口，数据在 core/region（go:embed，无表无迁移）。
package region

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/region"
	"nexadmin/core/response"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "region" }

// Register 注册省市区接口（登录即可，不设权限码）：
//
//	GET /api/v1/regions/tree?level=3   一次取回前 level 级的树
//	GET /api/v1/regions?parent=<code>  按需取下级；parent 为空返回省级
func (module) Register(r *gin.RouterGroup, _ *core.Deps) {
	r.GET("/regions/tree", func(c *gin.Context) {
		level, _ := strconv.Atoi(c.Query("level"))
		tree, err := region.Tree(level)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, tree)
	})
	r.GET("/regions", func(c *gin.Context) {
		list, ok, err := region.Children(c.Query("parent"))
		if err != nil {
			response.Fail(c, err)
			return
		}
		if !ok {
			response.Fail(c, response.NotFound("error.not_found"))
			return
		}
		response.OK(c, list)
	})
}
