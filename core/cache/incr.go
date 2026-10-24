package cache

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	incrFallbackMu   sync.Mutex
	incrFallbackWarn sync.Once
)

// Incr 把 key 加 1 并返回新值；key 首次创建时按 ttl 设置过期（之后的自增不延长）。
// c（或其装饰器内层）实现了 Counter 时是原子的；否则回落为进程内加锁的 Get+Set，
// 同进程内不丢更新，但多实例共享同一后端时不精确，进程内首次回落记一条 warn。
// 与 GetDel 辅助函数同一风格，不是 Cache 接口的一部分。
func Incr(c Cache, ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if ctr, ok := As[Counter](c); ok {
		n, _, err := ctr.Incr(ctx, key, ttl)
		return n, err
	}
	incrFallbackWarn.Do(func() {
		zap.L().Warn("cache: backend does not implement Counter, falling back to in-process Get+Set; failure counters are not exact across instances")
	})
	incrFallbackMu.Lock()
	defer incrFallbackMu.Unlock()
	var n int64
	switch v, err := c.Get(ctx, key); {
	case err == nil:
		n, _ = strconv.ParseInt(v, 10, 64)
	case !errors.Is(err, ErrNotFound):
		return 0, err
	}
	n++
	// 回落路径无法读取剩余 TTL，只能在首次创建时设置过期；之后的 Set 会重置为 ttl，这是回落的已知偏差。
	if err := c.Set(ctx, key, strconv.FormatInt(n, 10), ttl); err != nil {
		return 0, err
	}
	return n, nil
}
