package auth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/idgen"
)

// SessionBinding 必须同时落进令牌并回调 SessionTracker（登录/二次验证后的签发都走 passGates）。
func TestSessionBindingReachesTokenAndTracker(t *testing.T) {
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = strings.Repeat("k", 32)
	cfg.Auth.AccessTTL = time.Hour
	cfg.Auth.RefreshTTL = 24 * time.Hour
	svc, err := NewService(cfg, c)
	if err != nil {
		t.Fatal(err)
	}

	seen := make(chan string, 1)
	RegisterSessionTracker(func(_ context.Context, cl *Claims) {
		select {
		case seen <- cl.SessionBinding:
		default:
		}
	})

	u := &User{ID: idgen.ID(5), TenantID: 1, Username: "alice"}
	out, err := svc.passGates(context.Background(), u, LoginMeta{Method: "saml", SessionBinding: "saml:9:alice"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.Pair.AccessToken == "" {
		t.Fatal("expected an issued pair")
	}
	select {
	case got := <-seen:
		if got != "saml:9:alice" {
			t.Fatalf("tracker saw binding %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("session tracker was not called")
	}
	cl, err := svc.tokenClaims(out.Pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if cl.SessionBinding != "saml:9:alice" || cl.ID == "" || cl.RID == "" {
		t.Fatalf("unexpected claims: binding=%q jti=%q rid=%q", cl.SessionBinding, cl.ID, cl.RID)
	}
}

// 没有绑定（普通本地登录）不触发跟踪器。
func TestNoBindingSkipsTracker(t *testing.T) {
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = strings.Repeat("k", 32)
	cfg.Auth.AccessTTL = time.Hour
	cfg.Auth.RefreshTTL = 24 * time.Hour
	svc, err := NewService(cfg, c)
	if err != nil {
		t.Fatal(err)
	}
	called := make(chan struct{}, 1)
	RegisterSessionTracker(func(context.Context, *Claims) {
		select {
		case called <- struct{}{}:
		default:
		}
	})
	u := &User{ID: idgen.ID(5), TenantID: 1, Username: "bob"}
	if _, err := svc.passGates(context.Background(), u, LoginMeta{Method: "password"}, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
		t.Fatal("tracker must not fire without SessionBinding")
	case <-time.After(200 * time.Millisecond):
	}
}

// 二次验证挑战要把绑定存进 challengeRec；验码后 passGates 用它签发带绑定的令牌。
func TestChallengeCarriesSessionBinding(t *testing.T) {
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = strings.Repeat("k", 32)
	svc, err := NewService(cfg, c)
	if err != nil {
		t.Fatal(err)
	}
	u := &User{ID: idgen.ID(5), TenantID: 1, Username: "alice"}
	ch, err := svc.newChallenge(context.Background(), u,
		LoginMeta{Method: "saml", SessionBinding: "saml:9:alice"}, "totp", &GateRequirement{Methods: []string{"totp"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.Get(context.Background(), challengeKey(ch.ID))
	if err != nil {
		t.Fatal(err)
	}
	var rec challengeRec
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Binding != "saml:9:alice" {
		t.Fatalf("challenge lost binding: %q", rec.Binding)
	}
}

// 刷新轮换出的新令牌继续带同一绑定（否则 IdP 发起的登出吊销不到刷新后的会话）。
func TestRefreshPairKeepsSessionBinding(t *testing.T) {
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = strings.Repeat("k", 32)
	cfg.Auth.AccessTTL = time.Hour
	cfg.Auth.RefreshTTL = 24 * time.Hour
	svc, err := NewService(cfg, c)
	if err != nil {
		t.Fatal(err)
	}
	u := &User{ID: idgen.ID(5), TenantID: 1, Username: "alice"}
	first, err := svc.issuePair(context.Background(), u, "", 0, "saml:9:alice")
	if err != nil {
		t.Fatal(err)
	}
	cl, err := svc.tokenClaims(first.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	next, err := svc.issuePair(context.Background(), u, cl.ID, cl.SessionStart, cl.SessionBinding)
	if err != nil {
		t.Fatal(err)
	}
	cl2, err := svc.tokenClaims(next.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if cl2.SessionBinding != "saml:9:alice" || cl2.ID == cl.ID {
		t.Fatalf("rotated pair lost binding or jti: binding=%q jti=%q", cl2.SessionBinding, cl2.ID)
	}
}
