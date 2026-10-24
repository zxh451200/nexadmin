package user

import (
	"errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/modules/internal/oauthflow"
	"nexadmin/pkg/oauth"
)

// expected 判断是否属于“预期内的第三方登录失败”（state 无效/过期/重放、回调地址不符、换取身份失败）。
// 这类错误统一成一条提示；其余（缓存/数据库故障等）按服务端错误处理。
func expected(err error) bool {
	return errors.Is(err, oauthflow.ErrState) || errors.Is(err, oauthflow.ErrRedirect) ||
		errors.Is(err, oauthflow.ErrNotConfigured) || errors.Is(err, oauth.ErrExchange)
}

// callback POST /auth/oauth/:provider/callback  body: {code, state}
// 登录：返回与 /auth/login 相同的 {accessToken, refreshToken, user}；首次登录且匹配不上返回 {needBind:true, bindToken}。
// 个人中心发起的绑定（state 记着发起用户）：需带登录令牌，成功返回 {bound:true}。
func (h *oauthHandler) callback(c *gin.Context) {
	var req struct {
		Code  string `json:"code" binding:"required,max=2048"`
		State string `json:"state" binding:"required,max=256"`
	}
	if !response.BindJSON(c, &req) {
		return
	}
	ctx, key := c.Request.Context(), c.Param("provider")
	ip, ua := c.ClientIP(), c.Request.UserAgent()
	if err := h.d.Auth.PreLogin(ctx, ip, "", ua); err != nil { // 登录前置检查（IP 黑白名单等），最先执行
		response.Fail(c, err)
		return
	}
	id, rec, err := h.flow.Finish(ctx, key, req.Code, req.State)
	if err != nil {
		if !expected(err) {
			response.Fail(c, err)
			return
		}
		h.d.Log.Warn("oauth callback rejected", zap.String("provider", key), zap.String("ip", ip), zap.Error(err))
		if auth.KnownProvider(key) {
			writeLoginLog(ctx, h.d, "-", key, ip, ua, reasonOther)
		}
		response.Fail(c, errOAuthFailed())
		return
	}
	conf, ok := h.flow.Conf(key)
	if !ok {
		response.Fail(c, errOAuthFailed())
		return
	}
	if rec.Purpose == oauthflow.PurposeBind {
		h.completeBind(c, key, rec, id, conf.AllowedEmailDomains)
		return
	}
	out, reason, err := h.m.resolveLogin(ctx, h.d, h.flow, conf, id)
	if err != nil {
		if reason != "" {
			h.d.Log.Warn("oauth login refused", zap.String("provider", key), zap.String("reason", reason), zap.String("external", identLabel(id)), zap.String("ip", ip))
			writeLoginLog(ctx, h.d, identLabel(id), key, ip, ua, reason)
		}
		response.Fail(c, err)
		return
	}
	if out.NeedBind {
		response.OK(c, gin.H{"needBind": true, "bindToken": out.BindToken})
		return
	}
	h.session(c, out.User, key)
}

func (h *oauthHandler) completeBind(c *gin.Context, key string, rec *oauthflow.StateData, id *oauth.Identity, domains []string) {
	cl := auth.ClaimsFrom(c)
	if cl == nil || rec.UID == "" || cl.UserID.String() != rec.UID || !domainAllowed(domains, id) {
		response.Fail(c, errOAuthFailed())
		return
	}
	if err := linkIdentity(c.Request.Context(), h.d.DB, cl.UserID, id); err != nil {
		if errors.Is(err, errOAuthConflict) {
			response.Fail(c, response.Conflict("error.bad_request").WithMsg("该第三方账号已绑定到其他用户，或你已绑定过此平台"))
			return
		}
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"bound": true, "provider": key})
}

// bind POST /auth/oauth/bind  body: {bindToken, username, password}
// 把待绑定的第三方身份绑到一个已有账号（输一次账号密码）。账号密码的校验、失败计数与锁定走和 /auth/login 完全相同的逻辑。
func (h *oauthHandler) bind(c *gin.Context) {
	var req struct {
		BindToken string `json:"bindToken" binding:"required,max=256"`
		Username  string `json:"username" binding:"required,max=64"`
		Password  string `json:"password" binding:"required,max=128"`
	}
	if !response.BindJSON(c, &req) {
		return
	}
	ctx := c.Request.Context()
	ip, ua := c.ClientIP(), c.Request.UserAgent()
	if err := h.d.Auth.PreLogin(ctx, ip, req.Username, ua); err != nil {
		response.Fail(c, err)
		return
	}
	id, err := h.flow.LookupBindToken(ctx, req.BindToken)
	if errors.Is(err, oauthflow.ErrBindToken) {
		response.Fail(c, response.BadRequest("auth.oauth_failed").WithMsg("绑定已失效，请重新使用第三方登录"))
		return
	}
	if err != nil {
		response.Fail(c, err)
		return
	}
	conf, ok := h.flow.Conf(id.Provider)
	if !ok || !domainAllowed(conf.AllowedEmailDomains, id) {
		response.Fail(c, errOAuthFailed())
		return
	}
	au, err := h.m.passwordLogin(ctx, auth.LoginRequest{Type: "password", Username: req.Username, Password: req.Password, IP: ip, UserAgent: ua})
	if err != nil {
		reason := reasonCredentials
		var re *response.Error
		if errors.As(err, &re) && re.Status == 423 {
			reason = reasonLocked
		}
		writeLoginLog(ctx, h.d, clip(req.Username, 64), id.Provider, ip, ua, reason)
		response.Fail(c, err)
		return
	}
	var u User
	if err := h.d.DB.WithContext(ctx).First(&u, "id = ?", int64(au.ID)).Error; err != nil {
		response.Fail(c, err)
		return
	}
	// 账号密码已通过：原子消费令牌（多实例下并发的两次绑定只有一个成功），再写入绑定。
	if id, err = h.flow.ConsumeBindToken(ctx, req.BindToken); err != nil {
		if errors.Is(err, oauthflow.ErrBindToken) {
			response.Fail(c, response.BadRequest("auth.oauth_failed").WithMsg("绑定已失效，请重新使用第三方登录"))
			return
		}
		response.Fail(c, err)
		return
	}
	if err := linkIdentity(ctx, h.d.DB, u.ID, id); err != nil {
		if errors.Is(err, errOAuthConflict) {
			response.Fail(c, errOAuthFailed())
			return
		}
		response.Fail(c, err)
		return
	}
	h.session(c, &u, id.Provider)
}
