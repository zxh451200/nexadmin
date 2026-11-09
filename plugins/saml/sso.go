package saml

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	"nexadmin/core/cache"
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
	md := sp.Metadata()
	// 轮换期间元数据同时列出当前 / 待切换 / 上一代签名证书，IdP 任意一份都算可信。
	for _, cert := range publishedSPCerts(m) {
		ensureSigningKeyDescriptor(md, cert)
	}
	raw, err := xml.MarshalIndent(md, "", "  ")
	if err != nil {
		response.Fail(c, err)
		return
	}
	if m.SignMetadata {
		// 用当前 SP 私钥签整份元数据；IdP 侧按同一张证书验签（元数据里已列出当前证书）。
		keyPEM, err := decryptSecret(m.SPKeyEnc)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request").WithMsg("读取 SP 私钥失败: "+err.Error()))
			return
		}
		raw, err = signMetadataXML(raw, m.SPCert, keyPEM)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
			return
		}
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
		s.failACS(c, response.BadRequest("saml.acs.err_source_disabled"), "")
		return
	}
	sp, err := s.provider(m, baseURL(c))
	if err != nil {
		s.failACS(c, response.BadRequest("saml.acs.err_metadata"), err.Error())
		return
	}
	// 库从 req.PostForm 读取 SAMLResponse：必须先解析表单，否则真实 IdP 回调一律失败。
	if err := c.Request.ParseForm(); err != nil {
		s.failACS(c, response.BadRequest("saml.acs.err_response"), "SAML 回调表单解析失败")
		return
	}
	if m.RequireEncryptedAssertion {
		raw := c.Request.PostForm.Get("SAMLResponse")
		data, derr := base64.StdEncoding.DecodeString(raw)
		if raw == "" || derr != nil || !hasEncryptedAssertion(data) {
			// 库对明文与加密断言都接受，要求加密时必须先看原始响应再交给库。
			s.failACS(c, response.BadRequest("saml.acs.err_encrypted"), "")
			return
		}
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
		// 底层原因（签名/受众/时限）对配置排障必要；不包含断言原文。
		detail := ""
		if ivr, ok := err.(*saml.InvalidResponseError); ok && ivr.PrivateErr != nil {
			detail = ivr.PrivateErr.Error()
		}
		s.failACS(c, response.BadRequest("saml.acs.err_response"), detail)
		return
	}
	if assertion.Subject == nil || assertion.Subject.NameID == nil {
		s.failACS(c, response.BadRequest("saml.acs.err_nameid"), "")
		return
	}
	subject := assertion.Subject.NameID.Value
	// SessionIndex 让 IdP 发起的登出能只吊销这一次 IdP 会话；IdP 没给就按 NameID 全量吊销。
	sessionIndex := ""
	if len(assertion.AuthnStatements) > 0 {
		sessionIndex = assertion.AuthnStatements[0].SessionIndex
	}
	username := attr(assertion, m.AttrUsername)
	if username == "" {
		username = subject
	}
	// 组映射：仅自动建号时授予角色；开了「必须命中组」而一个都没命中时拒绝登录。
	mapped, err := mappedRoleCodes(attrValues(assertion, m.AttrGroups), m.GroupRoleMappings, m.RequireMappedGroup)
	if err != nil {
		s.failACS(c, response.Forbidden("saml.acs.err_group"), "")
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
			SyncProfile:     true,
		},
		Method:         "saml",
		IP:             c.ClientIP(),
		UserAgent:      c.Request.UserAgent(),
		SessionBinding: samlBinding(m.ID, subject, sessionIndex),
	})
	if err != nil {
		s.failACS(c, asResponseError(err), "")
		return
	}
	// 保存 NameID 供 SP 发起登出使用（需二次验证的挑战阶段还没有本地用户，跳过）。
	if out.User != nil && s.cache != nil && assertion.Subject.NameID.Value != "" {
		_ = s.cache.Set(c.Request.Context(), sloKey(out.User.TenantID, m.ID, out.User.ID),
			assertion.Subject.NameID.Value, 7*24*time.Hour)
	}
	if out.User != nil {
		// 角色对账放在会话签发之后：失败只留日志，不回滚这次登录。
		s.SyncRolesOnLogin(c.Request.Context(), m, out.User, mapped)
	}
	payload := out.Payload()
	if redirect != "" {
		// SP 发起：回到发起页，用一次性 code 换登录态（避免 token 出现在 URL）。
		if u := s.redeemURL(c, m.ID, payload, redirect); u != "" {
			c.Redirect(http.StatusFound, u)
			return
		}
	}
	// IdP 发起：RelayState 是站内路径时作为深链（登录后回到该页），否则回后台登录页；
	// 两种情况都由 SPA 兑换一次性 code。RelayState 的站内校验与 SP 发起侧同一口径。
	if m.AllowIDPInitiated {
		if u := s.redeemURL(c, m.ID, payload, idpInitiatedTarget(baseURL(c), c.Request.PostForm.Get("RelayState"))); u != "" {
			c.Redirect(http.StatusFound, u)
			return
		}
	}
	response.OK(c, payload)
}

