package saml

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// parseResponseSafe 包装 ParseResponse：GO-2026-6909（截至 crewjam/saml v0.6.2 上游
// 仍无修复版）可让构造的断言在库内 validateAssertion 触发 nil 指针 panic。ACS 是
// 未认证可达的端点，这里 recover 掉 panic、转成一次普通校验失败，防止打崩进程；
// 上游发修复版后可移除此包装。
func parseResponseSafe(sp *saml.ServiceProvider, r *http.Request, possibleIDs []string) (a *saml.Assertion, err error) {
	defer func() {
		if p := recover(); p != nil {
			a, err = nil, fmt.Errorf("saml: ParseResponse panicked: %v", p)
		}
	}()
	return sp.ParseResponse(r, possibleIDs)
}

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
	if c.Query("download") != "" {
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="saml-sp-%s.xml"`, m.ID.String()))
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
	redirect := safeRedirect(c.Query("redirect"))
	if s.cache != nil {
		val := req.ID
		if redirect != "" {
			val += "|" + redirect
		}
		if err := s.cache.Set(c.Request.Context(), "saml:req:"+relay, val, 10*time.Minute); err != nil {
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
	redirect := ""
	if relay := c.Request.PostForm.Get("RelayState"); relay != "" && s.cache != nil {
		if v, err := s.cache.Get(c.Request.Context(), "saml:req:"+relay); err == nil && v != "" {
			parts := strings.SplitN(v, "|", 2)
			possibleIDs = []string{parts[0]}
			if len(parts) == 2 {
				redirect = safeRedirect(parts[1])
			}
			_ = s.cache.Del(c.Request.Context(), "saml:req:"+relay)
		}
	}
	assertion, err := parseResponseSafe(sp, c.Request, possibleIDs)
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
	// 组映射：仅自动建号时授予角色；开了「必须命中组」而一个都没命中时拒绝登录。
	mapped, err := mappedRoleCodes(attrValues(assertion, m.AttrGroups), m.GroupRoleMappings, m.RequireMappedGroup)
	if err != nil {
		response.Fail(c, response.Forbidden("error.forbidden").WithMsg(err.Error()))
		return
	}
	// ProviderKey 留空：框架只接受 ldap-<数字> 或旧 ldap/cas 键，自定义键会被拒。
	// 多源隔离靠 Subject 前缀（源 ID），绑定键仍是 (saml, "<源ID>:<NameID>")。
	out, err := s.issuer.LoginAs(c.Request.Context(), auth.LoginAsRequest{
		Identity: auth.ExternalIdentity{
			Subject:         m.ID.String() + ":" + subject,
			Username:        username,
			Email:           attr(assertion, m.AttrEmail),
			Nickname:        attr(assertion, m.AttrNickname),
			Phone:           attr(assertion, m.AttrPhone),
			MatchExisting:   m.MatchExisting,
			AutoCreate:      m.AutoCreate,
			DefaultRole:     m.DefaultRole,
			MappedRoleCodes: mapped,
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
	payload := out.Payload()
	if redirect != "" {
		// SP 发起：回到发起页，用一次性 code 换登录态（避免 token 出现在 URL）。
		if u := s.redeemURL(c, m.ID, payload, redirect); u != "" {
			c.Redirect(http.StatusFound, u)
			return
		}
	}
	// IdP 发起：没有我们生成的 redirect，回到后台登录页，由 SPA 兑换 code。
	if m.AllowIDPInitiated {
		if u := s.redeemURL(c, m.ID, payload, baseURL(c)+"/admin/login"); u != "" {
			c.Redirect(http.StatusFound, u)
			return
		}
	}
	response.OK(c, payload)
}

// redeemURL 把登录结果存成一次性 code，追加到目标地址上。
func (s *Service) redeemURL(c *gin.Context, srcID idgen.ID, payload any, target string) string {
	if s.cache == nil {
		return ""
	}
	code := newRelay()
	b, err := json.Marshal(map[string]any{"payload": payload, "sourceId": srcID.String()})
	if err != nil {
		return ""
	}
	if err := s.cache.Set(c.Request.Context(), "saml:code:"+code, string(b), time.Minute); err != nil {
		return ""
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return target + sep + "samlCode=" + url.QueryEscape(code)
}

// publicSources 返回本租户已启用的 SAML 源（登录页入口用，只含 id/name）。
func (s *Service) publicSources(c *gin.Context) {
	list, err := s.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, m := range list {
		if m.Enabled {
			out = append(out, gin.H{"id": m.ID.String(), "name": m.Name})
		}
	}
	response.OK(c, out)
}

// exchange 用一次性 code 换登录结果（ACS 重定向前端时使用，避免 token 出现在 URL）。
func (s *Service) exchange(c *gin.Context) {
	var f struct {
		Code string `json:"code" binding:"required"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	if s.cache == nil {
		response.Fail(c, response.Unavailable("auth.provider_missing"))
		return
	}
	key := "saml:code:" + f.Code
	raw, err := s.cache.Get(c.Request.Context(), key)
	if err != nil || raw == "" {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("交换码无效或已过期"))
		return
	}
	_ = s.cache.Del(c.Request.Context(), key)
	var v struct {
		Payload  map[string]any `json:"payload"`
		SourceID string         `json:"sourceId"`
	}
	if json.Unmarshal([]byte(raw), &v) != nil || v.Payload == nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("交换码内容无效"))
		return
	}
	v.Payload["samlSourceId"] = v.SourceID
	response.OK(c, v.Payload)
}

// safeRedirect 只允许站内绝对路径，防开放重定向。
func safeRedirect(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\r\n") {
		return ""
	}
	return p
}

