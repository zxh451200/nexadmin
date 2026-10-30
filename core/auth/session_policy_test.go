package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"nexadmin/core/cache"
	"nexadmin/core/config"
)

func newPolicyService(t *testing.T, u *User, fn func() SessionPolicy) *Service {
	t.Helper()
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	s := &Service{secret: []byte("0123456789abcdef0123456789abcdef"), accessTTL: 30 * time.Minute, refreshTTL: 168 * time.Hour, cache: ch}
	s.SetProvider(userProvider{u})
	if fn != nil {
		s.SetSessionPolicyProvider(fn)
	}
	return s
}

func claimsOf(t *testing.T, s *Service, tok string) Claims {
	t.Helper()
	var cl Claims
	if _, err := jwt.ParseWithClaims(tok, &cl, func(*jwt.Token) (any, error) { return s.secret, nil }); err != nil {
		t.Fatal(err)
	}
	return cl
}

// 闲置时长决定 refresh token 的有效期：滑动窗口，不是无限续命。
func TestIdleTTLCapsRefreshLifetime(t *testing.T) {
	u := &User{ID: 1, Username: "u"}
	s := newPolicyService(t, u, func() SessionPolicy { return SessionPolicy{IdleTTL: 2 * time.Hour} })
	pair, err := s.IssuePair(u)
	if err != nil {
		t.Fatal(err)
	}
	rcl := claimsOf(t, s, pair.RefreshToken)
	left := time.Until(rcl.ExpiresAt.Time)
	if left > 2*time.Hour+time.Minute || left < 2*time.Hour-time.Minute {
		t.Fatalf("refresh lifetime = %v, want ~2h", left)
	}
	if rcl.SessionStart == 0 {
		t.Fatal("refresh token must carry session start")
	}
}

// 最长会话是绝对上限：刷新不会把截止时间推后，超过后刷新被拒。
func TestMaxLifetimeIsAbsoluteAcrossRefresh(t *testing.T) {
	u := &User{ID: 1, Username: "u"}
	start := time.Now().Add(-50 * time.Minute).Unix() // 会话已经进行了 50 分钟
	s := newPolicyService(t, u, func() SessionPolicy {
		return SessionPolicy{IdleTTL: 2 * time.Hour, MaxLifetime: time.Hour}
	})
	pair, err := s.issuePair(context.Background(), u, "", start)
	if err != nil {
		t.Fatal(err)
	}
	// 剩余额度只有 10 分钟：refresh 不能拿到 2h。
	rcl := claimsOf(t, s, pair.RefreshToken)
	if left := time.Until(rcl.ExpiresAt.Time); left > 11*time.Minute {
		t.Fatalf("refresh survived max lifetime: left=%v", left)
	}
	next, err := s.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	ncl := claimsOf(t, s, next.RefreshToken)
	if ncl.SessionStart != start {
		t.Fatalf("session start must be preserved on refresh: got %d want %d", ncl.SessionStart, start)
	}
	if left := time.Until(ncl.ExpiresAt.Time); left > 10*time.Minute+time.Minute {
		t.Fatalf("refresh extended past max lifetime: left=%v", left)
	}
}

// 存活超过最长会话的刷新令牌无法续命。
func TestRefreshRejectedAfterMaxLifetime(t *testing.T) {
	u := &User{ID: 1, Username: "u"}
	s := newPolicyService(t, u, func() SessionPolicy {
		return SessionPolicy{IdleTTL: time.Hour, MaxLifetime: 50 * time.Millisecond}
	})
	pair, err := s.IssuePair(u)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := s.Refresh(context.Background(), pair.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("refresh after max lifetime: %v", err)
	}
}

// 闲置超时同样直接拒绝刷新。
func TestRefreshRejectedAfterIdle(t *testing.T) {
	u := &User{ID: 1, Username: "u"}
	s := newPolicyService(t, u, func() SessionPolicy { return SessionPolicy{IdleTTL: time.Millisecond} })
	pair, err := s.IssuePair(u)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := s.Refresh(context.Background(), pair.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("refresh after idle: %v", err)
	}
}

// 没有注册策略来源时行为不变：仍用配置的 refresh_ttl。
func TestNoPolicyFallsBackToConfig(t *testing.T) {
	u := &User{ID: 1, Username: "u"}
	s := newPolicyService(t, u, nil)
	pair, err := s.IssuePair(u)
	if err != nil {
		t.Fatal(err)
	}
	rcl := claimsOf(t, s, pair.RefreshToken)
	if left := time.Until(rcl.ExpiresAt.Time); left < 167*time.Hour {
		t.Fatalf("refresh lifetime = %v, want config 168h", left)
	}
}
