package im

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// randomToken 生成 n 字节随机数的 hex 串（流票 / 会话号后缀用）。
func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// hub 是会话 → SSE 订阅者的进程内注册表。P0 单实例；多实例时把 publish
// 换成 Deps.Bus（cluster.Bus）广播再本地分发即可，订阅接口不变。
type hub struct {
	mu   sync.RWMutex
	subs map[string]map[chan []byte]struct{}
}

func newHub() *hub { return &hub{subs: map[string]map[chan []byte]struct{}{}} }

// subscribe 注册一个订阅者；返回的取消函数幂等（可多次调用），必须调用（SSE 连接结束时）。
func (h *hub) subscribe(convNo string) (<-chan []byte, func()) {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	if h.subs[convNo] == nil {
		h.subs[convNo] = map[chan []byte]struct{}{}
	}
	h.subs[convNo][ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if set, ok := h.subs[convNo]; ok {
				delete(set, ch)
				if len(set) == 0 {
					delete(h.subs, convNo)
				}
			}
			h.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// publish 向会话的所有订阅者投递；订阅者缓冲满则丢弃该订阅者的这一条
// （客户端靠轮询兜底，不因慢消费者阻塞其它订阅者）。
func (h *hub) publish(convNo string, payload []byte) {
	h.mu.RLock()
	set := h.subs[convNo]
	for ch := range set {
		select {
		case ch <- payload:
		default:
		}
	}
	h.mu.RUnlock()
}

// streamTicket 是 SSE 流的一次性入场票：Bearer 头带不进 EventSource，
// 坐席先认证换一张 30 秒内有效的票，拼在流 URL 上。进程内存储，与 SSE 同生命周期。
type streamTicket struct {
	mu      sync.Mutex
	tickets map[string]time.Time
	ttl     time.Duration
}

func newStreamTicket(ttl time.Duration) *streamTicket {
	return &streamTicket{tickets: map[string]time.Time{}, ttl: ttl}
}

func (s *streamTicket) issue() string {
	tok := randomToken(32)
	s.mu.Lock()
	s.prune()
	s.tickets[tok] = time.Now().Add(s.ttl)
	s.mu.Unlock()
	return tok
}

// take 校验并核销一张票（一次性）。
func (s *streamTicket) take(tok string) bool {
	if tok == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tickets[tok]
	if !ok || time.Now().After(exp) {
		delete(s.tickets, tok)
		return false
	}
	delete(s.tickets, tok)
	return true
}

func (s *streamTicket) prune() {
	now := time.Now()
	for k, exp := range s.tickets {
		if now.After(exp) {
			delete(s.tickets, k)
		}
	}
}
