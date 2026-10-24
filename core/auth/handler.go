package auth

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"

	"nexadmin/core/config"
	"nexadmin/core/i18n"
	"nexadmin/core/response"
)

// Handler 提供 /auth/* 的路由处理；路由在 core 里挂载。
type Handler struct {
	Svc *Service
	Cfg *config.Config
	// ForgotEnabled 报告「忘记密码」是否可用（邮件渠道已配置）；nil = 不可用。登录页据此显示 / 隐藏入口。
	ForgotEnabled func(ctx context.Context) bool
}

type loginResult struct {
	Pair
	User *User `json:"user"`
}

func normalize(u *User) *User {
	if u.Roles == nil {
		u.Roles = []string{}
	}
	if u.Perms == nil {
		u.Perms = []string{}
	}
	return u
}

func (h *Handler) provider(c *gin.Context) Provider {
	p := h.Svc.Provider()
	if p == nil {
		response.Fail(c, response.Unavailable("auth.provider_missing"))
	}
	return p
}

// Login POST /auth/login
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if !response.BindJSON(c, &req) {
		return
	}
	p := h.provider(c)
	if p == nil {
		return
	}
	req.IP, req.UserAgent = c.ClientIP(), c.Request.UserAgent()
	ctx := WithRequestInfo(c.Request.Context(), NewRequestInfo(c.Request, req.IP))
	pwd := req.Type == "" || req.Type == "password"
	preUser := ""
	if pwd {
		preUser = req.Username
	}
	if err := h.Svc.PreLogin(ctx, req.IP, preUser, req.UserAgent); err != nil { // 登录前置检查（IP 黑白名单等），最先执行
		response.Fail(c, err)
		return
	}
	if pwd { // 同一用户名或 IP 连续失败达到阈值后，必须先通过图形验证码，通过前不会去碰密码校验
		need, err := h.Svc.LoginNeedsCaptcha(ctx, req.Username, req.IP)
		if err != nil {
			response.Fail(c, err)
			return
		}
		if need {
			ok, err := h.Svc.VerifyCaptcha(ctx, req.CaptchaKey, req.Captcha)
			if err != nil {
				response.Fail(c, err)
				return
			}
			if !ok {
				e := response.BadRequest("auth.captcha_invalid")
				e.Reason = ReasonCaptchaRequired
				response.Fail(c, e)
				return
			}
		}
	}
	u, err := p.Login(ctx, req)
	if err != nil {
		response.Fail(c, h.afterLoginFailure(c, req, err, pwd))
		return
	}
	method := "password"
	if req.Type == "phone" {
		method = "phone"
	}
	if u.LoginMethod != "" {
		method = u.LoginMethod
	}
	// 凭据正确：租户校验 -> 二次验证门（若有）-> 签发令牌；凭据正确但租户已过期/停用或需二次验证时都不会直接发令牌
	out, err := h.Svc.FinishLogin(ctx, u, LoginMeta{Method: method, IP: req.IP, UserAgent: req.UserAgent})
	if err != nil {
		response.Fail(c, err)
		return
	}
	if pwd {
		_ = h.Svc.ClearLoginFailures(ctx, req.Username)
	}
	response.OK(c, out.Payload())
}

// LoginVerify POST /auth/login/verify  body: {challengeId, code, kind?}
// 用二次验证结果换令牌。匿名可访问，但必须持有登录时下发的、绑定 IP 的一次性挑战。
func (h *Handler) LoginVerify(c *gin.Context) {
	var req struct {
		ChallengeID string `json:"challengeId" binding:"required,max=128"`
		Code        string `json:"code" binding:"required,max=128"`
		Kind        string `json:"kind" binding:"max=32"`
	}
	if !response.BindJSON(c, &req) {
		return
	}
	out, err := h.Svc.CompleteLogin(WithRequestInfo(c.Request.Context(), NewRequestInfo(c.Request, c.ClientIP())), req.ChallengeID,
		LoginMeta{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()}, GateAnswer{Kind: req.Kind, Code: req.Code})
	if errors.Is(err, ErrNoProvider) {
		response.Fail(c, response.Unavailable("auth.provider_missing"))
		return
	}
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out.Payload())
}

