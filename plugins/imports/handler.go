package imports

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路由前缀:/api/v1/plugins/imports。权限码:imports:list 查看来源与台账;imports:run 执行导入。
// 租户级数据(台账按租户隔离),租户隔离自动生效。

type handler struct{ svc *Service }

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

func actor(c *gin.Context) string {
	if cl := auth.ClaimsFrom(c); cl != nil {
		return fmt.Sprintf("%d", int64(cl.UserID))
	}
	return ""
}

// GET /plugins/imports/sources —— 已注册导入源(来源下拉 + 列说明)。
func (h *handler) sources(c *gin.Context) {
	response.OK(c, ListSources())
}

// GET /plugins/imports/template?source=products —— 下载导入模板(CSV,带 BOM 与示例行)。
func (h *handler) template(c *gin.Context) {
	src, err := getSource(c.Query("source"))
	if err != nil {
		response.Fail(c, response.BadRequest("imports.source_unknown").WithMsg(err.Error()))
		return
	}
	c.Data(200, "text/csv; charset=utf-8", BuildTemplateCSV(src))
}

// POST /plugins/imports —— multipart:file(文件)+ source(来源标识)。同步执行,返回台账。
func (h *handler) run(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, response.BadRequest("imports.file_required"))
		return
	}
	source := c.PostForm("source")
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, response.BadRequest("imports.file_required"))
		return
	}
	defer func() { _ = f.Close() }()
	content := make([]byte, 0, fh.Size)
	buf := make([]byte, 64*1024)
	for {
		n, rerr := f.Read(buf)
		content = append(content, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	v, err := h.svc.Run(c.Request.Context(), RunInput{
		Source: source, FileName: fh.Filename, Content: content,
	}, actor(c))
	respond(c, v, err)
}

// GET /plugins/imports?source=&status=&page=&pageSize= —— 导入台账。
func (h *handler) list(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListJobs(c.Request.Context(), JobQuery{
		Source: c.Query("source"), Status: c.Query("status"), Page: page, PageSize: size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// GET /plugins/imports/:id
func (h *handler) get(c *gin.Context) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	v, err := h.svc.GetJob(c.Request.Context(), id)
	respond(c, v, err)
}

// GET /plugins/imports/:id/errors.csv —— 错误行报告(行号/列/原因)。
func (h *handler) errorsCSV(c *gin.Context) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	errs, err := h.svc.GetJobErrors(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Data(200, "text/csv; charset=utf-8", BuildErrorsCSV(errs))
}

// Register 注册路由(由 plugin.go 的 module 调用)。
func (h *handler) Register(r *gin.RouterGroup, d *core.Deps) {
	g := r.Group("/plugins/imports")
	g.GET("/sources", core.Perm("imports:list"), h.sources)
	g.GET("/template", core.Perm("imports:list"), h.template)
	g.POST("", core.Perm("imports:run"), h.run)
	g.GET("", core.Perm("imports:list"), h.list)
	g.GET("/:id", core.Perm("imports:list"), h.get)
	g.GET("/:id/errors.csv", core.Perm("imports:list"), h.errorsCSV)
}

var _ = http.StatusOK
