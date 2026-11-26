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

type completionProvider struct{ user User }

func (p completionProvider) Login(context.Context, LoginRequest) (*User, error) { return &p.user, nil }
func (p completionProvider) Profile(context.Context, idgen.ID) (*User, error)   { return &p.user, nil }

type completionGate struct{}

func (completionGate) Name() string { return "completiontest" }
func (completionGate) Check(context.Context, GateInput) (*GateRequirement, error) {
	return &GateRequirement{Methods: []string{"totp"}}, nil
}
func (completionGate) Verify(context.Context, GateChallenge, GateAnswer) error { return nil }

func TestLoginCompletionWaitsForFinalGateAndPreservesMetadata(t *testing.T) {
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
	u := &User{ID: idgen.ID(5), TenantID: 1, Username: "alice", ExternalCreated: true}
	provider := &completionProvider{user: *u}
	svc.SetProvider(provider)
	if err := svc.RegisterLoginGate(completionGate{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	payload := json.RawMessage(`{"sourceId":"9","roleCodes":["editor"]}`)
	if err := svc.RegisterLoginCompletion("saml", func(_ context.Context, got *User, data json.RawMessage) {
		calls++
		if !got.ExternalCreated || string(data) != string(payload) {
			t.Errorf("completion lost metadata: created=%v data=%s", got.ExternalCreated, data)
		}
		provider.user.Roles = []string{"editor"}
	}); err != nil {
		t.Fatal(err)
	}
	meta := LoginMeta{Method: "saml", IP: "127.0.0.1", Completion: payload}
	out, err := svc.FinishLogin(context.Background(), u, meta)
	if err != nil {
		t.Fatal(err)
	}
	if out.Challenge == nil || calls != 0 || out.CreatedUserID != u.ID {
		t.Fatalf("completion must wait for MFA and expose created user internally: challenge=%v calls=%d created=%d", out.Challenge, calls, out.CreatedUserID)
	}
	out, err = svc.CompleteLogin(context.Background(), out.Challenge.ID, meta, GateAnswer{Kind: "totp", Code: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Challenge != nil || out.Pair.AccessToken == "" || calls != 1 || !out.User.ExternalCreated {
		t.Fatalf("unexpected completed login: outcome=%+v calls=%d", out, calls)
	}
	if len(out.User.Roles) != 1 || out.User.Roles[0] != "editor" {
		t.Fatalf("completion role changes missing from login response: %v", out.User.Roles)
	}
}
