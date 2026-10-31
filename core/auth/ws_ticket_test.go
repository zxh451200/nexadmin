package auth

// WS 入场票：一次性、短时效、不可伪造。这里覆盖「用后即废」与「非法票面一律拒绝」。

import (
	"context"
	"strings"
	"testing"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/idgen"
)

func newTicketService(t *testing.T) *Service {
	t.Helper()
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return &Service{
		secret:     []byte("0123456789abcdef0123456789abcdef"),
		accessTTL:  30 * time.Minute,
		refreshTTL: 168 * time.Hour,
		cache:      ch,
	}
}

func TestWSTicketIsSingleUse(t *testing.T) {
	s := newTicketService(t)
	ctx := context.Background()
	cl := &Claims{UserID: idgen.ID(123), TenantID: idgen.ID(456), Username: "agent-1", Super: true}

	ticket, ttl, err := s.IssueWSTicket(ctx, cl)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if ttl != int(wsTicketTTL.Seconds()) {
		t.Fatalf("ttl = %d, want %d", ttl, int(wsTicketTTL.Seconds()))
	}

	got, err := s.RedeemWSTicket(ctx, ticket)
	if err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if got.UserID != cl.UserID || got.TenantID != cl.TenantID || got.Username != cl.Username || !got.Super {
		t.Fatalf("redeemed claims = %+v, want %+v", got, cl)
	}

	// 第二次消费必须失败：票据不是一次性就等于没换过。
	if _, err := s.RedeemWSTicket(ctx, ticket); err != ErrInvalidToken {
		t.Fatalf("second redeem err = %v, want ErrInvalidToken", err)
	}
}

func TestWSTicketRejectsGarbageAndExpired(t *testing.T) {
	s := newTicketService(t)
	ctx := context.Background()

	// 非十六进制或长度不对的票面不能拼进缓存键，也不能被当成有效票。
	for _, bad := range []string{"", strings.Repeat("0", 31), strings.Repeat("g", 32), strings.Repeat("0", 33)} {
		if _, err := s.RedeemWSTicket(ctx, bad); err != ErrInvalidToken {
			t.Fatalf("redeem %q err = %v, want ErrInvalidToken", bad, err)
		}
	}

	ticket, _, err := s.IssueWSTicket(ctx, &Claims{UserID: idgen.ID(7), TenantID: idgen.ID(8)})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	_ = ticket

	// 过期后必须自动失效：用极短 TTL 重新签一张，等它过去再消费。
	old := wsTicketTTL
	wsTicketTTL = time.Millisecond
	soon, _, err := s.IssueWSTicket(ctx, &Claims{UserID: idgen.ID(9), TenantID: idgen.ID(9)})
	wsTicketTTL = old
	if err != nil {
		t.Fatalf("issue short-lived: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := s.RedeemWSTicket(ctx, soon); err != ErrInvalidToken {
		t.Fatalf("expired redeem err = %v, want ErrInvalidToken", err)
	}
}

func TestIssueWSTicketRequiresPrincipal(t *testing.T) {
	s := newTicketService(t)
	for _, cl := range []*Claims{nil, {UserID: 0}} {
		if _, _, err := s.IssueWSTicket(context.Background(), cl); err == nil {
			t.Fatal("anonymous claims must not get a ws ticket")
		}
	}
}
