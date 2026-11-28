package saml

import (
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

const connectionTestRelayPrefix = "saml-test:"

type connectionTestState struct {
	TestID        string `json:"testId"`
	SourceID      string `json:"sourceId"`
	Mode          string `json:"mode"`
	RequestID     string `json:"requestId"`
	CorrelationID string `json:"correlationId"`
}

func (s *Service) startConnectionTest(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	var input struct {
		Mode   string `json:"mode"`
		NameID string `json:"nameId"`
	}
	if !response.BindJSON(c, &input) {
		return
	}
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "sso"
	}
	if mode != "sso" && mode != "slo" {
		response.Fail(c, response.BadRequest("saml.sso.operation_failed", "unsupported test mode"))
		return
	}
	sp, err := s.provider(m, s.baseURL(c, m))
	if err != nil {
		failDetail(c, err)
		return
	}
	testID := newRelay()
	state := connectionTestState{
		TestID: testID, SourceID: m.ID.String(), Mode: mode, CorrelationID: correlationID(c),
	}
	relay := connectionTestRelayPrefix + testID
	if mode == "sso" {
		info, err := parseIdP(m.IdPMetadata, m.IDPEntityID)
		if err != nil || info.SSOURL == "" {
			response.Fail(c, response.BadRequest("saml.sso.operation_failed", "IdP SSO endpoint unavailable"))
			return
		}
		binding := saml.HTTPRedirectBinding
		if info.SSOBinding == "post" {
			binding = saml.HTTPPostBinding
		}
		req, err := sp.MakeAuthenticationRequest(info.SSOURL, binding, saml.HTTPPostBinding)
		if err != nil {
			failDetail(c, err)
			return
		}
		state.RequestID = req.ID
		if err := s.saveConnectionTest(c, state); err != nil {
			response.Fail(c, err)
			return
		}
		s.recordEvent(c.Request.Context(), m, "connection_test", "started", state.CorrelationID, c.ClientIP(), mode)
		if binding == saml.HTTPPostBinding {
			response.OK(c, gin.H{"testId": testID, "mode": mode, "binding": "post", "url": info.SSOURL, "samlRequest": postSAMLRequest(req), "relayState": relay})
			return
		}
		u, err := req.Redirect(relay, sp)
		if err != nil {
			failDetail(c, err)
			return
		}
		response.OK(c, gin.H{"testId": testID, "mode": mode, "binding": "redirect", "url": u.String()})
		return
	}
	input.NameID = strings.TrimSpace(input.NameID)
	if input.NameID == "" {
		response.Fail(c, response.BadRequest("saml.sso.operation_failed", "NameID is required for SLO test"))
		return
	}
	relayState := relay
	if sloURL := sp.GetSLOBindingLocation(saml.HTTPRedirectBinding); sloURL != "" {
		req, err := sp.MakeLogoutRequest(sloURL, input.NameID)
		if err != nil {
			failDetail(c, err)
			return
		}
		u, err := sp.RedirectLogoutRequestURL(req, relayState)
		if err != nil {
			failDetail(c, err)
			return
		}
		state.RequestID = req.ID
		if err := s.saveConnectionTest(c, state); err != nil {
			response.Fail(c, err)
			return
		}
		s.recordEvent(c.Request.Context(), m, "connection_test", "started", state.CorrelationID, c.ClientIP(), mode)
		response.OK(c, gin.H{"testId": testID, "mode": mode, "binding": "redirect", "url": u.String()})
		return
	}
	if sloURL := sp.GetSLOBindingLocation(saml.HTTPPostBinding); sloURL != "" {
		req, err := sp.MakeLogoutRequest(sloURL, input.NameID)
		if err != nil {
			failDetail(c, err)
			return
		}
		state.RequestID = req.ID
		if err := s.saveConnectionTest(c, state); err != nil {
			response.Fail(c, err)
			return
		}
		s.recordEvent(c.Request.Context(), m, "connection_test", "started", state.CorrelationID, c.ClientIP(), mode)
		response.OK(c, gin.H{"testId": testID, "mode": mode, "binding": "post", "url": sloURL, "samlRequest": postSAMLRequest(req), "relayState": relayState})
		return
	}
	response.Fail(c, response.BadRequest("saml.sso.slo_unavailable"))
}

