package menu

import (
	"errors"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

type handler struct{ svc *service }

// pathID 解析路由里的 :id。
func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, notFound())
		return 0, false
	}
	return id, true
}

// anyPerm 满足任一权限码即可通过（超管直接通过）；同时把权限码登记到框架。
func anyPerm(codes ...string) gin.HandlerFunc {
	for _, code := range codes {
		auth.RegisterPerm(code)
	}
	return func(c *gin.Context) {
		cl := auth.ClaimsFrom(c)
		if cl == nil {
			response.Fail(c, response.Unauthorized("error.unauthorized"))
			return
		}
		if !cl.Super {
			svc := auth.Default()
			if svc == nil {
				response.Fail(c, response.Unavailable("auth.provider_missing"))
				return
			}
			u, err := svc.Profile(c.Request.Context(), cl)
			if errors.Is(err, auth.ErrNoProvider) {
				response.Fail(c, response.Unavailable("auth.provider_missing"))
				return
			}
			if err != nil {
				response.Fail(c, err)
				return
			}
			ok := false
			for _, code := range codes {
				ok = ok || auth.HasPerm(u.Perms, code)
			}
			if !ok {
				response.Fail(c, response.Forbidden("error.forbidden"))
				return
			}
		}
		c.Next()
	}
}

// tree GET /menus/tree
func (h *handler) tree(c *gin.Context) {
	out, err := h.svc.tree(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// mine GET /menus/mine：按当前用户的权限过滤。
func (h *handler) mine(c *gin.Context) {
	cl := auth.ClaimsFrom(c)
	svc := auth.Default()
	if cl == nil || svc == nil {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	u, err := svc.Profile(c.Request.Context(), cl)
	if errors.Is(err, auth.ErrNoProvider) {
		response.Fail(c, response.Unavailable("auth.provider_missing"))
		return
	}
	if err != nil {
		response.Fail(c, err)
		return
	}
	out, err := h.svc.mine(c.Request.Context(), u.Perms)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// get GET /menus/:id
func (h *handler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	m, err := h.svc.get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, toDTO(m))
}

// create POST /menus
func (h *handler) create(c *gin.Context) {
	var in menuInput
	if !response.BindJSON(c, &in) {
		return
	}
	m, err := h.svc.create(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, toDTO(m))
}

// update PUT /menus/:id
func (h *handler) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in menuInput
	if !response.BindJSON(c, &in) {
		return
	}
	m, err := h.svc.update(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, toDTO(m))
}

// remove DELETE /menus/:id
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

// sort PUT /menus/sort
func (h *handler) sort(c *gin.Context) {
	var in sortInput
	if !response.BindJSON(c, &in) {
		return
	}
	if err := h.svc.sort(c.Request.Context(), in); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// move PUT /menus/:id/move
func (h *handler) move(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in moveInput
	if !response.BindJSON(c, &in) {
		return
	}
	if err := h.svc.move(c.Request.Context(), id, in); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