// idpInitiatedTarget 构造 IdP 发起登录的回跳地址：RelayState 是站内路径时作为深链，
// 登录页兑换完 code 后回到该页；没有或不是站内路径时回后台登录页。
func idpInitiatedTarget(base, relay string) string {
	target := base + "/admin/login"
	next := safeRedirect(relay)
	if next == "" {
		return target
	}
	// 同时接受带部署前缀的写法（/admin/...）与 SPA 内部路径（/...）。
	if rest, ok := strings.CutPrefix(next, "/admin/"); ok {
		next = "/" + rest
	} else if next == "/admin" {
		next = "/"
	}
	return target + "?returnTo=" + url.QueryEscape(next)
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

// slo 同时处理两个方向：
//   - IdP 发起：带 SAMLRequest 的 LogoutRequest（重定向 / POST 表单），验签后吊销本地会话并回 LogoutResponse；
//   - SP 发起：带 SAMLResponse 的 LogoutResponse 回执，校验后回到发起页。
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
	if isLogoutRequest(c.Request) {
		s.idpInitiatedLogout(c, m, sp)
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

// isLogoutRequest 判断到达 SLO 端点的是 IdP 的 LogoutRequest 还是 SP 发起的 LogoutResponse。
// 两种绑定都把消息放在 SAMLRequest / SAMLResponse 里，按参数名区分，比按「校验哪种成功」猜更可靠。
func isLogoutRequest(r *http.Request) bool {
	if r.URL.Query().Get("SAMLRequest") != "" {
		return true
	}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		return r.PostForm.Get("SAMLRequest") != ""
	}
	return false
}

// idpInitiatedLogout 处理 IdP 发起的登出：LogoutRequest 必须来自元数据里的 IdP 且带有效签名
// （crewjam 校验 Issuer / Destination / 时限 / 重放），随后按 NameID 吊销该来源下已记录的本地会话。
func (s *Service) idpInitiatedLogout(c *gin.Context, m *Source, sp *saml.ServiceProvider) {
	req, err := sp.ValidateLogoutRequest(c.Request)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SLO 请求校验失败: "+err.Error()))
		return
	}
	nameID := ""
	if req.NameID != nil {
		nameID = req.NameID.Value
	}
	// 带 SessionIndex 时只吊销对应的 IdP 会话；不带才按 NameID 全量吊销。
	indexes := make([]string, 0, 2)
	for _, si := range req.AllSessionIndexes() {
		if v := strings.TrimSpace(si.Value); v != "" {
			indexes = append(indexes, v)
		}
	}
	s.revokeSessionsByNameID(c.Request.Context(), m, nameID, indexes)
	relay := c.Query("RelayState")
	if relay == "" {
		_ = c.Request.ParseForm()
		relay = c.Request.PostForm.Get("RelayState")
	}
	// 应答绑定以 IdP 元数据为准：有 HTTP-Redirect 就 302，否则退回 POST 自动提交页。
	if sp.GetSLOBindingLocation(saml.HTTPRedirectBinding) != "" {
		if u, err := sp.MakeRedirectLogoutResponse(req.ID, relay); err == nil {
			c.Redirect(http.StatusFound, u.String())
			return
		}
	}
	if html, err := sp.MakePostLogoutResponse(req.ID, relay); err == nil {
		c.Data(http.StatusOK, "text/html; charset=utf-8", html)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// revokeSessionsByNameID 吊销某 NameID 在本来源上的本地会话。indexes 非空时只吊销 SessionIndex
// 命中的那些（IdP 只结束了其中一次会话），为空时吊销全部（IdP 没给索引）。
// NameID 找不到（会话已过期 / 从未在这里登录）按成功处理：SAML 规范里登出未知会话不算失败。
func (s *Service) revokeSessionsByNameID(ctx context.Context, m *Source, nameID string, indexes []string) {
	if s.cache == nil || nameID == "" {
		return
	}
	key := sessionKey(m.ID, nameID)
	raw, err := s.cache.Get(ctx, key)
	if err != nil || raw == "" {
		return
	}
	rev, ok := s.issuer.(TokenRevoker)
	if !ok {
		return
	}
	var list []samlSession
	if json.Unmarshal([]byte(raw), &list) != nil {
		return
	}
	want := make(map[string]bool, len(indexes))
	for _, v := range indexes {
		want[v] = true
	}
	kept := make([]samlSession, 0, len(list))
	for _, sess := range list {
		// 升级前签发、还没有 SessionIndex 的会话无法按索引匹配，保留到自然过期。
		if len(want) > 0 && !(sess.SID != "" && want[sess.SID]) {
			kept = append(kept, sess)
			continue
		}
		_ = rev.RevokeByJTI(ctx, sess.JTI, sess.RID)
		_ = s.cache.Del(ctx, sloKey(sess.TenantID, m.ID, sess.UserID))
	}
	if len(kept) == 0 {
		_ = s.cache.Del(ctx, key)
		return
	}
	if b, err := json.Marshal(kept); err == nil {
		_ = s.cache.Set(ctx, key, string(b), samlSessionTTL)
	}
}

const (
	samlSessionTTL     = 7 * 24 * time.Hour
	maxTrackedSessions = 20
)

// samlSession 是登录时记下的本地会话标识；IdP 发起的 SLO 只有 NameID，没有本地令牌上下文。
type samlSession struct {
	TenantID idgen.ID  `json:"t"`
	UserID   idgen.ID  `json:"u"`
	JTI      string    `json:"j"`
	RID      string    `json:"r"`
	Exp      time.Time `json:"e"`
	SID      string    `json:"s,omitempty"`
}

// sessionKey 以 NameID 的摘要做键：NameID 可能很长或含空白，直接拼进缓存键不便于排查。
func sessionKey(srcID idgen.ID, nameID string) string {
	sum := sha256.Sum256([]byte(nameID))
	return "saml:sess:" + srcID.String() + ":" + hex.EncodeToString(sum[:])
}

// bindingSIDSep 把可选的 SessionIndex 附在绑定串末尾。NameID 允许含冒号，所以用控制字符作分隔，
// 旧格式（没有 SessionIndex）也能按同一套逻辑解析。
const bindingSIDSep = "\x1f"

// samlBinding 是带给 auth.SessionTracker 的不透明会话归属：源 ID + NameID（+ AuthnStatement.SessionIndex）。
func samlBinding(srcID idgen.ID, nameID, sessionIndex string) string {
	b := "saml:" + srcID.String() + ":" + nameID
	if sessionIndex != "" {
		b += bindingSIDSep + sessionIndex
	}
	return b
}

func parseSAMLBinding(binding string) (idgen.ID, string, string, bool) {
	rest, ok := strings.CutPrefix(binding, "saml:")
	if !ok {
		return 0, "", "", false
	}
	rest, sid, _ := strings.Cut(rest, bindingSIDSep)
	idPart, nameID, ok := strings.Cut(rest, ":")
	if !ok || nameID == "" {
		return 0, "", "", false
	}
	id, err := idgen.Parse(idPart)
	if err != nil || id == 0 {
		return 0, "", "", false
	}
	return id, nameID, sid, true
}

// appendSession 记下一次签发（含二次验证后的签发与刷新轮换）的 access / refresh 令牌 ID，
// 供 IdP 发起 SLO 时按 NameID 吊销。同一 NameID 最多保留 20 条未过期会话，重复 jti 只留一条。
func appendSession(ctx context.Context, c cache.Cache, srcID idgen.ID, nameID, sessionIndex string, cl *auth.Claims) {
	if c == nil || cl == nil || cl.ID == "" || nameID == "" {
		return
	}
	key := sessionKey(srcID, nameID)
	var list []samlSession
	if raw, err := c.Get(ctx, key); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &list)
	}
	now := time.Now()
	kept := make([]samlSession, 0, len(list)+1)
	for _, x := range list {
		if x.Exp.After(now) && x.JTI != cl.ID {
			kept = append(kept, x)
		}
	}
	if len(kept) >= maxTrackedSessions {
		kept = kept[len(kept)-maxTrackedSessions+1:]
	}
	kept = append(kept, samlSession{TenantID: cl.TenantID, UserID: cl.UserID, JTI: cl.ID, RID: cl.RID, Exp: cl.ExpiresAt.Time, SID: sessionIndex})
	if b, err := json.Marshal(kept); err == nil {
		_ = c.Set(ctx, key, string(b), samlSessionTTL)
	}
}

