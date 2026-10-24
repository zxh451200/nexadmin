package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type authSource map[idgen.ID]*tenant.Info

func (a authSource) TenantInfo(_ context.Context, id idgen.ID) (*tenant.Info, error) {
	return a[id], nil
}
func (a authSource) TenantByDomain(context.Context, string) (idgen.ID, bool, error) {
	return 0, false, nil
}

type userProvider struct{ u *User }

func (f userProvider) Login(context.Context, LoginRequest) (*User, error) { return f.u, nil }
func (f userProvider) Profile(context.Context, idgen.ID) (*User, error) {
	c := *f.u
	return &c, nil
}

func isExpired(err error) bool {
	var e *response.Error
	return errors.As(err, &e) && e.Status == 403 && e.Reason == tenant.ReasonExpired
}

func TestCheckTenantLoginAndSuperExemption(t *testing.T) {
	now := time.Now()
	tenant.SetSource(authSource{
		10: {ID: 10, Status: "active", ExpireAt: now.Add(-time.Hour)},
		11: {ID: 11, Status: "active", ExpireAt: now.Add(48 * time.Hour)},
		12: {ID: 12, Status: tenant.StatusSuspended},
	})
	t.Cleanup(func() { tenant.SetSource(nil) })
	s := &Service{}
	ctx := context.Background()

	if err := s.CheckTenant(ctx, &User{TenantID: 11}); err != nil {
		t.Fatalf("active: %v", err)
	}
	if err := s.CheckTenant(ctx, &User{TenantID: 10}); !isExpired(err) {
		t.Fatalf("expired tenant login must be rejected: %v", err)
	}
	if err := s.CheckTenant(ctx, &User{TenantID: 12}); !isExpired(err) {
		t.Fatalf("suspended tenant login must be rejected: %v", err)
	}
	if err := s.CheckTenant(ctx, &User{TenantID: 10, Super: true}); err != nil {
		t.Fatalf("super admin exempt: %v", err)
	}
	if err := s.CheckTenant(ctx, &User{TenantID: tenant.DefaultID}); err != nil {
		t.Fatalf("default tenant: %v", err)
	}
}

func TestRefreshRejectedAfterTenantExpires(t *testing.T) {
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	src := authSource{10: {ID: 10, Status: "active", ExpireAt: time.Now().Add(time.Hour)}}
	tenant.SetSource(src)
	t.Cleanup(func() { tenant.SetSource(nil) })

	u := &User{ID: 1, Username: "u", TenantID: 10}
	s := &Service{secret: []byte("0123456789abcdef0123456789abcdef"), accessTTL: time.Minute, refreshTTL: time.Hour, cache: ch}
	s.SetProvider(userProvider{u})
	pair, err := s.IssuePair(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Refresh(context.Background(), pair.RefreshToken); err != nil {
		t.Fatalf("refresh while active: %v", err)
	}

	pair, _ = s.IssuePair(u)
	src[10].ExpireAt = time.Now().Add(-time.Hour) // 租户到期
	tenant.ResetCache()
	if _, err := s.Refresh(context.Background(), pair.RefreshToken); !isExpired(err) {
		t.Fatalf("refresh after expiry must be rejected with TENANT_EXPIRED: %v", err)
	}
}

func TestDecorateExpireSoon(t *testing.T) {
	now := time.Now()
	tenant.SetSource(authSource{
		10: {ID: 10, Status: "active", ExpireAt: now.Add(5*24*time.Hour - time.Hour), WarnDays: 15},
		11: {ID: 11, Status: "active", ExpireAt: now.Add(60 * 24 * time.Hour), WarnDays: 15},
	})
	t.Cleanup(func() { tenant.SetSource(nil) })
	s := &Service{}
	ctx := context.Background()

	u := &User{TenantID: 10}
	s.Decorate(ctx, u)
	if u.TenantExpireSoon == nil || u.TenantExpireSoon.DaysLeft != 5 {
		t.Fatalf("expire soon = %+v", u.TenantExpireSoon)
	}
	far := &User{TenantID: 11}
	s.Decorate(ctx, far)
	super := &User{TenantID: 10, Super: true}
	s.Decorate(ctx, super)
	if far.TenantExpireSoon != nil || super.TenantExpireSoon != nil {
		t.Fatal("far-from-expiry tenants and super admins must not get the hint")
	}
}
