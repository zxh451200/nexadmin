package log

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是日志管理的 HTTP 层。
type Handler struct{ svc *Service }

// maxPageSize 比通用分页上限（200）大（历史上前端导出用它一次取 5000 条；现在导出走服务端接口，保留以兼容已有调用方）。
const maxPageSize = 5000

func query(c *gin.Context) Query {
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("pageSize"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return Query{Keyword: c.Query("keyword"), Status: c.Query("status"), Type: c.Query("type"), LoginType: c.Query("loginType"),
		StartDate: c.Query("startDate"), EndDate: c.Query("endDate"), Page: page, PageSize: size}
}

// scope 解析操作者的数据范围；失败时已写入响应。
func (h *Handler) scope(c *gin.Context) (*datascope.Scope, bool) {
	sc, err := datascope.FromClaims(c.Request.Context(), h.svc.db, auth.ClaimsFrom(c))
	if err != nil {
		response.Fail(c, err)
		return nil, false
	}
	return sc, true
}

// ListOper GET /oper-logs
func (h *Handler) ListOper(c *gin.Context) {
	q := query(c)
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	q.Scope = sc
	list, total, err := h.svc.ListOper(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, q.Page, q.PageSize)
}

// ListLogin GET /login-logs
func (h *Handler) ListLogin(c *gin.Context) {
	q := query(c)
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	q.Scope = sc
	list, total, err := h.svc.ListLogin(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, q.Page, q.PageSize)
}

// idsParam 解析 ?ids=1,2,3；为空或有非法值时已写入 400。
func idsParam(c *gin.Context) ([]idgen.ID, bool) {
	var ids []idgen.ID
	for _, s := range strings.Split(c.Query("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return nil, false
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return nil, false
	}
	return ids, true
}

// DeleteOper DELETE /oper-logs?ids=
func (h *Handler) DeleteOper(c *gin.Context) {
	if ids, ok := idsParam(c); ok {
		sc, ok := h.scope(c)
		if !ok {
			return
		}
		if err := h.svc.CheckOperIDs(c.Request.Context(), sc, ids); err != nil {
			response.Fail(c, err)
			return
		}
		if err := h.svc.DeleteOper(c.Request.Context(), ids); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// DeleteLogin DELETE /login-logs?ids=
func (h *Handler) DeleteLogin(c *gin.Context) {
	if ids, ok := idsParam(c); ok {
		sc, ok := h.scope(c)
		if !ok {
			return
		}
		if err := h.svc.CheckLoginIDs(c.Request.Context(), sc, ids); err != nil {
			response.Fail(c, err)
			return
		}
		if err := h.svc.DeleteLogin(c.Request.Context(), ids); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// CleanOper POST /oper-logs/clean  body: {days}，days=0 清空。
func (h *Handler) CleanOper(c *gin.Context) {
	var body struct {
		Days int `json:"days" binding:"min=0,max=36500"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	if err := h.svc.CheckCleanOper(c.Request.Context(), sc, body.Days); err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.CleanOper(c.Request.Context(), body.Days)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"removed": n})
}
