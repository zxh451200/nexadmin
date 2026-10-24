package tenant

import (
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是租户与套餐的 HTTP 层。
type Handler struct{ svc *Service }

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return 0, false
	}
	return id, true
}

// queryIDs 解析 ?ids=1,2,3。
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

func result(c *gin.Context, v any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// ---- 套餐 ----

func (h *Handler) ListPackages(c *gin.Context) {
	var q ListQuery
	if !response.BindQuery(c, &q) {
		return
	}
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListPackages(c.Request.Context(), q, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *Handler) PackageOptions(c *gin.Context) {
	v, err := h.svc.PackageOptions(c.Request.Context(), c.Query("keyword"))
	result(c, v, err)
}

func (h *Handler) GetPackage(c *gin.Context) {
	if id, ok := pathID(c); ok {
		v, err := h.svc.GetPackage(c.Request.Context(), id)
		result(c, v, err)
	}
}

func (h *Handler) CreatePackage(c *gin.Context) {
	var in PackageInput
	if response.BindJSON(c, &in) {
		v, err := h.svc.CreatePackage(c.Request.Context(), in)
		result(c, v, err)
	}
}

func (h *Handler) UpdatePackage(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in PackageInput
	if response.BindJSON(c, &in) {
		v, err := h.svc.UpdatePackage(c.Request.Context(), id, in)
		result(c, v, err)
	}
}

func (h *Handler) DeletePackage(c *gin.Context) {
	if id, ok := pathID(c); ok {
		result(c, nil, h.svc.DeletePackages(c.Request.Context(), []idgen.ID{id}))
	}
}

func (h *Handler) DeletePackages(c *gin.Context) {
	if ids, ok := queryIDs(c); ok {
		result(c, nil, h.svc.DeletePackages(c.Request.Context(), ids))
	}
}

// ---- 租户 ----

func (h *Handler) ListTenants(c *gin.Context) {
	var q ListQuery
	if !response.BindQuery(c, &q) {
		return
	}
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListTenants(c.Request.Context(), q, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *Handler) GetTenant(c *gin.Context) {
	if id, ok := pathID(c); ok {
		v, err := h.svc.GetTenant(c.Request.Context(), id)
		result(c, v, err)
	}
}

// Usage GET /tenants/:id/usage：租户用量与到期信息。
func (h *Handler) Usage(c *gin.Context) {
	if id, ok := pathID(c); ok {
		v, err := h.svc.Usage(c.Request.Context(), id)
		result(c, v, err)
	}
}

func (h *Handler) CreateTenant(c *gin.Context) {
	var in TenantInput
	if response.BindJSON(c, &in) {
		v, err := h.svc.CreateTenant(c.Request.Context(), in)
		result(c, v, err)
	}
}

func (h *Handler) UpdateTenant(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in TenantInput
	if response.BindJSON(c, &in) {
		v, err := h.svc.UpdateTenant(c.Request.Context(), id, in)
		result(c, v, err)
	}
}

func (h *Handler) Renew(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in struct {
		ExpireAt string `json:"expireAt" binding:"required"`
	}
	if response.BindJSON(c, &in) {
		v, err := h.svc.Renew(c.Request.Context(), id, in.ExpireAt)
		result(c, v, err)
	}
}

func (h *Handler) SetStatus(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status" binding:"required,oneof=active suspended expired"`
	}
	if response.BindJSON(c, &in) {
		v, err := h.svc.SetStatus(c.Request.Context(), id, in.Status)
		result(c, v, err)
	}
}

func (h *Handler) DeleteTenant(c *gin.Context) {
	if id, ok := pathID(c); ok {
		result(c, nil, h.svc.DeleteTenants(c.Request.Context(), []idgen.ID{id}))
	}
}

func (h *Handler) DeleteTenants(c *gin.Context) {
	if ids, ok := queryIDs(c); ok {
		result(c, nil, h.svc.DeleteTenants(c.Request.Context(), ids))
	}
}
