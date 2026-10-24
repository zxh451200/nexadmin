package oauth

import (
	"context"
	"net/url"
)

// ---- 钉钉（扫码登录，新版 OAuth2：login.dingtalk.com + api.dingtalk.com v1.0）----
// client_id = AppKey，client_secret = AppSecret。ExternalID 用 unionId（跨应用稳定）。

type dingtalk struct {
	c    Config
	hc   httpDoer
	auth string
	api  string
}

func newDingtalk(c Config, hc httpDoer) Provider {
	return &dingtalk{c: c, hc: hc, auth: "https://login.dingtalk.com/oauth2/auth", api: "https://api.dingtalk.com"}
}

func (d *dingtalk) Name() string   { return "dingtalk" }
func (d *dingtalk) UsesPKCE() bool { return false }

func (d *dingtalk) AuthURL(state, redirect string, _ *PKCE) string {
	return withQuery(d.auth, url.Values{
		"client_id": {d.c.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {"openid"}, "state": {state}, "prompt": {"consent"},
	})
}

func (d *dingtalk) Exchange(ctx context.Context, code, _ string, _ *PKCE) (*Identity, error) {
	var tok struct {
		AccessToken string `json:"accessToken"`
	}
	err := postJSON(ctx, d.hc, d.api+"/v1.0/oauth2/userAccessToken", map[string]string{
		"clientId": d.c.ClientID, "clientSecret": d.c.ClientSecret, "code": code, "grantType": "authorization_code",
	}, &tok)
	if err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, fail("dingtalk: empty access token")
	}
	var me struct {
		UnionID   string `json:"unionId"`
		OpenID    string `json:"openId"`
		Nick      string `json:"nick"`
		AvatarURL string `json:"avatarUrl"`
		Mobile    string `json:"mobile"`
		Email     string `json:"email"`
	}
	if err := getJSON(ctx, d.hc, d.api+"/v1.0/contact/users/me", map[string]string{"x-acs-dingtalk-access-token": tok.AccessToken}, &me); err != nil {
		return nil, err
	}
	ext := firstNonEmpty(me.UnionID, me.OpenID)
	if ext == "" {
		return nil, fail("dingtalk: no user id")
	}
	// 钉钉的邮箱由用户自填，不视为已验证；手机号来自实名账号。
	return &Identity{Provider: "dingtalk", ExternalID: ext, Name: me.Nick, Email: me.Email, Phone: me.Mobile, Avatar: me.AvatarURL}, nil
}
