package user

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

const fgEmail = "alice@example.org"

func wrongCode(right string) string {
	if right == "000000" {
		return "111111"
	}
	return "000000"
}

// 并发输错：达到上限后验证码被 Del 作废，之后正确的码也不能通过。
func TestForgotConcurrentWrongAttemptsInvalidateCode(t *testing.T) {
	e := newFgEnv(t, true)
	ctx := context.Background()
	code := e.codeFor(t, fgEmail)
	wrong := wrongCode(code)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if e.f.verify(ctx, fgEmail, wrong) == nil {
				t.Error("wrong code accepted")
			}
		}()
	}
	close(start)
	wg.Wait()
	if ok, _ := e.f.cache.Exists(ctx, codeKey(ctx, fgEmail)); ok {
		t.Fatal("达到输错上限后验证码必须被作废")
	}
	if err := e.f.verify(ctx, fgEmail, code); err == nil {
		t.Fatal("作废后正确的码也不能通过")
	}
}

// 并发混合：1 个正确 + 39 个错误，成功至多 1 次（GetDel 一次性）。
func TestForgotConcurrentMixedAtMostOneSuccess(t *testing.T) {
	e := newFgEnv(t, true)
	ctx := context.Background()
	code := e.codeFor(t, fgEmail)
	wrong := wrongCode(code)
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 40; i++ {
		c := wrong
		if i == 7 {
			c = code
		}
		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			<-start
			if e.f.verify(ctx, fgEmail, c) == nil {
				wins.Add(1)
			}
		}(c)
	}
	close(start)
	wg.Wait()
	if wins.Load() > 1 {
		t.Fatalf("成功 %d 次，期望至多 1 次", wins.Load())
	}
	if err := e.f.verify(ctx, fgEmail, code); err == nil {
		t.Fatal("验证码在消费或作废后不可再用")
	}
}

// 精确计数：恰好 forgotMaxTries-1 次输错后，正确的码仍可用；计数 key 带 TTL 且以十进制存放。
func TestForgotAttemptCounterExact(t *testing.T) {
	e := newFgEnv(t, true)
	ctx := context.Background()
	code := e.codeFor(t, fgEmail)
	wrong := wrongCode(code)
	var wg sync.WaitGroup
	for i := 0; i < forgotMaxTries-1; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = e.f.verify(ctx, fgEmail, wrong) }()
	}
	wg.Wait()
	raw, _ := e.f.cache.Get(ctx, codeKey(ctx, fgEmail))
	salt := raw[:16]
	if v, err := e.f.cache.Get(ctx, triesKey(ctx, fgEmail, salt)); err != nil || v != "4" {
		t.Fatalf("尝试计数 %q (%v)，期望精确为 4", v, err)
	}
	if err := e.f.verify(ctx, fgEmail, code); err != nil {
		t.Fatalf("未到上限时正确的码应通过: %v", err)
	}
}
