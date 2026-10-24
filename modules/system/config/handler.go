package config

import (
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	tn "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// platformCaller 报告调用方是否为平台级：超管，或平台（默认）租户。
// 全局 AI 配置（base_url / api_key / model）是平台级的，租户管理员不能修改，也看不到地址。
func platformCaller(c *gin.Context) bool {
	if cl := auth.ClaimsFrom(c); cl != nil && cl.Super {
		return true
	}
	tid, ok := tn.From(c.Request.Context())
	return ok && tid == tn.DefaultID
}

// platformOnlySection 是只允许平台级调用方修改 / 测试的分区。
func platformOnlySection(name string) bool { return name == "ai" }

// restrictAI 把 GET /settings 里的 ai 分区缩减成租户可见的最小信息（不含地址与密钥）。
func restrictAI(all map[string]any) {
	if ai, ok := all["ai"].(map[string]any); ok {
		all["ai"] = map[string]any{"enabled": ai["enabled"], "model": ai["model"]}
	}
}

// Handler 是参数与设置页的 HTTP 层。
type Handler struct{ svc *Service }

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return 0, false
	}
	return id, true
}

func done(c *gin.Context, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// List GET /configs
func (h *Handler) List(c *gin.Context) {
	var q ListQuery
	if !response.BindQuery(c, &q) {
		return
	}
	page, size := response.ParsePage(c)
	list, total, err := h.svc.List(c.Request.Context(), q, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// Get GET /configs/:id
func (h *Handler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Create POST /configs
func (h *Handler) Create(c *gin.Context) {
	var in Input
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Update PUT /configs/:id
func (h *Handler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in Input
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.Update(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// DeleteOne DELETE /configs/:id
func (h *Handler) DeleteOne(c *gin.Context) {
	if id, ok := pathID(c); ok {
		done(c, h.svc.Delete(c.Request.Context(), []idgen.ID{id}))
	}
}

// DeleteMany DELETE /configs?ids=1,2
func (h *Handler) DeleteMany(c *gin.Context) {
	var ids []idgen.ID
	for _, s := range strings.Split(c.Query("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
			return
		}
		ids = append(ids, id)
	}
	done(c, h.svc.Delete(c.Request.Context(), ids))
}

// GetUpload GET /configs/upload 与 GET /config/upload（后者供上传组件读取，登录即可）
func (h *Handler) GetUpload(c *gin.Context) {
	v, err := h.svc.Upload(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// SaveUpload PUT /configs/upload
func (h *Handler) SaveUpload(c *gin.Context) {
	var in UploadSettings
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.SaveUpload(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// UploadTypes GET /configs/upload-types
func (h *Handler) UploadTypes(c *gin.Context) { response.OK(c, uploadTypeOptions) }

// RefreshCache POST /configs/refresh-cache：参数每次直接读库；唯一的进程内缓存是上传限制（几秒短缓存），这里立即丢弃它。
func (h *Handler) RefreshCache(c *gin.Context) {
	h.svc.invalidateUpload()
	response.OK(c, nil)
}

// ---- 设置页 ----

// Settings GET /settings
func (h *Handler) Settings(c *gin.Context) {
	v, err := h.svc.AllSettings(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !platformCaller(c) {
		restrictAI(v)
	}
	response.OK(c, v)
}

// PublicSettings GET /settings/public（匿名）
func (h *Handler) PublicSettings(c *gin.Context) {
	v, err := h.svc.Public(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// SaveSection PUT /settings/:section
func (h *Handler) SaveSection(c *gin.Context) {
	if platformOnlySection(c.Param("section")) && !platformCaller(c) {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return
	}
	var body map[string]any
	if !response.BindJSON(c, &body) {
		return
	}
	v, err := h.svc.SaveSection(c.Request.Context(), c.Param("section"), body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// TestSection POST /settings/:section/test
func (h *Handler) TestSection(c *gin.Context) {
	if platformOnlySection(c.Param("section")) && !platformCaller(c) {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return
	}
	var body map[string]any
	if !response.BindJSON(c, &body) {
		return
	}
	v, err := h.svc.TestSection(c.Request.Context(), c.Param("section"), body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
