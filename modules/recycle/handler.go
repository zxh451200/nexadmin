package recycle

import (
	"nexadmin/core"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是回收站的 HTTP 层。
type Handler struct{ svc *Service }

// List GET /recycle?type=&keyword=&from=&to=&page=&pageSize=
// from/to 是删除日期（YYYY-MM-DD，含当天），任一可省。
func (h *Handler) List(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.List(c.Request.Context(), ListFilter{
		Type:    c.Query("type"),
		Keyword: strings.TrimSpace(c.Query("keyword")),
		From:    c.Query("from"),
		To:      c.Query("to"),
		Page:    page,
		Size:    size,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *Handler) act(c *gin.Context, fn func(*Service, *gin.Context, idgen.ID) error) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	if err := fn(h.svc, c, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Restore POST /recycle/:id/restore
func (h *Handler) Restore(c *gin.Context) {
	h.act(c, func(s *Service, c *gin.Context, id idgen.ID) error {
		return s.Restore(c.Request.Context(), id, core.IsPlatformCaller(c))
	})
}

// Purge DELETE /recycle/:id
func (h *Handler) Purge(c *gin.Context) {
	h.act(c, func(s *Service, c *gin.Context, id idgen.ID) error {
		return s.Purge(c.Request.Context(), id, core.IsPlatformCaller(c))
	})
}

// BatchForm 是批量还原 / 批量彻底删除的入参。
type BatchForm struct {
	IDs []idgen.ID `json:"ids" binding:"required,min=1,max=200"`
}

// RestoreBatch POST /recycle/restore —— 批量还原，冲突与源行消失按条计数返回。
func (h *Handler) RestoreBatch(c *gin.Context) {
	var f BatchForm
	if !response.BindJSON(c, &f) {
		return
	}
	res, err := h.svc.RestoreBatch(c.Request.Context(), f.IDs, core.IsPlatformCaller(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// PurgeBatch DELETE /recycle?ids=1,2,3 —— 批量彻底删除。
func (h *Handler) PurgeBatch(c *gin.Context) {
	var ids []idgen.ID
	for _, s := range strings.Split(c.Query("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil || id == 0 {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	res, err := h.svc.PurgeBatch(c.Request.Context(), ids, core.IsPlatformCaller(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}
