package codegen

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
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

// POST /plugins/codegen/schema：已有表给 table，新建表给 define。
func (h *handler) schema(c *gin.Context) {
	var req SchemaRequest
	if !response.BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Schema(c.Request.Context(), req)
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

// InstallForm 是直写项目的入参：渲染入参 + 是否覆盖已存在文件（默认不覆盖，只新增）
// + 是否把模块空导入注册进 internal/modules/modules.go（默认注册）。
type InstallForm struct {
	GenRequest
	Overwrite bool `json:"overwrite"`
	Register  bool `json:"register"`
}

// POST /plugins/codegen/install：把渲染结果写进项目根目录（后端进程工作目录，
// 可用 NEXADMIN_ROOT 指定）。路径白名单在渲染层把关，这里只做落盘与模块注册。
func (h *handler) install(c *gin.Context) {
	var f InstallForm
	if !response.BindJSON(c, &f) {
		return
	}
	res, err := h.svc.Install(c.Request.Context(), f.GenRequest, f.Overwrite, f.Register)
	respond(c, res, err)
}

// ---- 保存的生成配置 ----

// GET /plugins/codegen/configs
func (h *handler) listConfigs(c *gin.Context) {
	list, err := h.svc.ListConfigs(c.Request.Context())
	respond(c, list, err)
}

// POST /plugins/codegen/configs —— 按名字保存（同名即更新）。
func (h *handler) saveConfig(c *gin.Context) {
	var f ConfigForm
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.SaveConfig(c.Request.Context(), f)
	respond(c, v, err)
}

// DELETE /plugins/codegen/configs/:id
func (h *handler) deleteConfig(c *gin.Context) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id <= 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	respond(c, nil, h.svc.DeleteConfig(c.Request.Context(), id))
}

// DELETE /plugins/codegen/configs?ids=1,2,3 —— 批量删除，权限码复用单条删除的 codegen:edit。
// ids 缺失 / 整体为空 / 任一项非法都由 response.ParseIDsQuery 直接 400：
// 「不带 ids 就删光所有配置」是批量删除里最难发现的一种误删。
func (h *handler) deleteConfigs(c *gin.Context) {
	ids, err := response.ParseIDsQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeleteConfigs(c.Request.Context(), ids)
	respond(c, gin.H{"deleted": n}, err)
}
