package sso

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"sync"
	"testing"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/idgen"
)

type memStore struct {
	mu      sync.Mutex
	app     *App
	user    *userRow
	refresh map[string]*Refresh // by hash
}

func (m *memStore) appByClientID(_ context.Context, id string) (*App, error) {
	if m.app != nil && m.app.ClientID == id {
		return m.app, nil
	}
	return nil, nil
}
func (m *memStore) loadUser(_ context.Context, id idgen.ID) (*userRow, error) {
	if m.user != nil && m.user.ID == id {
		return m.user, nil
	}
	return nil, nil
}
func (m *memStore) createRefresh(_ context.Context, r *Refresh) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refresh[r.TokenHash] = r
	return nil
}
func (m *memStore) consumeRefresh(_ context.Context, hash string, now time.Time) (*Refresh, refreshState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.refresh[hash]
	if r == nil {
		return nil, refreshUnknown, nil
	}
	if r.UsedAt != nil {
		return r, refreshReused, nil
	}
	r.UsedAt = &now
	return r, refreshOK, nil
}
func (m *memStore) revokeFamily(_ context.Context, fam string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, r := range m.refresh {
		if r.FamilyID == fam {
			delete(m.refresh, h)
		}
	}
	return nil
}
func (m *memStore) revokeUserApp(_ context.Context, cid string, uid idgen.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, r := range m.refresh {
		if r.ClientID == cid && r.UserID == uid {
			delete(m.refresh, h)
		}
	}
	return nil
}

const (
	testRedirect = "https://bi.example.com/callback"
	testSecret   = "s3cret-value-for-tests"
	testIss      = "https://idp.example.com"
)

func newTestProvider(t *testing.T) (*provider, *memStore) {
	t.Helper()
	ks, err := loadKeyStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{Name: "BI", ClientID: "nxsso_test", SecretHash: hashSecret(testSecret), Enabled: true,
		RedirectURIs: encodeList([]string{testRedirect}), RoleIDs: "[]"}
	app.ID, app.TenantID = 11, 1
	st := &memStore{app: app, refresh: map[string]*Refresh{},
		user: &userRow{ID: 42, TenantID: 1, Username: "alice", Nickname: "Alice", Email: "a@x.com", RoleIDs: []string{"5"}, RoleCodes: []string{"ops"}}}
	return &provider{st: st, keys: ks, cache: ch, accessTTL: time.Hour, refreshTTL: 24 * time.Hour, now: time.Now}, st
}

func pkcePair() (verifier, challenge string) {
	verifier = randToken(32)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorizeCode 走完授权步骤，返回 code 与 verifier。
func authorizeCode(t *testing.T, p *provider, st *memStore, redirect string) (code, verifier string, e *oauthErr) {
	t.Helper()
	v, ch := pkcePair()
	to, e := p.issueCode(context.Background(), st.app, st.user, authzParams{ClientID: st.app.ClientID, RedirectURI: redirect,
		Scope: "openid profile email", State: "st-1", Nonce: "n-1", Challenge: ch, Method: "S256"}, time.Now())
	if e != nil {
		return "", "", e
	}
	u, err := url.Parse(to)
	if err != nil || u.Query().Get("state") != "st-1" {
		t.Fatalf("state not echoed: %s", to)
	}
	return u.Query().Get("code"), v, nil
}

func exchange(p *provider, code, verifier, redirect string) (*tokenResponse, *oauthErr) {
	return p.token(context.Background(), testIss, tokenRequest{GrantType: "authorization_code", Code: code,
		RedirectURI: redirect, CodeVerifier: verifier, ClientID: "nxsso_test", ClientSecret: testSecret})
}

func TestPKCE(t *testing.T) {
	// RFC 7636 附录 B 的向量
	if !verifyPKCE("E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk") {
		t.Fatal("RFC 7636 vector must verify")
	}
	_, ch := pkcePair()
	if verifyPKCE(ch, "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk") || verifyPKCE(ch, "short") || verifyPKCE(ch, "") {
		t.Fatal("wrong or malformed verifier must fail")
	}
	p, st := newTestProvider(t)
	code, _, _ := authorizeCode(t, p, st, testRedirect)
	if _, e := exchange(p, code, "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk", testRedirect); e == nil || e.Code != "invalid_grant" {
		t.Fatalf("wrong verifier should be invalid_grant, got %v", e)
	}
	// plain 方法与缺失 challenge 都被拒
	_, ch2 := pkcePair()
	if _, e := p.issueCode(context.Background(), st.app, st.user, authzParams{ClientID: "nxsso_test", RedirectURI: testRedirect,
		Scope: "openid", Challenge: ch2, Method: "plain"}, time.Now()); e == nil {
		t.Fatal("plain must be rejected")
	}
	if _, e := p.issueCode(context.Background(), st.app, st.user, authzParams{ClientID: "nxsso_test", RedirectURI: testRedirect,
		Scope: "openid"}, time.Now()); e == nil {
		t.Fatal("missing challenge must be rejected")
	}
}

func TestCodeSingleUse(t *testing.T) {
	p, st := newTestProvider(t)
	code, v, e := authorizeCode(t, p, st, testRedirect)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := exchange(p, code, v, testRedirect); e != nil {
		t.Fatalf("first exchange: %v", e)
	}
	if _, e := exchange(p, code, v, testRedirect); e == nil || e.Code != "invalid_grant" {
		t.Fatalf("code reuse must fail with invalid_grant, got %v", e)
	}
	// 校验失败也会烧掉 code
	code2, v2, _ := authorizeCode(t, p, st, testRedirect)
	_, _ = exchange(p, code2, v2, "https://evil.example.com/cb")
	if _, e := exchange(p, code2, v2, testRedirect); e == nil {
		t.Fatal("failed attempt must also consume the code")
	}
}
