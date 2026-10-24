package session

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func memCache(t *testing.T) cache.Cache {
	t.Helper()
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func claims(uid int64, jti string) *auth.Claims {
	return &auth.Claims{UserID: idgen.ID(uid), Username: fmt.Sprintf("u%d", uid), RID: "r-" + jti,
		RegisteredClaims: registered(jti)}
}

func touch1(t *testing.T, s *Store, ctx context.Context, uid int64, ip string) string {
	t.Helper()
	cl := claims(uid, fmt.Sprintf("j-%d-%s", uid, ip))
	if err := s.Touch(ctx, cl, ip, "Mozilla/5.0 (X11; Linux x86_64) Chrome/120 Safari/537", func() string { return "nick" }); err != nil {
		t.Fatal(err)
	}
	return SID(cl.UserID, ip, "Mozilla/5.0 (X11; Linux x86_64) Chrome/120 Safari/537")
}

// 多实例（两个 Store 共享同一缓存）并发登记：旧实现整份读-改-写会丢更新，新实现一个都不能少。
func TestConcurrentTouchFromTwoInstancesLosesNothing(t *testing.T) {
	c := memCache(t)
	a, b := NewStore(c, time.Hour), NewStore(c, time.Hour)
	ctx := tenant.With(context.Background(), 1)
	const n = 60
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := a
			if i%2 == 1 {
				s = b
			}
			touch1(t, s, ctx, int64(1000+i), fmt.Sprintf("10.0.0.%d", i))
		}()
	}
	wg.Wait()
	for name, s := range map[string]*Store{"a": a, "b": b} {
		got, err := s.List(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != n {
			t.Fatalf("instance %s sees %d sessions, want %d", name, len(got), n)
		}
	}
}

func TestListFilterKickAndTenantIsolation(t *testing.T) {
	c := memCache(t)
	a, b := NewStore(c, time.Hour), NewStore(c, time.Hour)
	t1, t2 := tenant.With(context.Background(), 1), tenant.With(context.Background(), 2)
	s1 := touch1(t, a, t1, 7, "1.1.1.1")
	s2 := touch1(t, a, t1, 7, "2.2.2.2")
	touch1(t, a, t1, 8, "3.3.3.3")
	touch1(t, a, t2, 9, "4.4.4.4")

	if got, _ := b.List(t1, 7); len(got) != 2 {
		t.Fatalf("user 7 sessions = %d, want 2", len(got))
	}
	if got, _ := b.List(t1, 0); len(got) != 3 {
		t.Fatalf("tenant 1 sessions = %d, want 3", len(got))
	}
	if got, _ := b.List(t2, 0); len(got) != 1 {
		t.Fatalf("tenant 2 sessions = %d, want 1", len(got))
	}

	// 另一个实例强退：吊销令牌 + 会话消失，原实例也看不到
	if _, ok, err := b.Kick(t1, s1, 8); err != nil || ok {
		t.Fatalf("kick with wrong user = ok:%v err:%v, want not found", ok, err)
	}
	ses, ok, err := b.Kick(t1, s1, 7)
	if err != nil || !ok || ses.ID != s1 {
		t.Fatalf("kick = %v %v %v", ses, ok, err)
	}
	if v, err := c.Get(t1, cache.GlobalKey("auth", "revoked", "j-7-1.1.1.1")); err != nil || v != "1" {
		t.Fatalf("access jti not revoked: %q %v", v, err)
	}
	if v, err := c.Get(t1, cache.GlobalKey("auth", "revoked", "r-j-7-1.1.1.1")); err != nil || v != "1" {
		t.Fatalf("refresh jti not revoked: %q %v", v, err)
	}
	if _, ok, _ := a.Kick(t1, s1, 0); ok {
		t.Fatal("kicked session must be gone")
	}
	if got, _ := a.List(t1, 7); len(got) != 1 || got[0].ID != s2 {
		t.Fatalf("after kick: %v", got)
	}

	// 改密码：踢掉除当前外的全部会话
	touch1(t, a, t1, 7, "5.5.5.5")
	if err := b.KickUser(t1, 7, s2); err != nil {
		t.Fatal(err)
	}
	got, _ := a.List(t1, 7)
	if len(got) != 1 || got[0].ID != s2 {
		t.Fatalf("KickUser kept %v, want only %s", got, s2)
	}
	if got, _ := a.List(t1, 8); len(got) != 1 {
		t.Fatal("KickUser must not touch other users")
	}
}

func TestSessionsExpireAndStaleIndexEntriesArePruned(t *testing.T) {
	c := memCache(t)
	s := NewStore(c, 150*time.Millisecond)
	ctx := tenant.With(context.Background(), 1)
	sid := touch1(t, s, ctx, 7, "1.1.1.1")
	if got, _ := s.List(ctx, 0); len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	// 会话 key 被删（过期 / 清缓存），索引项还在：List 应自动清理
	_ = c.Del(ctx, sessionKey(ctx, sid))
	if got, _ := s.List(ctx, 0); len(got) != 0 {
		t.Fatalf("stale entry listed: %v", got)
	}
	zs, _ := cache.As[cache.SortedSet](c)
	if ids, _ := zs.ZRange(ctx, indexKey(ctx), 0); len(ids) != 0 {
		t.Fatalf("index not pruned: %v", ids)
	}
	touch1(t, s, ctx, 7, "2.2.2.2")
	time.Sleep(250 * time.Millisecond)
	if got, _ := s.List(ctx, 0); len(got) != 0 {
		t.Fatalf("expired session listed: %v", got)
	}
}

func TestTouchThrottleAndLoginAtPreserved(t *testing.T) {
	c := memCache(t)
	s := NewStore(c, time.Hour)
	ctx := tenant.With(context.Background(), 1)
	cl := claims(7, "j1")
	nick := 0
	do := func(cl *auth.Claims) {
		if err := s.Touch(ctx, cl, "1.1.1.1", "ua", func() string { nick++; return "Nick" }); err != nil {
			t.Fatal(err)
		}
	}
	do(cl)
	first, _ := s.List(ctx, 7)
	time.Sleep(20 * time.Millisecond)
	do(cl) // 同 jti 30 秒内不重复写
	again, _ := s.List(ctx, 7)
	if !again[0].LastActiveAt.Equal(first[0].LastActiveAt) {
		t.Fatal("throttled Touch must not rewrite")
	}
	do(claims(7, "j2")) // 令牌刷新：更新 jti，保留 LoginAt 与昵称
	third, _ := s.List(ctx, 7)
	if third[0].AccessJTI != "j2" || !third[0].LoginAt.Equal(first[0].LoginAt) || third[0].Nickname != "Nick" {
		t.Fatalf("refresh lost data: %+v vs %+v", third[0], first[0])
	}
	if nick != 1 {
		t.Fatalf("nick looked up %d times, want 1", nick)
	}
}

// 缓存没有有序集合能力（外部实现的 Cache）：退回进程内存储，功能不变。
type bare struct{ cache.Cache }

func TestFallbackWhenCacheLacksSortedSet(t *testing.T) {
	m := memCache(t)
	s := NewStore(bare{m}, time.Hour)
	ctx := tenant.With(context.Background(), 1)
	sid := touch1(t, s, ctx, 7, "1.1.1.1")
	if got, _ := s.List(ctx, 0); len(got) != 1 || got[0].ID != sid {
		t.Fatalf("fallback list = %v", got)
	}
	if _, ok, _ := s.Kick(ctx, sid, 0); !ok {
		t.Fatal("fallback kick failed")
	}
}
