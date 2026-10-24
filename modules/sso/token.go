package sso

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type tokenRequest struct {
	GrantType, Code, RedirectURI, CodeVerifier, RefreshToken, Scope string
	ClientID, ClientSecret                                          string
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

// authClient 校验 client 凭据。未知/停用/密钥错误统一返回同一个 invalid_client，且都做一次哈希比较，
// 避免通过响应内容或耗时探测 client_id 是否存在。
func (p *provider) authClient(ctx context.Context, clientID, secret string) (*App, *oauthErr) {
	bad := oerr(http.StatusUnauthorized, "invalid_client", "client authentication failed")
	if clientID == "" || secret == "" {
		_ = checkSecret(dummySecretHash, secret)
		return nil, bad
	}
	app, err := p.st.appByClientID(ctx, clientID)
	if err != nil {
		return nil, oerr(http.StatusInternalServerError, "server_error", "internal error")
	}
	if app == nil {
		_ = checkSecret(dummySecretHash, secret)
		return nil, bad
	}
	if !checkSecret(app.SecretHash, secret) || !app.Enabled {
		return nil, bad
	}
	return app, nil
}

// token 处理 /oauth/token：authorization_code 与 refresh_token 两种 grant。
func (p *provider) token(ctx context.Context, iss string, r tokenRequest) (*tokenResponse, *oauthErr) {
	if r.GrantType != "authorization_code" && r.GrantType != "refresh_token" && r.GrantType != grantClientCredentials {
		return nil, oerr(http.StatusBadRequest, "unsupported_grant_type", "grant_type not supported")
	}
	app, e := p.authClient(ctx, r.ClientID, r.ClientSecret)
	if e != nil {
		return nil, e
	}
	if r.GrantType == grantClientCredentials {
		return p.byClientCredentials(iss, app, r)
	}
	if r.GrantType == "authorization_code" {
		return p.byCode(ctx, iss, app, r)
	}
	return p.byRefresh(ctx, iss, app, r)
}

func invalidGrant() *oauthErr {
	return oerr(http.StatusBadRequest, "invalid_grant", "invalid or expired grant")
}

func (p *provider) byCode(ctx context.Context, iss string, app *App, r tokenRequest) (*tokenResponse, *oauthErr) {
	if r.Code == "" {
		return nil, oerr(http.StatusBadRequest, "invalid_request", "code is required")
	}
	d, ok := p.takeCode(ctx, r.Code) // 无论后续校验成败，code 都已作废
	if !ok || d.ClientID != app.ClientID || d.RedirectURI != r.RedirectURI {
		return nil, invalidGrant()
	}
	if !verifyPKCE(d.Challenge, r.CodeVerifier) {
		return nil, invalidGrant()
	}
	u, err := p.st.loadUser(ctx, d.UserID)
	if err != nil {
		return nil, oerr(http.StatusInternalServerError, "server_error", "internal error")
	}
	if u == nil || u.TenantID != app.TenantID && !u.Super || !visibleTo(app, u) {
		return nil, invalidGrant()
	}
	return p.issueTokens(ctx, iss, app, u, d.Scope, d.Nonce, time.Unix(d.AuthTime, 0), "")
}

func (p *provider) byRefresh(ctx context.Context, iss string, app *App, r tokenRequest) (*tokenResponse, *oauthErr) {
	if r.RefreshToken == "" {
		return nil, oerr(http.StatusBadRequest, "invalid_request", "refresh_token is required")
	}
	row, state, err := p.st.consumeRefresh(ctx, sha256Hex(r.RefreshToken), p.now())
	if err != nil {
		return nil, oerr(http.StatusInternalServerError, "server_error", "internal error")
	}
	switch state {
	case refreshUnknown:
		return nil, invalidGrant()
	case refreshReused: // 已轮换的旧令牌再次出现：整条链作废
		_ = p.st.revokeFamily(ctx, row.FamilyID)
		return nil, invalidGrant()
	}
	if row.ClientID != app.ClientID || !p.now().Before(row.ExpiresAt) {
		return nil, invalidGrant()
	}
	scope := row.Scope
	if r.Scope != "" { // 只允许收窄
		want := strings.Fields(r.Scope)
		have := strings.Fields(row.Scope)
		for _, s := range want {
			if !slices.Contains(have, s) {
				return nil, oerr(http.StatusBadRequest, "invalid_scope", "scope exceeds the original grant")
			}
		}
		if !slices.Contains(want, "openid") {
			return nil, oerr(http.StatusBadRequest, "invalid_scope", "scope must include openid")
		}
		scope = strings.Join(want, " ")
	}
	u, err := p.st.loadUser(ctx, row.UserID)
	if err != nil {
		return nil, oerr(http.StatusInternalServerError, "server_error", "internal error")
	}
	if u == nil || u.TenantID != app.TenantID && !u.Super || !visibleTo(app, u) {
		_ = p.st.revokeFamily(ctx, row.FamilyID)
		return nil, invalidGrant()
	}
	return p.issueTokens(ctx, iss, app, u, scope, "", row.AuthTime, row.FamilyID)
}

// issueTokens 签发 access/id/refresh；family 为空表示新会话（授权码换取），否则沿用（refresh 轮换）。
func (p *provider) issueTokens(ctx context.Context, iss string, app *App, u *userRow, scope, nonce string,
	authTime time.Time, family string) (*tokenResponse, *oauthErr) {
	now := p.now()
	sub := u.ID.String()
	at, err := p.keys.sign(jwt.MapClaims{
		"iss": iss, "sub": sub, "aud": app.ClientID, "client_id": app.ClientID, "scope": scope,
		"iat": now.Unix(), "exp": now.Add(p.accessTTL).Unix(), "jti": randToken(12),
	}, "at+jwt")
	if err != nil {
		return nil, oerr(http.StatusInternalServerError, "server_error", "sign failed")
	}
	idc := jwt.MapClaims{
		"iss": iss, "sub": sub, "aud": app.ClientID, "azp": app.ClientID,
		"iat": now.Unix(), "exp": now.Add(p.accessTTL).Unix(), "auth_time": authTime.Unix(),
	}
	if nonce != "" {
		idc["nonce"] = nonce
	}
	idt, err := p.keys.sign(idc, "JWT")
	if err != nil {
		return nil, oerr(http.StatusInternalServerError, "server_error", "sign failed")
	}
	if family == "" {
		family = randToken(16)[:22]
	}
	rt := randToken(32)
	row := &Refresh{AppID: app.ID, ClientID: app.ClientID, UserID: u.ID, FamilyID: family, TokenHash: sha256Hex(rt),
		Scope: scope, AuthTime: authTime, ExpiresAt: now.Add(p.refreshTTL)}
	row.TenantID = app.TenantID
	if err := p.st.createRefresh(ctx, row); err != nil {
		return nil, oerr(http.StatusInternalServerError, "server_error", "internal error")
	}
	return &tokenResponse{AccessToken: at, IDToken: idt, RefreshToken: rt, TokenType: "Bearer",
		ExpiresIn: int64(p.accessTTL.Seconds()), Scope: scope}, nil
}

// userClaims 按 scope 组装 userinfo 声明。
func userClaims(u *userRow, scope string) map[string]any {
	sc := strings.Fields(scope)
	out := map[string]any{"sub": u.ID.String()}
	if slices.Contains(sc, "profile") {
		name := u.Nickname
		if name == "" {
			name = u.Username
		}
		out["name"], out["preferred_username"], out["picture"] = name, u.Username, u.Avatar
		out["tenant_id"], out["dept"] = u.TenantID.String(), u.Dept
	}
	if slices.Contains(sc, "email") {
		out["email"] = u.Email
	}
	if slices.Contains(sc, "phone") {
		out["phone_number"] = u.Phone
	}
	if slices.Contains(sc, "roles") {
		out["roles"] = append([]string{}, u.RoleCodes...)
	}
	return out
}
