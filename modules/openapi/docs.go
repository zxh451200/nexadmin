package openapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/response"
)

// registerDocs 注册接口文档：openapi.json（OpenAPI 3.0.3，原始 JSON，不套统一响应体，便于导入工具）
// 与扁平路由清单。权限沿用 openapi:list（超管天然通过）。
func registerDocs(r *gin.RouterGroup, d *core.Deps) {
	if d.APIDoc == nil {
		return
	}
	doc := d.APIDoc
	r.GET("/openapi.json", core.Perm("openapi:list"), func(c *gin.Context) {
		b, err := doc.JSON()
		if err != nil {
			response.Fail(c, err)
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", b)
	})
	r.GET("/openapi/routes", core.Perm("openapi:list"), func(c *gin.Context) {
		response.OK(c, doc.Routes())
	})
}