func (s *Service) saveConnectionTest(c *gin.Context, state connectionTestState) error {
	if s.cache == nil {
		return response.Unavailable("auth.provider_missing")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.cache.Set(c.Request.Context(), "saml:test:"+state.TestID, string(raw), 10*time.Minute)
}

func (s *Service) consumeConnectionTest(c *gin.Context, relay, expectedMode string) (connectionTestState, bool) {
	if !strings.HasPrefix(relay, connectionTestRelayPrefix) || s.cache == nil {
		return connectionTestState{}, false
	}
	testID := strings.TrimPrefix(relay, connectionTestRelayPrefix)
	raw, err := s.cache.Get(c.Request.Context(), "saml:test:"+testID)
	if err != nil || raw == "" {
		return connectionTestState{}, false
	}
	var state connectionTestState
	if json.Unmarshal([]byte(raw), &state) != nil || state.TestID != testID {
		_ = s.cache.Del(c.Request.Context(), "saml:test:"+testID)
		return connectionTestState{}, false
	}
	if state.SourceID != c.Param("id") || state.Mode != expectedMode {
		return connectionTestState{}, false
	}
	_ = s.cache.Del(c.Request.Context(), "saml:test:"+testID)
	c.Set("samlTestState", state)
	c.Set("samlTestCorrelation", state.CorrelationID)
	c.Set("samlTestMode", state.Mode)
	return state, true
}

func (s *Service) finishConnectionTest(c *gin.Context, m *Source, state connectionTestState, err error) {
	result, detail := "success", "SAML round trip completed"
	if err != nil {
		result, detail = "failed", sanitizeDiagnosticDetail(err)
	}
	s.recordEvent(c.Request.Context(), m, "connection_test", result, state.CorrelationID, c.ClientIP(), state.Mode+": "+detail)
	if wantsHTML(c.Request) {
		lang := "zh-CN"
		if strings.HasPrefix(strings.ToLower(c.Request.Header.Get("Accept-Language")), "en") {
			lang = "en-US"
		}
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderConnectionTestPage(lang, result == "success", detail, s.baseURL(c, m)+"/admin/system/saml")))
		return
	}
	if err != nil {
		response.Fail(c, response.BadRequest("saml.sso.operation_failed", detail))
		return
	}
	response.OK(c, gin.H{"ok": true, "testId": state.TestID, "mode": state.Mode})
}

func renderConnectionTestPage(lang string, ok bool, detail, backURL string) string {
	title, label, back := "SAML 连接测试失败", "测试未通过", "返回 SAML 管理"
	if lang == "en-US" {
		title, label, back = "SAML connection test failed", "Test failed", "Back to SAML administration"
	}
	if ok {
		title, label = "SAML 连接测试成功", "测试通过"
		if lang == "en-US" {
			title, label = "SAML connection test succeeded", "Test passed"
		}
	}
	esc := html.EscapeString
	return `<!doctype html><html lang="` + esc(lang) + `"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + esc(title) + `</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px;background:#f5f6f8;color:#111827;font:15px/1.6 system-ui,sans-serif}main{max-width:560px;background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:28px}h1{margin:0 0 12px;font-size:20px}p{color:#4b5563;word-break:break-word}a{display:inline-block;margin-top:16px;color:#2563eb}@media(prefers-color-scheme:dark){body{background:#0b1020;color:#e5e7eb}main{background:#111827;border-color:#1f2937}p{color:#9ca3af}a{color:#93c5fd}}</style></head><body><main><h1>` + esc(title) + `</h1><p><strong>` + esc(label) + `</strong></p><p>` + esc(detail) + `</p><a href="` + esc(backURL) + `">` + esc(back) + `</a></main></body></html>`
}

func postSAMLRequest(req interface{ Element() *etree.Element }) string {
	doc := etree.NewDocument()
	doc.SetRoot(req.Element())
	raw, err := doc.WriteToBytes()
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func isConnectionTestFailure(c *gin.Context) bool {
	return c.GetString("samlTestCorrelation") != ""
}
