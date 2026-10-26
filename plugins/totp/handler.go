package totp

import (
	"context"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// gate 实现 auth.LoginGate:已绑定 TOTP 的用户登录时要求输入动态码。
// Name 为 "totp"——成功/失败都写进登录日志的 login_type。
type gate struct{ svc *Service }

func (g *gate) Name() string { return "totp" }

// Check 用户有已启用的 TOTP → 要求二次验证;否则放行(不强制全员绑定,P0 由用户自主)。
func (g *gate) Check(_ context.Context, in auth.GateInput) (*auth.GateRequirement, error) {
	uid := uint64(in.User.ID)
	if uid == 0 {
		return nil, nil
	}
	if !g.svc.IsEnrolledAndEnabled(context.Background(), uid) {
		return nil, nil
	}
	return &auth.GateRequirement{Methods: []string{"totp"}}, nil
}

// Verify 校验动态码 + 步长防重放。挑战记录里已有 UserID/TenantID,无需再取登录态。
func (g *gate) Verify(_ context.Context, in auth.GateChallenge, ans auth.GateAnswer) error {
	if ans.Kind != "" && ans.Kind != "totp" {
		return response.BadRequest("totp.kind_invalid")
	}
	uid, err := strconv.ParseUint(in.UserID.String(), 10, 64)
	if err != nil || uid == 0 {
		return response.BadRequest("totp.code_invalid")
	}
	return g.svc.VerifyForGate(context.Background(), uid, strings.TrimSpace(ans.Code))
}

// handler 是个人中心路由(登录即可;自己的数据自己读写)。
type handler struct{ svc *Service }

func uidOf(c *gin.Context) uint64 {
	if cl := auth.ClaimsFrom(c); cl != nil {
		u, _ := strconv.ParseUint(cl.UserID.String(), 10, 64)
		return u
	}
	return 0
}

// GET /plugins/totp/status
func (h *handler) status(c *gin.Context) {
	response.OK(c, h.svc.Status(c.Request.Context(), uidOf(c)))
}

// POST /plugins/totp/enroll —— 生成密钥与 otpauth:// URI(未确认,不生效)。
func (h *handler) enroll(c *gin.Context) {
	var f struct {
		Issuer string `json:"issuer"`
	}
	_ = c.ShouldBindJSON(&f)
	uid := uidOf(c)
	if uid == 0 {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	cl := auth.ClaimsFrom(c)
	account := cl.Username
	issuer := f.Issuer
	if issuer == "" {
		issuer = "NexAdmin"
	}
	sec, uri, err := h.svc.EnrollStart(c.Request.Context(), uid, account, issuer)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"secret": sec, "uri": uri})
}

// POST /plugins/totp/confirm  {"code":"123456"} —— 确认绑定(启用)。
func (h *handler) confirm(c *gin.Context) {
	var f struct {
		Code string `json:"code" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	respond(c, nil, h.svc.Confirm(c.Request.Context(), uidOf(c), strings.TrimSpace(f.Code)))
}

// POST /plugins/totp/disable  {"code":"123456"} —— 解绑(需验证码)。
func (h *handler) disable(c *gin.Context) {
	var f struct {
		Code string `json:"code" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	respond(c, nil, h.svc.Disable(c.Request.Context(), uidOf(c), strings.TrimSpace(f.Code)))
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

var _ = idgen.Next
var _ = core.Deps{}
