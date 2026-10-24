package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestPKCE(t *testing.T) {
	// RFC 7636 附录 B 的示例
	p := PKCEFromVerifier("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if p.Challenge != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challenge = %s", p.Challenge)
	}
	a, _ := NewPKCE()
	b, _ := NewPKCE()
	if a.Verifier == b.Verifier || len(a.Verifier) < 43 {
		t.Fatal("verifier must be random and >= 43 chars")
	}
}

func query(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestAuthURLs(t *testing.T) {
	hc, _ := NewHTTPClient("", 0)
	for _, name := range []string{"wecom", "dingtalk", "feishu", "github", "google"} {
		p, err := New(name, Config{ClientID: "cid", ClientSecret: "sec", AgentID: "1000002"}, hc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var pk *PKCE
		if p.UsesPKCE() {
			pk = PKCEFromVerifier("v")
		}
		q := query(t, p.AuthURL("st", "https://app.example.com/cb", pk))
		if q.Get("state") != "st" {
			t.Errorf("%s: state missing", name)
		}
		if q.Get("redirect_uri") != "https://app.example.com/cb" {
			t.Errorf("%s: redirect_uri = %q", name, q.Get("redirect_uri"))
		}
		if (q.Get("code_challenge") != "") != p.UsesPKCE() || (p.UsesPKCE() && q.Get("code_challenge_method") != "S256") {
			t.Errorf("%s: PKCE params wrong: %v", name, q)
		}
	}
	if _, err := New("wecom", Config{ClientID: "a", ClientSecret: "b"}, hc); err == nil {
		t.Error("wecom without agent_id must fail")
	}
	if _, err := New("oidc", Config{ClientID: "a", ClientSecret: "b"}, hc); err == nil {
		t.Error("oidc without issuer must fail")
	}
	if _, err := New("github", Config{}, hc); err == nil {
		t.Error("missing client credentials must fail")
	}
}

func TestGithubExchangeVerifiedPrimaryEmail(t *testing.T) {
	run := func(emails string) *Identity {
		mux := http.NewServeMux()
		mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.Form.Get("code") != "c1" || r.Form.Get("client_secret") != "sec" {
				w.WriteHeader(400)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"tok"}`))
		})
		mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"id":42,"login":"octo","name":"","avatar_url":"http://a/x.png","email":"public@x.com"}`))
		})
		mux.HandleFunc("/user/emails", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(emails)) })
		srv := httptest.NewServer(mux)
		defer srv.Close()
		hc, _ := NewHTTPClient("", 0)
		g := newGithub(Config{ClientID: "cid", ClientSecret: "sec"}, hc).(*github)
		g.token, g.api = srv.URL+"/token", srv.URL
		id, err := g.Exchange(context.Background(), "c1", "https://app/cb", nil)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := run(`[{"email":"a@x.com","primary":false,"verified":true},{"email":"me@corp.com","primary":true,"verified":true}]`)
	if id.ExternalID != "42" || id.Name != "octo" || id.Email != "me@corp.com" || !id.EmailVerified {
		t.Fatalf("unexpected identity %+v", id)
	}
	// 主邮箱未验证：不带邮箱（也不能用 /user 里的公开邮箱）
	if id := run(`[{"email":"me@corp.com","primary":true,"verified":false},{"email":"b@corp.com","primary":false,"verified":true}]`); id.Email != "" || id.EmailVerified {
		t.Fatalf("unverified primary must be dropped: %+v", id)
	}
	if id := run(`[{"email":"1+octo@users.noreply.github.com","primary":true,"verified":true}]`); id.Email != "" {
		t.Fatalf("noreply address must be dropped: %+v", id)
	}
}

func TestGithubTokenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":"bad_verification_code"}`))
	}))
	defer srv.Close()
	hc, _ := NewHTTPClient("", 0)
	g := newGithub(Config{ClientID: "a", ClientSecret: "b"}, hc).(*github)
	g.token = srv.URL
	if _, err := g.Exchange(context.Background(), "x", "r", nil); err == nil {
		t.Fatal("want error")
	}
}

