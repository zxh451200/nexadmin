package user

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/tenant"
)

// 每个包装器代表一个应用实例，原子操作由共同的缓存后端完成。
type loginCacheInstance struct{ cache.Cache }

func (c loginCacheInstance) Unwrap() cache.Cache { return c.Cache }

type loginCounterGate struct {
	cache.Cache
	arrived atomic.Int32
	ready   chan struct{}
	workers int32
}

func (c *loginCounterGate) Unwrap() cache.Cache { return c.Cache }

func (c *loginCounterGate) Incr(ctx context.Context, key string, ttl time.Duration) (int64, time.Duration, error) {
	// 让所有实例都通过前置锁定检查，再竞争计数和首次写锁。
	if c.arrived.Add(1) == c.workers {
		close(c.ready)
	}
	<-c.ready
	counter, _ := cache.As[cache.Counter](c.Cache)
	return counter.Incr(ctx, key, ttl)
}

// 并发的密码猜测不能靠读改写竞态突破锁定阈值：恰好只有 maxRetry-1 次“未锁定”的失败。
func TestRecordFailureIsAtomicUnderConcurrency(t *testing.T) {
	e := newEnv(t) // sqlmock 没有预期的查询 -> loginPolicy 回落默认值（5 次 / 15 分钟）
	const workers = 40
	backend := &loginCounterGate{Cache: e.d.Cache, ready: make(chan struct{}), workers: workers}
	var open atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			secs, err := recordFailure(e.ctx, e.d.DB, loginCacheInstance{backend}, "victim")
			if err != nil {
				t.Error(err)
				return
			}
			if secs == 0 {
				open.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := open.Load(); got != defaultMaxRetry-1 {
		t.Fatalf("%d guesses were counted without locking, want exactly %d", got, defaultMaxRetry-1)
	}
	if until, err := lockedUntil(e.ctx, e.d.Cache, "victim"); err != nil || until.IsZero() {
		t.Fatalf("account must be locked: until=%v err=%v", until, err)
	}
}

func TestRecordFailureLockLifecycle(t *testing.T) {
	e := newEnv(t)
	for i := 1; i <= defaultMaxRetry; i++ {
		secs, err := recordFailure(e.ctx, e.d.DB, e.d.Cache, "Victim")
		if err != nil || (secs > 0) != (i == defaultMaxRetry) {
			t.Fatalf("failure %d: secs=%d err=%v", i, secs, err)
		}
	}
	before, err := lockedUntil(e.ctx, e.d.Cache, "victim")
	if err != nil || time.Until(before) < 15*time.Minute-time.Second {
		t.Fatalf("lock must retain full duration: %v, %v", before, err)
	}
	// 计数保留且锁定期间不再增长，后续请求不延长截止时间。
	count, _ := e.d.Cache.Get(e.ctx, failKey(e.ctx, "victim"))
	if count != strconv.Itoa(defaultMaxRetry) {
		t.Fatalf("counter was reset: %q", count)
	}
	if secs, err := recordFailure(e.ctx, e.d.DB, e.d.Cache, "victim"); err != nil || secs <= 0 {
		t.Fatalf("locked failure: %d, %v", secs, err)
	}
	after, _ := lockedUntil(e.ctx, e.d.Cache, "victim")
	if !before.Equal(after) {
		t.Fatalf("lock extended: %v -> %v", before, after)
	}
	other := tenant.With(context.Background(), 2)
	if secs, err := recordFailure(other, e.d.DB, e.d.Cache, "victim"); err != nil || secs != 0 {
		t.Fatalf("tenant isolation: %d, %v", secs, err)
	}
	if err := clearLock(e.ctx, e.d.Cache, "victim"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{failKey(e.ctx, "victim"), lockKey(e.ctx, "victim")} {
		if _, err := e.d.Cache.Get(e.ctx, key); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("unlock did not delete %s: %v", key, err)
		}
	}
	if secs, err := recordFailure(e.ctx, e.d.DB, e.d.Cache, "victim"); err != nil || secs != 0 {
		t.Fatalf("fresh counter after unlock: %d, %v", secs, err)
	}
}

type faultyLoginCache struct {
	cache.Cache
	cache.Locker
	getErr, incrErr, lockErr error
	loseLock                 bool
}

func (c faultyLoginCache) Get(ctx context.Context, key string) (string, error) {
	if c.getErr != nil {
		return "", c.getErr
	}
	return c.Cache.Get(ctx, key)
}

func (c faultyLoginCache) Incr(ctx context.Context, key string, ttl time.Duration) (int64, time.Duration, error) {
	if c.incrErr != nil {
		return 0, 0, c.incrErr
	}
	counter, _ := cache.As[cache.Counter](c.Cache)
	return counter.Incr(ctx, key, ttl)
}

func (c faultyLoginCache) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if c.lockErr != nil || c.loseLock {
		return false, c.lockErr
	}
	locker, _ := cache.As[cache.Locker](c.Cache)
	return locker.SetNX(ctx, key, value, ttl)
}

