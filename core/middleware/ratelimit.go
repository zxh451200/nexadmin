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
	"nexadmin/core/tenant"
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

// ByTenant 按请求所在租户限流（一个租户一个配额桶，隔离“一家把带宽吃满”）；解析不到租户时放行。
func ByTenant() KeyFunc {
	return func(c *gin.Context) string {
		if id, ok := tenant.From(c.Request.Context()); ok {
			return "t:" + strconv.FormatInt(int64(id), 10)
		}
		return ""
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
// 否则退化为 Get+Set（非原子，只会少算，不会误杀）。cache 出错默认放行（fail-open）并限频告警；
// 安全敏感规则（failClosedRules）反其道行之：拒绝服务也不给暴力破解开窗。
type Limiter struct {
	cache      cache.Cache
	log        *zap.Logger
	scope      string
	max        int
	window     time.Duration
	warned     atomic.Int64 // 上次告警的 unix 秒
	failClosed bool         // true = cache 故障时拒绝请求（503）而不是放行
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
	ok, retry, _ := l.HitDegrade(ctx, k)
	return ok, retry
}

// LimitResult 是动态配额判定的完整结果，用于响应头与审计。
type LimitResult struct {
	Allowed    bool
	Count      int64
	Limit      int
	Remaining  int64
	ResetAfter time.Duration
	RetryAfter time.Duration
	Degraded   bool
}

// HitRule 按本次请求携带的 limit/window 计数；同一 Limiter 可服务不同凭证策略。
func (l *Limiter) HitRule(ctx context.Context, k string, limit int, window time.Duration) LimitResult {
	if l == nil || l.cache == nil || limit <= 0 || k == "" {
		return LimitResult{Allowed: true}
	}
	if window <= 0 {
		window = time.Minute
	}
	n, left, err := l.incr(ctx, l.key(k), window)
	if err != nil {
		l.degraded(err)
		return LimitResult{Allowed: !l.failClosed, Limit: limit, ResetAfter: window, RetryAfter: window, Degraded: true}
	}
	remaining := max(int64(limit)-n, 0)
	res := LimitResult{Allowed: n <= int64(limit), Count: n, Limit: limit, Remaining: remaining, ResetAfter: left}
	if !res.Allowed {
		res.RetryAfter = left
	}
	return res
}

// HitDegrade 与 Hit 相同，但额外报告这次判定是否因 cache 故障而降级：
// fail-closed 规则据此把响应写成 503（服务端不可用）而不是 429（客户端超限）。
func (l *Limiter) HitDegrade(ctx context.Context, k string) (allowed bool, retry time.Duration, degraded bool) {
	if l == nil || l.max <= 0 || k == "" {
		return true, 0, false
	}
	res := l.HitRule(ctx, k, l.max, l.window)
	return res.Allowed, res.RetryAfter, res.Degraded
}

// Fail 记一次失败（不判断是否超限），配合 Blocked 做“失败达到上限后拒绝”。
func (l *Limiter) Fail(ctx context.Context, k string) {
	if l == nil || l.max <= 0 || k == "" {
		return
	}
	if _, _, err := l.incr(ctx, l.key(k), l.window); err != nil {
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

func (l *Limiter) incr(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	if ctr, ok := l.cache.(cache.Counter); ok {
		return ctr.Incr(ctx, key, window)
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
	if err := l.cache.Set(ctx, key, strconv.FormatInt(n, 10), window); err != nil {
		return 0, 0, err
	}
	return n, window, nil
}

// failClosedRules 是安全敏感的限流规则：cache 故障时宁可 503 拒绝也不放行。
// 这些端点的失败计数 / 验证码 / 锁定全部依赖 cache，放行等于把暴力破解的窗口打开
// （ASVS 2.2.1：反自动化控制在依赖降级时应 fail-closed 或有补偿）。其余规则保持
// fail-open：可用性优先，短时的 cache 抖动不该拖垮浏览与业务接口。
var failClosedRules = map[string]bool{
	"login":    true,
	"refresh":  true,
	"captcha":  true,
	"sms_code": true,
	"token":    true,
	"setup":    true,
}

// RateLimit 固定窗口限流：同一 key 在 window 内最多 limit 次，超出返回 429 + Retry-After。
// scope 区分不同路由的计数；limit <= 0 或 key 为空时放行；cache 不可用时放行并告警。
func RateLimit(c cache.Cache, log *zap.Logger, scope string, key KeyFunc, limit int, window time.Duration) gin.HandlerFunc {
	return rateLimit(c, log, scope, key, limit, window, false)
}

func rateLimit(c cache.Cache, log *zap.Logger, scope string, key KeyFunc, limit int, window time.Duration, failClosed bool) gin.HandlerFunc {
	if limit <= 0 || c == nil {
		return func(c *gin.Context) { c.Next() }
	}
	lim := &Limiter{cache: c, log: log, scope: scope, max: limit, window: window, failClosed: failClosed}
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions { // CORS 预检不计数
			c.Next()
			return
		}
		ok, retry, degraded := lim.HitDegrade(c.Request.Context(), key(c))
		if ok {
			c.Next()
			return
		}
		if degraded && failClosed {
			// cache 故障下的安全规则：这不是客户端超限（429），是限流器自己不可用（503）。
			response.Fail(c, response.New(http.StatusServiceUnavailable, "error.unavailable"))
			return
		}
		TooManyRequests(c, retry)
	}
}

// RateLimitRule 按配置里的一条规则创建限流中间件（规则未启用时返回直通中间件）；
// 安全敏感规则（failClosedRules）在 cache 故障时 503 拒绝，其余 fail-open。
func RateLimitRule(c cache.Cache, log *zap.Logger, cfg config.RateLimit, rule string, key KeyFunc) gin.HandlerFunc {
	r := cfg.Rule(rule)
	return rateLimit(c, log, rule, key, r.Limit, r.Window, failClosedRules[rule])
}

// TooManyRequests 写 429 + Retry-After（至少 1 秒）并中止请求。
func TooManyRequests(c *gin.Context, retry time.Duration) {
	secs := int(math.Ceil(retry.Seconds()))
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	response.Fail(c, response.New(http.StatusTooManyRequests, "error.rate_limited"))
}
