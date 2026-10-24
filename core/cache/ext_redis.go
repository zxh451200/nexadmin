package cache

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// redis / valkey 实现的可选能力：Locker / SortedSet / PubSub。

// Shared 实现 Sharer：redis 被所有实例共享。
func (r *redisCache) Shared() bool { return true }

// compareAndSwapScript 实现 CompareSwapper 的统一语义（见 ext.go）：
//   - GET 不等于 old（含 key 不存在：GET 返回 false，false ~= 字符串 恒为真）-> 0
//   - PTTL == -2：判断后恰好过期 / 被删 -> 0（绝不重建）；PTTL == 0：不足 1ms 即将过期，当作已过期 -> 0
//   - PTTL == -1：key 没有 TTL -> SET 不带过期，保持“无 TTL”（旧实现在这里返回 0，与 memory 不一致）
//   - PTTL > 0：SET ... PX 剩余毫秒，保持原有剩余 TTL
var compareAndSwapScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
local ttl = redis.call('PTTL', KEYS[1])
if ttl == -2 or ttl == 0 then return 0 end
if ttl == -1 then
  redis.call('SET', KEYS[1], ARGV[2])
else
  redis.call('SET', KEYS[1], ARGV[2], 'PX', ttl)
end
return 1
`)

func (r *redisCache) CompareAndSwap(ctx context.Context, key, old, next string) (bool, error) {
	n, err := compareAndSwapScript.Run(ctx, r.cli, []string{key}, old, next).Int64()
	return n == 1, err
}

func (r *redisCache) UpdateLeases(ctx context.Context, key string, ttl time.Duration, update func(map[string]LeaseEntry) error) error {
	if ttl <= 0 {
		return errLeaseTTL
	}
	// 乐观事务被并发写打断时重试：最多 maxLeaseTries 次、指数退避带抖动（见 retryOptimistic），热点 key 上不会空转。
	return retryOptimistic(ctx, func() error {
		return r.cli.Watch(ctx, func(tx *redis.Tx) error {
			raw, err := tx.Get(ctx, key).Result()
			if err != nil && !errors.Is(err, redis.Nil) {
				return err
			}
			raw, err = updateLeaseData(raw, update)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, raw, ttl)
				return nil
			})
			return err
		}, key)
	}, func(err error) bool { return errors.Is(err, redis.TxFailedErr) })
}

// ---- Locker ----

func (r *redisCache) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if ttl < 0 {
		ttl = 0
	}
	return r.cli.SetNX(ctx, key, value, ttl).Result()
}

// 服务端原子的“值相等才续期 / 才删除”，不要求 Redis 的任何新命令。
var (
	expireIfEqualScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  if tonumber(ARGV[2]) > 0 then
    return redis.call('PEXPIRE', KEYS[1], ARGV[2])
  end
  return redis.call('PERSIST', KEYS[1]) + 1
end
return 0
`)
	delIfEqualScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)
)

func (r *redisCache) ExpireIfEqual(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	ms := ttl.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	n, err := expireIfEqualScript.Run(ctx, r.cli, []string{key}, value, ms).Int64()
	return n > 0, err
}

func (r *redisCache) DelIfEqual(ctx context.Context, key, value string) (bool, error) {
	n, err := delIfEqualScript.Run(ctx, r.cli, []string{key}, value).Int64()
	return n > 0, err
}

// ---- SortedSet ----

func (r *redisCache) ZAdd(ctx context.Context, key, member string, score float64, ttl time.Duration) error {
	if ttl <= 0 {
		return r.cli.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
	}
	pipe := r.cli.TxPipeline() // MULTI/EXEC：加成员与续期一起生效
	pipe.ZAdd(ctx, key, redis.Z{Score: score, Member: member})
	pipe.PExpire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *redisCache) ZRem(ctx context.Context, key string, members ...string) error {
	if len(members) == 0 {
		return nil
	}
	args := make([]any, len(members))
	for i, m := range members {
		args[i] = m
	}
	return r.cli.ZRem(ctx, key, args...).Err()
}

func (r *redisCache) ZRange(ctx context.Context, key string, minScore float64) ([]string, error) {
	return r.cli.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: strconv.FormatFloat(minScore, 'f', -1, 64), Max: "+inf",
	}).Result()
}

func (r *redisCache) ZRemRangeByScore(ctx context.Context, key string, maxScore float64) error {
	return r.cli.ZRemRangeByScore(ctx, key, "-inf", "("+strconv.FormatFloat(maxScore, 'f', -1, 64)).Err()
}

// ---- PubSub ----

func (r *redisCache) Publish(ctx context.Context, channel, msg string) error {
	return r.cli.Publish(ctx, channel, msg).Err()
}

const (
	subReadTimeout = 30 * time.Second // 超过这么久没收到任何东西就发一个 PING 探活，防止半开连接
	subBackoffMin  = 200 * time.Millisecond
	subBackoffMax  = 10 * time.Second
	subFirstWait   = 3 * time.Second
)

// Subscribe 订阅 channel；断线后按指数退避自动重连（重新 SUBSCRIBE），成功后调用 onReset。
func (r *redisCache) Subscribe(channel string, onMsg func(string), onReset func()) (func(), error) {
	if r.ctx.Err() != nil {
		return nil, errClosed
	}
	ctx, cancel := context.WithCancel(r.ctx)
	ready := make(chan struct{})
	var readyOnce sync.Once
	signal := func() { readyOnce.Do(func() { close(ready) }) }
	done := make(chan struct{})

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer close(done)
		r.subscribeLoop(ctx, channel, onMsg, onReset, signal)
	}()

	// 等第一次连接结果（成功或失败）再返回：成功时后续 Publish 一定能被收到。
	select {
	case <-ready:
	case <-time.After(subFirstWait):
	}
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done }) }, nil
}

func (r *redisCache) subscribeLoop(ctx context.Context, channel string, onMsg func(string), onReset func(), signal func()) {
	defer signal()
	backoff := subBackoffMin
	dropped := false // 之前是否有过没连上 / 断开
	sleep := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > subBackoffMax {
			backoff = subBackoffMax
		}
		return true
	}
	for ctx.Err() == nil {
		ps := r.cli.Subscribe(ctx, channel)
		cctx, ccancel := context.WithTimeout(ctx, subFirstWait)
		_, err := ps.Receive(cctx) // 等订阅确认
		ccancel()
		if err != nil {
			_ = ps.Close()
			dropped = true
			signal()
			if ctx.Err() != nil {
				return
			}
			zap.L().Warn("cache: pubsub subscribe failed, will retry", zap.String("channel", channel), zap.Error(err))
			if !sleep() {
				return
			}
			continue
		}
		stopWatch := context.AfterFunc(ctx, func() { _ = ps.Close() }) // 退出时让阻塞中的读立刻返回
		backoff = subBackoffMin
		if dropped && onReset != nil {
			onReset()
		}
		signal()
		for {
			msg, err := ps.ReceiveTimeout(ctx, subReadTimeout)
			if err == nil {
				if m, ok := msg.(*redis.Message); ok {
					onMsg(m.Payload)
				}
				continue
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() && ctx.Err() == nil {
				if perr := ps.Ping(ctx); perr == nil {
					continue
				}
			}
			break
		}
		stopWatch()
		_ = ps.Close()
		if ctx.Err() != nil {
			return
		}
		dropped = true
		zap.L().Warn("cache: pubsub connection lost, reconnecting", zap.String("channel", channel))
		if !sleep() {
			return
		}
	}
}
