package sso

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// apiErr 构造带 OAuth 错误码（Reason）的统一响应错误，供 SPA 授权页展示。
func apiErr(e *oauthErr) *response.Error {
	r := response.New(e.Status, "error.bad_request").WithMsg(e.Desc)
	r.Reason = e.Code
	return r
}

func (h *handler) currentUser(c *gin.Context) (*userRow, *auth.Claims, bool) {
	cl := auth.ClaimsFrom(c)
	u, err := h.p.st.loadUser(c.Request.Context(), cl.UserID)
	if err != nil {
		response.Fail(c, err)
		return nil, nil, false
	}
	if u == nil {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return nil, nil, false
	}
	return u, cl, true
}

// AuthorizeInfo GET /oauth/authorize/info?client_id=&redirect_uri=
func (h *handler) authorizeInfo(c *gin.Context) {
	u, _, ok := h.currentUser(c)
	if !ok {
		return
	}
	app, err := h.p.st.appByClientID(c.Request.Context(), c.Query("client_id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	scopes := supportedScopes
	if raw := c.Query("scope"); raw != "" {
		if s, e := parseScope(raw); e == nil {
			scopes = s
		}
	}
	// 只校验 client / redirect_uri / 可见性；PKCE 在提交授权时校验。
	if app == nil || !app.Enabled || (!u.Super && app.TenantID != u.TenantID) {
		response.Fail(c, apiErr(oerr(http.StatusBadRequest, "unauthorized_client", "unknown or disabled client")))
		return
	}
	if !containsStr(app.RedirectList(), c.Query("redirect_uri")) {
		response.Fail(c, apiErr(oerr(http.StatusBadRequest, "invalid_request", "redirect_uri not registered")))
		return
	}
	if !visibleTo(app, u) {
		response.Fail(c, apiErr(oerr(http.StatusForbidden, "access_denied", "application is not available to this user")))
		return
	}
	response.OK(c, gin.H{"name": app.Name, "logo": app.Logo, "scopes": scopes})
}

func containsStr(list []string, s string) bool {
	if s == "" {
		return false
	}
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type authorizeBody struct {
	ClientID            string `json:"clientId"`
	RedirectURI         string `json:"redirectUri"`
	Scope               string `json:"scope"`
	State               string `json:"state"`
	Nonce               string `json:"nonce"`
	CodeChallenge       string `json:"codeChallenge"`
	CodeChallengeMethod string `json:"codeChallengeMethod"`
}

// Authorize POST /oauth/authorize：用户同意后签发一次性 code，返回回跳地址。
func (h *handler) authorize(c *gin.Context) {
	var b authorizeBody
	if !response.BindJSON(c, &b) {
		return
	}
	u, cl, ok := h.currentUser(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	app, err := h.p.st.appByClientID(ctx, b.ClientID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	authTime := time.Now()
	if cl.IssuedAt != nil {
		authTime = cl.IssuedAt.Time
	}
	to, e := h.p.issueCode(ctx, app, u, authzParams{ClientID: b.ClientID, RedirectURI: b.RedirectURI, Scope: b.Scope,
		State: b.State, Nonce: b.Nonce, Challenge: b.CodeChallenge, Method: b.CodeChallengeMethod}, authTime)
	if e != nil {
		response.Fail(c, apiErr(e))
		return
	}
	response.OK(c, gin.H{"redirectTo": to})
}

// Portal GET /sso/portal：当前用户可见且启用的应用。
func (h *handler) portal(c *gin.Context) {
	u, _, ok := h.currentUser(c)
	if !ok {
		return
	}
	var apps []App
	if err := h.db.WithContext(c.Request.Context()).Where("enabled = ?", true).Order("created_at ASC, id ASC").Find(&apps).Error; err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]portalDTO, 0, len(apps))
	for i := range apps {
		a := &apps[i]
		if visibleTo(a, u) {
			out = append(out, portalDTO{ID: a.ID, Name: a.Name, Logo: a.Logo, HomeURL: a.HomeURL, Description: a.Description})
		}
	}
	response.OK(c, out)
}

func parseID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return id, true
}
