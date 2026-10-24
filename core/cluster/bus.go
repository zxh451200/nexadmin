// Package cluster 放多实例协同的小工具：基于 core/cache 发布订阅的事件广播（Bus），
// 以及 database.node_id 的租约检查（AcquireNodeLease）。
//
// 单实例（memory 缓存）下两者都退化为进程内行为：广播的接收端会忽略自己发出的事件（本实例的动作由调用方直接本地执行），
// 租约检查直接跳过，所以行为与引入本包之前一致。
package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/cache"
)

var (
	idOnce sync.Once
	instID string
)

// InstanceID 返回本进程的唯一标识（主机名:pid:随机串），进程内不变；用作事件来源和租约持有者。
func InstanceID() string {
	idOnce.Do(func() {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		host, _ := os.Hostname()
		instID = fmt.Sprintf("%s:%d:%s", host, os.Getpid(), hex.EncodeToString(b))
	})
	return instID
}

// Channel 是事件广播使用的 pub/sub 频道（带 nex: 前缀，恢复出厂清 nex:* 时一并处理）。
func Channel() string { return cache.GlobalKey("cluster", "events") }

// 已定义的事件种类。
const (
	KindMessagePoke      = "message.poke"      // 站内消息变化：{"tenant":id,"user":id}
	KindTenantInvalidate = "tenant.invalidate" // 租户信息变更：{"ids":[...],"all":bool}
	KindI18nInvalidate   = "i18n-editor.invalidate"
	KindSecurityRules    = "security.rules" // IP 规则变更：{"tenant":id}，其它实例清规则缓存
)

type envelope struct {
	Origin string          `json:"o"`
	Kind   string          `json:"k"`
	Data   json.RawMessage `json:"d,omitempty"`
}

type debState struct {
	pending bool
	timer   *time.Timer
}

// DefaultDebounce 是默认去抖窗口：失效类事件（i18n 发布、IP 规则变更）1 秒内合并。
const DefaultDebounce = time.Second

type handler struct {
	kind string
	fn   func(json.RawMessage)
	// reset 在订阅重连成功后调用（断开期间的事件可能已丢），可为 nil。
	reset func()
}

const publishQueue = 1024

// Bus 把事件广播给所有实例。Publish 不阻塞调用方（进队列，由后台协程发送）；
// 接收端只处理别的实例发来的事件，本实例的动作由发布方自己本地完成，所以不会重复。
type Bus struct {
	id  string
	log *zap.Logger
	ps  cache.PubSub // nil = 缓存不支持发布订阅，只剩本地行为

	mu       sync.RWMutex
	handlers []handler

	// debounce：这些事件种类在 window 内相同载荷的重复事件被合并（先立即执行一次，窗口结束若期间又来过则补一次）。
	debMu     sync.Mutex
	debounce  map[string]time.Duration
	deb       map[string]*debState
	debClosed bool

	stopSub func()
	queue   chan envelope
	wg      sync.WaitGroup

	closeMu sync.Mutex
	closed  bool
	dropped atomic.Int64
}

// New 创建总线并开始订阅。缓存不支持发布订阅时返回的总线只是空操作（Distributed() == false）。
func New(c cache.Cache, log *zap.Logger) *Bus { return NewWithID(c, log, InstanceID()) }

// NewWithID 同 New，但自己指定来源标识；同一进程里要模拟多个实例（测试）时用，id 必须各不相同。
func NewWithID(c cache.Cache, log *zap.Logger, id string) *Bus {
	if log == nil {
		log = zap.NewNop()
	}
	b := &Bus{id: id, log: log, deb: map[string]*debState{},
		debounce: map[string]time.Duration{KindI18nInvalidate: DefaultDebounce, KindSecurityRules: DefaultDebounce}}
	ps, ok := cache.As[cache.PubSub](c)
	if !ok {
		return b
	}
	b.ps = ps
	b.queue = make(chan envelope, publishQueue)
	stop, err := ps.Subscribe(Channel(), b.dispatch, b.onReset)
	if err != nil {
		log.Warn("cluster: subscribe failed, cross-instance events disabled", zap.Error(err))
		b.ps, b.queue = nil, nil
		return b
	}
	b.stopSub = stop
	b.wg.Add(1)
	go b.publishLoop()
	return b
}

// Distributed 报告事件是否会真正发给其它进程（只有共享缓存才是）。
func (b *Bus) Distributed() bool { return b != nil && b.ps != nil }

// SetDebounce 设置某类事件的去抖窗口（window <= 0 关闭）。语义：同一处理函数、同一载荷的事件，
// 第一次立即处理；窗口内再来的只记一笔，窗口结束时若记过就再处理一次（保证最终看到最新状态），
// 所以一阵爆发最多触发 2 次处理，而且不会丢掉“最后一次变更之后”的刷新。载荷不同的事件互不合并。
// 合并后的补处理在定时器协程里执行，与订阅协程可能并发，处理函数必须线程安全。nil 总线上调用是空操作。
func (b *Bus) SetDebounce(kind string, window time.Duration) {
	if b == nil {
		return
	}
	b.debMu.Lock()
	defer b.debMu.Unlock()
	if window <= 0 {
		delete(b.debounce, kind)
		return
	}
	b.debounce[kind] = window
}

