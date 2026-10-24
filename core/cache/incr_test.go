package cache

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"
)

// bareCache 只暴露 Cache 接口，隐藏 Counter / AtomicGetDel 等可选能力，用来测回落路径。
type bareCache struct{ Cache }

func hammerIncr(t *testing.T, c Cache, workers int) {
	t.Helper()
	ctx := context.Background()
	var wg sync.WaitGroup
	seen := make([]int64, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			n, err := Incr(c, ctx, "k", time.Minute)
			if err != nil {
				t.Error(err)
			}
			seen[i] = n
		}(i)
	}
	close(start)
	wg.Wait()
	uniq := map[int64]bool{}
	for _, n := range seen {
		uniq[n] = true
	}
	if len(uniq) != workers {
		t.Fatalf("每次自增应拿到不同的值，得到 %d 个不同值，期望 %d", len(uniq), workers)
	}
	v, err := c.Get(ctx, "k")
	if err != nil || v != strconv.Itoa(workers) {
		t.Fatalf("最终计数 %q (%v)，期望 %d", v, err, workers)
	}
}

func TestIncrHelperConcurrentAtomic(t *testing.T) {
	m := newMemory()
	defer m.Close()
	hammerIncr(t, m, 200)
}

func TestIncrHelperConcurrentFallback(t *testing.T) {
	m := newMemory()
	defer m.Close()
	hammerIncr(t, bareCache{m}, 200)
}

func TestIncrHelperSetsTTLOnCreateOnly(t *testing.T) {
	m := newMemory()
	defer m.Close()
	ctx := context.Background()
	if _, err := Incr(m, ctx, "k", 80*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if n, _ := Incr(m, ctx, "k", 80*time.Millisecond); n != 2 {
		t.Fatalf("n=%d", n)
	}
	time.Sleep(50 * time.Millisecond) // 距创建 100ms：自增不延长 TTL，key 已过期
	if n, _ := Incr(m, ctx, "k", 80*time.Millisecond); n != 1 {
		t.Fatalf("过期后应从 1 重新计数，得到 %d", n)
	}
}
