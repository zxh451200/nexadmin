package user

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/i18n"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/modules/internal/oauthflow"
	"nexadmin/pkg/oauth"
)

// oauthHandler 实现第三方快捷登录的四个公开接口。账号匹配、绑定、签发 JWT 都在本模块（它是 auth.Provider）。
type oauthHandler struct {
	m    *module
	d    *core.Deps
	flow *oauthflow.Flow
	// 快捷注册与 /auth/register 同口径的 IP 闸门（每小时 10 次），所有 provider 共用一个桶
	regLimiter *middleware.Limiter
}

// registerOAuth 注册 /auth/providers 与 /auth/oauth/*（匿名可访问；靠一次性 state、白名单与统一失败提示防护）。
func (m *module) registerOAuth(r *gin.RouterGroup, d *core.Deps) {
	h := &oauthHandler{m: m, d: d, flow: oauthflow.Shared(d.Config, d.Cache, d.Log),
		regLimiter: middleware.NewLimiter(d.Cache, d.Log, "oauth_register", 10, time.Hour)}
	lim := middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "login", middleware.ByIPAndPath())
	r.GET("/auth/providers", h.providers)
	r.GET("/auth/oauth/:provider/start", lim, h.start)
	r.POST("/auth/oauth/:provider/callback", lim, h.callback)
	r.POST("/auth/oauth/bind", lim, h.bind)
}

// providers GET /auth/providers：只返回已配置且可用的第三方登录方式，未配置的不出现。
func (h *oauthHandler) providers(c *gin.Context) {
	type item struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	ctx := c.Request.Context()
	out := []item{}
	for _, k := range h.flow.Available(ctx) {
		name := i18n.T(ctx, "auth.provider."+k)
		if pc, ok := h.flow.Conf(ctx, k); ok && k == "oidc" && pc.Name != "" {
			name = pc.Name
		} else if name == "" || name == "auth.provider."+k {
			name = auth.ProviderDisplay(k)
		}
		out = append(out, item{Key: k, Name: name})
	}
	response.OK(c, out)
}

// start GET /auth/oauth/:provider/start?redirect=<回调地址>&intent=register：返回第三方授权地址 {authUrl}。
// redirect 就是授权完成后第三方跳回的前端页面，必须与 auth.providers.<name>.redirect_uris 之一完全一致。
// intent=register 是注册页发起的快捷注册：与 /auth/register 同一道运行时开关和 IP 闸门，
// 授权回来后匹配不上就建号（见 resolveLogin），而不是要求绑定已有账号。
func (h *oauthHandler) start(c *gin.Context) {
	ctx := c.Request.Context()
	purpose := oauthflow.PurposeLogin
	if c.Query("intent") == "register" {
		if !auth.RegisterEnabled(ctx) { // 与 /auth/register 关闭时的表现一致
			response.Fail(c, response.NotFound("error.not_found"))
			return
		}
		if ok, retry := h.regLimiter.Hit(ctx, "ip:"+c.ClientIP()); !ok {
			middleware.TooManyRequests(c, retry)
			return
		}
		purpose = oauthflow.PurposeRegister
	}
	u, err := h.flow.Start(ctx, c.Param("provider"), c.Query("redirect"), purpose, 0)
	if err = startError(err); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"authUrl": u})
}

// ReasonOAuthRedirect 是 start / bind 失败时的 reason：前端据此给出可照做的提示
// （当前页面地址不在回调地址白名单里，去设置页添加），而不是一句笼统的“回调地址不在允许范围内”。
const ReasonOAuthRedirect = "OAUTH_REDIRECT_NOT_ALLOWED"

// startError 把流程错误翻成对外的 HTTP 错误；供登录与个人中心绑定共用。
func startError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, oauthflow.ErrNotConfigured):
		return response.NotFound("error.not_found")
	case errors.Is(err, oauthflow.ErrRedirect):
		e := response.BadRequest("auth.redirect_uri_invalid")
		e.Reason = ReasonOAuthRedirect
		return e
	}
	return err
}

// session 为用户签发同一套 JWT 双 token，并更新最后登录时间、记登录日志（登录方式 = provider）。
func (h *oauthHandler) session(c *gin.Context, u *User, provider string) {
	ctx := c.Request.Context()
	db := h.d.DB.WithContext(ctx)
	au, err := buildAuthUser(db, u)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if err := db.Model(&User{}).Where("id = ?", int64(u.ID)).UpdateColumn("last_login_at", time.Now().UTC()).Error; err != nil {
		h.d.Log.Warn("update last_login_at failed", zap.Error(err))
	}
	// 租户校验 -> 二次验证门（若有）-> 签发令牌：第三方登录与密码登录走同一个出口，不能绕过 LoginGate
	out, err := h.d.Auth.FinishLogin(ctx, au, auth.LoginMeta{Method: provider, IP: c.ClientIP(), UserAgent: c.Request.UserAgent()})
	if err != nil {
		response.Fail(c, err)
		return
	}
	writeLoginLog(ctx, h.d, u.Username, provider, c.ClientIP(), c.Request.UserAgent(), "")
	response.OK(c, out.Payload())
}

func identLabel(id *oauth.Identity) string { return clip(id.Provider+":"+id.ExternalID, 64) }