func TestWecomExchange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cgi-bin/gettoken", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("corpid") != "corp" || r.URL.Query().Get("corpsecret") != "sec" {
			_, _ = w.Write([]byte(`{"errcode":40013}`))
			return
		}
		_, _ = w.Write([]byte(`{"errcode":0,"access_token":"AT"}`))
	})
	mux.HandleFunc("/cgi-bin/auth/getuserinfo", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("code") {
		case "member":
			_, _ = w.Write([]byte(`{"errcode":0,"userid":"zhangsan"}`))
		default: // 非企业成员只有 openid
			_, _ = w.Write([]byte(`{"errcode":0,"openid":"oXYZ"}`))
		}
	})
	mux.HandleFunc("/cgi-bin/user/get", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":0,"name":"张三","mobile":"13800138000","email":"self@x.com","biz_mail":"zs@corp.com","avatar":"http://a"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	hc, _ := NewHTTPClient("", 0)
	p, _ := newWecom(Config{ClientID: "corp", ClientSecret: "sec", AgentID: "1"}, hc)
	w := p.(*wecom)
	w.api = srv.URL
	id, err := w.Exchange(context.Background(), "member", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id.ExternalID != "zhangsan" || id.Name != "张三" || id.Phone != "13800138000" || id.Email != "zs@corp.com" || !id.EmailVerified || id.EmployeeNo != "zhangsan" {
		t.Fatalf("unexpected identity %+v", id)
	}
	if _, err := w.Exchange(context.Background(), "guest", "", nil); err == nil {
		t.Fatal("non-member must be rejected")
	}
}

func TestDingtalkExchange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/oauth2/userAccessToken", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["code"] != "c" || in["clientSecret"] != "sec" || in["grantType"] != "authorization_code" {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte(`{"accessToken":"AT"}`))
	})
	mux.HandleFunc("/v1.0/contact/users/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-acs-dingtalk-access-token") != "AT" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"nick":"李四","unionId":"U1","openId":"O1","mobile":"13900139000","email":"ls@x.com","avatarUrl":"http://a"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	hc, _ := NewHTTPClient("", 0)
	d := newDingtalk(Config{ClientID: "k", ClientSecret: "sec"}, hc).(*dingtalk)
	d.api = srv.URL
	id, err := d.Exchange(context.Background(), "c", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id.ExternalID != "U1" || id.Name != "李四" || id.Phone != "13900139000" || id.Email != "ls@x.com" || id.EmailVerified {
		t.Fatalf("unexpected identity %+v", id)
	}
}

