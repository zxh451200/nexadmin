package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// LeaseEntry 是有独立到期时间的非负字节预占。
type LeaseEntry struct {
	Bytes int64     `json:"bytes"`
	Until time.Time `json:"until"`
}

// Lease 原子更新同一个 key 下的预占集合。update 可能被重试，必须没有写入副作用；
// 返回错误时不提交。过期条目在回调前移除，key TTL 为 ttl。
// 不支持此能力的缓存不能用于受限上传。
type Lease interface {
	UpdateLeases(context.Context, string, time.Duration, func(map[string]LeaseEntry) error) error
}

var errLeaseTTL = fmt.Errorf("cache: lease TTL must be positive")

// ErrLeaseContention 表示乐观并发更新（WATCH/MULTI）被反复打断，超过 maxLeaseTries 次仍没提交。调用方应当把它当作“稍后重试”。
var ErrLeaseContention = errors.New("cache: lease update contended too long, giving up")

const (
	maxLeaseTries    = 20
	leaseBackoffBase = 2 * time.Millisecond
	leaseBackoffMax  = 100 * time.Millisecond
)

// leaseBackoff 返回第 attempt 次（从 0 起）冲突之后的等待：指数增长到上限，再取 [d/2, d) 的随机值打散热点 key 上的竞争者。
func leaseBackoff(attempt int) time.Duration {
	d := leaseBackoffBase
	for i := 0; i < attempt && d < leaseBackoffMax; i++ {
		d *= 2
	}
	if d > leaseBackoffMax {
		d = leaseBackoffMax
	}
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

// leaseSleep 可被测试替换。
var leaseSleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryOptimistic 反复执行 once 直到它不再报告冲突：最多 maxLeaseTries 次，冲突之间指数退避并带抖动，
// ctx 取消立即返回。耗尽次数返回 ErrLeaseContention。
func retryOptimistic(ctx context.Context, once func() error, conflict func(error) bool) error {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := once()
		if !conflict(err) {
			return err
		}
		if attempt+1 >= maxLeaseTries {
			return ErrLeaseContention
		}
		if err := leaseSleep(ctx, leaseBackoff(attempt)); err != nil {
			return err
		}
	}
}

func updateLeaseData(raw string, update func(map[string]LeaseEntry) error) (string, error) {
	entries := map[string]LeaseEntry{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return "", err
		}
	}
	if entries == nil {
		entries = map[string]LeaseEntry{}
	}
	now := time.Now()
	for id, entry := range entries {
		if entry.Bytes < 0 {
			return "", fmt.Errorf("cache: negative lease")
		}
		if !now.Before(entry.Until) {
			delete(entries, id)
		}
	}
	if err := update(entries); err != nil {
		return "", err
	}
	var total int64
	for _, entry := range entries {
		if entry.Bytes < 0 || total > math.MaxInt64-entry.Bytes {
			return "", fmt.Errorf("cache: negative or overflowing lease")
		}
		total += entry.Bytes
	}
	b, err := json.Marshal(entries)
	return string(b), err
}

// 下面这些都是“可选能力”接口，沿用 Counter / AtomicGetDel 的做法：不加进 Cache 接口，
// 避免破坏外部实现者和现有测试里的假缓存；memory 与 redis 两个内置实现都提供。
// 调用方用 As[T] 取能力（能穿透 metrics 之类的装饰器），按业务要求处理能力缺失；
// 需要原子保证的配额操作必须拒绝，不能降级为读改写。

// Locker 是带 TTL 的“占位/租约”原语，用于多实例互斥（node_id 租约等）。
type Locker interface {
	// SetNX 仅当 key 不存在（或已过期）时写入并返回 true；ttl <= 0 表示不过期。
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	// ExpireIfEqual 仅当 key 当前值等于 value 时把剩余存活时间重置为 ttl，返回是否匹配（原子）。
	ExpireIfEqual(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	// DelIfEqual 仅当 key 当前值等于 value 时删除，返回是否删除（原子，防止误删别人的租约）。
	DelIfEqual(ctx context.Context, key, value string) (bool, error)
}

// CompareSwapper replaces only an existing matching value, preserving expiry.
// Missing or expired keys must never be recreated.
//
// 统一语义（memory 与 redis 必须一致）：
//   - key 必须存在、未过期，且当前值等于 old，才替换为 next 并返回 true；否则不改动并返回 false。
//   - 保留原有存活时间：有 TTL 的保持剩余 TTL，没有 TTL 的保持没有 TTL（不会被意外加上或抹掉过期）。
//   - 不会创建不存在或已过期的 key。
type CompareSwapper interface {
	CompareAndSwap(context.Context, string, string, string) (bool, error)
}

// SortedSet 是有序集合：member 唯一，按 score 排序。在线会话索引用它代替“整份读-改-写”。
// 集合与普通 key 共用命名空间，Del 也能删除它。
type SortedSet interface {
	// ZAdd 添加或更新 member 的 score；ttl > 0 时把整个集合的存活时间重置为 ttl，ttl <= 0 不改动现有过期时间。
	ZAdd(ctx context.Context, key, member string, score float64, ttl time.Duration) error
	// ZRem 删除 members，不存在的忽略。
	ZRem(ctx context.Context, key string, members ...string) error
	// ZRange 返回 score >= minScore 的 member，按 score 升序（同分按 member 字典序）。
	ZRange(ctx context.Context, key string, minScore float64) ([]string, error)
	// ZRemRangeByScore 删除 score < maxScore 的 member。
	ZRemRangeByScore(ctx context.Context, key string, maxScore float64) error
}

// PubSub 是发布/订阅。redis 用 Pub/Sub，memory 是进程内广播。消息“至多一次”：订阅断开期间的消息会丢，
// 所以订阅方必须有兜底（定时重算、TTL），并可通过 onReset 在重连成功后做一次全量校正。
type PubSub interface {
	// Publish 向 channel 发布一条消息（包括本进程内的订阅者）。
	Publish(ctx context.Context, channel, msg string) error
	// Subscribe 订阅 channel：onMsg 在单个协程里按序回调（应尽快返回）；
	// 连接断开会自动重连，重连成功后调用 onReset（可为 nil），表示断开期间的消息可能已丢失。
	// 返回的 stop 取消订阅并等待回调协程退出；cache 关闭时所有订阅自动结束。
	// 首次连接失败不是错误（后台继续重试）；只有 cache 已关闭才返回 error。
	Subscribe(channel string, onMsg func(msg string), onReset func()) (stop func(), err error)
}

// Sharer 报告缓存是否被多个进程共享（redis 是，memory 不是）。
// 多实例才有意义的检查（node_id 租约）据此在单实例下跳过。
type Sharer interface{ Shared() bool }

// Unwrapper 由缓存装饰器实现，暴露被包装的缓存，让 As 能找到内层的可选能力。
type Unwrapper interface{ Unwrap() Cache }

// As 在 c 及其被包装的缓存中查找实现了 T 的那一层。
func As[T any](c Cache) (T, bool) {
	for c != nil {
		if v, ok := c.(T); ok {
			return v, true
		}
		u, ok := c.(Unwrapper)
		if !ok {
			break
		}
		c = u.Unwrap()
	}
	var zero T
	return zero, false
}

// IsShared 报告 c 是否被多个进程共享；不实现 Sharer 的缓存按“不共享”处理。
func IsShared(c Cache) bool {
	s, ok := As[Sharer](c)
	return ok && s.Shared()
}
