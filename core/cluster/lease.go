package cluster

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/metrics"
)

const (
	nodeLeaseTTL   = 30 * time.Second
	nodeLeaseRenew = 10 * time.Second
	// leaseFailLimit：连续这么多个续约周期都没续上，就判定租约不健康（ERROR 日志 + node_id_lease_ok=0）。
	// 只告警不退出：进程继续服务，避免因为缓存抖动误杀整个实例。
	leaseFailLimit = 3
)

// leaseMetric 是 /metrics 里的指标名：1 = 租约健康，0 = 连续 leaseFailLimit 个周期续约失败（别的实例可能已用同一 node_id）。
const leaseMetric = "nexadmin_node_id_lease_ok"

var healthyLease atomic.Pointer[Lease] // 本进程当前持有的租约；没有（单实例 / memory 缓存）为 nil

// NodeLeaseOK 报告本进程的 node_id 租约是否健康。没有租约（单实例、不共享缓存、不支持 Locker）时恒为 true。
func NodeLeaseOK() bool {
	l := healthyLease.Load()
	return l == nil || !l.unhealthy.Load()
}

// ErrNodeIDInUse 表示 database.node_id 已被另一个存活实例占用。
var ErrNodeIDInUse = errors.New("cluster: database.node_id is already in use")

// Lease 是 database.node_id 的租约：持有期间别的实例无法用同一个 node_id 启动。
type Lease struct {
	lk    cache.Locker
	key   string
	token string
	ttl   time.Duration
	log   *zap.Logger

	stop chan struct{}
	done chan struct{}
	once sync.Once

	failLimit int          // 连续失败多少个周期判定不健康
	fails     atomic.Int32 // 当前连续失败周期数
	unhealthy atomic.Bool
}

func leaseKey(nodeID int) string { return cache.GlobalKey("node", strconv.Itoa(nodeID)) }

// AcquireNodeLease 用共享缓存给 node_id 做带 TTL 的租约（SET NX，之后定期续约）。
//   - 缓存不共享（memory，单实例）或不支持 Locker：不检查，返回 (nil, nil)；Release 对 nil 安全。
//   - node_id 已被别的存活实例占用：返回包装了 ErrNodeIDInUse 的错误，调用方应拒绝启动。
//   - 续约失败只记日志（Warn / Error），不 panic、不退出。
//
// 租约值是 InstanceID()，同一进程重复申请（如向导完成后重新装配）视为同一持有者。
// 实例崩溃后租约要等 TTL（30 秒）到期，期间用同一 node_id 重启会被拒绝，错误信息里有说明。
func AcquireNodeLease(ctx context.Context, c cache.Cache, nodeID int, log *zap.Logger) (*Lease, error) {
	return acquireLease(ctx, c, nodeID, InstanceID(), nodeLeaseTTL, nodeLeaseRenew, log)
}

func acquireLease(ctx context.Context, c cache.Cache, nodeID int, token string, ttl, renew time.Duration, log *zap.Logger) (*Lease, error) {
	if log == nil {
		log = zap.NewNop()
	}
	if !cache.IsShared(c) {
		return nil, nil
	}
	lk, ok := cache.As[cache.Locker](c)
	if !ok {
		log.Warn("cluster: cache does not support leases, skipping database.node_id duplicate check")
		return nil, nil
	}
	l := &Lease{lk: lk, key: leaseKey(nodeID), token: token, ttl: ttl, log: log, failLimit: leaseFailLimit,
		stop: make(chan struct{}), done: make(chan struct{})}
	got, err := lk.SetNX(ctx, l.key, token, ttl)
	if err != nil {
		return nil, fmt.Errorf("cluster: claim database.node_id %d lease: %w", nodeID, err)
	}
	if !got {
		holder, gerr := c.Get(ctx, l.key)
		switch {
		case gerr == nil && holder == token: // 本进程已持有（重复装配），直接续期
			if ok, err := lk.ExpireIfEqual(ctx, l.key, token, ttl); err != nil || !ok {
				return nil, fmt.Errorf("cluster: renew database.node_id %d lease: %v", nodeID, err)
			}
		case errors.Is(gerr, cache.ErrNotFound): // 刚好过期，再抢一次
			if got, err = lk.SetNX(ctx, l.key, token, ttl); err != nil || !got {
				return nil, fmt.Errorf("%w: node_id %d was just taken by another instance (err=%v)", ErrNodeIDInUse, nodeID, err)
			}
		default:
			who := holder
			if gerr != nil {
				who = "unknown: " + gerr.Error()
			}
			return nil, fmt.Errorf("%w: database.node_id %d is held by another running instance (%s). "+
				"Every instance must use a distinct database.node_id (0-1023), otherwise snowflake IDs collide. "+
				"If that instance has just crashed, its lease expires within %s; retry after that",
				ErrNodeIDInUse, nodeID, who, ttl)
		}
	}
	healthyLease.Store(l)
	registerLeaseMetric()
	go l.loop(renew)
	return l, nil
}

