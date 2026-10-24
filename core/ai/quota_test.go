package ai

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func policyGW(t *testing.T, p *TenantPolicy) (*Gateway, *atomic.Int32) {
	t.Helper()
	var upstreamCalls atomic.Int32
	srv, _ := upstream(t, func(chatRequest) string {
		upstreamCalls.Add(1)
		return `{"filters":{},"explanation":"x"}`
	})
	g, _ := gateway(srv.URL)
	g.Policy = func(context.Context, Caller) (*TenantPolicy, error) { return p, nil }
	g.Quota = NewMemoryCounter()
	g.Limiter = nil // 这里只测租户策略，不受每用户限流干扰
	return g, &upstreamCalls
}

func filter(g *Gateway, who Caller) error {
	_, err := g.Filter(context.Background(), who, "user", "停用的用户")
	return err
}

func TestTenantDisabledBlocksBeforeUpstream(t *testing.T) {
	g, calls := policyGW(t, &TenantPolicy{Disabled: true})
	if err := filter(g, Caller{UserID: "1", TenantID: "9"}); !errors.Is(err, ErrTenantDisabled) {
		t.Fatalf("err = %v, want ErrTenantDisabled", err)
	}
	if calls.Load() != 0 {
		t.Fatal("disabled tenant must not reach the upstream")
	}
	// 其它入口同样被挡
	if _, err := g.Translate(context.Background(), tester, TranslateRequest{Texts: []string{"a"}, From: "en", To: "zh", Format: "text"}); !errors.Is(err, ErrTenantDisabled) {
		t.Fatalf("translate err = %v", err)
	}
	if _, err := g.Write(context.Background(), tester, WriteRequest{Mode: "write", Text: "x"}); !errors.Is(err, ErrTenantDisabled) {
		t.Fatalf("write err = %v", err)
	}
	// /ai/status 对被关闭的租户返回未启用，前端据此隐藏入口
	if st := g.Status(context.Background(), Caller{TenantID: "9"}); st.Enabled {
		t.Fatalf("status for disabled tenant = %+v", st)
	}
}

