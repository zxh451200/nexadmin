package totp

import (
	"context"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// gate 实现 auth.LoginGate:已绑定 TOTP 的用户登录时要求输入动态码。
// Name 为 "totp"——成功/失败都写进登录日志的 login_type。
type gate struct{ svc *Service }

func (g *gate) Name() string { return "totp" }

// Check 用户有已启用的 TOTP → 要求二次验证;否则放行(不强制全员绑定,P0 由用户自主)。
// 必须带用户租户：表有 tenant_id，无租户上下文查询会失败；失败即拒绝登录，不能当成未绑定放行。
func (g *gate) Check(ctx context.Context, in auth.GateInput) (*auth.GateRequirement, error) {
	if in.User == nil {
		return nil, nil
	}
	uid := uint64(in.User.ID)
	if uid == 0 {
		return nil, nil
	}
	enrolled, err := g.svc.EnrolledAndEnabled(tenant.With(ctx, in.User.TenantID), uid)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, nil
	}
	// recovery：恢复码兜底（验证器丢失时），登录页据此前置切换输入框
	return &auth.GateRequirement{Methods: []string{"totp", "recovery"}}, nil
}

// Verify 校验动态码 + 步长防重放；kind=recovery 时消费一枚恢复码。
// 挑战记录里已有 UserID/TenantID,按挑战租户查询。
func (g *gate) Verify(ctx context.Context, in auth.GateChallenge, ans auth.GateAnswer) error {
	uid, err := strconv.ParseUint(in.UserID.String(), 10, 64)
	if err != nil || uid == 0 {
		return response.BadRequest("totp.code_invalid")
	}
	ctx = tenant.With(ctx, in.TenantID)
	code := strings.TrimSpace(ans.Code)
	if ans.Kind == "recovery" {
		return g.svc.VerifyRecoveryForGate(ctx, uid, code)
	}
	if ans.Kind != "" && ans.Kind != "totp" {
		return response.BadRequest("totp.kind_invalid")
	}
	return g.svc.VerifyForGate(ctx, uid, code)
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

// POST /plugins/totp/confirm  {"code":"123456"} —— 确认绑定(启用)，返回首批恢复码。
func (h *handler) confirm(c *gin.Context) {
	var f struct {
		Code string `json:"code" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	res, err := h.svc.Confirm(c.Request.Context(), uidOf(c), strings.TrimSpace(f.Code), c.ClientIP())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// POST /plugins/totp/recovery-codes —— 重新生成恢复码（旧码整批作废），新明文只显示这一次。
func (h *handler) regenRecovery(c *gin.Context) {
	uid := uidOf(c)
	if uid == 0 {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	codes, err := h.svc.RegenerateRecovery(c.Request.Context(), uid)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"recoveryCodes": codes})
}

// POST /plugins/totp/disable  {"code":"123456","password":"…"} —— 解绑(需当前密码 + 验证码)。
// password 缺失单独报 totp.password_required:旧客户端/版本错位期间能一眼看出缺的是密码,而不是通用参数错误。
func (h *handler) disable(c *gin.Context) {
	var f struct {
		Code     string `json:"code" binding:"required"`
		Password string `json:"password"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	if f.Password == "" {
		response.Fail(c, response.BadRequest("totp.password_required"))
		return
	}
	respond(c, nil, h.svc.Disable(c.Request.Context(), uidOf(c), strings.TrimSpace(f.Code), f.Password, c.ClientIP()))
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
