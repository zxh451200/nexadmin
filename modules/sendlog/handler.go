package sendlog

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是发送日志的 HTTP 层。所有接口都经 core.Perm 登记权限，数据只限当前租户（见 Service）。
type Handler struct{ svc *Service }

func badRequest() *response.Error { return response.BadRequest("error.bad_request") }

func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// List GET /sendlogs?channel=&status=&recipient=&startDate=&endDate=&page=&pageSize=
func (h *Handler) List(c *gin.Context) {
	page, size := response.ParsePage(c)
	q := Query{Channel: c.Query("channel"), Status: c.Query("status"), Recipient: c.Query("recipient"),
		StartDate: c.Query("startDate"), EndDate: c.Query("endDate"), Page: page, PageSize: size}
	if (q.Channel != "" && !oneOf(q.Channel, channels)) || (q.Status != "" && !oneOf(q.Status, statuses)) ||
		!validDate(q.StartDate) || !validDate(q.EndDate) {
		response.Fail(c, badRequest())
		return
	}
	list, total, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// Get GET /sendlogs/:id
func (h *Handler) Get(c *gin.Context) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	v, err := h.svc.Get(c.Request.Context(), id)
	switch {
	case err == nil:
		response.OK(c, v)
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.Fail(c, response.NotFound("error.not_found"))
	default:
		response.Fail(c, err)
	}
}

// idsParam 解析 ?ids=1,2,3；为空、有非法值或超过 maxDeleteIDs 个时已写入 400。
func idsParam(c *gin.Context) ([]idgen.ID, bool) {
	var ids []idgen.ID
	for _, s := range strings.Split(c.Query("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil {
			response.Fail(c, badRequest())
			return nil, false
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > maxDeleteIDs {
		response.Fail(c, badRequest())
		return nil, false
	}
	return ids, true
}

// Delete DELETE /sendlogs?ids=1,2,3
func (h *Handler) Delete(c *gin.Context) {
	ids, ok := idsParam(c)
	if !ok {
		return
	}
	n, err := h.svc.Delete(c.Request.Context(), ids)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"removed": n})
}

// Clean POST /sendlogs/clean  body: {days}，清理 days 天前的记录；days 必须 >= 1（不提供“全部清空”，避免空 body 误删）。
func (h *Handler) Clean(c *gin.Context) {
	var body struct {
		Days int `json:"days" binding:"required,min=1,max=36500"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	n, err := h.svc.Clean(c.Request.Context(), body.Days)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"removed": n})
}
