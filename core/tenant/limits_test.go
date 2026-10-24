package tenant

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func TestStorageQuotaRejectsInvalidArithmetic(t *testing.T) {
	install(t, &fakeSource{infos: map[idgen.ID]*Info{
		1: {ID: 1, MaxStorageMB: 1},
		2: {ID: 2, MaxStorageMB: math.MaxInt64},
		3: {ID: 3, MaxStorageMB: -1},
	}})
	for _, tc := range []struct {
		tid       idgen.ID
		used, add int64
	}{
		{1, -1, 1}, {1, 1, -1}, {1, math.MaxInt64, 1},
		{2, 0, 1}, {3, 0, 1},
	} {
		if err := CheckStorageQuota(With(context.Background(), tc.tid), tc.used, tc.add); err == nil {
			t.Fatalf("invalid arithmetic accepted: %+v", tc)
		}
	}
}

type fakeSource struct {
	infos   map[idgen.ID]*Info
	domains map[string]idgen.ID
	err     error
	calls   atomic.Int32
}

func (f *fakeSource) TenantInfo(_ context.Context, id idgen.ID) (*Info, error) {
	f.calls.Add(1)
	return f.infos[id], f.err
}

func (f *fakeSource) TenantByDomain(_ context.Context, host string) (idgen.ID, bool, error) {
	f.calls.Add(1)
	id, ok := f.domains[host]
	return id, ok, f.err
}

func install(t *testing.T, f *fakeSource) {
	t.Helper()
	SetSource(f)
	t.Cleanup(func() { SetSource(nil); now = time.Now })
}

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func reasonOf(err error) string {
	var e *response.Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return ""
}

func TestStateAtExpiryAndGrace(t *testing.T) {
	exp := t0
	cases := []struct {
		name  string
		info  *Info
		at    time.Time
		state State
	}{
		{"nil info", nil, t0, StateActive},
		{"before expiry", &Info{Status: "active", ExpireAt: exp}, exp.Add(-time.Hour), StateActive},
		{"at expiry", &Info{Status: "active", ExpireAt: exp}, exp, StateActive},
		{"after expiry no grace", &Info{Status: "active", ExpireAt: exp}, exp.Add(time.Second), StateExpired},
		{"inside grace", &Info{Status: "active", ExpireAt: exp, GraceDays: 3}, exp.Add(2 * day), StateGrace},
		{"grace boundary", &Info{Status: "active", ExpireAt: exp, GraceDays: 3}, exp.Add(3 * day), StateGrace},
		{"after grace", &Info{Status: "active", ExpireAt: exp, GraceDays: 3}, exp.Add(3*day + time.Second), StateExpired},
		{"suspended wins", &Info{Status: StatusSuspended, ExpireAt: exp.Add(100 * day)}, t0, StateSuspended},
		{"status expired", &Info{Status: StatusExpired, ExpireAt: exp.Add(100 * day)}, t0, StateExpired},
		{"zero expire never expires", &Info{Status: "active"}, t0.Add(1000 * day), StateActive},
	}
	for _, c := range cases {
		if got := c.info.StateAt(c.at); got != c.state {
			t.Errorf("%s: got %v want %v", c.name, got, c.state)
		}
	}
	if !StateGrace.Allowed() || StateExpired.Allowed() || StateSuspended.Allowed() {
		t.Fatal("Allowed mismatch")
	}
}