// logout 是 SP 发起的单点登出：吊销本地令牌并 302 到 IdP 的 SLO 端点。
func (s *Service) logout(c *gin.Context) {
	if u, ok := s.logoutURL(c); ok {
		c.Redirect(http.StatusFound, u)
	}
}

// logoutJSON 同 logout，但返回跳转地址（前端带 token 调用后自行跳转）。
func (s *Service) logoutJSON(c *gin.Context) {
	if u, ok := s.logoutURL(c); ok {
		response.OK(c, gin.H{"redirect": u})
	}
}

func (s *Service) logoutURL(c *gin.Context) (string, bool) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return "", false
	}
	m, ok := s.sourceOrFail(c)
	if !ok {
		return "", false
	}
	if s.cache == nil {
		response.Fail(c, response.Unavailable("auth.provider_missing"))
		return "", false
	}
	key := sloKey(cl.TenantID, m.ID, cl.UserID)
	nameID, err := s.cache.Get(c.Request.Context(), key)
	if err != nil || nameID == "" {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("当前会话不是通过该 SAML 源登录的"))
		return "", false
	}
	sp, err := s.provider(m, baseURL(c))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return "", false
	}
	relay := newRelay()
	if next := safeRedirect(c.Query("next")); next != "" {
		_ = s.cache.Set(c.Request.Context(), "saml:next:"+relay, next, 10*time.Minute)
	}
	u, err := sp.MakeRedirectLogoutRequest(nameID, relay)
	if err != nil {
		response.Fail(c, err)
		return "", false
	}
	_ = s.cache.Del(c.Request.Context(), key)
	if r, ok := s.issuer.(SessionRevoker); ok {
		_ = r.RevokeSession(c.Request.Context(), cl)
	}
	return u.String(), true
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
	// 完成回执后回到发起登出的前端页面（next 在白名单校验后写入缓存）。
	relay := c.Query("RelayState")
	if relay == "" {
		_ = c.Request.ParseForm()
		relay = c.Request.PostForm.Get("RelayState")
	}
	if relay != "" && s.cache != nil {
		if next, err := s.cache.Get(c.Request.Context(), "saml:next:"+relay); err == nil && next != "" {
			_ = s.cache.Del(c.Request.Context(), "saml:next:"+relay)
			c.Redirect(http.StatusFound, next)
			return
		}
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
	base := baseURL(c)
	out := make([]SourceInfo, 0, len(list))
	for i := range list {
		out = append(out, inspectSource(&list[i], base))
	}
	response.OK(c, out)
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

// sourceURLs 返回 IdP 侧要填的 SP 信息（EntityID / ACS / SLO / 元数据 / 登录入口）。
func (s *Service) sourceURLs(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	sp := spInfo(m, baseURL(c))
	response.OK(c, gin.H{
		"entityId":    sp.EntityID,
		"acsUrl":      sp.ACSUrl,
		"sloUrl":      sp.SLOUrl,
		"metadataUrl": sp.MetadataURL,
		"loginUrl":    sp.LoginURL,
	})
}

// setEnabled 快捷启停；启用前先确认元数据 / 证书 / SP 密钥真的可用。
func (s *Service) setEnabled(c *gin.Context) {
	id := c.Param("id")
	if _, err := idgen.Parse(id); err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	var f struct {
		Enabled bool `json:"enabled"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	if err := s.SetEnabled(c.Request.Context(), id, f.Enabled); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	response.OK(c, nil)
}

// testSource 配置自检：不发起登录，只校验元数据、端点、证书与（配置了 URL 时的）远程一致性。
func (s *Service) testSource(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	response.OK(c, s.testSourceChecks(c.Request.Context(), m))
}

// refreshMetadata 手动按 URL 重新拉取元数据；changed 表示内容真的变了。
func (s *Service) refreshMetadata(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	changed, err := s.RefreshMetadata(c.Request.Context(), m)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	fresh, err := s.Get(c.Request.Context(), m.ID.String())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"changed": changed, "source": inspectSource(fresh, baseURL(c))})
}

// rotateSPCert 重新生成 SP 密钥对；IdP 侧必须同步更新证书。
func (s *Service) rotateSPCert(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	fresh, err := s.RotateSPKey(c.Request.Context(), m.ID.String())
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	notAfter, err := spCertNotAfter(fresh.SPCert)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SP 证书不可用: "+err.Error()))
		return
	}
	response.OK(c, gin.H{"cert": fresh.SPCert, "notAfter": notAfter.UTC().Format(time.RFC3339)})
}

// spCert 返回 SP 证书 PEM（IdP 需要时复制或下载）。
func (s *Service) spCert(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	notAfter, err := spCertNotAfter(m.SPCert)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SP 证书不可用: "+err.Error()))
		return
	}
	response.OK(c, gin.H{"cert": m.SPCert, "notAfter": notAfter.UTC().Format(time.RFC3339)})
}

// inspectMetadata 解析/拉取 IdP 元数据并返回摘要，供表单「拉取并预览」使用。
func (s *Service) inspectMetadata(c *gin.Context) {
	var f struct {
		URL string `json:"url"`
		XML string `json:"xml"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	raw := strings.TrimSpace(f.XML)
	if raw == "" {
		if strings.TrimSpace(f.URL) == "" {
			response.Fail(c, response.BadRequest("error.bad_request").WithMsg("请提供元数据 XML 或 URL"))
			return
		}
		b, err := fetchMetadata(c.Request.Context(), strings.TrimSpace(f.URL))
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
			return
		}
		raw = string(b)
	}
	if len(raw) > maxMetadataBytes {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("元数据超过 1 MiB"))
		return
	}
	info, err := parseIdP(raw, "")
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	info.MetadataURL = strings.TrimSpace(f.URL)
	response.OK(c, gin.H{"metadata": raw, "idp": info})
}
