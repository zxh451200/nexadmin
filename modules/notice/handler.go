package notice

import (
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是公告的 HTTP 层，只做参数解析和响应。
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func paramID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return id, true
}

// canPlatform 超管或拥有 notice:platform 权限点才能发平台公告。
func canPlatform(c *gin.Context, cl *auth.Claims) bool {
	if cl.Super {
		return true
	}
	if svc := auth.Default(); svc != nil && svc.Provider() != nil {
		if u, err := svc.Profile(c.Request.Context(), cl); err == nil {
			return auth.HasPerm(u.Perms, "notice:platform")
		}
	}
	return false
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

// guard 校验管理端对这些公告的访问在数据范围内；不通过时已写入响应。
func (h *Handler) guard(c *gin.Context, ids ...idgen.ID) bool {
	sc, ok := h.scope(c)
	if !ok {
		return false
	}
	if err := h.svc.CheckIDs(c.Request.Context(), sc, ids...); err != nil {
		response.Fail(c, err)
		return false
	}
	return true
}

// List GET /notices
func (h *Handler) List(c *gin.Context) {
	page, size := response.ParsePage(c)
	q := ListQuery{Keyword: strings.TrimSpace(c.Query("keyword")), Type: c.Query("type"), Level: c.Query("level"),
		Status: c.Query("status"), Page: page, PageSize: size}
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	q.Scope = sc
	list, total, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// Get GET /notices/:id
func (h *Handler) Get(c *gin.Context) {
	if id, ok := paramID(c); ok && h.guard(c, id) {
		v, err := h.svc.Get(c.Request.Context(), id)
		respond(c, v, err)
	}
}

// ReadStats GET /notices/:id/read-stats
func (h *Handler) ReadStats(c *gin.Context) {
	if id, ok := paramID(c); ok && h.guard(c, id) {
		v, err := h.svc.ReadStats(c.Request.Context(), id)
		respond(c, v, err)
	}
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

func (h *Handler) save(c *gin.Context, id idgen.ID) {
	var f Form
	if !response.BindJSON(c, &f) {
		return
	}
	cl := auth.ClaimsFrom(c)
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	if err := h.svc.CheckAudience(c.Request.Context(), sc, f); err != nil {
		response.Fail(c, err)
		return
	}
	v, err := h.svc.Save(c.Request.Context(), id, f, cl.UserID, canPlatform(c, cl))
	respond(c, v, err)
}

// Create POST /notices
func (h *Handler) Create(c *gin.Context) { h.save(c, 0) }

// Update PUT /notices/:id
func (h *Handler) Update(c *gin.Context) {
	if id, ok := paramID(c); ok && h.guard(c, id) {
		h.save(c, id)
	}
}

// Publish POST /notices/:id/publish
func (h *Handler) Publish(c *gin.Context) {
	if id, ok := paramID(c); ok && h.guard(c, id) {
		v, err := h.svc.Publish(c.Request.Context(), id)
		respond(c, v, err)
	}
}

// Revoke POST /notices/:id/revoke
func (h *Handler) Revoke(c *gin.Context) {
	if id, ok := paramID(c); ok && h.guard(c, id) {
		v, err := h.svc.Revoke(c.Request.Context(), id)
		respond(c, v, err)
	}
}

// Delete DELETE /notices/:id（软删除，进回收站）
func (h *Handler) Delete(c *gin.Context) {
	if id, ok := paramID(c); ok && h.guard(c, id) {
		respond(c, nil, h.svc.Delete(c.Request.Context(), []idgen.ID{id}, auth.ClaimsFrom(c).Username))
	}
}

// DeleteBatch DELETE /notices?ids=1,2,3
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
	if !h.guard(c, ids...) {
		return
	}
	respond(c, nil, h.svc.Delete(c.Request.Context(), ids, auth.ClaimsFrom(c).Username))
}

// Popup GET /notices/popup
func (h *Handler) Popup(c *gin.Context) {
	list, err := h.svc.Popup(c.Request.Context(), auth.ClaimsFrom(c).UserID)
	respond(c, list, err)
}

// Mine GET /notices/mine?unread=1&type=&keyword=
func (h *Handler) Mine(c *gin.Context) {
	page, size := response.ParsePage(c)
	unread := c.Query("unread") == "1" || c.Query("unread") == "true"
	list, total, err := h.svc.Mine(c.Request.Context(), auth.ClaimsFrom(c).UserID,
		strings.TrimSpace(c.Query("keyword")), c.Query("type"), unread, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// ClearMine DELETE /notices/mine：清空“我的公告”（只影响当前用户，公告本身不动）
func (h *Handler) ClearMine(c *gin.Context) {
	respond(c, nil, h.svc.Clear(c.Request.Context(), auth.ClaimsFrom(c).UserID))
}

// MarkRead POST /notices/:id/read
func (h *Handler) MarkRead(c *gin.Context) {
	if id, ok := paramID(c); ok {
		respond(c, nil, h.svc.MarkRead(c.Request.Context(), auth.ClaimsFrom(c).UserID, id))
	}
}