func TestCheckActive(t *testing.T) {
	f := &fakeSource{infos: map[idgen.ID]*Info{
		10: {ID: 10, Status: "active", ExpireAt: t0.Add(24 * time.Hour)},
		11: {ID: 11, Status: "active", ExpireAt: t0.Add(-24 * time.Hour)},
		12: {ID: 12, Status: "active", ExpireAt: t0.Add(-24 * time.Hour), GraceDays: 7},
		13: {ID: 13, Status: StatusSuspended, ExpireAt: t0.Add(24 * time.Hour)},
	}}
	install(t, f)
	now = func() time.Time { return t0 }
	ctx := context.Background()

	for _, id := range []idgen.ID{10, 12, 1, 999} { // 正常、宽限期内、默认租户、无记录
		if err := CheckActive(ctx, id); err != nil {
			t.Errorf("tenant %d should pass: %v", id, err)
		}
	}
	for _, id := range []idgen.ID{11, 13} {
		err := CheckActive(ctx, id)
		var e *response.Error
		if !errors.As(err, &e) || e.Status != 403 || e.Reason != ReasonExpired {
			t.Fatalf("tenant %d: want 403 TENANT_EXPIRED, got %v", id, err)
		}
	}
	var e *response.Error
	_ = errors.As(CheckActive(ctx, 13), &e)
	if e.Key != "tenant.suspended" {
		t.Fatalf("suspended key = %q", e.Key)
	}
}

func TestCheckActiveSourceErrorFailsClosed(t *testing.T) {
	install(t, &fakeSource{err: errors.New("db down")})
	if err := CheckActive(context.Background(), 5); err == nil {
		t.Fatal("source error must not be treated as allowed")
	}
}

func TestNoSourceAllowsEverything(t *testing.T) {
	SetSource(nil)
	if err := CheckActive(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if _, ok := DefaultDomainLookup("a.example.dev"); ok {
		t.Fatal("no source: lookup must miss")
	}
}

func TestLookupCachesWithinTTL(t *testing.T) {
	f := &fakeSource{infos: map[idgen.ID]*Info{10: {ID: 10, Status: "active"}}}
	install(t, f)
	cur := t0
	now = func() time.Time { return cur }
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _ = Lookup(ctx, 10)
		_, _ = Lookup(ctx, 99) // 无记录也缓存
	}
	if n := f.calls.Load(); n != 2 {
		t.Fatalf("source calls = %d, want 2", n)
	}
	cur = cur.Add(infoTTL + time.Second)
	_, _ = Lookup(ctx, 10)
	if n := f.calls.Load(); n != 3 {
		t.Fatalf("after TTL calls = %d, want 3", n)
	}
	Invalidate(10)
	_, _ = Lookup(ctx, 10)
	if n := f.calls.Load(); n != 4 {
		t.Fatalf("after Invalidate calls = %d, want 4", n)
	}
}

func TestDefaultDomainLookup(t *testing.T) {
	f := &fakeSource{domains: map[string]idgen.ID{"acme.example.dev": 7}}
	install(t, f)
	if id, ok := DefaultDomainLookup("ACME.Example.dev."); !ok || id != 7 {
		t.Fatalf("case-insensitive lookup failed: %v %v", id, ok)
	}
	if _, ok := DefaultDomainLookup("other.example.dev"); ok {
		t.Fatal("unbound domain must miss")
	}
	before := f.calls.Load()
	DefaultDomainLookup("acme.example.dev")
	DefaultDomainLookup("other.example.dev")
	if f.calls.Load() != before {
		t.Fatal("hits and misses should both be cached")
	}
}

func TestExpireSoon(t *testing.T) {
	mk := func(daysFromNow float64, warn, grace int) *Info {
		return &Info{Status: "active", ExpireAt: t0.Add(time.Duration(daysFromNow * float64(day))), WarnDays: warn, GraceDays: grace}
	}
	if n, ok := mk(10, 15, 0).ExpireSoon(t0); !ok || n != 10 {
		t.Fatalf("10 days left: %d %v", n, ok)
	}
	if n, ok := mk(9.5, 15, 0).ExpireSoon(t0); !ok || n != 10 { // 向上取整
		t.Fatalf("9.5 days -> %d %v", n, ok)
	}
	if _, ok := mk(16, 15, 0).ExpireSoon(t0); ok {
		t.Fatal("outside window must not warn")
	}
	if _, ok := mk(3, 0, 0).ExpireSoon(t0); ok {
		t.Fatal("warn_days=0 disables the warning")
	}
	if _, ok := mk(-1, 15, 0).ExpireSoon(t0); ok {
		t.Fatal("expired without grace is not 'soon'")
	}
	if n, ok := mk(-1, 15, 3).ExpireSoon(t0); !ok || n != -1 {
		t.Fatalf("inside grace should still warn: %d %v", n, ok)
	}
	susp := mk(3, 15, 0)
	susp.Status = StatusSuspended
	if _, ok := susp.ExpireSoon(t0); ok {
		t.Fatal("suspended tenant is not 'soon'")
	}
	var nilInfo *Info
	if _, ok := nilInfo.ExpireSoon(t0); ok {
		t.Fatal("nil info")
	}
}

