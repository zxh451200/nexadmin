package oauth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type tokenResp struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
}

type oidcClaims struct {
	jwt.RegisteredClaims
	Nonce         string `json:"nonce"`
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"`
	Name          string `json:"name"`
	Preferred     string `json:"preferred_username"`
	Picture       string `json:"picture"`
	Phone         string `json:"phone_number"`
	PhoneVerified any    `json:"phone_number_verified"`
}

// isTrue 兼容 bool 与 "true" 字符串两种写法。
func isTrue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true"
	}
	return false
}

func (o *oidc) Exchange(ctx context.Context, code, redirect string, pkce *PKCE) (*Identity, error) {
	m, err := o.metadata(ctx)
	if err != nil {
		return nil, err
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"client_id": {o.c.ClientID}, "client_secret": {o.c.ClientSecret}}
	if pkce != nil {
		form.Set("code_verifier", pkce.Verifier)
	}
	var tok tokenResp
	if err := postForm(ctx, o.hc, m.TokenURL, form, &tok); err != nil {
		return nil, err
	}
	if tok.IDToken == "" {
		return nil, fail("%s: no id_token", o.name)
	}
	cl, err := o.verify(ctx, m, tok.IDToken)
	if err != nil {
		return nil, err
	}
	if pkce != nil && cl.Nonce != pkce.Challenge {
		return nil, fail("%s: nonce mismatch", o.name)
	}
	id := &Identity{Provider: o.name, ExternalID: cl.Subject, Name: firstNonEmpty(cl.Name, cl.Preferred),
		Email: cl.Email, EmailVerified: isTrue(cl.EmailVerified), Avatar: cl.Picture}
	if cl.Phone != "" && (cl.PhoneVerified == nil || isTrue(cl.PhoneVerified)) {
		id.Phone = cl.Phone
	}
	if id.Email == "" && m.UserInfo != "" && tok.AccessToken != "" { // id_token 不带邮箱时补一次 userinfo，sub 必须一致
		var ui oidcClaims
		hdr := map[string]string{"Authorization": "Bearer " + tok.AccessToken}
		if getJSON(ctx, o.hc, m.UserInfo, hdr, &ui) == nil && ui.Subject == cl.Subject {
			id.Email, id.EmailVerified = ui.Email, isTrue(ui.EmailVerified)
			id.Name = firstNonEmpty(id.Name, ui.Name, ui.Preferred)
		}
	}
	return id, nil
}

// verify 校验 id_token：签名（JWKS）、iss、aud、exp，且必须有 sub。
func (o *oidc) verify(ctx context.Context, m *oidcMeta, raw string) (*oidcClaims, error) {
	var cl oidcClaims
	_, err := jwt.ParseWithClaims(raw, &cl, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return o.key(ctx, m, kid)
	}, jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384"}),
		jwt.WithAudience(o.c.ClientID), jwt.WithExpirationRequired(), jwt.WithLeeway(30*time.Second))
	if err != nil {
		return nil, fail("%s id_token: %v", o.name, err)
	}
	okIss := false
	for _, i := range o.issuer {
		okIss = okIss || cl.Issuer == i || cl.Issuer == i+"/"
	}
	if !okIss || cl.Subject == "" {
		return nil, fail("%s id_token: bad iss/sub", o.name)
	}
	return &cl, nil
}

// key 按 kid 取公钥；未知 kid 时最多每分钟重取一次 JWKS（应对密钥轮换）。
func (o *oidc) key(ctx context.Context, m *oidcMeta, kid string) (crypto.PublicKey, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	pick := func() crypto.PublicKey {
		if k, ok := o.keys[kid]; ok {
			return k
		}
		if kid == "" && len(o.keys) == 1 {
			for _, k := range o.keys {
				return k
			}
		}
		return nil
	}
	if k := pick(); k != nil && time.Since(o.keysAt) < metaTTL {
		return k, nil
	}
	if o.keys != nil && time.Since(o.keysAt) < time.Minute {
		return nil, fail("%s: unknown signing key", o.name)
	}
	var set struct {
		Keys []struct {
			Kty, Kid, Crv, N, E, X, Y string
		} `json:"keys"`
	}
	if err := getJSON(ctx, o.hc, m.JWKSURI, nil, &set); err != nil {
		return nil, err
	}
	o.keys, o.keysAt = map[string]crypto.PublicKey{}, time.Now()
	for _, k := range set.Keys {
		if pub := parseJWK(k.Kty, k.Crv, k.N, k.E, k.X, k.Y); pub != nil {
			o.keys[k.Kid] = pub
		}
	}
	if k := pick(); k != nil {
		return k, nil
	}
	return nil, fail("%s: unknown signing key", o.name)
}

func b64(s string) []byte { b, _ := base64.RawURLEncoding.DecodeString(s); return b }

func parseJWK(kty, crv, n, e, x, y string) crypto.PublicKey {
	switch kty {
	case "RSA":
		if n == "" || e == "" {
			return nil
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(b64(n)), E: int(new(big.Int).SetBytes(b64(e)).Int64())}
	case "EC":
		var c elliptic.Curve
		switch crv {
		case "P-256":
			c = elliptic.P256()
		case "P-384":
			c = elliptic.P384()
		default:
			return nil
		}
		pub := &ecdsa.PublicKey{Curve: c, X: new(big.Int).SetBytes(b64(x)), Y: new(big.Int).SetBytes(b64(y))}
		if !c.IsOnCurve(pub.X, pub.Y) {
			return nil
		}
		return pub
	}
	return nil
}