// On 注册某类事件的处理函数；fn 在订阅协程里串行调用，应尽快返回。
// reset 在订阅断线重连成功后调用，用来做一次全量校正（可为 nil）。nil 总线上调用是空操作。
func (b *Bus) On(kind string, fn func(json.RawMessage), reset func()) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.handlers = append(b.handlers, handler{kind: kind, fn: fn, reset: reset})
	b.mu.Unlock()
}

// Publish 把事件广播给其它实例（不含自己）。尽力而为：队列满或发布失败只记日志。nil 总线上调用是空操作。
func (b *Bus) Publish(kind string, payload any) {
	if !b.Distributed() {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		b.log.Warn("cluster: marshal event failed", zap.String("kind", kind), zap.Error(err))
		return
	}
	b.closeMu.Lock()
	defer b.closeMu.Unlock()
	if b.closed {
		return
	}
	select {
	case b.queue <- envelope{Origin: b.id, Kind: kind, Data: raw}:
	default:
		if b.dropped.Add(1) == 1 {
			b.log.Warn("cluster: event queue full, dropping events", zap.String("kind", kind))
		}
	}
}

func (b *Bus) publishLoop() {
	defer b.wg.Done()
	for env := range b.queue {
		msg, err := json.Marshal(env)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := b.ps.Publish(ctx, Channel(), string(msg)); err != nil {
			b.log.Warn("cluster: publish event failed", zap.String("kind", env.Kind), zap.Error(err))
		}
		cancel()
	}
}

func (b *Bus) dispatch(msg string) {
	var env envelope
	if json.Unmarshal([]byte(msg), &env) != nil || env.Origin == b.id {
		return // 坏消息忽略；自己发的不处理（已经在本地做过了），这是防重复的关键
	}
	b.mu.RLock()
	hs := append([]handler(nil), b.handlers...)
	b.mu.RUnlock()
	for i, h := range hs {
		if h.kind != env.Kind {
			continue
		}
		b.debMu.Lock()
		window := b.debounce[env.Kind]
		b.debMu.Unlock()
		if window > 0 {
			b.runDebounced(strconv.Itoa(i)+"|"+env.Kind+"|"+string(env.Data), env.Kind, window, h.fn, env.Data)
			continue
		}
		b.safe(env.Kind, func() { h.fn(env.Data) })
	}
}

func (b *Bus) runDebounced(key, kind string, window time.Duration, fn func(json.RawMessage), data json.RawMessage) {
	b.debMu.Lock()
	if b.debClosed {
		b.debMu.Unlock()
		return
	}
	if st, ok := b.deb[key]; ok { // 窗口内的重复事件:只记一笔
		st.pending = true
		b.debMu.Unlock()
		return
	}
	st := &debState{}
	b.deb[key] = st
	st.timer = time.AfterFunc(window, func() { b.debFire(key, kind, window, fn, data) })
	b.debMu.Unlock()
	b.safe(kind, func() { fn(data) }) // 前沿:立即处理第一次
}

func (b *Bus) debFire(key, kind string, window time.Duration, fn func(json.RawMessage), data json.RawMessage) {
	b.debMu.Lock()
	st, ok := b.deb[key]
	if !ok || b.debClosed {
		b.debMu.Unlock()
		return
	}
	if !st.pending { // 窗口内没有新事件:结束
		delete(b.deb, key)
		b.debMu.Unlock()
		return
	}
	st.pending = false
	st.timer.Reset(window) // 补处理后再开一个窗口,继续限频
	b.debMu.Unlock()
	b.safe(kind, func() { fn(data) })
}

func (b *Bus) onReset() {
	b.log.Warn("cluster: event subscription re-established, resyncing")
	b.mu.RLock()
	hs := append([]handler(nil), b.handlers...)
	b.mu.RUnlock()
	for _, h := range hs {
		if h.reset != nil {
			b.safe(h.kind, h.reset)
		}
	}
}

func (b *Bus) safe(kind string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("cluster: event handler panicked", zap.String("kind", kind), zap.Any("panic", r))
		}
	}()
	f()
}

// Close 取消订阅并把队列里剩余的事件发完（单次最多 2 秒）。可重复调用，nil 安全。
func (b *Bus) Close() {
	if b == nil || b.ps == nil {
		return
	}
	b.closeMu.Lock()
	if b.closed {
		b.closeMu.Unlock()
		return
	}
	b.closed = true
	close(b.queue)
	b.closeMu.Unlock()
	b.debMu.Lock()
	b.debClosed = true
	for k, st := range b.deb {
		st.timer.Stop()
		delete(b.deb, k)
	}
	b.debMu.Unlock()
	b.wg.Wait()
	if b.stopSub != nil {
		b.stopSub()
	}
}
