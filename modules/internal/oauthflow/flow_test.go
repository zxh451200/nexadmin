package oauthflow

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/oauth"
)

type clock struct{ t time.Time }

type tcache struct {
	clk  *clock
	mu   sync.Mutex
	data map[string]struct {
		v   string
		exp time.Time
	}
}

func (c *tcache) Get(_ context.Context, k string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.data[k]
	if !ok || (!it.exp.IsZero() && !c.clk.t.Before(it.exp)) {
		return "", cache.ErrNotFound
	}
	return it.v, nil
}
func (c *tcache) Set(_ context.Context, k, v string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := struct {
		v   string
		exp time.Time
	}{v: v}
	if ttl > 0 {
		e.exp = c.clk.t.Add(ttl)
	}
	c.data[k] = e
	return nil
}
func (c *tcache) Del(_ context.Context, ks ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range ks {
		delete(c.data, k)
	}
	return nil
}

// GetDel 实现 cache.AtomicGetDel：同一把锁内取出并删除。
func (c *tcache) GetDel(_ context.Context, k string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.data[k]
	delete(c.data, k)
	if !ok || (!it.exp.IsZero() && !c.clk.t.Before(it.exp)) {
		return "", cache.ErrNotFound
	}
	return it.v, nil
}
func (c *tcache) Exists(ctx context.Context, k string) (bool, error) {
	_, err := c.Get(ctx, k)
	return err == nil, nil
}
func (c *tcache) Close() error { return nil }

type stub struct {
	pkce         bool
	gotVerifier  string
	gotRedirect  string
	gotCode      string
	exchangeCall int
}

func (s *stub) Name() string   { return "oidc" }
func (s *stub) UsesPKCE() bool { return s.pkce }
func (s *stub) AuthURL(state, redirect string, p *oauth.PKCE) string {
	v := url.Values{"state": {state}, "redirect_uri": {redirect}}
	if p != nil {
		v.Set("code_challenge", p.Challenge)
		v.Set("code_challenge_method", "S256")
	}
	return "https://idp.example/auth?" + v.Encode()
}
func (s *stub) Exchange(_ context.Context, code, redirect string, p *oauth.PKCE) (*oauth.Identity, error) {
	s.exchangeCall++
	s.gotCode, s.gotRedirect = code, redirect
	if p != nil {
		s.gotVerifier = p.Verifier
	}
	return &oauth.Identity{ExternalID: "u1", Email: "a@corp.com", EmailVerified: true}, nil
}

const cb = "https://admin.example.com/login/oauth/oidc"

func newFlow(t *testing.T, pkce bool) (*Flow, *stub, *clock) {
	t.Helper()
	clk := &clock{t: time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)}
	cfg := &config.Config{}
	cfg.Auth.Providers = map[string]any{
		"oidc":   map[string]any{"client_id": "cid", "client_secret": "sec", "issuer": "https://idp.example", "redirect_uris": []any{cb}},
		"github": map[string]any{"client_id": "cid"}, // 缺 secret / redirect_uris：视为未配置
		"bogus":  map[string]any{"client_id": "a", "client_secret": "b", "redirect_uris": []any{cb}},
	}
	st := &stub{pkce: pkce}
	f := &Flow{cfg: cfg, cache: &tcache{clk: clk, data: map[string]struct {
		v   string
		exp time.Time
	}{}}, log: zap.NewNop(), adapters: map[string]oauth.Provider{}}
	f.build = func(string, config.ProviderConf) (oauth.Provider, error) { return st, nil }
	return f, st, clk
}

func stateOf(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

func TestOnlyConfiguredProvidersAvailable(t *testing.T) {
	f, _, _ := newFlow(t, true)
	got := f.Available()
	if len(got) != 1 || got[0] != "oidc" {
		t.Fatalf("available = %v, want [oidc] (incomplete and unknown providers must be hidden)", got)
	}
	if _, err := f.Start(context.Background(), "github", cb, PurposeLogin, 0); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unconfigured provider: %v", err)
	}
	if _, err := f.Start(context.Background(), "bogus", cb, PurposeLogin, 0); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unknown provider: %v", err)
	}
}

func TestRedirectWhitelist(t *testing.T) {
	f, _, _ := newFlow(t, true)
	ctx := context.Background()
	for _, bad := range []string{"", "https://evil.example.com/cb", cb + "/", cb + "?x=1", "http://admin.example.com/login/oauth/oidc", "https://admin.example.com.evil.com/login/oauth/oidc"} {
		if _, err := f.Start(ctx, "oidc", bad, PurposeLogin, 0); !errors.Is(err, ErrRedirect) {
			t.Errorf("redirect %q should be rejected, got %v", bad, err)
		}
	}
	if _, err := f.Start(ctx, "oidc", cb, PurposeLogin, 0); err != nil {
		t.Fatalf("whitelisted redirect rejected: %v", err)
	}
}