func registerLeaseMetric() {
	metrics.AddCollector("node_lease", func(w *metrics.Writer) {
		w.Family(leaseMetric, "1 if this instance still holds its database.node_id lease, 0 after consecutive renewal failures.", "gauge")
		v := 1.0
		if !NodeLeaseOK() {
			v = 0
		}
		w.Sample(leaseMetric, v)
	})
}

// markRenewal 记录一个续约周期的结果：成功清零；连续失败达到 failLimit 个周期转为不健康并打 ERROR，
// 之后每满一个 failLimit 周期再提醒一次。恢复时打一条 INFO。
func (l *Lease) markRenewal(ok bool, why string) {
	if ok {
		l.fails.Store(0)
		if l.unhealthy.CompareAndSwap(true, false) {
			l.log.Info("cluster: database.node_id lease is healthy again", zap.String("key", l.key))
		}
		return
	}
	n := l.fails.Add(1)
	if int(n) < l.failLimit {
		return
	}
	if l.unhealthy.CompareAndSwap(false, true) || int(n)%l.failLimit == 0 {
		l.log.Error("cluster: database.node_id lease is UNHEALTHY (renewal failed for consecutive cycles); "+
			"another instance may be using the same node_id and snowflake IDs may collide. "+
			"Process keeps running; /metrics reports "+leaseMetric+"=0. Check the shared cache and database.node_id",
			zap.String("key", l.key), zap.Int32("failed_cycles", n), zap.String("reason", why))
	}
}

func (l *Lease) loop(every time.Duration) {
	defer close(l.done)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			l.renew(every)
		}
	}
}

// renew 续约一次。失败只告警：续约丢失意味着别的实例可能抢走 node_id，但这不应该把正在服务的进程搞挂。
func (l *Lease) renew(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ok, err := l.lk.ExpireIfEqual(ctx, l.key, l.token, l.ttl)
	switch {
	case err != nil:
		l.log.Warn("cluster: renewing database.node_id lease failed; if this persists another instance may claim the node_id",
			zap.String("key", l.key), zap.Error(err))
		l.markRenewal(false, err.Error())
	case !ok:
		// 租约不见了（缓存故障期间过期 / 缓存重启 / 被别人占用）：尝试重新占位。
		got, err := l.lk.SetNX(ctx, l.key, l.token, l.ttl)
		switch {
		case err != nil:
			l.log.Warn("cluster: database.node_id lease lost and re-claim failed", zap.String("key", l.key), zap.Error(err))
			l.markRenewal(false, err.Error())
		case got:
			l.log.Warn("cluster: database.node_id lease had expired, re-claimed it", zap.String("key", l.key))
			l.markRenewal(true, "")
		default:
			l.log.Error("cluster: database.node_id lease is now held by ANOTHER instance; duplicate node_id, snowflake IDs may collide. Fix database.node_id and restart",
				zap.String("key", l.key))
			l.markRenewal(false, "held by another instance")
		}
	default:
		l.markRenewal(true, "")
	}
}

// Release 停止续约并释放租约（只删自己持有的）。可重复调用，nil 安全。
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		close(l.stop)
		<-l.done
		healthyLease.CompareAndSwap(l, nil)
		metrics.AddCollector("node_lease", nil)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := l.lk.DelIfEqual(ctx, l.key, l.token); err != nil {
			l.log.Warn("cluster: releasing database.node_id lease failed (it will expire by TTL)", zap.String("key", l.key), zap.Error(err))
		}
	})
}
