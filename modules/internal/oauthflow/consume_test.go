package oauthflow

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"nexadmin/pkg/oauth"
)

// runConcurrently 同时放行 n 个调用，返回成功（err == nil）的个数。
func runConcurrently(n int, fn func() error) int {
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if fn() == nil {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	return int(wins.Load())
}

func TestConsumeBindTokenAtomic(t *testing.T) {
	f, _, _ := newFlow(t, false)
	ctx := context.Background()
	tok, err := f.NewBindToken(ctx, &oauth.Identity{Provider: "oidc", ExternalID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if wins := runConcurrently(8, func() error { _, err := f.ConsumeBindToken(ctx, tok); return err }); wins != 1 {
		t.Fatalf("并发消费同一个 bindToken，成功 %d 次，期望恰好 1 次", wins)
	}
	if _, err := f.ConsumeBindToken(ctx, tok); !errors.Is(err, ErrBindToken) {
		t.Fatalf("已消费应返回 ErrBindToken：%v", err)
	}
	if _, err := f.LookupBindToken(ctx, tok); !errors.Is(err, ErrBindToken) {
		t.Fatal("消费后令牌应失效")
	}
	if _, err := f.ConsumeBindToken(ctx, "  "); !errors.Is(err, ErrBindToken) {
		t.Fatal("空令牌应被拒绝")
	}
}

func TestConsumeBindTokenReturnsIdentity(t *testing.T) {
	f, _, _ := newFlow(t, false)
	ctx := context.Background()
	tok, _ := f.NewBindToken(ctx, &oauth.Identity{Provider: "oidc", ExternalID: "u9"})
	id, err := f.ConsumeBindToken(ctx, tok)
	if err != nil || id.ExternalID != "u9" || id.Provider != "oidc" {
		t.Fatalf("id=%+v err=%v", id, err)
	}
}

func TestTakeStateAtomic(t *testing.T) {
	f, _, _ := newFlow(t, false)
	ctx := context.Background()
	u, err := f.Start(ctx, "oidc", cb, PurposeLogin, 0)
	if err != nil {
		t.Fatal(err)
	}
	state := stateOf(t, u)
	if wins := runConcurrently(8, func() error { _, err := f.Take(ctx, "oidc", state); return err }); wins != 1 {
		t.Fatalf("并发消费同一个 state，成功 %d 次，期望恰好 1 次", wins)
	}
}
