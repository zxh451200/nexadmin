package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/config"
)

// 包装后的缓存仍要实现 cache.AtomicGetDel（嵌入接口不会自动带上可选能力），且转发给底层的原子实现。
func TestWrapCacheKeepsAtomicGetDel(t *testing.T) {
	base, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	c := WrapCache(base)
	if _, ok := c.(cache.AtomicGetDel); !ok {
		t.Fatal("wrapped cache lost cache.AtomicGetDel")
	}
	ctx := context.Background()
	_ = c.Set(ctx, "k", "v", time.Minute)
	if v, err := cache.GetDel(c, ctx, "k"); err != nil || v != "v" {
		t.Fatalf("GetDel = %q %v", v, err)
	}
	if _, err := cache.GetDel(c, ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("已取出应 ErrNotFound：%v", err)
	}
}
