package profile

import (
	"regexp"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/modules/internal/oauthflow"
	"nexadmin/modules/internal/session"
	"nexadmin/modules/internal/smscode"
	"nexadmin/pkg/idgen"
)

// Handler 是个人中心的 HTTP 层。
type Handler struct {
	svc      *Service
	sms      *smscode.Service
	flow     *oauthflow.Flow
	auth     *auth.Service
	cfg      *config.Config
	sessions *session.Store
}

var phoneRe = regexp.MustCompile(`^1\d{10}$`)

func uidOf(c *gin.Context) idgen.ID { return auth.ClaimsFrom(c).UserID }

func done(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// Get GET /profile
func (h *Handler) Get(c *gin.Context) {
	p, err := h.svc.Get(c.Request.Context(), uidOf(c))
	done(c, p, err)
}

// Update PUT /profile
func (h *Handler) Update(c *gin.Context) {
	var in ProfileInput
	if !response.BindJSON(c, &in) {
		return
	}
	p, err := h.svc.Update(c.Request.Context(), uidOf(c), in)
	done(c, p, err)
}

// ChangePassword PUT /profile/password：改密成功后，其他设备的会话一并下线。
func (h *Handler) ChangePassword(c *gin.Context) {
	var in struct {
		OldPassword string `json:"oldPassword" binding:"required,max=128"`
		NewPassword string `json:"newPassword" binding:"required,max=128"`
	}
	if !response.BindJSON(c, &in) {
		return
	}
	ctx := c.Request.Context()
	if err := h.svc.ChangePassword(ctx, uidOf(c), in.OldPassword, in.NewPassword); err != nil {
		response.Fail(c, err)
		return
	}
	done(c, nil, h.sessions.KickUser(ctx, uidOf(c), session.Current(c)))
}

// Bindings GET /profile/bindings
func (h *Handler) Bindings(c *gin.Context) {
	b, err := h.svc.Bindings(c.Request.Context(), uidOf(c))
	done(c, b, err)
}

// SendSMS 与 BindProvider 见 bindings.go。

// BindPhone POST /profile/bindings/phone  body: {phone, code}
func (h *Handler) BindPhone(c *gin.Context) {
	var in struct {
		Phone string `json:"phone" binding:"required"`
		Code  string `json:"code" binding:"required"`
	}
	if !response.BindJSON(c, &in) {
		return
	}
	if !phoneRe.MatchString(in.Phone) {
		response.Fail(c, response.BadRequest("user.phone_invalid"))
		return
	}
	// 校验与发送同场景（notify.SceneBindPhone）：场景隔离的验证码 key 要求两侧一致
	ctx := smscode.WithScene(c.Request.Context(), notify.SceneBindPhone)
	if err := h.sms.Verify(ctx, in.Phone, in.Code); err != nil {
		response.Fail(c, err)
		return
	}
	done(c, nil, h.svc.SetPhone(ctx, uidOf(c), in.Phone))
}

// UnbindPhone DELETE /profile/bindings/phone
func (h *Handler) UnbindPhone(c *gin.Context) {
	done(c, nil, h.svc.SetPhone(c.Request.Context(), uidOf(c), ""))
}

// UnbindProvider DELETE /profile/bindings/:key
func (h *Handler) UnbindProvider(c *gin.Context) {
	done(c, nil, h.svc.UnbindProvider(c.Request.Context(), uidOf(c), c.Param("key")))
}

// Sessions GET /profile/sessions：返回数组（不是分页），当前会话 current=true。
func (h *Handler) Sessions(c *gin.Context) {
	list, err := h.sessions.List(c.Request.Context(), uidOf(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	cur := session.Current(c)
	out := make([]LoginSession, 0, len(list))
	for _, s := range list {
		out = append(out, LoginSession{ID: s.ID, Device: s.Device, Browser: s.Browser, OS: s.OS, IP: s.IP,
			LoginAt: s.LoginAt, LastActiveAt: s.LastActiveAt, Current: s.ID == cur})
	}
	response.OK(c, out)
}

// KickSession DELETE /profile/sessions/:id：下线自己的其他设备，不能下线当前会话。
func (h *Handler) KickSession(c *gin.Context) {
	sid := c.Param("id")
	if _, err := idgen.Parse(sid); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	if sid == session.Current(c) {
		response.Fail(c, response.BadRequest("session.kick_self"))
		return
	}
	_, found, err := h.sessions.Kick(c.Request.Context(), sid, uidOf(c))
	if err == nil && !found {
		err = response.NotFound("error.not_found")
	}
	done(c, nil, err)
}
