package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"nexadmin/core/cache"
	"nexadmin/pkg/idgen"
)

// ExternalSession is server-created metadata, never accepted from login JSON.
// Its absolute deadline bounds refresh rotation and cache retention.
type ExternalSession struct {
	Key   string    `json:"key"`
	Until time.Time `json:"until"`
}

type externalSessionContextKey struct{}

func WithExternalSession(ctx context.Context, session ExternalSession) context.Context {
	return context.WithValue(ctx, externalSessionContextKey{}, session)
}

func externalSessionFrom(ctx context.Context) ExternalSession {
	v, _ := ctx.Value(externalSessionContextKey{}).(ExternalSession)
	return v
}

func externalSessionKey(tid idgen.ID, scope, index string) string {
	h := sha256.Sum256([]byte(tid.String() + "\x00" + scope + "\x00" + index))
	return cache.GlobalKey("auth", "external-session", hex.EncodeToString(h[:]))
}

func (s *Service) externalSessionTTL() time.Duration {
	challengeTTL, _ := s.limits()
	return max(s.accessTTL, s.refreshTTL) + challengeTTL
}

// BindExternalSession must only be called after the upstream ticket is verified.
// Lease's atomic update prevents a concurrent SLO from being overwritten.
func (s *Service) BindExternalSession(ctx context.Context, tid idgen.ID, scope, index string) (ExternalSession, error) {
	var session ExternalSession
	leases, ok := cache.As[cache.Lease](s.cache)
	if !ok || tid == 0 || scope == "" || index == "" {
		return session, errors.New("auth: external session requires scoped atomic cache")
	}
	ttl := s.externalSessionTTL()
	session = ExternalSession{Key: externalSessionKey(tid, scope, index), Until: time.Now().Add(ttl)}
	err := leases.UpdateLeases(ctx, session.Key, ttl, func(entries map[string]cache.LeaseEntry) error {
		if _, revoked := entries["revoked"]; revoked {
			return ErrInvalidToken
		}
		if active, exists := entries["active"]; exists {
			session.Until = active.Until
		} else {
			entries["active"] = cache.LeaseEntry{Until: session.Until}
		}
		return nil
	})
	return session, err
}

// RevokeExternalSession also records unknown indices: this closes the window
// between upstream validation and local binding without targeting any user.
func (s *Service) RevokeExternalSession(ctx context.Context, tid idgen.ID, scope, index string) error {
	leases, ok := cache.As[cache.Lease](s.cache)
	if !ok || tid == 0 || scope == "" || index == "" {
		return errors.New("auth: external session requires scoped atomic cache")
	}
	ttl := s.externalSessionTTL()
	until := time.Now().Add(ttl)
	return leases.UpdateLeases(ctx, externalSessionKey(tid, scope, index), ttl, func(entries map[string]cache.LeaseEntry) error {
		entries["revoked"] = cache.LeaseEntry{Until: until}
		return nil
	})
}

func (s *Service) checkExternalSession(ctx context.Context, session ExternalSession) error {
	if session.Key == "" {
		return nil
	}
	if s.cache == nil || !time.Now().Before(session.Until) {
		return ErrInvalidToken
	}
	raw, err := s.cache.Get(ctx, session.Key)
	if err != nil {
		return ErrInvalidToken
	}
	var entries map[string]cache.LeaseEntry
	if json.Unmarshal([]byte(raw), &entries) != nil {
		return ErrInvalidToken
	}
	if _, revoked := entries["revoked"]; revoked {
		return ErrInvalidToken
	}
	active, ok := entries["active"]
	if !ok || active.Until.Before(session.Until) || !time.Now().Before(active.Until) {
		return ErrInvalidToken
	}
	return nil
}
