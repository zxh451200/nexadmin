package sso

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/middleware"
	"nexadmin/pkg/idgen"
)

type handler struct {
	p      *provider
	db     *gorm.DB
	log    *zap.Logger
	issuer string              // 配置的 sso.issuer；空则取请求 scheme+host
	lim    *middleware.Limiter // token 端点失败计数（复用 core/middleware 的限流器）
	org    orgStore            // 组织同步只读数据
}

const apiOAuthPath = "/api/v1/oauth"

func (h *handler) issuerFor(c *gin.Context) string {
	if h.issuer != "" {
		return h.issuer
	}
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	// 默认 issuer 带 /api/v1/oauth 后缀：这样标准的 <issuer>/.well-known/openid-configuration 能直接命中本模块，
	// 下游 OIDC 客户端校验 discovery 里的 issuer 与请求地址一致。生产环境仍建议显式配置 sso.issuer。
	return scheme + "://" + c.Request.Host + apiOAuthPath
}

// endpoints 由 issuer 推出各端点：issuer 若已带 /api/v1/oauth 后缀（此时标准的
// <issuer>/.well-known/openid-configuration 就能直接命中本模块），否则拼接。
func endpoints(iss string) (origin, base string) {
	if strings.HasSuffix(iss, apiOAuthPath) {
		return strings.TrimSuffix(iss, apiOAuthPath), iss
	}
	return iss, iss + apiOAuthPath
}

func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}

func (h *handler) writeErr(c *gin.Context, e *oauthErr) {
	noStore(c)
	if e.Status == http.StatusUnauthorized && e.Code == "invalid_client" {
		c.Header("WWW-Authenticate", `Basic realm="oauth"`)
	}
	c.AbortWithStatusJSON(e.Status, gin.H{"error": e.Code, "error_description": e.Desc})
}

func (h *handler) ready(c *gin.Context) bool {
	if h.p.keys == nil {
		h.writeErr(c, oerr(http.StatusInternalServerError, "server_error", "signing keys unavailable"))
		return false
	}
	return true
}

// Discovery GET /oauth/.well-known/openid-configuration
func (h *handler) discovery(c *gin.Context) {
	iss := strings.TrimRight(h.issuerFor(c), "/")
	origin, base := endpoints(iss)
	c.JSON(http.StatusOK, gin.H{
		"issuer":                                iss,
		"authorization_endpoint":                origin + "/sso/authorize",
		"token_endpoint":                        base + "/token",
		"userinfo_endpoint":                     base + "/userinfo",
		"jwks_uri":                              base + "/jwks",
		"end_session_endpoint":                  base + "/logout",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token", "client_credentials"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      supportedScopes,
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256"},
		"claims_supported": []string{"sub", "iss", "aud", "exp", "iat", "nonce", "auth_time", "name", "preferred_username",
			"email", "phone_number", "picture", "tenant_id", "roles", "dept"},
	})
}

// JWKS GET /oauth/jwks
func (h *handler) jwks(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, h.p.keys.jwks())
}

// clientCreds 取 client 凭据：client_secret_basic 或 client_secret_post（不允许同时使用）。
func clientCreds(c *gin.Context) (id, secret string, e *oauthErr) {
	pid, psec := c.PostForm("client_id"), c.PostForm("client_secret")
	if bid, bsec, ok := c.Request.BasicAuth(); ok {
		if psec != "" {
			return "", "", oerr(http.StatusBadRequest, "invalid_request", "multiple client authentication methods")
		}
		i, err1 := url.QueryUnescape(bid)
		s, err2 := url.QueryUnescape(bsec)
		if err1 != nil || err2 != nil || (pid != "" && pid != i) {
			return "", "", oerr(http.StatusUnauthorized, "invalid_client", "client authentication failed")
		}
		return i, s, nil
	}
	return pid, psec, nil
}

