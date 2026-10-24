package report

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路径前缀：/api/v1/plugins/report。权限码：
//   report:dataset 数据集管理（含可选数据表白名单）；report:chart 图表；report:report 报表；
//   report:screen 大屏管理（拥有它可查看全部大屏）；report:view 查看报表与已授权的大屏、导出。

type handler struct {
	svc *Service
	db  *gorm.DB
	// 以下两个函数可在测试里替换。
	scopeFn func(c *gin.Context, cl *auth.Claims) (*datascope.Scope, error)
	permFn  func(c *gin.Context, code string) bool
}

func newHandler(svc *Service, db *gorm.DB) *handler {
	h := &handler{svc: svc, db: db}
	h.scopeFn = func(c *gin.Context, cl *auth.Claims) (*datascope.Scope, error) {
		return datascope.FromClaims(c.Request.Context(), db, cl)
	}
	h.permFn = defaultHasPerm
	return h
}

func defaultHasPerm(c *gin.Context, code string) bool {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return false
	}
	if cl.Super {
		return true
	}
	if cl.Principal == auth.PrincipalApp {
		return auth.HasPerm(cl.Perms, code)
	}
	svc := auth.Default()
	if svc == nil {
		return false
	}
	u, err := svc.Profile(c.Request.Context(), cl)
	return err == nil && auth.HasPerm(u.Perms, code)
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// limitBody 限制请求体大小，防止超大 JSON。
func limitBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		c.Next()
	}
}

func pathID(c *gin.Context, name string) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param(name))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return id, true
}

// viewer 解析操作者身份、数据范围与是否大屏管理者。
func (h *handler) viewer(c *gin.Context) (Viewer, bool) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return Viewer{}, false
	}
	sc, err := h.scopeFn(c, cl)
	if err != nil {
		response.Fail(c, response.Internal().Wrap(err))
		return Viewer{}, false
	}
	return Viewer{UserID: cl.UserID, Super: cl.Super, Manager: h.permFn(c, "report:screen"), Scope: sc}, true
}

func (h *handler) keywordPage(c *gin.Context) (string, int, int) {
	page, size := response.ParsePage(c)
	return strings.TrimSpace(c.Query("keyword")), page, size
}

// ---- 数据集 ----

func (h *handler) sources(c *gin.Context) { response.OK(c, h.svc.Sources()) }

func (h *handler) listDatasets(c *gin.Context) {
	kw, page, size := h.keywordPage(c)
	list, total, err := h.svc.ListDatasets(c.Request.Context(), kw, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *handler) datasetOptions(c *gin.Context) {
	list, err := h.svc.ListDatasetOptions(c.Request.Context())
	respond(c, list, err)
}

func (h *handler) getDataset(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		v, err := h.svc.GetDataset(c.Request.Context(), id)
		respond(c, v, err)
	}
}

func (h *handler) saveDataset(id idgen.ID) gin.HandlerFunc {
	return func(c *gin.Context) {
		var f DatasetForm
		if !response.BindJSON(c, &f) {
			return
		}
		v, err := h.svc.SaveDataset(c.Request.Context(), id, f)
		respond(c, v, err)
	}
}

func (h *handler) updateDataset(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		h.saveDataset(id)(c)
	}
}

func (h *handler) deleteDataset(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		respond(c, nil, h.svc.DeleteDataset(c.Request.Context(), id))
	}
}

func (h *handler) previewDataset(c *gin.Context) {
	var f DatasetForm
	if !response.BindJSON(c, &f) {
		return
	}
	v, ok := h.viewer(c)
	if !ok {
		return
	}
	res, err := h.svc.PreviewDataset(c.Request.Context(), f, v)
	respond(c, res, err)
}

// ---- 图表 ----

func (h *handler) listCharts(c *gin.Context) {
	kw, page, size := h.keywordPage(c)
	var ds idgen.ID
	if s := c.Query("datasetId"); s != "" {
		var err error
		if ds, err = idgen.Parse(s); err != nil {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
	}
	list, total, err := h.svc.ListCharts(c.Request.Context(), kw, ds, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *handler) chartOptions(c *gin.Context) {
	list, _, err := h.svc.ListCharts(c.Request.Context(), "", 0, 1, 200)
	respond(c, list, err)
}

func (h *handler) getChart(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		v, err := h.svc.GetChart(c.Request.Context(), id)
		respond(c, v, err)
	}
}

func (h *handler) saveChart(id idgen.ID) gin.HandlerFunc {
	return func(c *gin.Context) {
		var f ChartForm
		if !response.BindJSON(c, &f) {
			return
		}
		v, err := h.svc.SaveChart(c.Request.Context(), id, f)
		respond(c, v, err)
	}
}

func (h *handler) updateChart(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		h.saveChart(id)(c)
	}
}

func (h *handler) deleteChart(c *gin.Context) {
	if id, ok := pathID(c, "id"); ok {
		respond(c, nil, h.svc.DeleteChart(c.Request.Context(), id))
	}
}

func (h *handler) previewChart(c *gin.Context) {
	var f ChartForm
	if !response.BindJSON(c, &f) {
		return
	}
	v, ok := h.viewer(c)
	if !ok {
		return
	}
	res, err := h.svc.PreviewChart(c.Request.Context(), f, v)
	respond(c, res, err)
}

func (h *handler) queryChart(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	v, ok := h.viewer(c)
	if !ok {
		return
	}
	res, err := h.svc.QueryChart(c.Request.Context(), id, v)
	respond(c, res, err)
}
