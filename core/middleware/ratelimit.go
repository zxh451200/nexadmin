package middleware

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/response"
)

// KeyFunc 从请求里取限流维度（IP、IP+路径、用户…）；返回空串表示这个请求不参与限流。
type KeyFunc func(c *gin.Context) string

// ByIP 按客户端 IP 限流。
func ByIP() KeyFunc { return func(c *gin.Context) string { return "ip:" + c.ClientIP() } }

// ByIPAndPath 按客户端 IP + 路由模板限流（同一 IP 对不同接口各自计数）。
func ByIPAndPath() KeyFunc {
	return func(c *gin.Context) string { return "ip:" + c.ClientIP() + "|p:" + routePath(c) }
}

// ByUser 按登录用户限流；未登录（没有有效令牌）时退回按 IP。
func ByUser() KeyFunc {
	return func(c *gin.Context) string {
		if cl := auth.ClaimsFrom(c); cl != nil {
			return "u:" + strconv.FormatInt(int64(cl.UserID), 10)
		}
		return "ip:" + c.ClientIP()
	}
}

func routePath(c *gin.Context) string {
	if p := c.FullPath(); p != "" {
		return p
	}
	return c.Request.URL.Path
}

// Limiter 是基于 cache 的固定窗口计数器：窗口从某个 key 的第一次计数开始，到期整体清零。
// cache 实现了 cache.Counter（memory / redis 都有）时计数是原子的、多实例共享；
// 否则退化为 Get+Set（非原子，只会少算，不会误杀）。cache 出错一律放行（fail-open）并限频告警。
type Limiter struct {
	cache  cache.Cache
	log    *zap.Logger
	scope  string
	max    int
	window time.Duration
	warned atomic.Int64 // 上次告警的 unix 秒
}

// NewLimiter 创建计数器。scope 用来隔离不同用途的计数（会进 cache key）；max <= 0 表示不限流。
func NewLimiter(c cache.Cache, log *zap.Logger, scope string, max int, window time.Duration) *Limiter {
	if log == nil {
		log = zap.NewNop()
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{cache: c, log: log, scope: scope, max: max, window: window}
}

// Window 返回计数窗口长度。
func (l *Limiter) Window() time.Duration { return l.window }

func (l *Limiter) key(k string) string { return cache.GlobalKey("rl", l.scope, k) }

func (l *Limiter) degraded(err error) {
	now := time.Now().Unix()
	if last := l.warned.Load(); now-last >= 30 && l.warned.CompareAndSwap(last, now) {
		l.log.Warn("ratelimit: cache unavailable, failing open", zap.String("scope", l.scope), zap.Error(err))
	}
}

// Hit 记一次请求并返回是否放行；不放行时 retry 是建议的等待时间。
func (l *Limiter) Hit(ctx context.Context, k string) (allowed bool, retry time.Duration) {
	if l == nil || l.max <= 0 || k == "" {
		return true, 0
	}
	n, left, err := l.incr(ctx, l.key(k))
	if err != nil {
		l.degraded(err)
		return true, 0
	}
	if n > int64(l.max) {
		return false, left
	}
	return true, 0
}

// Fail 记一次失败（不判断是否超限），配合 Blocked 做“失败达到上限后拒绝”。
func (l *Limiter) Fail(ctx context.Context, k string) {
	if l == nil || l.max <= 0 || k == "" {
		return
	}
	if _, _, err := l.incr(ctx, l.key(k)); err != nil {
		l.degraded(err)
	}
}

// Blocked 报告 k 在当前窗口内的计数是否已达上限（只读，不计数）。
func (l *Limiter) Blocked(ctx context.Context, k string) bool {
	if l == nil || l.max <= 0 || k == "" {
		return false
	}
	v, err := l.cache.Get(ctx, l.key(k))
	if errors.Is(err, cache.ErrNotFound) {
		return false
	}
	if err != nil {
		l.degraded(err)
		return false
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n >= int64(l.max)
}

func (l *Limiter) incr(ctx context.Context, key string) (int64, time.Duration, error) {
	if ctr, ok := l.cache.(cache.Counter); ok {
		return ctr.Incr(ctx, key, l.window)
	}
	var n int64
	v, err := l.cache.Get(ctx, key)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return 0, 0, err
	}
	if err == nil {
		n, _ = strconv.ParseInt(v, 10, 64)
	}
	n++
	// 退化路径无法保留原窗口起点，每次都续满窗口；只用于不支持原子计数的第三方 cache 实现。
	if err := l.cache.Set(ctx, key, strconv.FormatInt(n, 10), l.window); err != nil {
		return 0, 0, err
	}
	return n, l.window, nil
}

// RateLimit 固定窗口限流：同一 key 在 window 内最多 limit 次，超出返回 429 + Retry-After。
// scope 区分不同路由的计数；limit <= 0 或 key 为空时放行；cache 不可用时放行并告警。
func RateLimit(c cache.Cache, log *zap.Logger, scope string, key KeyFunc, limit int, window time.Duration) gin.HandlerFunc {
	if limit <= 0 || c == nil {
		return func(c *gin.Context) { c.Next() }
	}
	lim := NewLimiter(c, log, scope, limit, window)
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions { // CORS 预检不计数
			c.Next()
			return
		}
		ok, retry := lim.Hit(c.Request.Context(), key(c))
		if ok {
			c.Next()
			return
		}
		TooManyRequests(c, retry)
	}
}

// RateLimitRule 按配置里的一条规则创建限流中间件（规则未启用时返回直通中间件）。
func RateLimitRule(c cache.Cache, log *zap.Logger, cfg config.RateLimit, rule string, key KeyFunc) gin.HandlerFunc {
	r := cfg.Rule(rule)
	return RateLimit(c, log, rule, key, r.Limit, r.Window)
}

// TooManyRequests 写 429 + Retry-After（至少 1 秒）并中止请求。
func TooManyRequests(c *gin.Context, retry time.Duration) {
	secs := int(math.Ceil(retry.Seconds()))
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	response.Fail(c, response.New(http.StatusTooManyRequests, "error.too_many_requests").WithMsg("请求过于频繁，请稍后再试"))
}