func TestRecordFailureCacheErrors(t *testing.T) {
	unavailable := errors.New("cache unavailable")
	for _, stage := range []string{"get", "incr", "lock", "missing capability", "lock disappeared"} {
		t.Run(stage, func(t *testing.T) {
			e := newEnv(t)
			c := faultyLoginCache{Cache: e.d.Cache}
			switch stage {
			case "get":
				c.getErr = unavailable
			case "incr":
				c.incrErr = unavailable
			case "lock":
				c.lockErr = unavailable
			case "lock disappeared":
				c.loseLock = true
			}
			if err := e.d.Cache.Set(e.ctx, failKey(e.ctx, "victim"), "4", time.Minute); err != nil {
				t.Fatal(err)
			}
			var backend cache.Cache = c
			if stage == "missing capability" {
				backend = struct{ cache.Cache }{e.d.Cache}
			}
			secs, err := recordFailure(e.ctx, e.d.DB, backend, "victim")
			if err == nil || secs != 0 {
				t.Fatalf("cache failure must return error: secs=%d err=%v", secs, err)
			}
			if stage == "get" || stage == "incr" || stage == "lock" {
				if !errors.Is(err, unavailable) {
					t.Fatalf("original error lost: %v", err)
				}
			}
			if stage == "lock" {
				// 写锁失败不能丢掉计数；恢复后的下一次失败仍必须锁定。
				if secs, err := recordFailure(e.ctx, e.d.DB, e.d.Cache, "victim"); err != nil || secs <= 0 {
					t.Fatalf("retry after cache recovery: %d, %v", secs, err)
				}
			}
		})
	}
}

func TestLockedUntilFormatsAndExpiry(t *testing.T) {
	e := newEnv(t)
	for _, value := range []string{
		time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10),
	} {
		if err := e.d.Cache.Set(e.ctx, lockKey(e.ctx, "victim"), value, time.Minute); err != nil {
			t.Fatal(err)
		}
		if until, err := lockedUntil(e.ctx, e.d.Cache, "victim"); err != nil || until.IsZero() {
			t.Fatalf("valid lock %q: %v, %v", value, until, err)
		}
	}
	if err := e.d.Cache.Set(e.ctx, lockKey(e.ctx, "victim"), "corrupt", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := lockedUntil(e.ctx, e.d.Cache, "victim"); err == nil {
		t.Fatal("corrupt lock must fail closed")
	}
	if err := e.d.Cache.Set(e.ctx, lockKey(e.ctx, "victim"),
		time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), time.Minute); err != nil {
		t.Fatal(err)
	}
	if until, err := lockedUntil(e.ctx, e.d.Cache, "victim"); err != nil || !until.IsZero() {
		t.Fatalf("elapsed deadline must unlock even before key eviction: %v, %v", until, err)
	}
	// 过期的旧计数及锁定由后端 TTL 清理，下一次失败创建新窗口。
	for _, key := range []string{lockKey(e.ctx, "victim"), failKey(e.ctx, "victim")} {
		if err := e.d.Cache.Set(e.ctx, key, "99", time.Nanosecond); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for {
			_, err := e.d.Cache.Get(e.ctx, key)
			if errors.Is(err, cache.ErrNotFound) {
				break
			}
			if err != nil || time.Now().After(deadline) {
				t.Fatalf("TTL failed: %v", err)
			}
		}
	}
	if secs, err := recordFailure(e.ctx, e.d.DB, e.d.Cache, "victim"); err != nil || secs != 0 {
		t.Fatalf("expired window: %d, %v", secs, err)
	}
}

func TestRecordFailureThresholdStartsFullLock(t *testing.T) {
	e := newEnv(t)
	// 失败窗口即将结束时才达到阈值，仍应从此刻锁定完整的策略时长。
	if err := e.d.Cache.Set(e.ctx, failKey(e.ctx, "victim"), "4", time.Minute); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if secs, err := recordFailure(e.ctx, e.d.DB, e.d.Cache, "victim"); err != nil || secs != 15*60 {
		t.Fatalf("threshold lock: %d, %v", secs, err)
	}
	until, err := lockedUntil(e.ctx, e.d.Cache, "victim")
	if err != nil || until.Before(start.Add(15*time.Minute)) || until.After(time.Now().Add(15*time.Minute)) {
		t.Fatalf("lock duration must start at threshold: %v, %v", until, err)
	}
	counter, _ := cache.As[cache.Counter](e.d.Cache)
	_, remaining, err := counter.Incr(e.ctx, failKey(e.ctx, "victim"), 15*time.Minute)
	if err != nil || remaining <= 0 || remaining > time.Minute {
		t.Fatalf("failure window must keep its original TTL: %v, %v", remaining, err)
	}
}
