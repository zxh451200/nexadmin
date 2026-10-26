package website

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ipLimiter 是进程内滑动窗口 IP 限流，作为公开官网页的兜底（identity 插件 CAS 入口同款思路；
// 阈值放宽到正常访客与爬虫无感）。多实例部署时每实例独立计数。
type ipLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	max    int
	window time.Duration
}

func newIPLimiter(max int, window time.Duration) *ipLimiter {
	return &ipLimiter{hits: map[string][]time.Time{}, max: max, window: window}
}

func (l *ipLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.hits[ip][:0:0]
	for _, t := range l.hits[ip] {
		if now.Sub(t) <= l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[ip] = kept
		return false
	}
	l.hits[ip] = append(kept, now)
	// 防御性清理：键过多时丢弃已过期的 IP，避免长跑进程内存缓慢增长。
	if len(l.hits) > 8192 {
		for k, v := range l.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > l.window {
				delete(l.hits, k)
			}
		}
	}
	return true
}

// Middleware 包装成 gin 中间件；超限直接 429（官网是 HTML，用纯文本而不是统一 JSON 报文）。
func (l *ipLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow(c.ClientIP()) {
			c.Header("Retry-After", "60")
			c.String(http.StatusTooManyRequests, "too many requests")
			c.Abort()
			return
		}
		c.Next()
	}
}
