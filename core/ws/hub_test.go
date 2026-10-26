package ws

import (
	"testing"
	"time"
)

// 注册/订阅/发布/取消/关闭的基本生命周期。
func TestHubLifecycle(t *testing.T) {
	h := NewHub(0)
	c := h.Register("c1", "u1", nil)
	h.Subscribe("c1", "topic1")

	h.Publish("topic1", map[string]string{"hello": "world"})
	select {
	case f := <-c.Send:
		if f.Type != TypeMessage || f.Topic != "topic1" {
			t.Fatalf("unexpected frame %+v", f)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive")
	}

	h.Unsubscribe("c1", "topic1")
	h.Publish("topic1", "x")
	select {
	case f := <-c.Send:
		t.Fatalf("should not receive after unsubscribe: %+v", f)
	case <-time.After(50 * time.Millisecond):
	}

	c.Close()
	// 关闭后 topic 集合被清理
	if h.TopicCount() != 0 {
		t.Fatalf("topic not cleaned: %d", h.TopicCount())
	}
	// 再发布不 panic
	h.Publish("topic1", "x")
}

// 未订阅的主题不投递；PublishToUser 只投给指定用户。
func TestHubTargeting(t *testing.T) {
	h := NewHub(0)
	c1 := h.Register("c1", "alice", nil)
	c2 := h.Register("c2", "bob", nil)
	h.Subscribe("c1", "chat")
	h.Subscribe("c2", "other")

	h.Publish("chat", "hi")
	select {
	case <-c1.Send: // alice 收到
	default:
		t.Fatal("alice should receive")
	}
	select {
	case f := <-c2.Send:
		t.Fatalf("bob should not receive chat: %+v", f)
	default:
	}

	h.PublishToUser("bob", "通知")
	select {
	case f := <-c2.Send:
		if f.Topic != "user:bob" {
			t.Fatalf("user topic = %q", f.Topic)
		}
	default:
		t.Fatal("bob should receive user push")
	}
	select {
	case f := <-c1.Send:
		t.Fatalf("alice should not receive bob's push: %+v", f)
	default:
	}
}

// onClose 回调在连接关闭时触发，携带全部订阅主题。
func TestHubOnClose(t *testing.T) {
	h := NewHub(0)
	closed := make(chan []string, 1)
	c := h.Register("c1", "u1", func(id string, topics []string) {
		closed <- topics
	})
	h.Subscribe("c1", "t1")
	h.Subscribe("c1", "t2")
	c.Close()

	select {
	case topics := <-closed:
		if len(topics) != 2 {
			t.Fatalf("topics = %v", topics)
		}
	case <-time.After(time.Second):
		t.Fatal("onClose not called")
	}
}

// 出站队列满时丢帧不阻塞发布者。
func TestHubSlowConsumer(t *testing.T) {
	h := NewHub(2) // 队列深度 2
	fast := h.Register("fast", "u1", nil)
	slow := h.Register("slow", "u2", nil)
	h.Subscribe("fast", "t")
	h.Subscribe("slow", "t")

	for i := 0; i < 20; i++ {
		h.Publish("t", i) // slow 队列满后应丢弃,不阻塞
	}
	select {
	case <-fast.Send: // fast 至少收到一条
	default:
		t.Fatal("fast starved by slow")
	}
	// Close 幂等
	slow.Close()
	slow.Close()
}
