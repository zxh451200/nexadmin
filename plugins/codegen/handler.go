package codegen

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// 路径前缀：/api/v1/plugins/codegen。
// 权限码：codegen:list 查看表 / 列 / 预览；codegen:download 下载 zip。

type handler struct{ svc *Service }

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// GET /plugins/codegen/tables
func (h *handler) tables(c *gin.Context) {
	list, err := h.svc.Tables(c.Request.Context())
	respond(c, list, err)
}

// GET /plugins/codegen/tables/:name/columns
func (h *handler) columns(c *gin.Context) {
	v, err := h.svc.Columns(c.Request.Context(), c.Param("name"))
	respond(c, v, err)
}

// POST /plugins/codegen/preview
func (h *handler) preview(c *gin.Context) {
	var req GenRequest
	if !response.BindJSON(c, &req) {
		return
	}
	out, err := h.svc.Generate(c.Request.Context(), req)
	respond(c, out, err)
}

// POST /plugins/codegen/download：同 preview 的入参，返回内存生成的 zip，不写磁盘。
func (h *handler) download(c *gin.Context) {
	var req GenRequest
	if !response.BindJSON(c, &req) {
		return
	}
	out, err := h.svc.Generate(c.Request.Context(), req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	data, err := out.Zip()
	if err != nil {
		response.Fail(c, response.Internal().Wrap(err))
		return
	}
	// req.Module 已通过 ^[a-z][a-z0-9]{0,30}$ 校验，可安全放进响应头
	c.Header("Content-Disposition", `attachment; filename="`+req.Module+`-codegen.zip"`)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "application/zip", data)
}
