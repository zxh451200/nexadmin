package sso

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"nexadmin/core/cache"
	"nexadmin/pkg/idgen"
)

var supportedScopes = []string{"openid", "profile", "email", "phone", "roles"}

const codeTTL = 60 * time.Second

// provider 是协议核心：授权码签发/兑换、令牌签发、refresh 轮换。HTTP 层只做参数解析。
type provider struct {
	st         store
	keys       *keyStore
	cache      cache.Cache
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

type authzParams struct {
	ClientID, RedirectURI, Scope, State, Nonce, Challenge, Method string
}

type codeData struct {
	ClientID    string   `json:"c"`
	RedirectURI string   `json:"r"`
	Challenge   string   `json:"h"`
	Scope       string   `json:"s"`
	Nonce       string   `json:"n"`
	UserID      idgen.ID `json:"u"`
	AuthTime    int64    `json:"t"`
}

func codeKey(code string) string { return cache.GlobalKey("sso", "code", sha256Hex(code)) }

// parseScope 过滤出支持的 scope（去重、保持顺序）；必须包含 openid。
func parseScope(raw string) ([]string, *oauthErr) {
	var out []string
	for _, s := range strings.Fields(raw) {
		if slices.Contains(supportedScopes, s) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	if !slices.Contains(out, "openid") {
		return nil, oerr(http.StatusBadRequest, "invalid_scope", "scope must include openid")
	}
	return out, nil
}

// visibleTo 报告用户是否可见该应用：roleIds 为空 = 所有登录用户；超管始终可见。
func visibleTo(app *App, u *userRow) bool {
	if u.Super {
		return true
	}
	roles := app.RoleList()
	if len(roles) == 0 {
		return true
	}
	for _, r := range u.RoleIDs {
		if slices.Contains(roles, r) {
			return true
		}
	}
	return false
}

// checkAuthz 校验授权请求：应用启用、redirect_uri 严格匹配、PKCE(S256) 必填、用户可见。
func (p *provider) checkAuthz(app *App, u *userRow, a authzParams) ([]string, *oauthErr) {
	if app == nil || !app.Enabled || (!u.Super && app.TenantID != u.TenantID) {
		return nil, oerr(http.StatusBadRequest, "unauthorized_client", "unknown or disabled client")
	}
	if a.RedirectURI == "" || !slices.Contains(app.RedirectList(), a.RedirectURI) { // 严格字符串匹配，无通配
		return nil, oerr(http.StatusBadRequest, "invalid_request", "redirect_uri not registered")
	}
	if a.Method != "S256" || !reChallenge.MatchString(a.Challenge) {
		return nil, oerr(http.StatusBadRequest, "invalid_request", "PKCE with S256 is required")
	}
	if !visibleTo(app, u) {
		return nil, oerr(http.StatusForbidden, "access_denied", "application is not available to this user")
	}
	return parseScope(a.Scope)
}

// issueCode 签发一次性授权码并返回回跳地址。
func (p *provider) issueCode(ctx context.Context, app *App, u *userRow, a authzParams, authTime time.Time) (string, *oauthErr) {
	scopes, e := p.checkAuthz(app, u, a)
	if e != nil {
		return "", e
	}
	code := randToken(32)
	b, _ := json.Marshal(codeData{ClientID: app.ClientID, RedirectURI: a.RedirectURI, Challenge: a.Challenge,
		Scope: strings.Join(scopes, " "), Nonce: a.Nonce, UserID: u.ID, AuthTime: authTime.Unix()})
	if err := p.cache.Set(ctx, codeKey(code), string(b), codeTTL); err != nil {
		return "", oerr(http.StatusInternalServerError, "server_error", "cache unavailable")
	}
	u2, err := url.Parse(a.RedirectURI)
	if err != nil {
		return "", oerr(http.StatusBadRequest, "invalid_request", "bad redirect_uri")
	}
	q := u2.Query()
	q.Set("code", code)
	if a.State != "" {
		q.Set("state", a.State) // 原样回传
	}
	u2.RawQuery = q.Encode()
	return u2.String(), nil
}

// takeCode 取出并立即作废授权码（一次性）。用 cache.GetDel 原子消费：多实例下并发兑换同一个 code，只有一个能成功。
func (p *provider) takeCode(ctx context.Context, code string) (*codeData, bool) {
	raw, err := cache.GetDel(p.cache, ctx, codeKey(code))
	if err != nil {
		return nil, false
	}
	var d codeData
	if json.Unmarshal([]byte(raw), &d) != nil {
		return nil, false
	}
	return &d, true
}
