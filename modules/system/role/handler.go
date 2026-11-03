package role

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/core/roleassign"
	"nexadmin/pkg/idgen"
)

type handler struct{ svc *service }

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, notFound())
		return 0, false
	}
	return id, true
}

// list GET /roles?page&pageSize&keyword&status
// 角色页一次拉全量（pageSize=1000），所以这里的上限放宽到 1000。
func (h *handler) list(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("pageSize"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}
	if size > 1000 {
		size = 1000
	}
	list, total, err := h.svc.list(c.Request.Context(), listQuery{
		Page: page, PageSize: size, Keyword: c.Query("keyword"), Status: c.Query("status"),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// options GET /roles/options
// 默认返回全部启用角色（公告目标、SSO/开放平台可见角色等下拉用）；scene=assign（用户表单分配角色）时，
// 非超管只返回他确实能分配的角色（可分配 + 权限子集 + 数据范围不更宽）。
func (h *handler) options(c *gin.Context) {
	var op *roleassign.Operator // 开放平台“应用”没有用户身份，按全部处理（nil）
	if cl := auth.ClaimsFrom(c); cl != nil && cl.Principal != auth.PrincipalApp && c.Query("scene") == "assign" {
		var err error
		if op, err = roleassign.LoadOperator(c.Request.Context(), h.svc.db, cl.UserID, cl.Super); err != nil {
			response.Fail(c, err)
			return
		}
	}
	out, err := h.svc.options(c.Request.Context(), op)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// get GET /roles/:id
func (h *handler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	out, err := h.svc.get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// create POST /roles
func (h *handler) create(c *gin.Context) {
	var in roleInput
	if !response.BindJSON(c, &in) {
		return
	}
	out, err := h.svc.create(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// update PUT /roles/:id
func (h *handler) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in roleInput
	if !response.BindJSON(c, &in) {
		return
	}
	out, err := h.svc.update(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// remove DELETE /roles/:id
func (h *handler) remove(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.remove(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// removeAll DELETE /roles?ids=1,2,3 —— 批量删除。
// ids 由 response.ParseIDsQuery 校验：任一项非法或整体为空都是 400，绝不退化成「不带 ids 就动整张表」。
// 逐条护栏与单条删除一致，任一条不合格整批回滚，返回体给出实际删除条数。
func (h *handler) removeAll(c *gin.Context) {
	ids, err := response.ParseIDsQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.deleteMany(c.Request.Context(), ids)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, map[string]int64{"deleted": n})
}

// sort PUT /roles/sort  body: {ids}
func (h *handler) sort(c *gin.Context) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	if err := h.svc.sort(c.Request.Context(), body.IDs); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// menus GET /roles/:id/menus
func (h *handler) menus(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	out, err := h.svc.menus(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// assignMenus PUT /roles/:id/menus  body: {menuIds}
func (h *handler) assignMenus(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var body struct {
		MenuIDs []string `json:"menuIds"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	if err := h.svc.assignMenus(c.Request.Context(), id, body.MenuIDs); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
