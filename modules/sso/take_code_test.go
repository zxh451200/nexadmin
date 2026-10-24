package sso

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
)

// 授权码一次性：并发兑换同一个 code，只有一个能取到（cache.GetDel 原子消费，不再依赖进程内锁）。
func TestTakeCodeAtomic(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()
	b, _ := json.Marshal(codeData{ClientID: "nxsso_test", UserID: 42})
	if err := p.cache.Set(ctx, codeKey("code-1"), string(b), codeTTL); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if d, ok := p.takeCode(ctx, "code-1"); ok && d.UserID == 42 {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("并发兑换同一授权码成功 %d 次，期望恰好 1 次", wins.Load())
	}
	if _, ok := p.takeCode(ctx, "code-1"); ok {
		t.Fatal("已兑换的授权码不能再用")
	}
}
