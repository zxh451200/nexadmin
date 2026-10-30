package saml

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func newRelay() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// baseURL 从请求推导站点基址（反向代理场景采信 X-Forwarded-Proto 的首个值）。
func baseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if p := c.GetHeader("X-Forwarded-Proto"); p != "" {
		scheme = strings.TrimSpace(strings.Split(p, ",")[0])
	}
	return scheme + "://" + c.Request.Host
}

func (s *Service) sourceOrFail(c *gin.Context) (*Source, bool) {
	id := c.Param("id")
	if _, err := idgen.Parse(id); err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return nil, false
	}
	m, err := s.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return nil, false
	}
	return m, true
}

// metadata 输出 SP 元数据（IdP 侧需要导入）。
func (s *Service) metadata(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	sp, err := s.provider(m, baseURL(c))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	raw, err := xml.MarshalIndent(sp.Metadata(), "", "  ")
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Data(http.StatusOK, "application/samlmetadata+xml; charset=utf-8", raw)
}

// login 发起 SP-initiated 登录：302 到 IdP 的 SSO 端点。
func (s *Service) login(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	if !m.Enabled {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SAML 源未启用"))
		return
	}
	sp, err := s.provider(m, baseURL(c))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	req, err := sp.MakeAuthenticationRequest(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPRedirectBinding)
	if err != nil {
		response.Fail(c, err)
		return
	}
	relay := newRelay()
	if s.cache != nil {
		if err := s.cache.Set(c.Request.Context(), "saml:req:"+relay, req.ID, 10*time.Minute); err != nil {
			response.Fail(c, err)
			return
		}
	}
	u, err := req.Redirect(relay, sp)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Redirect(http.StatusFound, u.String())
}

// acs 接收 IdP 的 SAMLResponse：校验签名/受众/时限后签发登录态。
// 成功返回与 /auth/login 同形的 {accessToken,...} 或 {challenge}（需二次验证时）。
func (s *Service) acs(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	if !m.Enabled {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SAML 源未启用"))
		return
	}
	sp, err := s.provider(m, baseURL(c))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	// 库从 req.PostForm 读取 SAMLResponse：必须先解析表单，否则真实 IdP 回调一律失败。
	if err := c.Request.ParseForm(); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SAML 回调表单解析失败"))
		return
	}
	// SP-initiated：按 RelayState 找回本次 AuthnRequest ID，库才会接受 InResponseTo。
	// 找不到（含 IdP-initiated）时传空，带 InResponseTo 的响应会被拒绝（fail closed）。
	var possibleIDs []string
	if relay := c.Request.PostForm.Get("RelayState"); relay != "" && s.cache != nil {
		if id, err := s.cache.Get(c.Request.Context(), "saml:req:"+relay); err == nil && id != "" {
			possibleIDs = []string{id}
			_ = s.cache.Del(c.Request.Context(), "saml:req:"+relay)
		}
	}
	assertion, err := sp.ParseResponse(c.Request, possibleIDs)
	if err != nil {
		msg := "SAML 响应校验失败"
		// 底层原因（签名/受众/时限）对配置排障必要；不包含断言原文。
		if ivr, ok := err.(*saml.InvalidResponseError); ok && ivr.PrivateErr != nil {
			msg += ": " + ivr.PrivateErr.Error()
		}
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(msg))
		return
	}
	if assertion.Subject == nil || assertion.Subject.NameID == nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SAML 断言缺少 NameID"))
		return
	}
	subject := assertion.Subject.NameID.Value
	username := attr(assertion, m.AttrUsername)
	if username == "" {
		username = subject
	}
	// ProviderKey 留空：框架只接受 ldap-<数字> 或旧 ldap/cas 键，自定义键会被拒。
	// 多源隔离靠 Subject 前缀（源 ID），绑定键仍是 (saml, "<源ID>:<NameID>")。
	out, err := s.issuer.LoginAs(c.Request.Context(), auth.LoginAsRequest{
		Identity: auth.ExternalIdentity{
			Subject:       m.ID.String() + ":" + subject,
			Username:      username,
			Email:         attr(assertion, m.AttrEmail),
			Nickname:      attr(assertion, m.AttrNickname),
			MatchExisting: m.MatchExisting,
			AutoCreate:    m.AutoCreate,
			DefaultRole:   m.DefaultRole,
		},
		Method:    "saml",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 保存 NameID 供 SP 发起登出使用（需二次验证的挑战阶段还没有本地用户，跳过）。
	if out.User != nil && s.cache != nil && assertion.Subject.NameID.Value != "" {
		_ = s.cache.Set(c.Request.Context(), sloKey(out.User.TenantID, m.ID, out.User.ID),
			assertion.Subject.NameID.Value, 7*24*time.Hour)
	}
	response.OK(c, out.Payload())
}

// logout 是 SP 发起的单点登出：吊销本地令牌并 302 到 IdP 的 SLO 端点。
func (s *Service) logout(c *gin.Context) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	if s.cache == nil {
		response.Fail(c, response.Unavailable("auth.provider_missing"))
		return
	}
	key := sloKey(cl.TenantID, m.ID, cl.UserID)
	nameID, err := s.cache.Get(c.Request.Context(), key)
	if err != nil || nameID == "" {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("当前会话不是通过该 SAML 源登录的"))
		return
	}
	sp, err := s.provider(m, baseURL(c))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	u, err := sp.MakeRedirectLogoutRequest(nameID, newRelay())
	if err != nil {
		response.Fail(c, err)
		return
	}
	_ = s.cache.Del(c.Request.Context(), key)
	if r, ok := s.issuer.(SessionRevoker); ok {
		_ = r.RevokeSession(c.Request.Context(), cl)
	}
	c.Redirect(http.StatusFound, u.String())
}

// slo 接收 IdP 的 LogoutResponse（SP 发起登出的回执），校验后确认完成。
func (s *Service) slo(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	sp, err := s.provider(m, baseURL(c))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	if err := sp.ValidateLogoutResponseRequest(c.Request); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SLO 回执校验失败: "+err.Error()))
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// ---- 管理接口 ----

func (s *Service) listSources(c *gin.Context) {
	list, err := s.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

func (s *Service) createSource(c *gin.Context) {
	var f SourceForm
	if !response.BindJSON(c, &f) {
		return
	}
	m, err := s.Create(c.Request.Context(), f)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	response.OK(c, m)
}

func (s *Service) updateSource(c *gin.Context) {
	id := c.Param("id")
	if _, err := idgen.Parse(id); err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	var f SourceForm
	if !response.BindJSON(c, &f) {
		return
	}
	m, err := s.Update(c.Request.Context(), id, f)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	response.OK(c, m)
}

func (s *Service) deleteSource(c *gin.Context) {
	id := c.Param("id")
	if _, err := idgen.Parse(id); err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	if err := s.Delete(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// sourceURLs 返回 IdP 侧要填的 SP 信息（EntityID / ACS / 元数据地址）。
func (s *Service) sourceURLs(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	base := baseURL(c)
	response.OK(c, gin.H{
		"entityId":    base + "/api/v1/auth/saml/" + m.ID.String() + "/metadata",
		"acsUrl":      base + "/api/v1/auth/saml/" + m.ID.String() + "/acs",
		"metadataUrl": base + "/api/v1/auth/saml/" + m.ID.String() + "/metadata",
		"loginUrl":    base + "/api/v1/auth/saml/" + m.ID.String() + "/login",
	})
}
