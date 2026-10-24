package recycle

import (
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是回收站的 HTTP 层。
type Handler struct{ svc *Service }

// List GET /recycle?type=&keyword=&page=&pageSize=
func (h *Handler) List(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.List(c.Request.Context(), c.Query("type"), strings.TrimSpace(c.Query("keyword")), page, size)
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
	h.act(c, func(s *Service, c *gin.Context, id idgen.ID) error { return s.Restore(c.Request.Context(), id) })
}

// Purge DELETE /recycle/:id
func (h *Handler) Purge(c *gin.Context) {
	h.act(c, func(s *Service, c *gin.Context, id idgen.ID) error { return s.Purge(c.Request.Context(), id) })
}
