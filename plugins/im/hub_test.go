package im

import (
	"strings"
	"testing"
	"time"
)

// hub：订阅者收到发布，取消后不再收到，慢消费者不阻塞发布者。
func TestHubPublishSubscribe(t *testing.T) {
	h := newHub()
	ch, cancel := h.subscribe("IM1")
	defer cancel()

	h.publish("IM1", []byte(`{"seq":1}`))
	select {
	case got := <-ch:
		if string(got) != `{"seq":1}` {
			t.Fatalf("got %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive payload")
	}

	// 其它会话不串音
	h.publish("IM2", []byte("other"))
	select {
	case got := <-ch:
		t.Fatalf("cross-conversation leak: %s", got)
	case <-time.After(50 * time.Millisecond):
	}

	// 取消后通道关闭、不再收到
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after cancel")
	}
}

// 慢消费者（缓冲满）只丢自己的消息，不影响其它订阅者。
func TestHubSlowSubscriberDrops(t *testing.T) {
	h := newHub()
	_, cancelSlow := h.subscribe("IM1")
	defer cancelSlow()
	fast, cancelFast := h.subscribe("IM1")
	defer cancelFast()

	for i := 0; i < 20; i++ {
		h.publish("IM1", []byte("x"))
	}
	// fast 应至少收到一条（慢的丢了不阻塞发布）
	select {
	case <-fast:
	case <-time.After(time.Second):
		t.Fatal("fast subscriber starved by slow one")
	}
}

// 流票一次性 + 过期失效。
func TestStreamTicketOneTimeAndExpiry(t *testing.T) {
	s := newStreamTicket(50 * time.Millisecond)
	tok := s.issue()
	if !s.take(tok) {
		t.Fatal("fresh ticket should be valid")
	}
	if s.take(tok) {
		t.Fatal("ticket must be one-time")
	}

	// 过期
	s2 := newStreamTicket(1 * time.Millisecond)
	tok2 := s2.issue()
	time.Sleep(5 * time.Millisecond)
	if s2.take(tok2) {
		t.Fatal("expired ticket should be rejected")
	}
	if s.take("") {
		t.Fatal("empty ticket rejected")
	}
}

// randomToken 长度与 hex 形态。
func TestRandomToken(t *testing.T) {
	tok := randomToken(16)
	if len(tok) != 32 || strings.ToLower(tok) != tok {
		t.Fatalf("unexpected token %q", tok)
	}
}
