package smscode

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// 两个“实例”（各自的进程内锁）共用同一份缓存：同一验证码并发校验，只有一个能成功。
func TestVerifyConsumesAtomicallyAcrossInstances(t *testing.T) {
	s1, rec, _ := newSvc()
	s2 := &Service{cache: s1.cache, log: s1.log, prov: s1.prov, template: s1.template, now: s1.now}
	ctx := context.Background()
	if err := s1.Send(ctx, phone, "1.1.1.1", true); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, s := range []*Service{s1, s2, s1, s2, s1, s2} {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			<-start
			if s.Verify(ctx, phone, rec.code) == nil {
				wins.Add(1)
			}
		}(s)
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("同一验证码并发校验成功 %d 次，期望恰好 1 次", wins.Load())
	}
}
