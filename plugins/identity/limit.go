package identity

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/middleware"
)

// localLimiter 是进程内固定窗口计数器，作为公开接口的兜底限流：
// 即使 Config / Cache 没有装配（rate limit 中间件无法创建），公开的 CAS / SLO 接口也不会裸奔。
// 表满且没有过期项可清时，新的 key 一律拒绝（宁可误拒，也不让内存被无限撑大）。
type localLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	cap    int
	hits   map[string]*localWindow
	now    func() time.Time
}

type localWindow struct {
	start time.Time
	n     int
}

func newLocalLimiter(max int, window time.Duration) *localLimiter {
	return &localLimiter{max: max, window: window, cap: 10000, hits: map[string]*localWindow{}, now: time.Now}
}

func (l *localLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w, ok := l.hits[key]
	if ok && now.Sub(w.start) >= l.window {
		ok = false
	}
	if !ok {
		if len(l.hits) >= l.cap {
			for k, v := range l.hits {
				if now.Sub(v.start) >= l.window {
					delete(l.hits, k)
				}
			}
			if len(l.hits) >= l.cap {
				return false
			}
		}
		l.hits[key] = &localWindow{start: now, n: 1}
		return true
	}
	w.n++
	return w.n <= l.max
}

// limitByIP 按客户端 IP 限流的 gin 中间件（超限 429）。
func limitByIP(l *localLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow("ip:" + c.ClientIP()) {
			middleware.TooManyRequests(c, l.window)
			return
		}
		c.Next()
	}
}
