package ai

import (
	"sync"
	"time"
)

// Limiter 是每用户限流接口。NewGateway 默认用下面的进程内滑动窗口（单实例 / 测试 / 没有缓存的调用方）；
// HTTP 装配处（modules/ai）换成基于 core/middleware.Limiter 的适配实现，计数放 cache，多实例共享。
type Limiter interface {
	Allow(key string) bool
}

type windowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
	now    func() time.Time
}

// NewWindowLimiter 返回按 key 计数的滑动窗口限流（进程内）：window 内最多 limit 次。
func NewWindowLimiter(limit int, window time.Duration) Limiter {
	return &windowLimiter{limit: limit, window: window, hits: map[string][]time.Time{}, now: time.Now}
}

func (l *windowLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cut := now.Add(-l.window)
	if len(l.hits) > 10000 { // 顺带清理久未出现的 key
		for k, v := range l.hits {
			if len(v) == 0 || !v[len(v)-1].After(cut) {
				delete(l.hits, k)
			}
		}
	}
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
