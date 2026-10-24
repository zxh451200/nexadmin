package user

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
)

// 两个“实例”（各自的进程内锁）共用同一份缓存：同一验证码并发校验，只有一个能成功。
func TestForgotVerifyConsumesAtomicallyAcrossInstances(t *testing.T) {
	e := newFgEnv(t, true)
	f2 := newForgotService(e.st, e.f.cache, e.ns, zap.NewNop())
	ctx := context.Background()
	code := e.codeFor(t, "alice@example.org")

	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, f := range []*forgotService{e.f, f2, e.f, f2, e.f, f2} {
		wg.Add(1)
		go func(f *forgotService) {
			defer wg.Done()
			<-start
			if f.verify(ctx, "alice@example.org", code) == nil {
				wins.Add(1)
			}
		}(f)
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("同一验证码并发校验成功 %d 次，期望恰好 1 次", wins.Load())
	}
}
