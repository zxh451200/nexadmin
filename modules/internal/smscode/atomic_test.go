package smscode

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/config"
)

// newMemSvc 用真实的内存缓存（实现 cache.Counter / AtomicGetDel）：并发测试走原子路径。
func newMemSvc(t *testing.T) (*Service, *recorder) {
	t.Helper()
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	rec := &recorder{}
	return &Service{cache: c, log: zap.NewNop(), prov: rec, template: "login", now: time.Now}, rec
}

// 并发 bump：计数不丢，恰好 limit 次放行，最终计数精确等于调用次数。
func TestBumpConcurrentExactCount(t *testing.T) {
	s, _ := newMemSvc(t)
	ctx := context.Background()
	const total, limit = 200, 10
	var allowed atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := s.bump(ctx, "k", limit, time.Minute)
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if allowed.Load() != limit {
		t.Fatalf("放行 %d 次，期望恰好 %d", allowed.Load(), limit)
	}
	if v, _ := s.cache.Get(ctx, "k"); v != strconv.Itoa(total) {
		t.Fatalf("最终计数 %q，期望 %d", v, total)
	}
}

// 并发输错：达到上限后验证码被作废，之后即使提交正确的码也失败。
func TestConcurrentWrongAttemptsInvalidateCode(t *testing.T) {
	s, rec := newMemSvc(t)
	ctx := context.Background()
	if err := s.Send(ctx, phone, "1.1.1.1", true); err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if rec.code == wrong {
		wrong = "111111"
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if s.Verify(ctx, phone, wrong) == nil {
				t.Error("wrong code accepted")
			}
		}()
	}
	close(start)
	wg.Wait()
	if ok, _ := s.cache.Exists(ctx, codeKey(phone)); ok {
		t.Fatal("达到输错上限后验证码必须被 Del 作废")
	}
	if err := s.Verify(ctx, phone, rec.code); err == nil {
		t.Fatal("作废后正确的码也不能通过")
	}
}

// 并发混合：1 个正确 + 39 个错误，成功至多 1 次，且错误尝试不会让验证码被多次消费。
func TestConcurrentMixedAttemptsAtMostOneSuccess(t *testing.T) {
	s, rec := newMemSvc(t)
	ctx := context.Background()
	if err := s.Send(ctx, phone, "1.1.1.1", true); err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if rec.code == wrong {
		wrong = "111111"
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 40; i++ {
		code := wrong
		if i == 20 {
			code = rec.code
		}
		wg.Add(1)
		go func(code string) {
			defer wg.Done()
			<-start
			if s.Verify(ctx, phone, code) == nil {
				wins.Add(1)
			}
		}(code)
	}
	close(start)
	wg.Wait()
	if wins.Load() > 1 {
		t.Fatalf("成功 %d 次，期望至多 1 次", wins.Load())
	}
	if err := s.Verify(ctx, phone, rec.code); err == nil {
		t.Fatal("验证码在消费或作废后不可再用")
	}
}

// 重新发送的新验证码从 0 开始计数：旧码的输错次数不带过来。
func TestResendResetsAttemptCounter(t *testing.T) {
	s, rec := newMemSvc(t)
	ctx := context.Background()
	_ = s.Send(ctx, phone, "1.1.1.1", true)
	wrong := "000000"
	if rec.code == wrong {
		wrong = "111111"
	}
	for i := 0; i < MaxAttempts-1; i++ {
		_ = s.Verify(ctx, phone, wrong)
	}
	_ = s.cache.Del(ctx, cache.GlobalKey("sms", "cool", digest(phone))) // 跳过 60 秒冷却
	if err := s.Send(ctx, phone, "1.1.1.1", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(ctx, phone, rec.code); err != nil {
		t.Fatalf("新验证码应有完整的尝试次数: %v", err)
	}
}

// countProv 是并发安全的发送计数器；err 非空时模拟渠道报错。
type countProv struct {
	n   atomic.Int32
	err error
}

func (p *countProv) Send(context.Context, string, string, map[string]string) error {
	p.n.Add(1)
	return p.err
}

// 冷却原子化：同一手机号并发 N 次（不同 IP，避免被 IP 限流干扰），只有 1 次放行，渠道只被调用 1 次。
func TestCooldownConcurrentOnlyOneWins(t *testing.T) {
	s, _ := newMemSvc(t)
	prov := &countProv{}
	s.prov = prov
	ctx := context.Background()
	const total = 100
	var ok, limited atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			switch err := s.Send(ctx, phone, "10.0.0."+strconv.Itoa(i), true); {
			case err == nil:
				ok.Add(1)
			case isStatus(err, 429):
				limited.Add(1)
			default:
				t.Error(err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if ok.Load() != 1 || limited.Load() != total-1 || prov.n.Load() != 1 {
		t.Fatalf("放行 %d、冷却中 %d、渠道调用 %d；期望 1、%d、1", ok.Load(), limited.Load(), prov.n.Load(), total-1)
	}
}

// 渠道报错时释放冷却：用户可以立刻重试，不会被锁 60 秒。
func TestCooldownReleasedWhenProviderFails(t *testing.T) {
	s, _ := newMemSvc(t)
	prov := &countProv{err: errors.New("channel down")}
	s.prov = prov
	ctx := context.Background()
	if err := s.Send(ctx, phone, "1.1.1.1", true); err == nil {
		t.Fatal("渠道报错应返回错误")
	}
	if ok, _ := s.cache.Exists(ctx, cache.GlobalKey("sms", "cool", digest(phone))); ok {
		t.Fatal("发送失败后冷却 key 应被释放")
	}
	prov.err = nil
	if err := s.Send(ctx, phone, "1.1.1.1", true); err != nil {
		t.Fatalf("释放冷却后应可立即重试: %v", err)
	}
	if err := s.Send(ctx, phone, "1.1.1.1", true); !isStatus(err, 429) {
		t.Fatalf("成功发送后应进入冷却: %v", err)
	}
}
