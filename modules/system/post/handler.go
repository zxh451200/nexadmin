package post

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是岗位的 HTTP 层。
type Handler struct{ svc *Service }

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return 0, false
	}
	return id, true
}

// List GET /posts?keyword=
func (h *Handler) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context(), c.Query("keyword"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Options GET /posts/options
func (h *Handler) Options(c *gin.Context) {
	list, err := h.svc.Options(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Get GET /posts/:id
func (h *Handler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	p, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, p)
}

// Create POST /posts
func (h *Handler) Create(c *gin.Context) {
	var in Input
	if !response.BindJSON(c, &in) {
		return
	}
	p, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, p)
}

// Update PUT /posts/:id
func (h *Handler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in Input
	if !response.BindJSON(c, &in) {
		return
	}
	p, err := h.svc.Update(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, p)
}

// Delete DELETE /posts/:id
func (h *Handler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// DeleteBatch DELETE /posts?ids=1,2,3 —— 批量删除。
// ids 走 response.ParseIDsQuery：任一项非法或整体为空都是 400，不会退化成「没带 ids 就动整张表」。
// 逐条护栏与单条删除一致，任一条不合格整批回滚，返回体给出实际删除条数。
func (h *Handler) DeleteBatch(c *gin.Context) {
	ids, err := response.ParseIDsQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeleteMany(c.Request.Context(), ids)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, map[string]int64{"deleted": n})
}

// Sort PUT /posts/sort
func (h *Handler) Sort(c *gin.Context) {
	var in SortInput
	if !response.BindJSON(c, &in) {
		return
	}
	if err := h.svc.Sort(c.Request.Context(), in.IDs); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
