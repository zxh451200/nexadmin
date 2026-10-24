package report

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// ---- 报表 ----

func (h *handler) listReports(c *gin.Context) {
	kw, page, size := h.keywordPage(c)
	list, total, err := h.svc.ListReports(c.Request.Context(), kw, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *handler) getReport(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		v, err := h.svc.GetReport(c.Request.Context(), id)
		respond(c, v, err)
	}
}

func (h *handler) saveReport(id idgen.ID) gin.HandlerFunc {
	return func(c *gin.Context) {
		var f ReportForm
		if !response.BindJSON(c, &f) {
			return
		}
		v, err := h.svc.SaveReport(c.Request.Context(), id, f)
		respond(c, v, err)
	}
}

func (h *handler) updateReport(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		h.saveReport(id)(c)
	}
}

func (h *handler) deleteReport(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		respond(c, nil, h.svc.DeleteReport(c.Request.Context(), id))
	}
}

// ---- 大屏（管理端） ----

func (h *handler) listScreens(c *gin.Context) {
	kw, page, size := h.keywordPage(c)
	list, total, err := h.svc.ListScreensManage(c.Request.Context(), kw, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *handler) getScreen(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		v, err := h.svc.GetScreenManage(c.Request.Context(), id)
		respond(c, v, err)
	}
}

func (h *handler) saveScreen(id idgen.ID) gin.HandlerFunc {
	return func(c *gin.Context) {
		var f ScreenForm
		if !response.BindJSON(c, &f) {
			return
		}
		v, err := h.svc.SaveScreen(c.Request.Context(), id, f)
		respond(c, v, err)
	}
}

func (h *handler) updateScreen(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		h.saveScreen(id)(c)
	}
}

func (h *handler) deleteScreen(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		respond(c, nil, h.svc.DeleteScreen(c.Request.Context(), id))
	}
}

func (h *handler) roles(c *gin.Context) {
	v, err := h.svc.Roles(c.Request.Context())
	respond(c, v, err)
}

// ---- 查看端（report:view） ----

// runBody 是查询/导出/候选值请求体：只有筛选器 key -> 值，没有列名、操作符、SQL。
type runBody struct {
	Filters map[string]any `json:"filters"`
}

func bindRun(c *gin.Context) (runBody, bool) {
	var b runBody
	if c.Request.ContentLength == 0 {
		return b, true
	}
	if !response.BindJSON(c, &b) {
		return b, false
	}
	return b, true
}

func (h *handler) viewReports(c *gin.Context) {
	list, _, err := h.svc.ListReports(c.Request.Context(), "", 1, 200)
	respond(c, list, err)
}

func (h *handler) viewReport(c *gin.Context) { h.getReport(c) }

func (h *handler) viewReportQuery(c *gin.Context) {
	rid, ok1 := pathID(c, "id")
	cid, ok2 := pathID(c, "chartId")
	if !ok1 || !ok2 {
		return
	}
	b, ok := bindRun(c)
	if !ok {
		return
	}
	v, ok := h.viewer(c)
	if !ok {
		return
	}
	res, err := h.svc.QueryReportChart(c.Request.Context(), rid, cid, b.Filters, v)
	respond(c, res, err)
}

func (h *handler) viewReportOptions(c *gin.Context) {
	rid, ok := pathID(c, "id")
	if !ok {
		return
	}
	v, ok := h.viewer(c)
	if !ok {
		return
	}
	res, err := h.svc.ReportFilterOptions(c.Request.Context(), rid, c.Param("key"), v)
	respond(c, res, err)
}

func (h *handler) viewReportExport(c *gin.Context) {
	rid, ok1 := pathID(c, "id")
	cid, ok2 := pathID(c, "chartId")
	if !ok1 || !ok2 {
		return
	}
	b, ok := bindRun(c)
	if !ok {
		return
	}
	v, ok := h.viewer(c)
	if !ok {
		return
	}
	err := h.svc.ExportReportChart(c.Request.Context(), func(f ExportFile) ioWriter {
		hd := c.Writer.Header()
		hd.Set("Content-Type", xlsxContentType)
		hd.Set("Content-Disposition", `attachment; filename="`+asciiName(safeFileName(f.Name))+`.xlsx"; filename*=UTF-8''`+urlEscape(safeFileName(f.Name)+".xlsx"))
		hd.Set("Cache-Control", "no-store")
		if f.Truncated {
			hd.Set("X-Report-Truncated", "1")
		}
		c.Status(http.StatusOK)
		return c.Writer
	}, rid, cid, b.Filters, v)
	if err != nil && !c.Writer.Written() {
		response.Fail(c, err)
	}
}

func (h *handler) viewScreens(c *gin.Context) {
	v, ok := h.viewer(c)
	if !ok {
		return
	}
	list, err := h.svc.ListScreensView(c.Request.Context(), v)
	respond(c, list, err)
}

func (h *handler) viewScreen(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	v, ok := h.viewer(c)
	if !ok {
		return
	}
	sv, err := h.svc.GetScreenView(c.Request.Context(), id, v)
	respond(c, sv, err)
}

func (h *handler) viewScreenQuery(c *gin.Context) {
	sid, ok1 := pathID(c, "id")
	cid, ok2 := pathID(c, "chartId")
	if !ok1 || !ok2 {
		return
	}
	v, ok := h.viewer(c)
	if !ok {
		return
	}
	res, err := h.svc.QueryScreenChart(c.Request.Context(), sid, cid, v)
	respond(c, res, err)
}

// overview 供工作台卡片：本租户报表数、当前账号可见的大屏数。
func (h *handler) overview(c *gin.Context) {
	v, ok := h.viewer(c)
	if !ok {
		return
	}
	_, reports, err := h.svc.ListReports(c.Request.Context(), "", 1, 1)
	if err != nil {
		response.Fail(c, err)
		return
	}
	screens, err := h.svc.ListScreensView(c.Request.Context(), v)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"reports": reports, "screens": len(screens)})
}