// recordBindingSession 是注册给 core/auth 的 SessionTracker：普通登录、二次验证后签发、
// 刷新轮换都会走到这里，所以 IdP 发起的登出能吊销当前真正有效的令牌。
func recordBindingSession(ctx context.Context, c cache.Cache, cl *auth.Claims) {
	srcID, nameID, sid, ok := parseSAMLBinding(cl.SessionBinding)
	if !ok {
		return
	}
	appendSession(ctx, c, srcID, nameID, sid, cl)
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

// SPCertInfo 是证书弹窗需要的状态：当前证书 + 已发布待切换 + 上一代兜底（含保留截止）。
type SPCertInfo struct {
	Cert         string `json:"cert"`
	NotAfter     string `json:"notAfter"`
	NextCert     string `json:"nextCert,omitempty"`
	NextNotAfter string `json:"nextNotAfter,omitempty"`
	PrevCert     string `json:"prevCert,omitempty"`
	PrevNotAfter string `json:"prevNotAfter,omitempty"`
	PrevUntil    string `json:"prevUntil,omitempty"`
}

func certInfo(m *Source) (SPCertInfo, error) {
	notAfter, err := spCertNotAfter(m.SPCert)
	if err != nil {
		return SPCertInfo{}, err
	}
	out := SPCertInfo{Cert: m.SPCert, NotAfter: notAfter.UTC().Format(time.RFC3339)}
	if strings.TrimSpace(m.NextSPCert) != "" {
		out.NextCert = m.NextSPCert
		if t, err := spCertNotAfter(m.NextSPCert); err == nil {
			out.NextNotAfter = t.UTC().Format(time.RFC3339)
		}
	}
	if strings.TrimSpace(m.PrevSPCert) != "" && (m.PrevSPExpiresAt == nil || time.Now().Before(*m.PrevSPExpiresAt)) {
		out.PrevCert = m.PrevSPCert
		if t, err := spCertNotAfter(m.PrevSPCert); err == nil {
			out.PrevNotAfter = t.UTC().Format(time.RFC3339)
		}
		if m.PrevSPExpiresAt != nil {
			out.PrevUntil = m.PrevSPExpiresAt.UTC().Format(time.RFC3339)
		}
	}
	return out, nil
}

// rotateSPCert 发布下一张 SP 证书（轮换第一步）；IdP 更新信任后再 promote。
func (s *Service) rotateSPCert(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	fresh, err := s.StageSPKey(c.Request.Context(), m.ID.String())
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	info, err := certInfo(fresh)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SP 证书不可用: "+err.Error()))
		return
	}
	response.OK(c, info)
}

