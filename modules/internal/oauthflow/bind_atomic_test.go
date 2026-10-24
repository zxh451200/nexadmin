package oauthflow

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/oauth"
)

// 真实内存缓存（实现 cache.Counter）上并发 Lookup：计数不丢，恰好 MaxBindTries 次放行，之后令牌被 Del 作废。
func TestLookupBindTokenConcurrentExactTries(t *testing.T) {
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f := &Flow{cfg: &config.Config{}, cache: c, log: zap.NewNop(), adapters: map[string]oauth.Provider{}}
	ctx := context.Background()
	tok, err := f.NewBindToken(ctx, &oauth.Identity{Provider: "oidc", ExternalID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	wins := runConcurrently(30, func() error { _, err := f.LookupBindToken(ctx, tok); return err })
	if wins != MaxBindTries {
		t.Fatalf("并发 Lookup 放行 %d 次，期望恰好 %d", wins, MaxBindTries)
	}
	if ok, _ := c.Exists(ctx, bindKey(tok)); ok {
		t.Fatal("超过上限后令牌必须被作废")
	}
	if _, err := f.ConsumeBindToken(ctx, tok); !errors.Is(err, ErrBindToken) {
		t.Fatalf("作废后不可消费: %v", err)
	}
}