// Token POST /oauth/token
func (h *handler) token(c *gin.Context) {
	noStore(c)
	if !h.ready(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	id, secret, e := clientCreds(c)
	if e != nil {
		h.writeErr(c, e)
		return
	}
	ipKey, clientKey := "ip:"+c.ClientIP(), "ip:"+c.ClientIP()+"|c:"+id
	ctx := c.Request.Context()
	if h.lim.Blocked(ctx, ipKey) || h.lim.Blocked(ctx, clientKey) {
		c.Header("Retry-After", strconv.Itoa(int(h.lim.Window().Seconds())))
		h.writeErr(c, oerr(http.StatusTooManyRequests, "temporarily_unavailable", "too many failed attempts, retry later"))
		return
	}
	resp, e := h.p.token(ctx, strings.TrimRight(h.issuerFor(c), "/"), tokenRequest{
		GrantType: c.PostForm("grant_type"), Code: c.PostForm("code"), RedirectURI: c.PostForm("redirect_uri"),
		CodeVerifier: c.PostForm("code_verifier"), RefreshToken: c.PostForm("refresh_token"), Scope: c.PostForm("scope"),
		ClientID: id, ClientSecret: secret,
	})
	if e != nil {
		if e.Status == http.StatusBadRequest || e.Status == http.StatusUnauthorized {
			h.lim.Fail(ctx, ipKey)
			h.lim.Fail(ctx, clientKey)
		}
		h.writeErr(c, e)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func bearer(c *gin.Context) string {
	if h := c.GetHeader("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func bearerFail(c *gin.Context) {
	noStore(c)
	c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
}

// Userinfo GET/POST /oauth/userinfo
func (h *handler) userinfo(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	tok := bearer(c)
	if tok == "" {
		bearerFail(c)
		return
	}
	cl, typ, err := h.p.keys.parse(tok, jwt.WithExpirationRequired())
	sub, _ := cl["sub"].(string)
	cid, _ := cl["client_id"].(string)
	scope, _ := cl["scope"].(string)
	uid, perr := idgen.Parse(sub)
	if err != nil || typ != "at+jwt" || cid == "" || perr != nil {
		bearerFail(c)
		return
	}
	ctx := c.Request.Context()
	app, aerr := h.p.st.appByClientID(ctx, cid)
	u, uerr := h.p.st.loadUser(ctx, uid)
	if aerr != nil || uerr != nil {
		h.writeErr(c, oerr(http.StatusInternalServerError, "server_error", "internal error"))
		return
	}
	if app == nil || !app.Enabled || u == nil || (u.TenantID != app.TenantID && !u.Super) || !visibleTo(app, u) {
		bearerFail(c)
		return
	}
	noStore(c)
	c.JSON(http.StatusOK, userClaims(u, scope))
}

// Logout GET /oauth/logout?id_token_hint=&post_logout_redirect_uri=&state=
func (h *handler) logout(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	hint := c.Query("id_token_hint")
	cl, typ, err := h.p.keys.parse(hint, jwt.WithoutClaimsValidation()) // 允许已过期的 id_token 作为提示
	aud, _ := cl.GetAudience()
	uid, perr := idgen.Parse(func() string { s, _ := cl["sub"].(string); return s }())
	if hint == "" || err != nil || typ == "at+jwt" || len(aud) != 1 || perr != nil {
		h.writeErr(c, oerr(http.StatusBadRequest, "invalid_request", "valid id_token_hint is required"))
		return
	}
	ctx := c.Request.Context()
	app, aerr := h.p.st.appByClientID(ctx, aud[0])
	if aerr != nil {
		h.writeErr(c, oerr(http.StatusInternalServerError, "server_error", "internal error"))
		return
	}
	if app == nil {
		h.writeErr(c, oerr(http.StatusBadRequest, "invalid_request", "unknown client"))
		return
	}
	target := c.Query("post_logout_redirect_uri")
	if target != "" && !slices.Contains(app.RedirectList(), target) && target != app.HomeURL {
		h.writeErr(c, oerr(http.StatusBadRequest, "invalid_request", "post_logout_redirect_uri not registered"))
		return
	}
	if err := h.p.st.revokeUserApp(ctx, app.ClientID, uid); err != nil {
		h.writeErr(c, oerr(http.StatusInternalServerError, "server_error", "internal error"))
		return
	}
	noStore(c)
	if target == "" {
		c.JSON(http.StatusOK, gin.H{"status": "logged_out"})
		return
	}
	u, perr := url.Parse(target)
	if perr != nil {
		h.writeErr(c, oerr(http.StatusBadRequest, "invalid_request", "bad post_logout_redirect_uri"))
		return
	}
	if s := c.Query("state"); s != "" {
		q := u.Query()
		q.Set("state", s)
		u.RawQuery = q.Encode()
	}
	c.Redirect(http.StatusFound, u.String())
}