// promoteSPCert 切换到已发布的新证书（轮换第二步），旧证书保留 7 天用于解密兜底。
func (s *Service) promoteSPCert(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	fresh, err := s.PromoteSPKey(c.Request.Context(), m.ID.String())
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	info, err := certInfo(fresh)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SP 证书不可用: "+err.Error()))
		return
	}
	response.OK(c, info)
}

// importSPKey 用管理员导入的证书 / 私钥替换当前 SP 密钥对（IdP 侧需同步换成这张证书）。
func (s *Service) importSPKey(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	var f struct {
		Cert string `json:"cert"`
		Key  string `json:"key"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	fresh, err := s.ImportSPKey(c.Request.Context(), m.ID.String(), f.Cert, f.Key)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	info, err := certInfo(fresh)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SP 证书不可用: "+err.Error()))
		return
	}
	response.OK(c, info)
}

// spCert 返回 SP 证书状态（IdP 需要时复制或下载当前证书）。
func (s *Service) spCert(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	info, err := certInfo(m)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SP 证书不可用: "+err.Error()))
		return
	}
	response.OK(c, info)
}

// inspectMetadata 解析/拉取 IdP 元数据并返回摘要，供表单「拉取并预览」使用。
func (s *Service) inspectMetadata(c *gin.Context) {
	var f struct {
		URL      string `json:"url"`
		XML      string `json:"xml"`
		CACert   string `json:"caCert"`
		SignCert string `json:"signCert"`
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
		b, err := fetchMetadata(c.Request.Context(), strings.TrimSpace(f.URL), f.CACert)
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
	// 表单里填了钉住的签名证书就先验一眼：Preview 阶段就能看到「元数据未签名 / 证书不匹配」。
	if strings.TrimSpace(f.SignCert) != "" {
		if err := verifyMetadataSignature(raw, f.SignCert); err != nil {
			response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
			return
		}
	}
	info, err := parseIdP(raw, "")
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg(err.Error()))
		return
	}
	info.MetadataURL = strings.TrimSpace(f.URL)
	response.OK(c, gin.H{"metadata": raw, "idp": info})
}
