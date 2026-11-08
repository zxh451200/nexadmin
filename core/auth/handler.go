package auth

import (
	"context"
	"encoding/base64"
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
	// ForgotEnabled 报告「忘记密码」的邮件渠道是否可用（SMTP 已配置）；nil = 邮件渠道不可用。
	// 登录页入口的最终显隐 = 邮件或短信任一可用（Methods 里的 forgotChannels）。
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
	// 平台共享配置的写入口只认平台级调用方：把判据随会话一起给出，前端才能做成只读而不是让人撞 403。
	u.Platform = u.IsPlatform()
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
	if pwd { // 「登录验证码」强制开启时必须先过图形验证码；否则同一用户名或 IP 连续失败达到阈值后才要求。
		// 两种情况都在碰密码校验之前验证，通过前不会去比对密码。
		need, err := h.Svc.LoginNeedsCaptcha(ctx, req.Username, req.IP)
		if err != nil {
			response.Fail(c, err)
			return
		}
		if !need {
			need = LoginCaptcha(ctx)
		}
		// 硬档（连错达到 hardAfterFails）在服务端只认点选 token：前端的形态切换只是
		// 表现层，直接调 /auth/slider 换 token 的脚本也必须被挡住。计数读取失败按软档
		// 处理——不因计数故障把正常人挡在门外。
		hard := false
		if need {
			if v, herr := h.Svc.LoginNeedsHardCaptcha(ctx, req.Username, req.IP); herr == nil {
				hard = v
			}
		}
		if need {
			verify := h.Svc.VerifyCaptcha
			if hard {
				verify = h.Svc.VerifyCaptchaHard
			}
			ok, err := verify(ctx, req.CaptchaKey, req.Captcha, req.IP)
			if err != nil {
				response.Fail(c, err)
				return
			}
			if !ok {
				e := response.BadRequest("auth.captcha_invalid")
				if hard {
					// 带上硬档 reason：页面刷新后前端默认回到滑块，这一步让它当场切点选。
					e.Reason = ReasonCaptchaRequiredHard
				} else {
					e.Reason = ReasonCaptchaRequired
				}
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

// ReasonCaptchaRequiredHard 是升级档 reason：失败计数达到硬阈值（hardAfterFails），
// 前端把默认形态从滑块换成点选——来者连滑块都过不了还坚持在试，值得更重的形式。
const ReasonCaptchaRequiredHard = "CAPTCHA_REQUIRED_HARD"

// afterLoginFailure 只对“用户名或密码错误”计数（账号锁定等其它错误原样返回，锁定逻辑由 Provider 负责）；
// 达到阈值时在同一个错误上带 reason，下一次请求起要求验证码（滑块）；达到硬阈值则升级为
// 点选档 reason。对存在与不存在的账号完全一致。
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
	hard, herr := h.Svc.LoginNeedsHardCaptcha(c.Request.Context(), req.Username, req.IP)
	if herr != nil {
		hard = false // 判级失败不拦升级路径：回落到滑块档，验证码照常要求
	}
	if hard {
		cp.Reason = ReasonCaptchaRequiredHard
	} else {
		cp.Reason = ReasonCaptchaRequired
	}
	captchaEscalate(hard)
	return &cp
}

// Captcha GET /auth/captcha：返回 {key, image}，60 秒有效、一次性。
func (h *Handler) Captcha(c *gin.Context) {
	key, img, err := h.Svc.NewCaptcha(c.Request.Context(), c.ClientIP())
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, gin.H{"key": key, "image": img})
}

// CaptchaAudio GET /auth/captcha/:key/audio：把同一张验证码读成音频（WAV 的 data URI），供视障用户听写。
// 只读不消费（消费只在 VerifyCaptcha），所以能反复听；lang 缺省时按请求协商出的界面语言选语音包。
// 走 JSON 而不是裸字节，是为了和图形验证码共用同一条前端链路（含 mock 模式），代价是 base64 膨胀约三分之一。
func (h *Handler) CaptchaAudio(c *gin.Context) {
	lang := c.Query("lang")
	if lang == "" {
		lang = i18n.Lang(c.Request.Context())
	}
	wav, err := h.Svc.CaptchaAudio(c.Request.Context(), c.Param("key"), c.ClientIP(), lang)
	if errors.Is(err, errCaptchaUnavailable) {
		response.Fail(c, response.NotFound("auth.captcha_invalid"))
		return
	}
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, gin.H{"audio": "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(wav)})
}

// SliderCaptcha GET /auth/slider：返回滑块挑战。孔位横坐标不外泄；pieceTop 是滑块图
// 在背景中的顶边偏移，width/height/pieceWidth/pieceHeight 让前端把 CSS 像素的拖动量
// 换算回图片像素（容差按图片像素判定，两侧必须同一口径）。
func (h *Handler) SliderCaptcha(c *gin.Context) {
	key, bg, piece, pieceTop, err := h.Svc.NewSliderCaptcha(c.Request.Context(), c.ClientIP())
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, gin.H{
		"key": key, "image": bg, "piece": piece, "pieceTop": pieceTop,
		"width": sliderBgW, "height": sliderBgH,
		"pieceWidth": sliderPieceW, "pieceHeight": sliderPieceH,
	})
}

// SliderVerify POST /auth/slider/verify  body: {key, x, trail}
// 校验通过返回 {token}：前端把它当作 captcha 字段连同 captchaKey 提交，
// 于是登录、短信、注册仍走 VerifyCaptcha 同一条消费通道。
// 失败（落点偏、轨迹不像人）与挑战不存在（过期、超次数、IP 不符）都回原句文案，
// 前端拿到任何非 2xx 都重画一张，不区分子原因也不泄露挑战是否还存在——唯一的
// 例外是拖错把 IP 升级计数推过硬阈值：响应带 CAPTCHA_REQUIRED_HARD，前端当场
// 把验证形态切成点选，而不是等下一次密码失败才升级。
func (h *Handler) SliderVerify(c *gin.Context) {
	var body struct {
		Key   string       `json:"key"`
		X     int          `json:"x"`
		Trail []trailPoint `json:"trail"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	token, err := h.Svc.VerifySliderCaptcha(c.Request.Context(), body.Key, body.X, body.Trail, c.ClientIP())
	if err != nil {
		switch {
		case errors.Is(err, errSliderFailed):
			e := response.BadRequest("auth.captcha_invalid")
			// VerifySliderCaptcha 已把本次拖错计入 IP 升级计数，这里只读 IP 维度的
			// 更新值（不读用户名维度：交卷时还没有登录尝试，且空用户名会在
			// failUserKey("") 留下人人共享的假桶）。
			if n, herr := h.Svc.failCount(c.Request.Context(), failIPKey(c.ClientIP())); herr == nil && n >= hardAfterFails {
				e.Reason = ReasonCaptchaRequiredHard
			}
			response.Fail(c, e)
		case errors.Is(err, errSliderUnavailable):
			response.Fail(c, response.NotFound("auth.captcha_invalid"))
		default:
			response.Fail(c, err)
		}
		return
	}
	response.OK(c, gin.H{"token": token})
}

// ClickCaptcha GET /auth/click：点选挑战（第三档），返回 {key, image, thumb, width, height,
// thumbWidth, thumbHeight}——点位只存服务端，与滑块同一套下发口径。
func (h *Handler) ClickCaptcha(c *gin.Context) {
	key, img, thumb, w, ht, tw, th, err := h.Svc.NewClickCaptcha(c.Request.Context(), c.ClientIP())
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, gin.H{
		"key": key, "image": img, "thumb": thumb,
		"width": w, "height": ht, "thumbWidth": tw, "thumbHeight": th,
	})
}

// ClickVerify POST /auth/click/verify  body: {key, dots:[{x,y}]}
// 与 SliderVerify 同一套响应形状：通过 {token}（走 captchaKey 消费通道），失败统一
// auth.captcha_invalid，不区分子原因也不泄露挑战是否还存在。
func (h *Handler) ClickVerify(c *gin.Context) {
	var body struct {
		Key  string       `json:"key"`
		Dots []clickPoint `json:"dots"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	token, err := h.Svc.VerifyClickCaptcha(c.Request.Context(), body.Key, body.Dots, c.ClientIP())
	if err != nil {
		switch {
		case errors.Is(err, errClickFailed):
			response.Fail(c, response.BadRequest("auth.captcha_invalid"))
		case errors.Is(err, errClickUnavailable):
			response.Fail(c, response.NotFound("auth.captcha_invalid"))
		default:
			response.Fail(c, err)
		}
		return
	}
	response.OK(c, gin.H{"token": token})
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
	forgotChannels := make([]string, 0, 2)
	if h.ForgotEnabled != nil && h.ForgotEnabled(c.Request.Context()) {
		forgotChannels = append(forgotChannels, "email")
	}
	if h.Cfg.SMS.Provider != "" { // 忘记密码也支持手机号短信找回（见 /auth/forgot/send-code）
		forgotChannels = append(forgotChannels, "phone")
	}
	response.OK(c, gin.H{
		"methods": methods, "providers": providers,
		"forgotEnabled": len(forgotChannels) > 0, "forgotChannels": forgotChannels,
		// 运行时开关（设置页「参数设置」的 user.register_enabled，默认关闭），与 /auth/register 的闸门同源
		"registerEnabled": RegisterEnabled(c.Request.Context()),
		// 「登录验证码」强制开启（设置页 login.captcha_enabled，出厂 true）：前端据此首屏显示验证码输入；
		// 关闭时仍是自适应（连续失败达到阈值后要求），由失败响应的 reason 通知前端
		"captchaEnabled": LoginCaptcha(c.Request.Context()),
	})
}
