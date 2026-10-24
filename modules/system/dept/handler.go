package dept

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是部门的 HTTP 层，只做参数解析与响应。
type Handler struct{ svc *Service }

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return 0, false
	}
	return id, true
}

// scope 解析当前操作者的数据范围；失败时已写入响应。
func (h *Handler) scope(c *gin.Context) (*datascope.Scope, bool) {
	sc, err := datascope.FromClaims(c.Request.Context(), h.svc.db, auth.ClaimsFrom(c))
	if err != nil {
		response.Fail(c, err)
		return nil, false
	}
	return sc, true
}

// readScope 返回读取接口用的范围：仅 scene=manage 时解析（可能受限），否则返回 nil（不裁剪）。
func (h *Handler) readScope(c *gin.Context) (*datascope.Scope, bool) {
	if c.Query("scene") != sceneManage {
		return nil, true
	}
	return h.scope(c)
}

// Tree GET /depts/tree
// 带 scene=manage（部门管理页）时按数据范围裁剪；不带 scene 的下拉调用返回完整树。
func (h *Handler) Tree(c *gin.Context) {
	sc, ok := h.readScope(c)
	if !ok {
		return
	}
	list, err := h.svc.TreeScoped(c.Request.Context(), sc)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Get GET /depts/:id
func (h *Handler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	sc, ok := h.readScope(c)
	if !ok {
		return
	}
	d, err := h.svc.GetScoped(c.Request.Context(), sc, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, d)
}

// Create POST /depts
func (h *Handler) Create(c *gin.Context) {
	var in Input
	if !response.BindJSON(c, &in) {
		return
	}
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	if err := h.svc.CheckCreate(c.Request.Context(), sc, in); err != nil {
		response.Fail(c, err)
		return
	}
	d, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, d)
}

// Update PUT /depts/:id
func (h *Handler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in Input
	if !response.BindJSON(c, &in) {
		return
	}
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	if err := h.svc.CheckUpdate(c.Request.Context(), sc, id, in); err != nil {
		response.Fail(c, err)
		return
	}
	d, err := h.svc.Update(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, d)
}

// Delete DELETE /depts/:id
func (h *Handler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	if err := h.svc.CheckDelete(c.Request.Context(), sc, id); err != nil {
		response.Fail(c, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Sort PUT /depts/sort
func (h *Handler) Sort(c *gin.Context) {
	var in SortInput
	if !response.BindJSON(c, &in) {
		return
	}
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	if err := h.svc.CheckSort(c.Request.Context(), sc, in); err != nil {
		response.Fail(c, err)
		return
	}
	if err := h.svc.Sort(c.Request.Context(), in); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
