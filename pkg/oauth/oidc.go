package oauth

import (
	"context"
	"crypto"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ---- 通用 OIDC（授权码 + PKCE；discovery + JWKS 校验 id_token）----
// Google 是同一个适配器加预设地址；Keycloak、Casdoor 等只需配 issuer。
// 没引入 go-oidc：discovery、JWKS 解析与 id_token 校验用标准库 + golang-jwt 手写，约两百行。

const metaTTL = time.Hour

type oidcMeta struct {
	Issuer   string `json:"issuer"`
	AuthURL  string `json:"authorization_endpoint"`
	TokenURL string `json:"token_endpoint"`
	UserInfo string `json:"userinfo_endpoint"`
	JWKSURI  string `json:"jwks_uri"`
}

type oidc struct {
	name   string
	c      Config
	hc     httpDoer
	scopes []string
	issuer []string // 可接受的 iss 值

	mu     sync.Mutex
	meta   *oidcMeta
	metaAt time.Time
	keys   map[string]crypto.PublicKey
	keysAt time.Time
}

func newOIDC(name string, c Config, hc httpDoer) (Provider, error) {
	o := &oidc{name: name, c: c, hc: hc, scopes: c.Scopes}
	if len(o.scopes) == 0 {
		o.scopes = []string{"openid", "email", "profile"}
	}
	iss := strings.TrimRight(c.Issuer, "/")
	if iss == "" && (c.AuthURL == "" || c.TokenURL == "" || c.JWKSURL == "") {
		return nil, fmt.Errorf("oauth: %s: issuer is required", name)
	}
	o.issuer = []string{iss}
	if name == "google" && iss == "https://accounts.google.com" { // 预设地址：不依赖 discovery，少一次外网往返
		o.issuer = append(o.issuer, "accounts.google.com")
		o.meta = &oidcMeta{Issuer: iss, AuthURL: firstNonEmpty(c.AuthURL, "https://accounts.google.com/o/oauth2/v2/auth"),
			TokenURL: firstNonEmpty(c.TokenURL, "https://oauth2.googleapis.com/token"),
			UserInfo: firstNonEmpty(c.UserInfoURL, "https://openidconnect.googleapis.com/v1/userinfo"),
			JWKSURI:  firstNonEmpty(c.JWKSURL, "https://www.googleapis.com/oauth2/v3/certs")}
		o.metaAt = time.Now().Add(100 * 365 * 24 * time.Hour)
	}
	return o, nil
}

func (o *oidc) Name() string   { return o.name }
func (o *oidc) UsesPKCE() bool { return true }

// metadata 返回端点：手工配置的优先，其次缓存的 discovery，最后现取。
func (o *oidc) metadata(ctx context.Context) (*oidcMeta, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.meta != nil && time.Now().Before(o.metaAt.Add(metaTTL)) {
		return o.meta, nil
	}
	if o.c.AuthURL != "" && o.c.TokenURL != "" && o.c.JWKSURL != "" {
		o.meta = &oidcMeta{Issuer: o.issuer[0], AuthURL: o.c.AuthURL, TokenURL: o.c.TokenURL, UserInfo: o.c.UserInfoURL, JWKSURI: o.c.JWKSURL}
		o.metaAt = time.Now()
		return o.meta, nil
	}
	var m oidcMeta
	if err := getJSON(ctx, o.hc, o.issuer[0]+"/.well-known/openid-configuration", nil, &m); err != nil {
		return nil, err
	}
	if strings.TrimRight(m.Issuer, "/") != o.issuer[0] {
		return nil, fail("%s discovery issuer mismatch", o.name)
	}
	if m.AuthURL == "" || m.TokenURL == "" || m.JWKSURI == "" {
		return nil, fail("%s discovery incomplete", o.name)
	}
	o.meta, o.metaAt = &m, time.Now()
	return o.meta, nil
}

// AuthURL 返回授权地址；discovery 失败时返回空串（调用方按失败处理）。
func (o *oidc) AuthURL(state, redirect string, pkce *PKCE) string {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	m, err := o.metadata(ctx)
	if err != nil {
		return ""
	}
	v := url.Values{"client_id": {o.c.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {strings.Join(o.scopes, " ")}, "state": {state}}
	if pkce != nil {
		v.Set("code_challenge", pkce.Challenge)
		v.Set("code_challenge_method", "S256")
		v.Set("nonce", pkce.Challenge)
	}
	return withQuery(m.AuthURL, v)
}
