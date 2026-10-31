package imports

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路由前缀:/api/v1/plugins/imports。权限码:imports:list 查看来源与台账;imports:run 执行导入。
// 租户级数据(台账按租户隔离),租户隔离自动生效。

const xlsxMIME = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

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
	response.OK(c, ListSources(c.Request.Context()))
}

// GET /plugins/imports/template?source=products&format=csv|xlsx —— 下载导入模板(表头 + 示例行)。
func (h *handler) template(c *gin.Context) {
	src, err := getSource(c.Query("source"))
	if err != nil {
		response.Fail(c, response.BadRequest("imports.source_unknown").Wrap(err))
		return
	}
	format := strings.ToLower(strings.TrimSpace(c.Query("format")))
	if format == "" {
		format = "csv"
	}
	switch format {
	case "csv":
		attachment(c, "import-template-"+src.Name+".csv", "text/csv; charset=utf-8", BuildTemplateCSV(c.Request.Context(), src))
	case "xlsx":
		body, err := BuildTemplateXLSX(c.Request.Context(), src)
		if err != nil {
			response.Fail(c, response.New(http.StatusInternalServerError, "imports.template_failed").Wrap(err))
			return
		}
		attachment(c, "import-template-"+src.Name+".xlsx", xlsxMIME, body)
	default:
		response.Fail(c, response.BadRequest("imports.format_unsupported"))
	}
}

// attachment 让浏览器把响应存成文件,而不是在新标签页里内联展示 CSV 文本。
func attachment(c *gin.Context, filename, contentType string, body []byte) {
	c.Header("Content-Disposition",
		mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	c.Data(200, contentType, body)
}

// POST /plugins/imports —— multipart:file(文件)+ source(来源标识)。同步执行,返回台账。
func (h *handler) run(c *gin.Context) {
	// 先卡住整个请求体，避免超大 multipart 在拒绝前被写到磁盘临时目录。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBytes+(1<<20))
	fh, err := c.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.Fail(c, response.BadRequest("imports.file_too_large"))
		} else {
			response.Fail(c, response.BadRequest("imports.file_required"))
		}
		return
	}
	source := c.PostForm("source")
	if fh.Size > MaxBytes {
		response.Fail(c, response.BadRequest("imports.file_too_large"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, response.BadRequest("imports.file_required"))
		return
	}
	defer func() { _ = f.Close() }()
	// 服务层还有一次大小校验；这里先按上限截断读取，避免超大文件在拒绝前就被读进内存。
	content, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		response.Fail(c, response.BadRequest("imports.file_required"))
		return
	}
	if len(content) > MaxBytes {
		response.Fail(c, response.BadRequest("imports.file_too_large"))
		return
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

// DELETE /plugins/imports/:id —— 删除导入台账（不影响已导入的数据）。
func (h *handler) remove(c *gin.Context) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	respond(c, nil, h.svc.DeleteJob(c.Request.Context(), id))
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
	attachment(c, fmt.Sprintf("import-errors-%d.csv", int64(id)), "text/csv; charset=utf-8", BuildErrorsCSV(errs))
}

// Register 注册路由(由 plugin.go 的 module 调用)。
func (h *handler) Register(r *gin.RouterGroup, d *core.Deps) {
	g := r.Group("/plugins/imports")
	g.GET("/sources", core.Perm("imports:list"), h.sources)
	g.GET("/template", core.Perm("imports:list"), h.template)
	g.POST("", core.Perm("imports:run"), h.run)
	// 删除台账沿用「执行导入」权限：能导入的人就能清理自己提交的记录
	g.DELETE("/:id", core.Perm("imports:run"), h.remove)
	g.GET("", core.Perm("imports:list"), h.list)
	g.GET("/:id", core.Perm("imports:list"), h.get)
	g.GET("/:id/errors.csv", core.Perm("imports:list"), h.errorsCSV)
}

var _ = http.StatusOK