func TestDailyQuotaCountsAndResets(t *testing.T) {
	g, calls := policyGW(t, &TenantPolicy{DailyQuota: 3})
	day := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	g.Now = func() time.Time { return day }
	a, b := Caller{UserID: "1", TenantID: "9"}, Caller{UserID: "2", TenantID: "9"}

	// 配额按租户共享：不同用户的调用合并计数
	for i, who := range []Caller{a, b, a} {
		if err := filter(g, who); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	err := filter(g, b)
	var qe *QuotaError
	if !errors.Is(err, ErrQuotaExceeded) || !errors.As(err, &qe) || qe.Limit != 3 {
		t.Fatalf("4th call: err = %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("upstream calls = %d, want 3 (over-quota call must not reach it)", calls.Load())
	}
	// 其它租户互不影响
	if err := filter(g, Caller{UserID: "1", TenantID: "10"}); err != nil {
		t.Fatalf("other tenant: %v", err)
	}
	// 第二天重置
	day = day.Add(24 * time.Hour)
	if err := filter(g, a); err != nil {
		t.Fatalf("next day: %v", err)
	}
}

func TestQuotaNotConsumedByInvalidInputOrRateLimit(t *testing.T) {
	g, _ := policyGW(t, &TenantPolicy{DailyQuota: 1})
	who := Caller{UserID: "1", TenantID: "9"}
	if _, err := g.Filter(context.Background(), who, "nope", "x"); !errors.Is(err, ErrBadInput) {
		t.Fatalf("err = %v", err)
	}
	if _, err := g.Filter(context.Background(), who, "user", "   "); !errors.Is(err, ErrBadInput) {
		t.Fatalf("err = %v", err)
	}
	if err := filter(g, who); err != nil { // 前面的非法请求没有消耗这唯一一次
		t.Fatalf("first valid call: %v", err)
	}
	// 被每用户限流挡住的请求也不消耗租户配额
	g2, _ := policyGW(t, &TenantPolicy{DailyQuota: 2})
	g2.Limiter = NewWindowLimiter(1, time.Hour)
	if err := filter(g2, who); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := filter(g2, who); !errors.Is(err, ErrRateLimited) {
			t.Fatalf("err = %v, want rate limited", err)
		}
	}
	g2.Limiter = nil
	if err := filter(g2, who); err != nil { // 配额 2 里只用了 1 次
		t.Fatalf("quota wrongly consumed by rate-limited calls: %v", err)
	}
}

func TestPlatformExemptAndUnlimited(t *testing.T) {
	g, calls := policyGW(t, &TenantPolicy{Disabled: true, DailyQuota: 1})
	exempt := Caller{UserID: "1", TenantID: "1", Exempt: true}
	for i := 0; i < 5; i++ { // 超管 / 平台租户：关闭开关与配额都不生效
		if err := filter(g, exempt); err != nil {
			t.Fatalf("exempt call %d: %v", i, err)
		}
	}
	if !g.Status(context.Background(), exempt).Enabled {
		t.Fatal("status must be enabled for exempt callers")
	}
	if calls.Load() != 5 {
		t.Fatalf("calls = %d", calls.Load())
	}
	// 配额 0 = 不限；策略为 nil（不受管租户）同样不限
	for _, p := range []*TenantPolicy{{DailyQuota: 0}, nil} {
		g, _ := policyGW(t, p)
		for i := 0; i < 5; i++ {
			if err := filter(g, Caller{UserID: "1", TenantID: "9"}); err != nil {
				t.Fatalf("policy %+v call %d: %v", p, i, err)
			}
		}
	}
}

// 策略来源或计数器故障时放行并记 warn，不让 AI 整体不可用。
func TestPolicyAndCounterFailuresFailOpen(t *testing.T) {
	g, _ := policyGW(t, &TenantPolicy{DailyQuota: 1})
	g.Policy = func(context.Context, Caller) (*TenantPolicy, error) { return nil, errors.New("db down") }
	if err := filter(g, tester); err != nil {
		t.Fatalf("policy error must fail open: %v", err)
	}
	g.Policy = func(context.Context, Caller) (*TenantPolicy, error) { return &TenantPolicy{DailyQuota: 1}, nil }
	g.Quota = failingCounter{}
	if err := filter(g, tester); err != nil {
		t.Fatalf("counter error must fail open: %v", err)
	}
}

type failingCounter struct{}

func (failingCounter) Incr(context.Context, string, time.Duration) (int64, error) {
	return 0, errors.New("cache down")
}

func TestQuotaKeyIsPerTenantPerSiteDay(t *testing.T) {
	d := time.Date(2026, 10, 17, 23, 59, 0, 0, time.UTC)
	k := quotaKey("9", d)
	if !strings.Contains(k, ":9:20261017") || k == quotaKey("10", d) || k == quotaKey("9", d.Add(2*time.Minute)) {
		t.Fatalf("key = %q", k)
	}
	if ttl := quotaTTL(d); ttl <= time.Hour || ttl > 2*time.Hour+time.Minute {
		t.Fatalf("ttl = %v", ttl)
	}
}

func TestMemoryCounterExpires(t *testing.T) {
	c := NewMemoryCounter().(*memoryCounter)
	now := time.Now()
	c.now = func() time.Time { return now }
	for i := int64(1); i <= 3; i++ {
		if n, _ := c.Incr(context.Background(), "k", time.Minute); n != i {
			t.Fatalf("n = %d, want %d", n, i)
		}
	}
	now = now.Add(2 * time.Minute)
	if n, _ := c.Incr(context.Background(), "k", time.Minute); n != 1 {
		t.Fatalf("after expiry n = %d", n)
	}
}