func TestQuotas(t *testing.T) {
	f := &fakeSource{infos: map[idgen.ID]*Info{
		10: {ID: 10, Status: "active", MaxUsers: 5, MaxRoles: 2, MaxStorageMB: 1},
		11: {ID: 11, Status: "active"}, // 全部不限
	}}
	install(t, f)
	ctx := With(context.Background(), 10)

	if err := CheckUserQuota(ctx, 4, 1); err != nil {
		t.Fatalf("5th user must pass: %v", err)
	}
	err := CheckUserQuota(ctx, 5, 1)
	if reasonOf(err) != ReasonQuotaUsers {
		t.Fatalf("6th user must be rejected, got %v", err)
	}
	var e *response.Error
	_ = errors.As(err, &e)
	if e.Key != "tenant.quota_users" || e.Status != 403 || len(e.Args) != 1 || e.Args[0] != 5 {
		t.Fatalf("bad quota error: %+v", e)
	}
	if err := CheckUserQuota(ctx, 3, 3); reasonOf(err) != ReasonQuotaUsers { // 批量一次性超出
		t.Fatalf("bulk over limit: %v", err)
	}
	if err := CheckRoleQuota(ctx, 1, 1); err != nil {
		t.Fatal(err)
	}
	if reasonOf(CheckRoleQuota(ctx, 2, 1)) != ReasonQuotaRoles {
		t.Fatal("role quota not enforced")
	}
	if err := CheckStorageQuota(ctx, 1<<20-10, 10); err != nil {
		t.Fatalf("exactly at limit must pass: %v", err)
	}
	if reasonOf(CheckStorageQuota(ctx, 1<<20-10, 11)) != ReasonQuotaStorage {
		t.Fatal("storage quota not enforced")
	}

	// 不限制（0）、跳过隔离、默认租户、无租户：全部放行
	if err := CheckUserQuota(With(context.Background(), 11), 1e9, 1); err != nil {
		t.Fatal(err)
	}
	if err := CheckUserQuota(Skip(ctx), 1e9, 1); err != nil {
		t.Fatal("platform-level (Skip) must bypass quotas")
	}
	if err := CheckUserQuota(With(context.Background(), DefaultID), 1e9, 1); err != nil {
		t.Fatal(err)
	}
	if err := CheckUserQuota(context.Background(), 1e9, 1); err != nil {
		t.Fatal(err)
	}
}

func TestPackageMenuIntersection(t *testing.T) {
	nodes := []MenuNode{
		{ID: 1, Type: "menu", Perm: "user:list"},
		{ID: 2, ParentID: 1, Type: "button", Perm: "user:create"},
		{ID: 3, Type: "menu", Perm: "role:list"},
		{ID: 4, ParentID: 3, Type: "button", Perm: "role:create"},
		{ID: 5, Type: "menu", Perm: "dashboard:view"}, // 平台级
		{ID: 6, Type: "dir"},                          // 无权限码，忽略
		{ID: 7, Type: "menu", Perm: "profile:view"},
	}
	allowed := AllowedPerms([]idgen.ID{1}, nodes) // 套餐只勾了“用户”菜单
	want := map[string]bool{"user:list": true, "user:create": true, "dashboard:view": true, "profile:view": true}
	if len(allowed) != len(want) {
		t.Fatalf("allowed = %v", allowed)
	}
	for k := range want {
		if !allowed[k] {
			t.Fatalf("missing %s in %v", k, allowed)
		}
	}

	got := IntersectPerms([]string{"user:list", "role:list", "role:create", "dashboard:view"}, allowed)
	if len(got) != 2 || got[0] != "dashboard:view" || got[1] != "user:list" {
		t.Fatalf("explicit perms: %v", got)
	}
	star := IntersectPerms([]string{"*"}, allowed)
	if len(star) != 4 {
		t.Fatalf("star expands to package perms: %v", star)
	}
	prefix := IntersectPerms([]string{"user:*", "role:*"}, allowed)
	if len(prefix) != 2 || prefix[0] != "user:create" || prefix[1] != "user:list" {
		t.Fatalf("prefix wildcard: %v", prefix)
	}
	if got := IntersectPerms([]string{"role:list"}, allowed); len(got) != 0 {
		t.Fatalf("not in package must be dropped: %v", got)
	}
}

