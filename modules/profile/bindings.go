package profile

import (
	"errors"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/modules/internal/oauthflow"
	"nexadmin/modules/internal/smscode"
)

// SendSMS POST /auth/sms-code  body: {phone, captcha?, captchaKey?}
// 匿名请求（手机号登录）按 IP 发码次数自适应要求人机验证（SendCaptchaAfter）；登录用户（绑定手机号）不需要。
// 登录场景下手机号没绑定任何账号时，限流照常计数、响应与成功发送完全一致，但不真正发短信（防枚举，也不浪费费用）。
func (h *Handler) SendSMS(c *gin.Context) {
	var in struct {
		Phone      string `json:"phone" binding:"required"`
		Captcha    string `json:"captcha"`
		CaptchaKey string `json:"captchaKey"`
	}
	if !response.BindJSON(c, &in) {
		return
	}
	if !phoneRe.MatchString(in.Phone) {
		response.Fail(c, response.BadRequest("user.phone_invalid"))
		return
	}
	ctx := c.Request.Context()
	if !h.sms.Enabled() {
		response.Fail(c, response.Unavailable("sms.disabled"))
		return
	}
	deliver := true
	if auth.ClaimsFrom(c) == nil { // 匿名：手机号登录场景
		if !h.phoneLoginOn() {
			response.Fail(c, response.Unavailable("sms.disabled"))
			return
		}
		// 验证码总开关（设置页「登录安全」）关闭时匿名取码不再要求验证码。
		// IP 自适应：同一 IP 窗口内发码次数达到 SendCaptchaAfter 后才要求过验证码——
		// 对齐密码登录「连续失败才出验证码」的体验，正常用户前两次发码无感；
		// 之后每次发码都必须过滑块，防短信轰炸。发码请求不产生登录失败计数，
		// 所以不能直接复用登录的失败阈值，这里按 IP 发码次数单独计。
		if auth.CaptchaEnabled(ctx) {
			required, err := h.sms.SendCaptchaRequired(ctx, c.ClientIP())
			if err != nil {
				response.Fail(c, err)
				return
			}
			if required {
				ok, err := h.auth.VerifyCaptcha(ctx, in.CaptchaKey, in.Captcha, c.ClientIP())
				if err != nil {
					response.Fail(c, err)
					return
				}
				if !ok {
					e := response.BadRequest("auth.captcha_invalid")
					e.Reason = auth.ReasonCaptchaRequired
					response.Fail(c, e)
					return
				}
			}
			if err := h.sms.BumpSendCount(ctx, c.ClientIP()); err != nil {
				response.Fail(c, err)
				return
			}
		}
		var err error
		if deliver, err = h.svc.PhoneBound(ctx, in.Phone); err != nil {
			response.Fail(c, err)
			return
		}
	} else { // 登录用户：绑定手机号；发送日志据此与手机号登录区分场景
		ctx = smscode.WithScene(ctx, notify.SceneBindPhone)
	}
	if err := h.sms.Send(ctx, in.Phone, c.ClientIP(), deliver); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"cooldown": int(smscode.Cooldown.Seconds())})
}

func (h *Handler) phoneLoginOn() bool {
	for _, m := range h.cfg.Auth.Methods {
		if m == "phone" {
			return true
		}
	}
	return false
}

// BindProvider POST /profile/bindings/:key  body: {redirect}
// 发起第三方账号绑定：返回授权地址 {authUrl}（redirect 必须在该 provider 的回调白名单内）。
// 用户授权后，前端回调页带着登录令牌调用 POST /auth/oauth/:provider/callback，由那里校验 state 属于当前用户并完成绑定。
func (h *Handler) BindProvider(c *gin.Context) {
	var in struct {
		Redirect string `json:"redirect"`
	}
	if !response.BindJSON(c, &in) {
		return
	}
	ctx, key := c.Request.Context(), c.Param("key")
	if bound, err := h.svc.HasIdentity(ctx, uidOf(c), key); err != nil {
		response.Fail(c, err)
		return
	} else if bound {
		response.Fail(c, response.Conflict("profile.already_bound"))
		return
	}
	u, err := h.flow.Start(ctx, key, in.Redirect, oauthflow.PurposeBind, uidOf(c))
	switch {
	case err == nil:
		response.OK(c, gin.H{"authUrl": u})
	case errors.Is(err, oauthflow.ErrNotConfigured):
		response.Fail(c, response.NotFound("error.not_found"))
	case errors.Is(err, oauthflow.ErrRedirect):
		response.Fail(c, response.BadRequest("auth.redirect_uri_invalid"))
	default:
		response.Fail(c, err)
	}
}
