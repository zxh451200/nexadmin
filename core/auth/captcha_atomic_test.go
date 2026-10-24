package auth

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// 同一张验证码被并发提交（正确答案）：GetDel 保证只有一个成功。
func TestCaptchaConcurrentVerifyConsumesOnce(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()
	key, _, _ := s.NewCaptcha(ctx, "9.9.9.9")
	ans := answerOf(t, s, key)
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ok, _ := s.VerifyCaptcha(ctx, key, ans, "9.9.9.9"); ok {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("并发校验同一验证码成功 %d 次，期望恰好 1 次", wins.Load())
	}
}

// 并发记录登录失败：用户名与 IP 两个维度的计数都精确，且第 captchaAfterFails 次起要求验证码。
func TestRecordLoginFailureConcurrentExactCount(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()
	const n = 100
	var needed atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if need, err := s.RecordLoginFailure(ctx, "Alice", "9.9.9.9"); err != nil {
				t.Error(err)
			} else if need {
				needed.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	for _, k := range []string{failUserKey("alice"), failIPKey("9.9.9.9")} {
		if v, _ := s.cache.Get(ctx, k); v != strconv.Itoa(n) {
			t.Fatalf("%s 计数 %q，期望精确为 %d", k, v, n)
		}
	}
	// 用户名与 IP 两个维度各自独立自增，同一 goroutine 在两键上的序号不保证一致；
	// need 只保证下界：两个序号都落在阈值之前（<=2）的 goroutine 至多 captchaAfterFails-1 个。
	if got := needed.Load(); got < n-captchaAfterFails+1 {
		t.Fatalf("要求验证码的次数 %d，至少应为 %d", got, n-captchaAfterFails+1)
	}
	if need, _ := s.LoginNeedsCaptcha(ctx, "alice", "1.1.1.1"); !need {
		t.Fatal("达到阈值后该用户名必须要求验证码")
	}
}
