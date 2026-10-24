package message

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/cluster"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// hub 管理本进程内的 SSE 订阅：按 (租户, 用户) 分组，有变化时 poke 一下，订阅方自己重新查未读数。
// 多实例部署时 poke 先通知本实例的连接，再经 cluster.Bus（core/cache 发布订阅）广播给其它实例，
// 其它实例收到后只通知自己的本地连接（Bus 不会把事件回送给发布者，所以不会重复推）；
// 订阅断线期间丢的事件由 stream 里的定时重算兜底，重连成功后还会立即全员重算一次。
type hub struct {
	mu   sync.Mutex
	subs map[hubKey]map[chan struct{}]struct{}
	bus  atomic.Pointer[cluster.Bus]
}

// pokeEvent 是 cluster.KindMessagePoke 的载荷。
type pokeEvent struct {
	Tenant idgen.ID `json:"tenant"`
	User   idgen.ID `json:"user"`
}

// attach 把 hub 接到跨实例总线上（模块注册时调用一次；bus 为 nil 或非分布式时只剩本地行为）。
func (h *hub) attach(b *cluster.Bus) {
	h.bus.Store(b)
	b.On(cluster.KindMessagePoke, func(raw json.RawMessage) {
		var ev pokeEvent
		if json.Unmarshal(raw, &ev) == nil {
			h.pokeLocal(hubKey{ev.Tenant, ev.User})
		}
	}, h.pokeAll)
}

type hubKey struct{ tenant, user idgen.ID }

var defaultHub = &hub{subs: map[hubKey]map[chan struct{}]struct{}{}}

func keyOf(ctx context.Context, uid idgen.ID) hubKey {
	tid, _ := tenant.From(ctx)
	return hubKey{tid, uid}
}

func (h *hub) subscribe(k hubKey) (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[k] == nil {
		h.subs[k] = map[chan struct{}]struct{}{}
	}
	h.subs[k][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[k], ch)
		if len(h.subs[k]) == 0 {
			delete(h.subs, k)
		}
		h.mu.Unlock()
	}
}

// poke 通知本实例的订阅连接，并广播给其它实例。
func (h *hub) poke(ctx context.Context, uid idgen.ID) {
	k := keyOf(ctx, uid)
	h.pokeLocal(k)
	h.bus.Load().Publish(cluster.KindMessagePoke, pokeEvent{Tenant: k.tenant, User: k.user})
}

// pokeLocal 只通知本实例的连接，不广播。
func (h *hub) pokeLocal(k hubKey) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[k] {
		wake(ch)
	}
}

// pokeAll 通知本实例的全部连接（订阅重连后校正用）。
func (h *hub) pokeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, set := range h.subs {
		for ch := range set {
			wake(ch)
		}
	}
}

func wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default: // 已有待处理的通知，合并
	}
}

const (
	streamRecheck = 15 * time.Second // 定时重算未读数，覆盖公告发布等不经过本模块的变化
	streamPing    = 25 * time.Second // 心跳，防止代理断开空闲连接
)

// Stream GET /messages/stream：SSE，推送未读数变化（event: unread，data 与 /messages/unread-count 相同）。
// 注意 EventSource 不能带 Authorization 头，前端需用 fetch 流式读取。
func (h *Handler) Stream(c *gin.Context) {
	ctx := c.Request.Context()
	uid := auth.ClaimsFrom(c).UserID
	ch, cancel := defaultHub.subscribe(keyOf(ctx, uid))
	defer cancel()

	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	w.Flush()

	var last UnreadCount
	first := true
	push := func() {
		cnt, err := h.svc.Unread(ctx, uid)
		if err != nil || (!first && cnt == last) {
			return
		}
		b, _ := json.Marshal(cnt)
		fmt.Fprintf(w, "event: unread\ndata: %s\n\n", b)
		w.Flush()
		last, first = cnt, false
	}
	push()

	recheck, ping := time.NewTicker(streamRecheck), time.NewTicker(streamPing)
	defer recheck.Stop()
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			push()
		case <-recheck.C:
			push()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			w.Flush()
		}
	}
}