func TestStateIsSingleUse(t *testing.T) {
	f, st, _ := newFlow(t, true)
	ctx := context.Background()
	u, err := f.Start(ctx, "oidc", cb, PurposeLogin, 0)
	if err != nil {
		t.Fatal(err)
	}
	state := stateOf(t, u)
	id, rec, err := f.Finish(ctx, "oidc", "code-1", state)
	if err != nil || id.ExternalID != "u1" || rec.Purpose != PurposeLogin {
		t.Fatalf("first use: %+v %v", id, err)
	}
	if st.gotCode != "code-1" || st.gotRedirect != cb {
		t.Fatalf("exchange args: %+v", st)
	}
	if _, _, err := f.Finish(ctx, "oidc", "code-1", state); !errors.Is(err, ErrState) {
		t.Fatalf("replayed state must fail, got %v", err)
	}
	if st.exchangeCall != 1 {
		t.Fatalf("exchange must not run on replay, calls=%d", st.exchangeCall)
	}
}

func TestStateExpiresAfterFiveMinutes(t *testing.T) {
	f, st, clk := newFlow(t, true)
	ctx := context.Background()
	u, _ := f.Start(ctx, "oidc", cb, PurposeLogin, 0)
	clk.t = clk.t.Add(StateTTL + time.Second)
	if _, _, err := f.Finish(ctx, "oidc", "c", stateOf(t, u)); !errors.Is(err, ErrState) {
		t.Fatalf("expired state: %v", err)
	}
	if st.exchangeCall != 0 {
		t.Fatal("must not exchange with an expired state")
	}
	u2, _ := f.Start(ctx, "oidc", cb, PurposeLogin, 0)
	clk.t = clk.t.Add(StateTTL - time.Second)
	if _, _, err := f.Finish(ctx, "oidc", "c", stateOf(t, u2)); err != nil {
		t.Fatalf("state within TTL must work: %v", err)
	}
}

func TestStateBoundToProviderAndUnknownRejected(t *testing.T) {
	f, _, _ := newFlow(t, true)
	ctx := context.Background()
	u, _ := f.Start(ctx, "oidc", cb, PurposeLogin, 0)
	if _, _, err := f.Finish(ctx, "github", "c", stateOf(t, u)); !errors.Is(err, ErrState) {
		t.Fatalf("state used on another provider: %v", err)
	}
	for _, s := range []string{"", "nope"} {
		if _, _, err := f.Finish(ctx, "oidc", "c", s); !errors.Is(err, ErrState) {
			t.Errorf("state %q: %v", s, err)
		}
	}
}

func TestPKCEParamsAndVerifierRoundTrip(t *testing.T) {
	f, st, _ := newFlow(t, true)
	ctx := context.Background()
	raw, _ := f.Start(ctx, "oidc", cb, PurposeLogin, 0)
	u, _ := url.Parse(raw)
	ch := u.Query().Get("code_challenge")
	if ch == "" || u.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("PKCE params missing: %s", raw)
	}
	if _, _, err := f.Finish(ctx, "oidc", "c", u.Query().Get("state")); err != nil {
		t.Fatal(err)
	}
	if st.gotVerifier == "" || oauth.PKCEFromVerifier(st.gotVerifier).Challenge != ch {
		t.Fatal("verifier passed to Exchange must match the challenge sent in the auth URL")
	}
	f2, _, _ := newFlow(t, false)
	raw2, _ := f2.Start(ctx, "oidc", cb, PurposeLogin, 0)
	if u2, _ := url.Parse(raw2); u2.Query().Get("code_challenge") != "" {
		t.Fatal("non-PKCE provider must not get PKCE params")
	}
}

func TestBindPurposeCarriesUser(t *testing.T) {
	f, _, _ := newFlow(t, false)
	ctx := context.Background()
	u, _ := f.Start(ctx, "oidc", cb, PurposeBind, 12345)
	_, rec, err := f.Finish(ctx, "oidc", "c", stateOf(t, u))
	if err != nil || rec.Purpose != PurposeBind || rec.UID != "12345" {
		t.Fatalf("%+v %v", rec, err)
	}
}

func TestBindTokenTriesAndExpiry(t *testing.T) {
	f, _, clk := newFlow(t, false)
	ctx := context.Background()
	tok, err := f.NewBindToken(ctx, &oauth.Identity{Provider: "oidc", ExternalID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxBindTries; i++ {
		id, err := f.LookupBindToken(ctx, tok)
		if err != nil || id.ExternalID != "u1" {
			t.Fatalf("try %d: %v", i+1, err)
		}
	}
	if _, err := f.LookupBindToken(ctx, tok); !errors.Is(err, ErrBindToken) {
		t.Fatalf("token must be void after %d tries: %v", MaxBindTries, err)
	}
	tok2, _ := f.NewBindToken(ctx, &oauth.Identity{ExternalID: "u2"})
	if err := f.DropBindToken(ctx, tok2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.LookupBindToken(ctx, tok2); !errors.Is(err, ErrBindToken) {
		t.Fatal("dropped token must be invalid")
	}
	tok3, _ := f.NewBindToken(ctx, &oauth.Identity{ExternalID: "u3"})
	clk.t = clk.t.Add(BindTokenTTL + time.Second)
	if _, err := f.LookupBindToken(ctx, tok3); !errors.Is(err, ErrBindToken) {
		t.Fatal("expired token must be invalid")
	}
}
