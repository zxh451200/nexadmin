package demotwo

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是 HTTP 层，只做参数解析和响应。
type Handler struct{ svc *Service }

// NewHandler 创建 Handler。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func paramID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return id, true
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// List GET /demotwos
func (h *Handler) List(c *gin.Context) {
	page, size := response.ParsePage(c)
	q := ListQuery{Page: page, PageSize: size}
	q.Title = strings.TrimSpace(c.Query("title"))
	if v := strings.TrimSpace(c.Query("qty")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		q.Qty = &n
	}
	switch c.Query("isHot") {
	case "1", "true":
		b := true
		q.IsHot = &b
	case "0", "false":
		b := false
		q.IsHot = &b
	}
	if v := strings.TrimSpace(c.Query("ownerId")); v != "" {
		id, err := idgen.Parse(v)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		q.OwnerID = &id
	}
	if v := strings.TrimSpace(c.Query("birthday")); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		q.Birthday = &t
	}
	q.Status = strings.TrimSpace(c.Query("status"))
	list, total, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// Get GET /demotwos/:id
func (h *Handler) Get(c *gin.Context) {
	if id, ok := paramID(c); ok {
		v, err := h.svc.Get(c.Request.Context(), id)
		respond(c, v, err)
	}
}

func (h *Handler) save(c *gin.Context, id idgen.ID) {
	var f Form
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.Save(c.Request.Context(), id, f)
	respond(c, v, err)
}

// Create POST /demotwos
func (h *Handler) Create(c *gin.Context) { h.save(c, 0) }

// Update PUT /demotwos/:id
func (h *Handler) Update(c *gin.Context) {
	if id, ok := paramID(c); ok {
		h.save(c, id)
	}
}

// Delete DELETE /demotwos/:id
func (h *Handler) Delete(c *gin.Context) {
	if id, ok := paramID(c); ok {
		respond(c, nil, h.svc.Delete(c.Request.Context(), []idgen.ID{id}))
	}
}

// DeleteBatch DELETE /demotwos?ids=1,2,3
func (h *Handler) DeleteBatch(c *gin.Context) {
	var ids []idgen.ID
	for _, s := range strings.Split(c.Query("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	respond(c, nil, h.svc.Delete(c.Request.Context(), ids))
}