func TestFeishuExchange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/authen/v2/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["code"] != "c" || in["redirect_uri"] != "https://app/cb" {
			_, _ = w.Write([]byte(`{"code":20001}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"access_token":"AT"}`))
	})
	mux.HandleFunc("/open-apis/authen/v1/user_info", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer AT" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"name":"王五","union_id":"un_1","open_id":"ou_1","email":"p@x.com","enterprise_email":"ww@corp.com","mobile":"+8613700137000","employee_no":"E100"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	hc, _ := NewHTTPClient("", 0)
	f := newFeishu(Config{ClientID: "a", ClientSecret: "b"}, hc).(*feishu)
	f.api = srv.URL
	id, err := f.Exchange(context.Background(), "c", "https://app/cb", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id.ExternalID != "un_1" || id.Email != "ww@corp.com" || !id.EmailVerified || id.EmployeeNo != "E100" {
		t.Fatalf("unexpected identity %+v", id)
	}
	if _, err := f.Exchange(context.Background(), "bad", "https://app/cb", nil); err == nil {
		t.Fatal("want error on failed token")
	}
}

// ---- OIDC ----

type idp struct {
	srv      *httptest.Server
	key      *rsa.PrivateKey
	kid      string
	claims   func(c jwt.MapClaims)
	gotForm  url.Values
	userinfo string
	jwksHits int
}

func newIDP(t *testing.T) *idp {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &idp{key: k, kid: "k1"}
	mux := http.NewServeMux()
	p.srv = httptest.NewServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/auth",
			"token_endpoint": p.srv.URL + "/token", "jwks_uri": p.srv.URL + "/jwks", "userinfo_endpoint": p.srv.URL + "/userinfo"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		p.jwksHits++
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes())
		n := base64.RawURLEncoding.EncodeToString(k.N.Bytes())
		_, _ = w.Write([]byte(`{"keys":[{"kty":"RSA","kid":"` + p.kid + `","n":"` + n + `","e":"` + e + `"}]}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(p.userinfo)) })
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		p.gotForm = r.Form
		c := jwt.MapClaims{"iss": p.srv.URL, "aud": "cid", "sub": "user-1", "exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(), "email": "u@corp.com", "email_verified": true, "name": "U", "nonce": r.Form.Get("nonce_for_test")}
		if p.claims != nil {
			p.claims(c)
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
		tok.Header["kid"] = "k1"
		s, _ := tok.SignedString(k)
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": s, "access_token": "AT"})
	})
	t.Cleanup(p.srv.Close)
	return p
}

func (p *idp) provider(t *testing.T) Provider {
	hc, _ := NewHTTPClient("", 0)
	o, err := New("oidc", Config{ClientID: "cid", ClientSecret: "sec", Issuer: p.srv.URL}, hc)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestOIDCSuccessAndPKCE(t *testing.T) {
	p := newIDP(t)
	o := p.provider(t)
	pk, _ := NewPKCE()
	p.claims = func(c jwt.MapClaims) { c["nonce"] = pk.Challenge }
	q := query(t, o.AuthURL("st", "https://app/cb", pk))
	if q.Get("code_challenge") != pk.Challenge || q.Get("code_challenge_method") != "S256" || q.Get("state") != "st" || q.Get("nonce") != pk.Challenge {
		t.Fatalf("auth params: %v", q)
	}
	if !strings.Contains(q.Get("scope"), "openid") {
		t.Fatalf("scope: %q", q.Get("scope"))
	}
	id, err := o.Exchange(context.Background(), "code1", "https://app/cb", pk)
	if err != nil {
		t.Fatal(err)
	}
	if id.ExternalID != "user-1" || id.Email != "u@corp.com" || !id.EmailVerified || id.Provider != "oidc" {
		t.Fatalf("identity %+v", id)
	}
	if p.gotForm.Get("code_verifier") != pk.Verifier || p.gotForm.Get("code") != "code1" || p.gotForm.Get("redirect_uri") != "https://app/cb" {
		t.Fatalf("token request: %v", p.gotForm)
	}
}

func TestOIDCRejectsBadTokens(t *testing.T) {
	cases := map[string]func(c jwt.MapClaims){
		"wrong audience": func(c jwt.MapClaims) { c["aud"] = "someone-else" },
		"wrong issuer":   func(c jwt.MapClaims) { c["iss"] = "https://evil.example" },
		"expired":        func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"no subject":     func(c jwt.MapClaims) { delete(c, "sub") },
		"nonce mismatch": func(c jwt.MapClaims) { c["nonce"] = "other" },
	}
	for name, mut := range cases {
		p := newIDP(t)
		o := p.provider(t)
		pk, _ := NewPKCE()
		p.claims = func(c jwt.MapClaims) { c["nonce"] = pk.Challenge; mut(c) }
		if _, err := o.Exchange(context.Background(), "c", "https://app/cb", pk); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestOIDCRejectsForgedSignature(t *testing.T) {
	p := newIDP(t)
	o := p.provider(t)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	p.key = other // JWKS 里仍是原密钥？不：JWKS 用闭包里的 k；这里改 token 签名密钥需另起 handler
	_ = o
	// 直接构造一个用别的密钥签名的 token，走 verify
	ob := o.(*oidc)
	m, err := ob.metadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := jwt.MapClaims{"iss": p.srv.URL, "aud": "cid", "sub": "x", "exp": time.Now().Add(time.Hour).Unix()}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
	tok.Header["kid"] = "k1"
	s, _ := tok.SignedString(other)
	if _, err := ob.verify(context.Background(), m, s); err == nil {
		t.Fatal("token signed by an unknown key must be rejected")
	}
	// alg=none 也必须拒绝
	none := jwt.NewWithClaims(jwt.SigningMethodNone, c)
	ns, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := ob.verify(context.Background(), m, ns); err == nil {
		t.Fatal("alg=none must be rejected")
	}
}

func TestOIDCUserinfoFallbackAndDiscoveryIssuerCheck(t *testing.T) {
	p := newIDP(t)
	o := p.provider(t)
	pk, _ := NewPKCE()
	p.claims = func(c jwt.MapClaims) { c["nonce"] = pk.Challenge; delete(c, "email"); delete(c, "email_verified") }
	p.userinfo = `{"sub":"user-1","email":"ui@corp.com","email_verified":"true"}`
	id, err := o.Exchange(context.Background(), "c", "https://app/cb", pk)
	if err != nil || id.Email != "ui@corp.com" || !id.EmailVerified {
		t.Fatalf("userinfo fallback: %+v %v", id, err)
	}
	p.userinfo = `{"sub":"someone-else","email":"x@corp.com","email_verified":true}` // sub 不一致：忽略
	if id, _ := newIDP2(t, p).Exchange(context.Background(), "c", "https://app/cb", pk); id == nil || id.Email != "" {
		t.Fatalf("userinfo with different sub must be ignored: %+v", id)
	}
	hc, _ := NewHTTPClient("", 0)
	bad, _ := New("oidc", Config{ClientID: "cid", ClientSecret: "s", Issuer: p.srv.URL + "/"}, hc)
	if bad.AuthURL("s", "r", pk) == "" {
		t.Fatal("trailing slash on issuer should be tolerated")
	}
	mismatch, _ := New("oidc", Config{ClientID: "cid", ClientSecret: "s", Issuer: p.srv.URL + "/x"}, hc)
	if mismatch.AuthURL("s", "r", pk) != "" {
		t.Fatal("discovery failure must yield empty AuthURL")
	}
}

func newIDP2(t *testing.T, p *idp) Provider { return p.provider(t) }

func TestGoogleUsesPresetEndpointsWithoutDiscovery(t *testing.T) {
	hc, _ := NewHTTPClient("", 0)
	g, _ := New("google", Config{ClientID: "cid", ClientSecret: "s"}, hc)
	pk := PKCEFromVerifier("v")
	u := g.AuthURL("st", "https://app/cb", pk)
	if !strings.HasPrefix(u, "https://accounts.google.com/o/oauth2/v2/auth?") {
		t.Fatalf("google auth url: %s", u)
	}
}

func TestProxyClient(t *testing.T) {
	if _, err := NewHTTPClient("not a url", 0); err == nil {
		t.Fatal("invalid proxy must error")
	}
	hc, err := NewHTTPClient("http://127.0.0.1:3128", 3*time.Second)
	if err != nil || hc.Timeout != 3*time.Second {
		t.Fatalf("client: %v %v", hc, err)
	}
}
