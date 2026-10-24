package cache

import (
	"context"
	"sort"
	"sync"
	"time"
)

// memory 实现的可选能力：Locker / SortedSet / PubSub。数据都在进程内，所以 Shared() 为 false。

// Shared 实现 Sharer：memory 缓存只在本进程可见。
func (m *memoryCache) Shared() bool { return false }

func (m *memoryCache) CompareAndSwap(ctx context.Context, key, old, next string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.live(key, time.Now())
	if !ok || it.val != old {
		return false, nil
	}
	it.val = next
	m.data[key] = it
	return true, nil
}

func (m *memoryCache) UpdateLeases(ctx context.Context, key string, ttl time.Duration, update func(map[string]LeaseEntry) error) error {
	if ttl <= 0 {
		return errLeaseTTL
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	it, _ := m.live(key, time.Now())
	raw, err := updateLeaseData(it.val, update)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.data[key] = memItem{val: raw, expires: time.Now().Add(ttl)}
	return nil
}

// ---- Locker ----

func (m *memoryCache) live(key string, now time.Time) (memItem, bool) {
	it, ok := m.data[key]
	if !ok || (!it.expires.IsZero() && now.After(it.expires)) {
		delete(m.data, key)
		return memItem{}, false
	}
	return it, true
}

func (m *memoryCache) SetNX(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if _, ok := m.live(key, now); ok {
		return false, nil
	}
	it := memItem{val: value}
	if ttl > 0 {
		it.expires = now.Add(ttl)
	}
	m.data[key] = it
	return true, nil
}

func (m *memoryCache) ExpireIfEqual(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	it, ok := m.live(key, now)
	if !ok || it.val != value {
		return false, nil
	}
	it.expires = time.Time{}
	if ttl > 0 {
		it.expires = now.Add(ttl)
	}
	m.data[key] = it
	return true, nil
}

func (m *memoryCache) DelIfEqual(_ context.Context, key, value string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.live(key, time.Now())
	if !ok || it.val != value {
		return false, nil
	}
	delete(m.data, key)
	return true, nil
}

// ---- SortedSet ----

type memZSet struct {
	scores  map[string]float64
	expires time.Time // 零值 = 不过期
}

// liveZ 返回未过期的集合；已过期的顺手删除。调用方持有 m.mu。
func (m *memoryCache) liveZ(key string, now time.Time) *memZSet {
	z, ok := m.zsets[key]
	if !ok {
		return nil
	}
	if !z.expires.IsZero() && now.After(z.expires) {
		delete(m.zsets, key)
		return nil
	}
	return z
}

func (m *memoryCache) ZAdd(_ context.Context, key, member string, score float64, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	z := m.liveZ(key, now)
	if z == nil {
		z = &memZSet{scores: map[string]float64{}}
		m.zsets[key] = z
	}
	z.scores[member] = score
	if ttl > 0 {
		z.expires = now.Add(ttl)
	}
	return nil
}

func (m *memoryCache) ZRem(_ context.Context, key string, members ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	z := m.liveZ(key, time.Now())
	if z == nil {
		return nil
	}
	for _, mem := range members {
		delete(z.scores, mem)
	}
	if len(z.scores) == 0 {
		delete(m.zsets, key) // 与 redis 一致：空集合不存在
	}
	return nil
}

func (m *memoryCache) ZRange(_ context.Context, key string, minScore float64) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	z := m.liveZ(key, time.Now())
	if z == nil {
		return nil, nil
	}
	out := make([]string, 0, len(z.scores))
	for mem, sc := range z.scores {
		if sc >= minScore {
			out = append(out, mem)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := z.scores[out[i]], z.scores[out[j]]
		if a != b {
			return a < b
		}
		return out[i] < out[j]
	})
	return out, nil
}

func (m *memoryCache) ZRemRangeByScore(_ context.Context, key string, maxScore float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	z := m.liveZ(key, time.Now())
	if z == nil {
		return nil
	}
	for mem, sc := range z.scores {
		if sc < maxScore {
			delete(z.scores, mem)
		}
	}
	if len(z.scores) == 0 {
		delete(m.zsets, key)
	}
	return nil
}

// ---- PubSub（进程内） ----

const memSubBuffer = 256

type memSub struct {
	ch   chan string
	done chan struct{}
	exit chan struct{} // 回调协程退出后关闭
	once sync.Once
}

func (s *memSub) stop() { s.once.Do(func() { close(s.done) }) }

func (m *memoryCache) Publish(_ context.Context, channel, msg string) error {
	m.psMu.Lock()
	defer m.psMu.Unlock()
	for s := range m.subs[channel] {
		select {
		case s.ch <- msg:
		default: // 订阅方处理太慢，丢弃（至多一次语义，订阅方有兜底）
		}
	}
	return nil
}

func (m *memoryCache) Subscribe(channel string, onMsg func(string), _ func()) (func(), error) {
	m.psMu.Lock()
	select {
	case <-m.stop:
		m.psMu.Unlock()
		return nil, errClosed
	default:
	}
	s := &memSub{ch: make(chan string, memSubBuffer), done: make(chan struct{}), exit: make(chan struct{})}
	if m.subs[channel] == nil {
		m.subs[channel] = map[*memSub]struct{}{}
	}
	m.subs[channel][s] = struct{}{}
	m.psMu.Unlock()

	go func() {
		defer close(s.exit)
		for {
			select {
			case msg := <-s.ch:
				onMsg(msg)
			case <-s.done:
				return
			case <-m.stop:
				return
			}
		}
	}()
	return func() {
		m.psMu.Lock()
		delete(m.subs[channel], s)
		if len(m.subs[channel]) == 0 {
			delete(m.subs, channel)
		}
		m.psMu.Unlock()
		s.stop()
		<-s.exit
	}, nil
}
