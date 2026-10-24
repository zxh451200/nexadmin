package cache

import (
	"context"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nexadmin/core/config"
)

// runExtContract 是 Locker / SortedSet / PubSub 的接口契约测试，memory 与 redis 实现共用。
func runExtContract(t *testing.T, c Cache) {
	t.Helper()
	ctx := context.Background()
	p := "nex:test:" + strconv.FormatInt(time.Now().UnixNano(), 36) + ":"
	t.Run("CompareAndSwap", func(t *testing.T) { runCASContract(t, c) })
	t.Run("Lease", func(t *testing.T) {
		l, ok := As[Lease](c)
		if !ok {
			t.Fatal("Lease not implemented")
		}
		key := p + "leases"
		defer c.Del(ctx, key)
		var wg sync.WaitGroup
		var accepted atomic.Int32
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				err := l.UpdateLeases(ctx, key, time.Hour, func(entries map[string]LeaseEntry) error {
					if len(entries) >= 8 {
						return ErrNotFound
					}
					entries[strconv.Itoa(i)] = LeaseEntry{Bytes: 1, Until: time.Now().Add(time.Hour)}
					return nil
				})
				if err == nil {
					accepted.Add(1)
				} else if err != ErrNotFound {
					t.Errorf("reserve: %v", err)
				}
			}(i)
		}
		wg.Wait()
		if accepted.Load() != 8 {
			t.Fatalf("accepted %d", accepted.Load())
		}
		if err := l.UpdateLeases(ctx, key, time.Hour, func(entries map[string]LeaseEntry) error {
			if len(entries) != 8 {
				t.Fatalf("leases = %d", len(entries))
			}
			for id := range entries {
				delete(entries, id)
			}
			entries["expired"] = LeaseEntry{Bytes: 1, Until: time.Now().Add(-time.Second)}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := l.UpdateLeases(ctx, key, time.Hour, func(entries map[string]LeaseEntry) error {
			if len(entries) != 0 {
				t.Fatal("expired lease retained")
			}
			entries["bad"] = LeaseEntry{Bytes: -1, Until: time.Now().Add(time.Hour)}
			return nil
		}); err == nil {
			t.Fatal("negative lease accepted")
		}
		if err := l.UpdateLeases(ctx, key, time.Hour, func(entries map[string]LeaseEntry) error {
			if _, ok := entries["bad"]; ok {
				t.Fatal("failed update committed")
			}
			delete(entries, "absent")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Locker", func(t *testing.T) {
		lk, ok := As[Locker](c)
		if !ok {
			t.Fatal("Locker not implemented")
		}
		k := p + "lock"
		if got, err := lk.SetNX(ctx, k, "a", time.Minute); err != nil || !got {
			t.Fatalf("first SetNX = %v, %v", got, err)
		}
		if got, _ := lk.SetNX(ctx, k, "b", time.Minute); got {
			t.Fatal("second SetNX must fail while held")
		}
		if v, _ := c.Get(ctx, k); v != "a" {
			t.Fatalf("value = %q, want a", v)
		}
		if ok, _ := lk.ExpireIfEqual(ctx, k, "b", time.Minute); ok {
			t.Fatal("ExpireIfEqual with wrong value must be false")
		}
		if ok, err := lk.ExpireIfEqual(ctx, k, "a", time.Minute); err != nil || !ok {
			t.Fatalf("ExpireIfEqual = %v, %v", ok, err)
		}
		if ok, _ := lk.DelIfEqual(ctx, k, "b"); ok {
			t.Fatal("DelIfEqual with wrong value must be false")
		}
		if ok, err := lk.DelIfEqual(ctx, k, "a"); err != nil || !ok {
			t.Fatalf("DelIfEqual = %v, %v", ok, err)
		}
		if got, _ := lk.SetNX(ctx, k, "c", 50*time.Millisecond); !got {
			t.Fatal("SetNX after delete must succeed")
		}
		time.Sleep(120 * time.Millisecond)
		if got, _ := lk.SetNX(ctx, k, "d", time.Minute); !got {
			t.Fatal("SetNX after expiry must succeed")
		}
		if ok, _ := lk.ExpireIfEqual(ctx, p+"absent", "x", time.Minute); ok {
			t.Fatal("ExpireIfEqual on absent key must be false")
		}
		_ = c.Del(ctx, k)
	})

	t.Run("SortedSet", func(t *testing.T) {
		zs, ok := As[SortedSet](c)
		if !ok {
			t.Fatal("SortedSet not implemented")
		}
		k := p + "z"
		for i, m := range []string{"c", "a", "b"} {
			if err := zs.ZAdd(ctx, k, m, float64(10+i), time.Minute); err != nil {
				t.Fatal(err)
			}
		}
		if err := zs.ZAdd(ctx, k, "c", 5, 0); err != nil { // 更新 score，不改过期
			t.Fatal(err)
		}
		got, _ := zs.ZRange(ctx, k, 0)
		if want := []string{"c", "a", "b"}; !equal(got, want) {
			t.Fatalf("ZRange = %v, want %v", got, want)
		}
		got, _ = zs.ZRange(ctx, k, 11) // scores: c=5 a=11 b=12
		if want := []string{"a", "b"}; !equal(got, want) {
			t.Fatalf("ZRange(min=11) = %v, want %v", got, want)
		}
		_ = zs.ZRemRangeByScore(ctx, k, 12) // 删除 score < 12：c、a
		got, _ = zs.ZRange(ctx, k, 0)
		if want := []string{"b"}; !equal(got, want) {
			t.Fatalf("after ZRemRangeByScore = %v, want %v", got, want)
		}
		_ = zs.ZRem(ctx, k, "b", "nope")
		if got, _ = zs.ZRange(ctx, k, 0); len(got) != 0 {
			t.Fatalf("after ZRem = %v, want empty", got)
		}
		if got, err := zs.ZRange(ctx, p+"absent", 0); err != nil || len(got) != 0 {
			t.Fatalf("absent set = %v, %v", got, err)
		}
		// TTL：集合整体过期；Del 能删除集合
		_ = zs.ZAdd(ctx, k, "x", 1, 80*time.Millisecond)
		time.Sleep(150 * time.Millisecond)
		if got, _ = zs.ZRange(ctx, k, 0); len(got) != 0 {
			t.Fatalf("expired set = %v, want empty", got)
		}
		_ = zs.ZAdd(ctx, k, "y", 1, time.Minute)
		_ = c.Del(ctx, k)
		if got, _ = zs.ZRange(ctx, k, 0); len(got) != 0 {
			t.Fatalf("after Del = %v, want empty", got)
		}
	})

	t.Run("PubSub", func(t *testing.T) {
		ps, ok := As[PubSub](c)
		if !ok {
			t.Fatal("PubSub not implemented")
		}
		ch := p + "ch"
		var mu sync.Mutex
		var got1, got2 []string
		stop1, err := ps.Subscribe(ch, func(m string) { mu.Lock(); got1 = append(got1, m); mu.Unlock() }, nil)
		if err != nil {
			t.Fatal(err)
		}
		stop2, err := ps.Subscribe(ch, func(m string) { mu.Lock(); got2 = append(got2, m); mu.Unlock() }, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range []string{"1", "2", "3"} {
			if err := ps.Publish(ctx, ch, m); err != nil {
				t.Fatal(err)
			}
		}
		_ = ps.Publish(ctx, ch+"-other", "x") // 别的频道不应收到
		waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got1) == 3 && len(got2) == 3 })
		mu.Lock()
		if !equal(got1, []string{"1", "2", "3"}) {
			t.Fatalf("subscriber 1 got %v (order must be kept)", got1)
		}
		mu.Unlock()
		stop1()
		stop1() // 幂等
		_ = ps.Publish(ctx, ch, "4")
		waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got2) == 4 })
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		if len(got1) != 3 {
			t.Fatalf("stopped subscriber still received: %v", got1)
		}
		mu.Unlock()
		stop2()
	})
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func waitFor(t *testing.T, cond func() bool) {
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

func TestMemoryExtContract(t *testing.T) {
	c, err := New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	runExtContract(t, c)
	if IsShared(c) {
		t.Fatal("memory cache must not report Shared")
	}
}

// 真实 redis / valkey 契约测试：设置 NEXADMIN_TEST_REDIS_ADDR=host:port 才运行（沙箱没有 redis 时跳过）。
func TestRedisExtContract(t *testing.T) {
	addr := os.Getenv("NEXADMIN_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NEXADMIN_TEST_REDIS_ADDR not set")
	}
	c, err := New(config.Cache{Driver: "redis", Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	runExtContract(t, c)
	if !IsShared(c) {
		t.Fatal("redis cache must report Shared")
	}
}

func TestMemoryPubSubCloseStopsSubscribers(t *testing.T) {
	c := newMemory()
	var n atomic.Int32
	if _, err := c.Subscribe("ch", func(string) { n.Add(1) }, nil); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if _, err := c.Subscribe("ch", func(string) {}, nil); err == nil {
		t.Fatal("Subscribe after Close must fail")
	}
}

type wrapper struct{ Cache }

func (w wrapper) Unwrap() Cache { return w.Cache }

func TestAsSeesThroughWrappers(t *testing.T) {
	m := newMemory()
	defer m.Close()
	w := wrapper{wrapper{m}}
	if _, ok := As[SortedSet](w); !ok {
		t.Fatal("As must find SortedSet through wrappers")
	}
	if _, ok := As[Locker](w); !ok {
		t.Fatal("As must find Locker through wrappers")
	}
	if _, ok := As[PubSub](w); !ok {
		t.Fatal("As must find PubSub through wrappers")
	}
	if _, ok := As[SortedSet](plain{}); ok {
		t.Fatal("As must not find SortedSet on a plain cache")
	}
}

type plain struct{ Cache }
