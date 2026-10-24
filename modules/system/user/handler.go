package user

import (
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

type handler struct {
	svc *service
	log *zap.Logger
}

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, notFound())
		return 0, false
	}
	return id, true
}

// me 取当前登录用户（路由已要求登录）。
func me(c *gin.Context) actor {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return actor{}
	}
	return actor{ID: cl.UserID, Super: cl.Super}
}

func queryOf(c *gin.Context) listQuery {
	page, size := response.ParsePage(c)
	return listQuery{
		Page: page, PageSize: size, Keyword: c.Query("keyword"), Status: c.Query("status"), DeptID: c.Query("deptId"),
		Gender: c.Query("gender"), CreatedFrom: c.Query("createdFrom"), CreatedTo: c.Query("createdTo"),
	}
}

// list GET /users
func (h *handler) list(c *gin.Context) {
	q := queryOf(c)
	list, total, err := h.svc.list(c.Request.Context(), q, me(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, q.Page, q.PageSize)
}

// export GET /users/export：按同样的筛选条件返回全部匹配行。
// 默认返回 JSON 列表（前端自行生成 CSV，保持兼容）；format=xlsx 直接返回 .xlsx 文件。
func (h *handler) export(c *gin.Context) {
	switch strings.ToLower(c.Query("format")) {
	case "", "json":
	case "xlsx":
		h.exportXLSX(c)
		return
	default:
		response.Fail(c, bad("format 仅支持 xlsx"))
		return
	}
	list, err := h.svc.export(c.Request.Context(), queryOf(c), me(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// get GET /users/:id
func (h *handler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	out, err := h.svc.getScoped(c.Request.Context(), id, me(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// create POST /users
func (h *handler) create(c *gin.Context) {
	var in userInput
	if !response.BindJSON(c, &in) {
		return
	}
	out, err := h.svc.create(c.Request.Context(), in, me(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// update PUT /users/:id
func (h *handler) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in userInput
	if !response.BindJSON(c, &in) {
		return
	}
	out, err := h.svc.update(c.Request.Context(), id, in, me(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// remove DELETE /users/:id
func (h *handler) remove(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.remove(c.Request.Context(), []idgen.ID{id}, me(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// removeMany DELETE /users?ids=1,2,3
func (h *handler) removeMany(c *gin.Context) {
	var ids []idgen.ID
	for _, raw := range strings.Split(c.Query("ids"), ",") {
		if raw = strings.TrimSpace(raw); raw == "" {
			continue
		}
		id, err := idgen.Parse(raw)
		if err != nil {
			response.Fail(c, bad("用户 ID 不正确"))
			return
		}
		ids = append(ids, id)
	}
	if err := h.svc.remove(c.Request.Context(), ids, me(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// setStatus PUT /users/:id/status  body: {status}
func (h *handler) setStatus(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	if err := h.svc.setStatus(c.Request.Context(), id, body.Status, me(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// resetPassword POST /users/:id/reset-password  body: {password}
func (h *handler) resetPassword(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	if err := h.svc.resetPassword(c.Request.Context(), id, body.Password, me(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// unlock POST /users/:id/unlock
func (h *handler) unlock(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.unlock(c.Request.Context(), id, me(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
