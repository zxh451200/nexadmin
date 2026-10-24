package cache

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"nexadmin/core/config"
)

// runCASContract 是 CompareSwapper 统一语义（见 ext.go）的契约测试：memory 与 redis 共用，
// 由 runExtContract 调用；redis 侧只有设置了 NEXADMIN_TEST_REDIS_ADDR 才会真正跑到 Lua 脚本。
func runCASContract(t *testing.T, c Cache) {
	t.Helper()
	cas, ok := As[CompareSwapper](c)
	if !ok {
		t.Fatal("CompareSwapper not implemented")
	}
	ctx := context.Background()
	p := "nex:test:cas:" + strconv.FormatInt(time.Now().UnixNano(), 36) + ":"

	// 不存在的 key:不替换,也不创建
	if ok, err := cas.CompareAndSwap(ctx, p+"missing", "a", "b"); err != nil || ok {
		t.Fatalf("missing key: ok=%v err=%v", ok, err)
	}
	if _, err := c.Get(ctx, p+"missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a failed swap must not create the key: %v", err)
	}

	// 值不等:不替换
	_ = c.Set(ctx, p+"k", "v1", time.Hour)
	if ok, _ := cas.CompareAndSwap(ctx, p+"k", "other", "v2"); ok {
		t.Fatal("swap must fail when the value differs")
	}
	if v, _ := c.Get(ctx, p+"k"); v != "v1" {
		t.Fatalf("value changed by a failed swap: %q", v)
	}

	// 没有 TTL 的 key:可以替换,并且仍然没有 TTL(旧的 redis 实现在这里返回 false)
	_ = c.Set(ctx, p+"nottl", "v1", 0)
	if ok, err := cas.CompareAndSwap(ctx, p+"nottl", "v1", "v2"); err != nil || !ok {
		t.Fatalf("key without TTL must be swappable: ok=%v err=%v", ok, err)
	}
	if v, _ := c.Get(ctx, p+"nottl"); v != "v2" {
		t.Fatalf("swap did not store the new value: %q", v)
	}
	if ok, _ := cas.CompareAndSwap(ctx, p+"nottl", "v1", "v3"); ok {
		t.Fatal("the old value must no longer match")
	}

	// 有 TTL 的 key:替换后沿用原来的剩余 TTL,不被延长
	_ = c.Set(ctx, p+"ttl", "a", 400*time.Millisecond)
	time.Sleep(250 * time.Millisecond)
	if ok, _ := cas.CompareAndSwap(ctx, p+"ttl", "a", "b"); !ok {
		t.Fatal("swap on a live key with TTL must succeed")
	}
	time.Sleep(250 * time.Millisecond) // 距离最初写入 500ms > 400ms
	if _, err := c.Get(ctx, p+"ttl"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the swap must not extend the original TTL: %v", err)
	}
	// 已过期:不能复活
	if ok, _ := cas.CompareAndSwap(ctx, p+"ttl", "b", "c"); ok {
		t.Fatal("an expired key must not be swapped or recreated")
	}
	if _, err := c.Get(ctx, p+"ttl"); !errors.Is(err, ErrNotFound) {
		t.Fatal("an expired key must stay gone")
	}
}

func TestMemoryCASContract(t *testing.T) {
	c, err := New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	runCASContract(t, c)
}

func TestLeaseBackoffBoundedAndJittered(t *testing.T) {
	for attempt := 0; attempt < 40; attempt++ {
		for i := 0; i < 50; i++ {
			d := leaseBackoff(attempt)
			if d <= 0 || d > leaseBackoffMax {
				t.Fatalf("attempt %d: backoff %v out of (0, %v]", attempt, d, leaseBackoffMax)
			}
		}
		if attempt == 0 && leaseBackoff(0) > leaseBackoffBase {
			t.Fatalf("first backoff must not exceed the base: %v", leaseBackoff(0))
		}
	}
	seen := map[time.Duration]bool{}
	for i := 0; i < 200; i++ {
		seen[leaseBackoff(10)] = true
	}
	if len(seen) < 5 {
		t.Fatalf("backoff at the cap must be jittered, saw only %d distinct values", len(seen))
	}
}

func TestRetryOptimisticGivesUpAfterMaxTries(t *testing.T) {
	var sleeps []time.Duration
	old := leaseSleep
	leaseSleep = func(_ context.Context, d time.Duration) error { sleeps = append(sleeps, d); return nil }
	defer func() { leaseSleep = old }()
	errConflict := errors.New("conflict")
	var calls int
	err := retryOptimistic(context.Background(), func() error { calls++; return errConflict },
		func(err error) bool { return errors.Is(err, errConflict) })
	if !errors.Is(err, ErrLeaseContention) {
		t.Fatalf("want ErrLeaseContention, got %v", err)
	}
	if calls != maxLeaseTries || len(sleeps) != maxLeaseTries-1 {
		t.Fatalf("calls=%d sleeps=%d, want %d/%d", calls, len(sleeps), maxLeaseTries, maxLeaseTries-1)
	}
	var total time.Duration
	for _, d := range sleeps {
		total += d
	}
	if total > time.Duration(maxLeaseTries)*leaseBackoffMax {
		t.Fatalf("total backoff %v is unbounded", total)
	}
}

func TestRetryOptimisticSucceedsAfterConflicts(t *testing.T) {
	old := leaseSleep
	leaseSleep = func(context.Context, time.Duration) error { return nil }
	defer func() { leaseSleep = old }()
	errConflict := errors.New("conflict")
	var calls atomic.Int32
	err := retryOptimistic(context.Background(), func() error {
		if calls.Add(1) < 5 {
			return errConflict
		}
		return nil
	}, func(err error) bool { return errors.Is(err, errConflict) })
	if err != nil || calls.Load() != 5 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
	// 非冲突错误原样返回,不重试
	boom := errors.New("boom")
	calls.Store(0)
	err = retryOptimistic(context.Background(), func() error { calls.Add(1); return boom },
		func(err error) bool { return errors.Is(err, errConflict) })
	if err != boom || calls.Load() != 1 {
		t.Fatalf("non-conflict error must not be retried: err=%v calls=%d", err, calls.Load())
	}
}

func TestRetryOptimisticStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errConflict := errors.New("conflict")
	var calls int
	err := retryOptimistic(ctx, func() error {
		calls++
		if calls == 2 {
			cancel()
		}
		return errConflict
	}, func(err error) bool { return errors.Is(err, errConflict) })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if calls > 3 {
		t.Fatalf("must stop promptly after cancel, ran %d times", calls)
	}
}
