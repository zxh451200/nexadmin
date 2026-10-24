package security

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 路径前缀：/api/v1/plugins/security。
// 权限码：security:self 个人 2FA；security:policy 强制策略；security:user 查看用户 2FA 状态；
// security:reset 重置某用户 2FA；security:ip 查看 IP 规则；security:ip:edit 增删改与启停；security:log 管理操作日志。

type handler struct {
	svc     *Service
	profile func(ctx context.Context, id idgen.ID) (*auth.User, error)
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// me 读取当前登录用户；角色来自 Provider.Profile（令牌里没有角色）。
func (h *handler) me(c *gin.Context) (User, bool) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return User{}, false
	}
	u := User{TID: cl.TenantID, ID: cl.UserID, Name: cl.Username, Super: cl.Super, IP: c.ClientIP(),
		Session: cl.ID, Principal: cl.Principal}
	if h.profile != nil {
		pu, err := h.profile(c.Request.Context(), cl.UserID)
		if err != nil {
			response.Fail(c, err)
			return User{}, false
		}
		if pu != nil {
			u.Roles = pu.Roles
		}
	}
	return u, true
}

func paramID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return id, true
}

type codeBody struct {
	Code string `json:"code" binding:"required,max=64"`
	Kind string `json:"kind" binding:"max=16"`
}

// GET /2fa/status
func (h *handler) status(c *gin.Context) {
	if u, ok := h.me(c); ok {
		v, err := h.svc.Status(c.Request.Context(), u)
		respond(c, v, err)
	}
}

// POST /2fa/setup {password}  需要当前登录密码重新认证
func (h *handler) setup(c *gin.Context) {
	u, ok := h.me(c)
	if !ok {
		return
	}
	var b struct {
		Password string `json:"password" binding:"required,max=128"`
	}
	if !response.BindJSON(c, &b) {
		return
	}
	v, err := h.svc.Setup(c.Request.Context(), u, b.Password)
	respond(c, v, err)
}

// POST /2fa/enable {code}
func (h *handler) enable(c *gin.Context) {
	u, ok := h.me(c)
	if !ok {
		return
	}
	var b codeBody
	if !response.BindJSON(c, &b) {
		return
	}
	codes, err := h.svc.Enable(c.Request.Context(), u, b.Code)
	respond(c, gin.H{"recoveryCodes": codes}, err)
}

// POST /2fa/disable {code, kind?}
func (h *handler) disable(c *gin.Context) {
	u, ok := h.me(c)
	if !ok {
		return
	}
	var b codeBody
	if !response.BindJSON(c, &b) {
		return
	}
	respond(c, nil, h.svc.Disable(c.Request.Context(), u, b.Kind, b.Code))
}

// POST /2fa/recovery-codes {code}
func (h *handler) regen(c *gin.Context) {
	u, ok := h.me(c)
	if !ok {
		return
	}
	var b codeBody
	if !response.BindJSON(c, &b) {
		return
	}
	codes, err := h.svc.RegenerateCodes(c.Request.Context(), u, b.Code)
	respond(c, gin.H{"recoveryCodes": codes}, err)
}

// GET /policy
func (h *handler) getPolicy(c *gin.Context) {
	if u, ok := h.me(c); ok {
		v, err := h.svc.GetPolicy(c.Request.Context(), u.TID)
		respond(c, v, err)
	}
}

// PUT /policy
func (h *handler) putPolicy(c *gin.Context) {
	u, ok := h.me(c)
	if !ok {
		return
	}
	var f PolicyForm
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.SavePolicy(c.Request.Context(), u, f)
	respond(c, v, err)
}

// GET /2fa/users?keyword=&page=&pageSize=
func (h *handler) listUsers(c *gin.Context) {
	u, ok := h.me(c)
	if !ok {
		return
	}
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListUsers(c.Request.Context(), u.TID, strings.TrimSpace(c.Query("keyword")), page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// POST /2fa/users/:id/reset
func (h *handler) resetUser(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	if u, ok := h.me(c); ok {
		respond(c, nil, h.svc.ResetUser(c.Request.Context(), u, id))
	}
}

// GET /ip-rules  -> {list, clientIp}
func (h *handler) listRules(c *gin.Context) {
	u, ok := h.me(c)
	if !ok {
		return
	}
	list, err := h.svc.ListRules(c.Request.Context(), u.TID)
	respond(c, gin.H{"list": list, "clientIp": u.IP,
		"ipRulesDisabled": h.svc.ipRulesDisabled, "loopbackExempt": h.svc.loopbackExempt}, err)
}

func (h *handler) saveRule(c *gin.Context, id idgen.ID) {
	u, ok := h.me(c)
	if !ok {
		return
	}
	var f RuleForm
	if !response.BindJSON(c, &f) {
		return
	}
	u.ConfirmLockout = f.ConfirmLockout
	v, err := h.svc.SaveRule(c.Request.Context(), u, id, f)
	respond(c, v, err)
}

// POST /ip-rules
func (h *handler) createRule(c *gin.Context) { h.saveRule(c, 0) }

// PUT /ip-rules/:id
func (h *handler) updateRule(c *gin.Context) {
	if id, ok := paramID(c); ok {
		h.saveRule(c, id)
	}
}

// PUT /ip-rules/:id/enabled {enabled}
func (h *handler) setRuleEnabled(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var b struct {
		Enabled        bool `json:"enabled"`
		ConfirmLockout bool `json:"confirmLockout"`
	}
	if !response.BindJSON(c, &b) {
		return
	}
	if u, ok := h.me(c); ok {
		u.ConfirmLockout = b.ConfirmLockout
		v, err := h.svc.SetRuleEnabled(c.Request.Context(), u, id, b.Enabled)
		respond(c, v, err)
	}
}

// DELETE /ip-rules/:id?confirmLockout=true
func (h *handler) deleteRule(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	if u, ok := h.me(c); ok {
		u.ConfirmLockout = c.Query("confirmLockout") == "true"
		respond(c, nil, h.svc.DeleteRule(c.Request.Context(), u, id))
	}
}

// GET /logs?page=&pageSize=
func (h *handler) listLogs(c *gin.Context) {
	u, ok := h.me(c)
	if !ok {
		return
	}
	page, size := response.ParsePage(c)
	list, total, err := h.svc.ListLogs(c.Request.Context(), u.TID, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}