// ReasonCaptchaRequired 是登录响应里的 reason：前端据此显示图形验证码输入。
const ReasonCaptchaRequired = "CAPTCHA_REQUIRED"

// afterLoginFailure 只对“用户名或密码错误”计数（账号锁定等其它错误原样返回，锁定逻辑由 Provider 负责）；
// 达到阈值时在同一个错误上带 reason，下一次请求起要求验证码。对存在与不存在的账号完全一致。
func (h *Handler) afterLoginFailure(c *gin.Context, req LoginRequest, err error, pwd bool) error {
	var e *response.Error
	if !pwd || !errors.As(err, &e) || e.Key != "auth.invalid_credentials" {
		return err
	}
	need, ferr := h.Svc.RecordLoginFailure(c.Request.Context(), req.Username, req.IP)
	if ferr != nil || !need {
		return err
	}
	cp := *e
	cp.Reason = ReasonCaptchaRequired
	return &cp
}

// Captcha GET /auth/captcha：返回 {key, image}，60 秒有效、一次性。
func (h *Handler) Captcha(c *gin.Context) {
	key, img, err := h.Svc.NewCaptcha(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, gin.H{"key": key, "image": img})
}

// Refresh POST /auth/refresh  body: {refreshToken}
func (h *Handler) Refresh(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refreshToken"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	pair, err := h.Svc.Refresh(WithRequestInfo(c.Request.Context(), NewRequestInfo(c.Request, c.ClientIP())), body.RefreshToken)
	switch {
	case err == nil:
		response.OK(c, pair)
	case errors.Is(err, ErrInvalidToken):
		response.Fail(c, response.Unauthorized("error.unauthorized"))
	case errors.Is(err, ErrNoProvider):
		response.Fail(c, response.Unavailable("auth.provider_missing"))
	default:
		response.Fail(c, err)
	}
}

// Logout POST /auth/logout（需登录）
func (h *Handler) Logout(c *gin.Context) {
	if cl := ClaimsFrom(c); cl != nil {
		if err := h.Svc.RevokeSession(c.Request.Context(), cl); err != nil {
			response.Fail(c, err)
			return
		}
	}
	response.OK(c, nil)
}

// Me GET /auth/me（需登录）
func (h *Handler) Me(c *gin.Context) {
	u, err := h.Svc.Profile(c.Request.Context(), ClaimsFrom(c))
	if errors.Is(err, ErrNoProvider) {
		response.Fail(c, response.Unavailable("auth.provider_missing"))
		return
	}
	if err != nil {
		response.Fail(c, err)
		return
	}
	h.Svc.Decorate(c.Request.Context(), u)
	response.OK(c, normalize(u))
}

// Methods GET /auth/methods：登录页据此决定展示哪些登录方式。
func (h *Handler) Methods(c *gin.Context) {
	methods := make([]string, 0, len(h.Cfg.Auth.Methods))
	for _, m := range h.Cfg.Auth.Methods {
		if m == "phone" && h.Cfg.SMS.Provider == "" { // 未配置短信则不启用手机号登录
			continue
		}
		methods = append(methods, m)
	}
	keys := h.Cfg.Auth.ProviderNames() // 只返回配置完整可用的；没配置或缺必填项的不显示，也不报错
	type provider struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	providers := make([]provider, 0, len(keys))
	for _, k := range keys {
		if !KnownProvider(k) {
			continue
		}
		name := i18n.T(c.Request.Context(), "auth.provider."+k)
		if name == "" || name == "auth.provider."+k {
			name = ProviderDisplay(k)
		}
		if pc, ok := h.Cfg.Auth.ProviderConf(k); ok && k == "oidc" && pc.Name != "" {
			name = pc.Name
		}
		providers = append(providers, provider{Key: k, Name: name})
	}
	forgot := h.ForgotEnabled != nil && h.ForgotEnabled(c.Request.Context())
	response.OK(c, gin.H{"methods": methods, "providers": providers, "forgotEnabled": forgot})
}
