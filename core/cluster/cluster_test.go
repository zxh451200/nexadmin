package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

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

// sharedFake 把 memory 缓存伪装成“被多进程共享”的缓存（只用于测试，真实 redis 未在这里验证）。
type sharedFake struct{ cache.Cache }

func (s sharedFake) Shared() bool        { return true }
func (s sharedFake) Unwrap() cache.Cache { return s.Cache }

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestBusDeliversToOthersNotSelf(t *testing.T) {
	c := memCache(t)
	a, b, d := NewWithID(c, nil, "A"), NewWithID(c, nil, "B"), NewWithID(c, nil, "C")
	defer a.Close()
	defer b.Close()
	defer d.Close()
	var gotA, gotB, gotC atomic.Int32
	a.On("k", func(json.RawMessage) { gotA.Add(1) }, nil)
	b.On("k", func(raw json.RawMessage) {
		var v struct{ N int }
		if json.Unmarshal(raw, &v) == nil && v.N == 7 {
			gotB.Add(1)
		}
	}, nil)
	d.On("k", func(json.RawMessage) { gotC.Add(1) }, nil)
	d.On("other", func(json.RawMessage) { t.Error("wrong kind delivered") }, nil)

	a.Publish("k", map[string]int{"N": 7})
	eventually(t, func() bool { return gotB.Load() == 1 && gotC.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if gotA.Load() != 0 {
		t.Fatalf("publisher received its own event %d times (would double-push)", gotA.Load())
	}
	if gotB.Load() != 1 || gotC.Load() != 1 {
		t.Fatalf("B=%d C=%d, want exactly 1 each", gotB.Load(), gotC.Load())
	}
}

func TestBusHandlerPanicDoesNotKillSubscription(t *testing.T) {
	c := memCache(t)
	a, b := NewWithID(c, nil, "A"), NewWithID(c, nil, "B")
	defer a.Close()
	defer b.Close()
	var n atomic.Int32
	b.On("k", func(json.RawMessage) { n.Add(1); panic("boom") }, nil)
	a.Publish("k", 1)
	a.Publish("k", 2)
	eventually(t, func() bool { return n.Load() == 2 })
}

func TestBusCloseIsSafeAndStopsPublishing(t *testing.T) {
	c := memCache(t)
	a, b := NewWithID(c, nil, "A"), NewWithID(c, nil, "B")
	var n atomic.Int32
	b.On("k", func(json.RawMessage) { n.Add(1) }, nil)
	a.Publish("k", 1) // 关闭前入队的事件会被发完
	a.Close()
	a.Close()
	a.Publish("k", 2) // 关闭后丢弃，不 panic
	eventually(t, func() bool { return n.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if n.Load() != 1 {
		t.Fatalf("got %d events, want 1", n.Load())
	}
	b.Close()
	var nilBus *Bus // nil 总线所有方法都是空操作
	nilBus.Publish("k", 1)
	nilBus.On("k", nil, nil)
	nilBus.Close()
	if nilBus.Distributed() {
		t.Fatal("nil bus must not be distributed")
	}
}

// 不支持发布订阅的缓存：总线退化为空操作。
func TestBusWithoutPubSubIsNoop(t *testing.T) {
	type plain struct{ cache.Cache }
	b := New(plain{}, nil)
	if b.Distributed() {
		t.Fatal("must not be distributed")
	}
	b.Publish("k", 1)
	b.Close()
}

// 订阅重连：reset 回调会触发（用假 PubSub 模拟 redis 重连后的 onReset）。
type resetPS struct {
	cache.Cache
	mu      sync.Mutex
	onMsg   func(string)
	onReset func()
}

func (r *resetPS) Publish(context.Context, string, string) error { return nil }
func (r *resetPS) Subscribe(_ string, onMsg func(string), onReset func()) (func(), error) {
	r.mu.Lock()
	r.onMsg, r.onReset = onMsg, onReset
	r.mu.Unlock()
	return func() {}, nil
}

func TestBusResetCallbackRunsOnReconnect(t *testing.T) {
	ps := &resetPS{}
	b := NewWithID(ps, nil, "A")
	defer b.Close()
	var reset atomic.Int32
	b.On("k", func(json.RawMessage) {}, func() { reset.Add(1) })
	ps.onReset()
	if reset.Load() != 1 {
		t.Fatal("reset callback not invoked")
	}
	// 坏消息被忽略，别的来源的消息正常处理
	var got atomic.Int32
	b.On("x", func(json.RawMessage) { got.Add(1) }, nil)
	ps.onMsg("not json")
	ps.onMsg(`{"o":"A","k":"x"}`)
	ps.onMsg(`{"o":"B","k":"x"}`)
	if got.Load() != 1 {
		t.Fatalf("got %d, want 1 (self and garbage ignored)", got.Load())
	}
}

// ---- 租户失效广播 ----

type countSource struct{ calls atomic.Int32 }

func (s *countSource) TenantInfo(_ context.Context, id idgen.ID) (*tenant.Info, error) {
	s.calls.Add(1)
	return &tenant.Info{ID: id, Status: "active"}, nil
}
func (s *countSource) TenantByDomain(context.Context, string) (idgen.ID, bool, error) {
	return 0, false, nil
}

func TestTenantInvalidateBroadcast(t *testing.T) {
	c := memCache(t)
	a, b := NewWithID(c, nil, "A"), NewWithID(c, nil, "B")
	defer a.Close()
	defer b.Close()
	src := &countSource{}
	tenant.SetSource(src)
	t.Cleanup(func() { tenant.SetSource(nil); tenant.SetBroadcaster(nil) })

	// 本进程扮演实例 A（发出广播）；实例 B 用 b.On 记录收到的失效事件，并按真实接线处理。
	unwire := WireTenant(a)
	defer unwire()
	var evs atomic.Int32
	b.On(KindTenantInvalidate, func(raw json.RawMessage) {
		var ev tenantInvalidate
		if json.Unmarshal(raw, &ev) == nil && len(ev.IDs) == 1 && ev.IDs[0] == 42 {
			evs.Add(1)
		}
	}, nil)

	ctx := context.Background()
	_, _ = tenant.Lookup(ctx, 42)
	_, _ = tenant.Lookup(ctx, 42)
	if src.calls.Load() != 1 {
		t.Fatalf("cache not used: %d source calls", src.calls.Load())
	}
	tenant.Invalidate(42)
	eventually(t, func() bool { return evs.Load() == 1 })
	_, _ = tenant.Lookup(ctx, 42)
	if src.calls.Load() != 2 {
		t.Fatalf("local cache not cleared: %d source calls", src.calls.Load())
	}
}

func TestTenantRemoteInvalidateClearsLocalCacheWithoutEcho(t *testing.T) {
	c := memCache(t)
	local, remote := NewWithID(c, nil, "local"), NewWithID(c, nil, "remote")
	defer local.Close()
	defer remote.Close()
	src := &countSource{}
	tenant.SetSource(src)
	t.Cleanup(func() { tenant.SetSource(nil); tenant.SetBroadcaster(nil) })
	unwire := WireTenant(local)
	defer unwire()

	var echoes atomic.Int32
	remote.On(KindTenantInvalidate, func(json.RawMessage) { echoes.Add(1) }, nil)

	ctx := context.Background()
	_, _ = tenant.Lookup(ctx, 7)
	// 远端实例改了租户 7：发布事件，本进程应清掉缓存，并且不会再广播回去。
	remote.Publish(KindTenantInvalidate, tenantInvalidate{IDs: []idgen.ID{7}})
	eventually(t, func() bool {
		_, _ = tenant.Lookup(ctx, 7)
		return src.calls.Load() >= 2
	})
	time.Sleep(50 * time.Millisecond)
	if echoes.Load() != 0 {
		t.Fatalf("remote invalidation was re-broadcast %d times", echoes.Load())
	}

	// all=true：清空全部租户缓存
	_, _ = tenant.Lookup(ctx, 8)
	before := src.calls.Load()
	remote.Publish(KindTenantInvalidate, tenantInvalidate{All: true})
	eventually(t, func() bool {
		_, _ = tenant.Lookup(ctx, 8)
		return src.calls.Load() > before
	})
}

// ---- node_id 租约 ----

func newLease(t *testing.T, c cache.Cache, node int, token string, ttl, renew time.Duration, log *zap.Logger) (*Lease, error) {
	t.Helper()
	return acquireLease(context.Background(), c, node, token, ttl, renew, log)
}

func TestLeaseSkippedForMemoryCache(t *testing.T) {
	l, err := AcquireNodeLease(context.Background(), memCache(t), 3, nil)
	if err != nil || l != nil {
		t.Fatalf("memory cache must skip the check, got lease=%v err=%v", l, err)
	}
	l.Release() // nil 安全
}

func TestLeaseRejectsDuplicateNodeID(t *testing.T) {
	c := sharedFake{memCache(t)}
	l1, err := newLease(t, c, 5, "inst-1", time.Minute, time.Minute, nil)
	if err != nil || l1 == nil {
		t.Fatalf("first acquire: %v %v", l1, err)
	}
	_, err = newLease(t, c, 5, "inst-2", time.Minute, time.Minute, nil)
	if !errors.Is(err, ErrNodeIDInUse) {
		t.Fatalf("duplicate node_id error = %v, want ErrNodeIDInUse", err)
	}
	for _, want := range []string{"node_id 5", "inst-1", "distinct"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err, want)
		}
	}
	// 不同 node_id 互不影响
	l3, err := newLease(t, c, 6, "inst-2", time.Minute, time.Minute, nil)
	if err != nil {
		t.Fatalf("different node_id: %v", err)
	}
	// 同一进程重复申请（同 token）视为同一持有者
	l4, err := newLease(t, c, 5, "inst-1", time.Minute, time.Minute, nil)
	if err != nil {
		t.Fatalf("re-acquire by same instance: %v", err)
	}
	l4.Release()
	l3.Release()
	l1.Release()
}

func TestLeaseReleaseFreesNodeID(t *testing.T) {
	c := sharedFake{memCache(t)}
	l1, _ := newLease(t, c, 9, "inst-1", time.Minute, time.Minute, nil)
	l1.Release()
	l1.Release() // 幂等
	l2, err := newLease(t, c, 9, "inst-2", time.Minute, time.Minute, nil)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	// 释放只删自己持有的：旧持有者的 Release 不能删掉新持有者的租约
	l1.Release()
	if _, err := newLease(t, c, 9, "inst-3", time.Minute, time.Minute, nil); !errors.Is(err, ErrNodeIDInUse) {
		t.Fatalf("stale Release must not free someone else's lease, got %v", err)
	}
	l2.Release()
}

func TestLeaseExpiresWhenHolderDies(t *testing.T) {
	c := sharedFake{memCache(t)}
	// 持有者“崩溃”：只占位不续约
	lk, _ := cache.As[cache.Locker](c)
	_, _ = lk.SetNX(context.Background(), leaseKey(11), "dead", 60*time.Millisecond)
	if _, err := newLease(t, c, 11, "new", time.Minute, time.Minute, nil); !errors.Is(err, ErrNodeIDInUse) {
		t.Fatalf("live lease must block, got %v", err)
	}
	time.Sleep(120 * time.Millisecond)
	l, err := newLease(t, c, 11, "new", time.Minute, time.Minute, nil)
	if err != nil {
		t.Fatalf("expired lease must be claimable: %v", err)
	}
	l.Release()
}

func TestLeaseRenewKeepsItAlive(t *testing.T) {
	c := sharedFake{memCache(t)}
	l, err := newLease(t, c, 12, "inst-1", 150*time.Millisecond, 30*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	time.Sleep(500 * time.Millisecond) // 超过 3 个 TTL
	if _, err := newLease(t, c, 12, "inst-2", time.Minute, time.Minute, nil); !errors.Is(err, ErrNodeIDInUse) {
		t.Fatalf("renewed lease must still block others, got %v", err)
	}
}

// failingLocker 模拟缓存故障。
type failingLocker struct {
	cache.Cache
	fail atomic.Bool
}

func (f *failingLocker) Shared() bool { return true }
func (f *failingLocker) inner() cache.Locker {
	l, _ := cache.As[cache.Locker](f.Cache)
	return l
}
func (f *failingLocker) SetNX(ctx context.Context, k, v string, ttl time.Duration) (bool, error) {
	if f.fail.Load() {
		return false, errors.New("cache down")
	}
	return f.inner().SetNX(ctx, k, v, ttl)
}
func (f *failingLocker) ExpireIfEqual(ctx context.Context, k, v string, ttl time.Duration) (bool, error) {
	if f.fail.Load() {
		return false, errors.New("cache down")
	}
	return f.inner().ExpireIfEqual(ctx, k, v, ttl)
}
func (f *failingLocker) DelIfEqual(ctx context.Context, k, v string) (bool, error) {
	if f.fail.Load() {
		return false, errors.New("cache down")
	}
	return f.inner().DelIfEqual(ctx, k, v)
}
func (f *failingLocker) Unwrap() cache.Cache { return f.Cache }

func TestLeaseRenewFailureLogsWarningNotPanic(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	c := &failingLocker{Cache: memCache(t)}
	l, err := newLease(t, c, 13, "inst-1", time.Minute, 20*time.Millisecond, zap.New(core))
	if err != nil {
		t.Fatal(err)
	}
	c.fail.Store(true)
	eventually(t, func() bool { return logs.FilterMessageSnippet("renewing database.node_id lease failed").Len() > 0 })
	c.fail.Store(false)
	time.Sleep(60 * time.Millisecond) // 恢复后继续续约，不 panic
	l.Release()
}

func TestLeaseLostThenReclaimedOrReportedAsDuplicate(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	c := sharedFake{memCache(t)}
	l, err := newLease(t, c, 14, "inst-1", time.Minute, 20*time.Millisecond, zap.New(core))
	if err != nil {
		t.Fatal(err)
	}
	// 租约被抹掉（缓存重启）：应自动重新占位
	_ = c.Del(context.Background(), leaseKey(14))
	eventually(t, func() bool { return logs.FilterMessageSnippet("re-claimed").Len() > 0 })
	// 被别的实例占走：必须报 Error 级告警（这里观察器只收 Warn 及以上）
	lk, _ := cache.As[cache.Locker](c)
	_, _ = lk.DelIfEqual(context.Background(), leaseKey(14), "inst-1")
	_, _ = lk.SetNX(context.Background(), leaseKey(14), "thief", time.Minute)
	eventually(t, func() bool { return logs.FilterMessageSnippet("held by ANOTHER instance").Len() > 0 })
	l.Release() // 不能删掉 thief 的租约
	if v, _ := c.Get(context.Background(), leaseKey(14)); v != "thief" {
		t.Fatalf("Release removed someone else's lease, value=%q", v)
	}
}

func TestLeaseWithoutLockerSkipsWithWarning(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	type sharedNoLock struct{ sharedPlain }
	c := sharedNoLock{}
	l, err := acquireLease(context.Background(), c, 1, "x", time.Minute, time.Minute, zap.New(core))
	if err != nil || l != nil {
		t.Fatalf("got %v %v", l, err)
	}
	if logs.Len() == 0 {
		t.Fatal("expected a warning")
	}
}

type sharedPlain struct{ cache.Cache }

func (sharedPlain) Shared() bool { return true }
