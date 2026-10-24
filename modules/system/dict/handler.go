package dict

import (
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是字典的 HTTP 层。
type Handler struct{ svc *Service }

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return 0, false
	}
	return id, true
}

// queryIDs 解析 ?ids=1,2,3（批量删除）。
func queryIDs(c *gin.Context) ([]idgen.ID, bool) {
	var ids []idgen.ID
	for _, s := range strings.Split(c.Query("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func done(c *gin.Context, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// ---- 字典类型 ----

func (h *Handler) ListTypes(c *gin.Context) {
	var q ListQuery
	if !response.BindQuery(c, &q) {
		return
	}
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListTypes(c.Request.Context(), q, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *Handler) GetType(c *gin.Context) {
	if id, ok := pathID(c); ok {
		t, err := h.svc.GetType(c.Request.Context(), id)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, t)
	}
}

func (h *Handler) CreateType(c *gin.Context) {
	var in TypeInput
	if !response.BindJSON(c, &in) {
		return
	}
	t, err := h.svc.CreateType(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, t)
}

func (h *Handler) UpdateType(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in TypeInput
	if !response.BindJSON(c, &in) {
		return
	}
	t, err := h.svc.UpdateType(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, t)
}

func (h *Handler) DeleteType(c *gin.Context) {
	if id, ok := pathID(c); ok {
		done(c, h.svc.DeleteTypes(c.Request.Context(), []idgen.ID{id}))
	}
}

func (h *Handler) DeleteTypes(c *gin.Context) {
	if ids, ok := queryIDs(c); ok {
		done(c, h.svc.DeleteTypes(c.Request.Context(), ids))
	}
}

func (h *Handler) SortTypes(c *gin.Context) {
	var in IDsInput
	if response.BindJSON(c, &in) {
		done(c, h.svc.SortTypes(c.Request.Context(), in.IDs))
	}
}

// ---- 字典数据 ----

func (h *Handler) ListData(c *gin.Context) {
	var q ListQuery
	if !response.BindQuery(c, &q) {
		return
	}
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListData(c.Request.Context(), q, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *Handler) GetData(c *gin.Context) {
	if id, ok := pathID(c); ok {
		d, err := h.svc.GetData(c.Request.Context(), id)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, d)
	}
}

func (h *Handler) CreateData(c *gin.Context) {
	var in DataInput
	if !response.BindJSON(c, &in) {
		return
	}
	d, err := h.svc.CreateData(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, d)
}

func (h *Handler) UpdateData(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in DataInput
	if !response.BindJSON(c, &in) {
		return
	}
	d, err := h.svc.UpdateData(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, d)
}

func (h *Handler) DeleteOneData(c *gin.Context) {
	if id, ok := pathID(c); ok {
		done(c, h.svc.DeleteData(c.Request.Context(), []idgen.ID{id}))
	}
}

func (h *Handler) DeleteManyData(c *gin.Context) {
	if ids, ok := queryIDs(c); ok {
		done(c, h.svc.DeleteData(c.Request.Context(), ids))
	}
}

func (h *Handler) SortData(c *gin.Context) {
	var in IDsInput
	if response.BindJSON(c, &in) {
		done(c, h.svc.SortData(c.Request.Context(), in.IDs))
	}
}

// Items GET /dicts/:code/items —— 前端下拉/标签取数，只要登录；Accept-Language 为 en* 时返回英文标签。
func (h *Handler) Items(c *gin.Context) {
	english := strings.HasPrefix(strings.ToLower(c.GetHeader("Accept-Language")), "en")
	list, err := h.svc.Items(c.Request.Context(), c.Param("code"), english)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}
