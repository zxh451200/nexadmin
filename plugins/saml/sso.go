package saml

import (
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/crewjam/saml"
	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

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
	u, err := req.Redirect("", sp)
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
	assertion, err := sp.ParseResponse(c.Request, nil)
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("SAML 响应校验失败: "+err.Error()))
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
	out, err := s.issuer.LoginAs(c.Request.Context(), auth.LoginAsRequest{
		Identity: auth.ExternalIdentity{
			ProviderKey:   "saml:" + m.ID.String(),
			Subject:       subject,
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
	response.OK(c, out.Payload())
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