func TestMeter(t *testing.T) {
	m := NewMeter(time.Minute)
	cur := t0
	m.now = func() time.Time { return cur }
	var loads int
	load := func() (int64, error) { loads++; return 100, nil }

	for i := 0; i < 3; i++ {
		if v, _ := m.Get(1, load); v != 100 {
			t.Fatal(v)
		}
	}
	if loads != 1 {
		t.Fatalf("loads = %d", loads)
	}
	m.Add(1, 50)
	if v, _ := m.Get(1, load); v != 150 {
		t.Fatalf("incremental add: %d", v)
	}
	m.Add(1, -1000)
	if v, _ := m.Get(1, load); v != 0 {
		t.Fatalf("never negative: %d", v)
	}
	m.Add(2, 5) // 未缓存的租户忽略
	if v, _ := m.Get(2, load); v != 100 {
		t.Fatalf("uncached add must be ignored: %d", v)
	}
	cur = cur.Add(2 * time.Minute)
	m.Get(1, load)
	if loads != 3 {
		t.Fatalf("expired entries reload: %d", loads)
	}
	if _, err := m.Get(3, func() (int64, error) { return 0, errors.New("x") }); err == nil {
		t.Fatal("load error must propagate")
	}
	m.Invalidate(1)
	m.Get(1, load)
	if loads != 4 {
		t.Fatalf("invalidate: %d", loads)
	}
}

func TestInvalidateBroadcastsButLocalDoesNot(t *testing.T) {
	type call struct {
		ids []idgen.ID
		all bool
	}
	var calls []call
	SetBroadcaster(func(ids []idgen.ID, all bool) { calls = append(calls, call{ids, all}) })
	t.Cleanup(func() { SetBroadcaster(nil) })

	f := &fakeSource{infos: map[idgen.ID]*Info{5: {ID: 5, Status: "active"}}}
	install(t, f)
	ctx := context.Background()
	_, _ = Lookup(ctx, 5)
	_, _ = Lookup(ctx, 5)
	if f.calls.Load() != 1 {
		t.Fatalf("expected cached lookup, source calls = %d", f.calls.Load())
	}

	InvalidateLocal(5) // 收到远端通知时用：清缓存但不广播
	if len(calls) != 0 {
		t.Fatalf("InvalidateLocal must not broadcast, got %v", calls)
	}
	_, _ = Lookup(ctx, 5)
	if f.calls.Load() != 2 {
		t.Fatal("InvalidateLocal did not clear the cache")
	}

	Invalidate(5, 6)
	if len(calls) != 1 || calls[0].all || len(calls[0].ids) != 2 {
		t.Fatalf("Invalidate broadcast = %v", calls)
	}
	_, _ = Lookup(ctx, 5)
	if f.calls.Load() != 3 {
		t.Fatal("Invalidate did not clear the local cache")
	}

	InvalidateAll()
	if len(calls) != 2 || !calls[1].all {
		t.Fatalf("InvalidateAll broadcast = %v", calls)
	}
	_, _ = Lookup(ctx, 5)
	if f.calls.Load() != 4 {
		t.Fatal("InvalidateAll did not clear the local cache")
	}

	SetBroadcaster(nil) // 单实例：没有广播器时行为与之前一致
	Invalidate(5)
	InvalidateAll()
}
