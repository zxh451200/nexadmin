package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/cache"
)

// 租户级限制错误。
var (
	// ErrTenantDisabled 表示该租户被平台关闭了 AI。
	ErrTenantDisabled = errors.New("ai: disabled for tenant")
	// ErrQuotaExceeded 表示该租户今日 AI 调用次数已用完（用 errors.Is 判断，errors.As 取 *QuotaError 拿到上限）。
	ErrQuotaExceeded = errors.New("ai: tenant daily quota exceeded")
)

// QuotaError 携带租户每日配额上限，用于文案。
type QuotaError struct{ Limit int }

func (e *QuotaError) Error() string {
	return fmt.Sprintf("ai: tenant daily quota %d exceeded", e.Limit)
}

// Is 让 errors.Is(err, ErrQuotaExceeded) 成立。
func (e *QuotaError) Is(target error) bool { return target == ErrQuotaExceeded }

// TenantPolicy 是某个租户的 AI 限制。DailyQuota 为 0 表示不限次数。
type TenantPolicy struct {
	Disabled   bool
	DailyQuota int
}

// PolicyFunc 返回调用方所在租户的 AI 限制。(nil, nil) 表示不受限（平台租户 / 不受管租户）。
// 返回错误时网关放行并记 warn：策略来源故障不应让整个 AI 不可用（租户过期等硬限制由别的中间件把守）。
type PolicyFunc func(ctx context.Context, who Caller) (*TenantPolicy, error)

// Counter 是按 key 的原子计数器；key 首次出现时按 ttl 设置存活时间。
type Counter interface {
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// CacheCounter 用 core/cache 的原子计数（memory / redis 都支持，多实例共享）；底层不支持 Counter 时回落到进程内计数。
func CacheCounter(c cache.Cache) Counter {
	if cc, ok := c.(cache.Counter); ok {
		return cacheCounter{cc}
	}
	return NewMemoryCounter()
}

type cacheCounter struct{ c cache.Counter }

func (c cacheCounter) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, _, err := c.c.Incr(ctx, key, ttl)
	return n, err
}

type memEntry struct {
	n   int64
	exp time.Time
}

type memoryCounter struct {
	mu  sync.Mutex
	m   map[string]memEntry
	now func() time.Time
}

// NewMemoryCounter 返回进程内计数器（单实例或测试用）。
func NewMemoryCounter() Counter { return &memoryCounter{m: map[string]memEntry{}, now: time.Now} }

func (c *memoryCounter) Incr(_ context.Context, key string, ttl time.Duration) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if len(c.m) > 10000 { // 顺带清理过期 key
		for k, e := range c.m {
			if !now.Before(e.exp) {
				delete(c.m, k)
			}
		}
	}
	e, ok := c.m[key]
	if !ok || !now.Before(e.exp) {
		e = memEntry{exp: now.Add(ttl)}
	}
	e.n++
	c.m[key] = e
	return e.n, nil
}

// quotaKey 是租户某天的计数 key；日期按站点时区，所以“每日”在站点的 0 点重置。
func quotaKey(tenantID string, day time.Time) string {
	return cache.GlobalKey("ai", "quota", tenantID, day.Format("20060102"))
}

// quotaTTL 是计数 key 的存活时间：到当天结束再加 1 小时余量。
func quotaTTL(now time.Time) time.Duration {
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	return end.Sub(now) + time.Hour
}

// policyFor 返回调用方的租户限制；平台超管 / 平台租户（Exempt）、未配置 Policy、策略来源出错都返回 nil（不限制）。
// 出错放行并记 warn：策略来源故障不应让整个 AI 不可用（租户过期等硬限制由租户中间件把守）。
func (g *Gateway) policyFor(ctx context.Context, who Caller) *TenantPolicy {
	if g.Policy == nil || who.Exempt {
		return nil
	}
	p, err := g.Policy(ctx, who)
	if err != nil {
		g.Log.Warn("ai tenant policy lookup failed, allowing", zap.Error(err))
		return nil
	}
	return p
}

// consumeQuota 把本次调用计入租户今日用量，超出每日上限返回 *QuotaError。
func (g *Gateway) consumeQuota(ctx context.Context, who Caller, p *TenantPolicy) error {
	if p == nil || p.DailyQuota <= 0 || g.Quota == nil {
		return nil
	}
	now := g.Now()
	n, err := g.Quota.Incr(ctx, quotaKey(who.TenantID, now), quotaTTL(now))
	if err != nil {
		g.Log.Warn("ai quota counter failed, allowing", zap.Error(err))
		return nil // 计数器故障（如缓存宕机）时放行：配额是成本控制，不应让 AI 整体不可用
	}
	if n > int64(p.DailyQuota) {
		return &QuotaError{Limit: p.DailyQuota}
	}
	return nil
}

// tenantAllowed 只看开关（不消耗配额）；供 /ai/status 隐藏前端入口。
func (g *Gateway) tenantAllowed(ctx context.Context, who Caller) bool {
	p := g.policyFor(ctx, who)
	return p == nil || !p.Disabled
}
