// Package event 是进程内事件总线：业务模块在关键动作后 Publish 一个事件，订阅者（如 Webhook 投递）异步处理。
//
// Publish 永远不阻塞调用方：事件进入带缓冲的队列，队列满则丢弃并告警（宁可丢事件，不拖慢业务请求）。
// 事件载荷只放 id、必要字段和 tenantId，不得带密码哈希、密钥等敏感字段（见 payload.go）。
package event

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// DefaultQueueSize 是默认总线的队列容量。
const DefaultQueueSize = 1024

// Event 是总线上流转的一条事件。
type Event struct {
	ID        string
	Name      string
	TenantID  idgen.ID
	CreatedAt time.Time
	Data      any
}

// Subscriber 处理事件；Handle 在总线的分发协程里被串行调用，应尽快返回（耗时工作自行异步化）。
type Subscriber interface{ Handle(Event) }

// SubscriberFunc 让普通函数可以当订阅者。
type SubscriberFunc func(Event)

// Handle 实现 Subscriber。
func (f SubscriberFunc) Handle(e Event) { f(e) }

// tenantOwner 由事件载荷实现：ctx 里没有租户（例如超管端跳过租户隔离）时，用它确定事件属于哪个租户。
type tenantOwner interface{ EventTenant() idgen.ID }

// Bus 是一条带缓冲队列的事件总线。
type Bus struct {
	queue   chan Event
	mu      sync.RWMutex
	subs    []Subscriber
	log     atomic.Pointer[zap.Logger]
	dropped atomic.Int64
	warned  atomic.Int64 // 上次告警的 unix 秒
	stop    chan struct{}
	once    sync.Once
}

// NewBus 创建总线并启动分发协程；size <= 0 时用 DefaultQueueSize。
func NewBus(size int) *Bus {
	if size <= 0 {
		size = DefaultQueueSize
	}
	b := &Bus{queue: make(chan Event, size), stop: make(chan struct{})}
	go b.loop()
	return b
}

// SetLogger 设置日志（nil = 不输出）。
func (b *Bus) SetLogger(l *zap.Logger) { b.log.Store(l) }

// Subscribe 注册订阅者。
func (b *Bus) Subscribe(s Subscriber) {
	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.mu.Unlock()
}

// Dropped 返回因队列已满被丢弃的事件总数。
func (b *Bus) Dropped() int64 { return b.dropped.Load() }

// Close 停止分发协程（队列中未处理的事件被丢弃）。
func (b *Bus) Close() { b.once.Do(func() { close(b.stop) }) }

// Publish 非阻塞地发布事件，返回是否入队。队列满时丢弃该事件并（限频）告警。
func (b *Bus) Publish(ctx context.Context, name string, payload any) bool {
	ev := Event{ID: newID(), Name: name, CreatedAt: time.Now().UTC(), Data: payload}
	if id, ok := tenant.From(ctx); ok {
		ev.TenantID = id
	} else if o, ok := payload.(tenantOwner); ok {
		ev.TenantID = o.EventTenant()
	}
	select {
	case b.queue <- ev:
		return true
	default:
		b.dropped.Add(1)
		b.warnDropped(name)
		return false
	}
}

func (b *Bus) warnDropped(name string) {
	l := b.log.Load()
	if l == nil {
		return
	}
	now := time.Now().Unix()
	if last := b.warned.Load(); now-last >= 10 && b.warned.CompareAndSwap(last, now) {
		l.Warn("event: queue full, event dropped", zap.String("event", name), zap.Int64("dropped_total", b.dropped.Load()))
	}
}

func (b *Bus) loop() {
	for {
		select {
		case ev := <-b.queue:
			b.mu.RLock()
			subs := append([]Subscriber(nil), b.subs...)
			b.mu.RUnlock()
			for _, s := range subs {
				b.safeHandle(s, ev)
			}
		case <-b.stop:
			return
		}
	}
}

func (b *Bus) safeHandle(s Subscriber, ev Event) {
	defer func() {
		if r := recover(); r != nil {
			if l := b.log.Load(); l != nil {
				l.Error("event: subscriber panicked", zap.String("event", ev.Name), zap.Any("panic", r))
			}
		}
	}()
	s.Handle(ev)
}

func newID() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return "evt_" + hex.EncodeToString(buf[:])
}

var (
	defOnce sync.Once
	defBus  *Bus
)

// Default 返回进程级默认总线（首次调用时创建）。
func Default() *Bus {
	defOnce.Do(func() { defBus = NewBus(DefaultQueueSize) })
	return defBus
}

// Publish 向默认总线发布事件；非阻塞。
func Publish(ctx context.Context, name string, payload any) { Default().Publish(ctx, name, payload) }

// Subscribe 向默认总线注册订阅者。
func Subscribe(s Subscriber) { Default().Subscribe(s) }
