package sso

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestRedirectURIStrictMatch(t *testing.T) {
	p, st := newTestProvider(t)
	for _, bad := range []string{
		testRedirect + "/", testRedirect + "?a=1", "http://bi.example.com/callback", "https://BI.example.com/callback",
		"https://bi.example.com/callback/../x", "https://bi.example.com", "", "https://*.example.com/callback",
	} {
		if _, _, e := authorizeCode(t, p, st, bad); e == nil {
			t.Errorf("redirect_uri %q must be rejected", bad)
		}
	}
	if _, _, e := authorizeCode(t, p, st, testRedirect); e != nil {
		t.Fatalf("exact match must pass: %v", e)
	}
	for _, s := range []string{"https://*.example.com/cb", "https://a.com/cb#frag", "javascript:alert(1)", "/relative", "https://u:p@a.com/cb"} {
		if validRedirectURI(s) {
			t.Errorf("registration of %q must be rejected", s)
		}
	}
	if !validRedirectURI("http://localhost:3000/cb?x=1") {
		t.Error("localhost callback should be allowed")
	}
}

func TestClientAuthAndVisibility(t *testing.T) {
	p, st := newTestProvider(t)
	code, v, _ := authorizeCode(t, p, st, testRedirect)
	_, e := p.token(context.Background(), testIss, tokenRequest{GrantType: "authorization_code", Code: code, RedirectURI: testRedirect,
		CodeVerifier: v, ClientID: "nxsso_test", ClientSecret: "wrong"})
	_, e2 := p.token(context.Background(), testIss, tokenRequest{GrantType: "authorization_code", Code: code, RedirectURI: testRedirect,
		CodeVerifier: v, ClientID: "nope", ClientSecret: "wrong"})
	if e == nil || e2 == nil || *e != *e2 || e.Code != "invalid_client" || e.Status != 401 {
		t.Fatalf("unknown client and wrong secret must look identical: %v / %v", e, e2)
	}
	st.app.RoleIDs = encodeList([]string{"999"})
	if _, _, e := authorizeCode(t, p, st, testRedirect); e == nil || e.Code != "access_denied" {
		t.Fatalf("role-restricted app must be hidden from other roles, got %v", e)
	}
	st.app.RoleIDs = encodeList([]string{"5"})
	if _, _, e := authorizeCode(t, p, st, testRedirect); e != nil {
		t.Fatalf("matching role must pass: %v", e)
	}
	st.app.Enabled = false
	if _, _, e := authorizeCode(t, p, st, testRedirect); e == nil {
		t.Fatal("disabled app must be rejected")
	}
}

func TestIDTokenVerifiesWithJWKS(t *testing.T) {
	p, st := newTestProvider(t)
	code, v, _ := authorizeCode(t, p, st, testRedirect)
	resp, e := exchange(p, code, v, testRedirect)
	if e != nil {
		t.Fatal(e)
	}
	// 只用 JWKS 暴露的公钥验签，证明外部下游可以独立校验。
	keys := p.keys.jwks()["keys"].([]map[string]string)
	pubs := map[string]*rsa.PublicKey{}
	for _, k := range keys {
		n, _ := base64.RawURLEncoding.DecodeString(k["n"])
		eb, _ := base64.RawURLEncoding.DecodeString(k["e"])
		pubs[k["kid"]] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(eb).Int64())}
	}
	cl := jwt.MapClaims{}
	tok, err := jwt.ParseWithClaims(resp.IDToken, cl, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return pubs[kid], nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(testIss), jwt.WithAudience("nxsso_test"), jwt.WithExpirationRequired())
	if err != nil || !tok.Valid {
		t.Fatalf("id_token must verify with JWKS: %v", err)
	}
	if cl["sub"] != "42" || cl["nonce"] != "n-1" || cl["auth_time"] == nil || cl["iat"] == nil {
		t.Fatalf("unexpected claims: %v", cl)
	}
	if tok.Header["kid"] == "" || tok.Header["alg"] != "RS256" {
		t.Fatalf("bad header: %v", tok.Header)
	}
	// 篡改后验签失败
	if _, _, err := p.keys.parse(resp.IDToken[:len(resp.IDToken)-4] + "AAAA"); err == nil {
		t.Fatal("tampered token must fail")
	}
	// 轮换后旧 key 仍在 JWKS，旧 token 依旧可验，新 token 用新 kid。
	if err := p.keys.Rotate(); err != nil {
		t.Fatal(err)
	}
	if len(p.keys.jwks()["keys"].([]map[string]string)) != 2 {
		t.Fatal("rotation must keep the old key published")
	}
	if _, _, err := p.keys.parse(resp.IDToken); err != nil {
		t.Fatalf("old token must still verify after rotation: %v", err)
	}
}

func TestRefreshRotation(t *testing.T) {
	p, st := newTestProvider(t)
	code, v, _ := authorizeCode(t, p, st, testRedirect)
	first, e := exchange(p, code, v, testRedirect)
	if e != nil {
		t.Fatal(e)
	}
	refresh := func(rt string) (*tokenResponse, *oauthErr) {
		return p.token(context.Background(), testIss, tokenRequest{GrantType: "refresh_token", RefreshToken: rt,
			ClientID: "nxsso_test", ClientSecret: testSecret})
	}
	second, e := refresh(first.RefreshToken)
	if e != nil {
		t.Fatalf("refresh: %v", e)
	}
	if second.RefreshToken == first.RefreshToken || second.AccessToken == "" || second.IDToken == "" {
		t.Fatal("refresh must rotate the refresh token and issue new tokens")
	}
	if _, e := refresh(first.RefreshToken); e == nil || e.Code != "invalid_grant" {
		t.Fatalf("old refresh must be invalid, got %v", e)
	}
	// 旧令牌重放 => 整条链作废，连新的也不能用（疑似泄露）。
	if _, e := refresh(second.RefreshToken); e == nil || e.Code != "invalid_grant" {
		t.Fatalf("family must be revoked after reuse, got %v", e)
	}
	// 另一个 client 不能用别人的 refresh（此处用错误密钥模拟）。
	if _, e := p.token(context.Background(), testIss, tokenRequest{GrantType: "refresh_token", RefreshToken: "x",
		ClientID: "nxsso_test", ClientSecret: "bad"}); e == nil || e.Code != "invalid_client" {
		t.Fatal("bad secret must be invalid_client")
	}
}

func TestUserinfoClaimsByScope(t *testing.T) {
	_, st := newTestProvider(t)
	c := userClaims(st.user, "openid email")
	if c["sub"] != "42" || c["email"] != "a@x.com" || c["name"] != nil || c["roles"] != nil {
		t.Fatalf("scope filtering broken: %v", c)
	}
	c = userClaims(st.user, "openid profile roles phone")
	if c["name"] != "Alice" || c["preferred_username"] != "alice" || c["phone_number"] == nil || c["roles"] == nil {
		t.Fatalf("claims missing: %v", c)
	}
}
