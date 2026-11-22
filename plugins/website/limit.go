package website

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/middleware"
)

// 在线留言投稿阈值：匿名写入口，比页面浏览紧得多。
const (
	contactRateMax    = 5
	contactRateWindow = time.Minute
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

// ipGate 统一公开端限流的两种实现：cache 装配时走 middleware.Limiter（redis 共享桶，
// 多实例部署时全实例同一计数），没装配退回进程内滑动窗口。阈值语义两种实现一致：
// 每 IP 每 window 最多 max 次。之前只有留言走共享桶，附件上传/下单/下载/翻译/评分
// 都是进程内计数——多实例部署下配额按实例数线性放大，限流形同虚设。
type ipGate struct {
	shared *middleware.Limiter
	local  *ipLimiter
}

func newIPGate(c cache.Cache, log *zap.Logger, scope string, max int, window time.Duration) *ipGate {
	if c != nil {
		return &ipGate{shared: middleware.NewLimiter(c, log, scope, max, window)}
	}
	return &ipGate{local: newIPLimiter(max, window)}
}

// check 记一次计数并报告是否放行；拒绝时 retry 是建议等待时长（本地实现给整个窗口）。
func (g *ipGate) check(ctx context.Context, ip string) (bool, time.Duration) {
	if g == nil {
		return true, 0
	}
	if g.shared != nil {
		return g.shared.Hit(ctx, "ip:"+ip)
	}
	if !g.local.allow(ip) {
		return false, g.local.window
	}
	return true, 0
}

// allow 是 check 的布尔形态（调用方不需要 Retry-After 时用）。
func (g *ipGate) allow(ctx context.Context, ip string) bool {
	ok, _ := g.check(ctx, ip)
	return ok
}

// Middleware 包装成 gin 中间件，语义与 ipLimiter.Middleware 相同。
func (g *ipGate) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if ok, _ := g.check(c.Request.Context(), c.ClientIP()); !ok {
			c.Header("Retry-After", "60")
			c.String(http.StatusTooManyRequests, "too many requests")
			c.Abort()
			return
		}
		c.Next()
	}
}
