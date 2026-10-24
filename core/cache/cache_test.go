package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestMemoryGetDel(t *testing.T) {
	c := newMemory()
	defer c.Close()
	ctx := context.Background()

	if _, err := GetDel(c, ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在应返回 ErrNotFound：%v", err)
	}
	_ = c.Set(ctx, "k", "v", time.Minute)
	if v, err := GetDel(c, ctx, "k"); err != nil || v != "v" {
		t.Fatalf("GetDel = %q %v", v, err)
	}
	if _, err := c.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatal("取出后应已删除")
	}
	_ = c.Set(ctx, "exp", "v", time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if _, err := GetDel(c, ctx, "exp"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("过期应返回 ErrNotFound：%v", err)
	}
}

// 并发取同一个 key，只有一个能拿到值。
func TestMemoryGetDelExactlyOnce(t *testing.T) {
	c := newMemory()
	defer c.Close()
	ctx := context.Background()
	for round := 0; round < 50; round++ {
		_ = c.Set(ctx, "once", "v", time.Minute)
		var wins atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := GetDel(c, ctx, "once"); err == nil {
					wins.Add(1)
				}
			}()
		}
		close(start)
		wg.Wait()
		if wins.Load() != 1 {
			t.Fatalf("round %d: %d 个调用者取到了值，期望恰好 1 个", round, wins.Load())
		}
	}
}

// plainCache 只实现 Cache 接口，模拟不支持 AtomicGetDel 的外部实现。
type plainCache struct {
	mu   sync.Mutex
	data map[string]string
}

func (p *plainCache) Get(_ context.Context, k string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.data[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (p *plainCache) Set(_ context.Context, k, v string, _ time.Duration) error {
	p.mu.Lock()
	p.data[k] = v
	p.mu.Unlock()
	return nil
}
func (p *plainCache) Del(_ context.Context, keys ...string) error {
	p.mu.Lock()
	for _, k := range keys {
		delete(p.data, k)
	}
	p.mu.Unlock()
	return nil
}
func (p *plainCache) Exists(ctx context.Context, k string) (bool, error) {
	_, err := p.Get(ctx, k)
	return err == nil, nil
}
func (p *plainCache) Close() error { return nil }

func TestGetDelFallbackWarnsOnce(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()
	getDelFallbackWarn = sync.Once{}

	p := &plainCache{data: map[string]string{"a": "1", "b": "2"}}
	ctx := context.Background()
	if _, ok := Cache(p).(AtomicGetDel); ok {
		t.Fatal("测试假实现不应实现 AtomicGetDel")
	}
	if v, err := GetDel(p, ctx, "a"); err != nil || v != "1" {
		t.Fatalf("回落 GetDel = %q %v", v, err)
	}
	if _, err := GetDel(p, ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("回落后应已删除：%v", err)
	}
	if v, err := GetDel(p, ctx, "b"); err != nil || v != "2" {
		t.Fatalf("回落 GetDel = %q %v", v, err)
	}
	if n := logs.Len(); n != 1 {
		t.Fatalf("回落告警应只记一次，实际 %d 次", n)
	}
}

func TestBuiltinsImplementAtomicGetDel(t *testing.T) {
	var _ AtomicGetDel = (*memoryCache)(nil)
	var _ AtomicGetDel = (*redisCache)(nil)
}
