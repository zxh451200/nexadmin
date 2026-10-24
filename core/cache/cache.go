// Package cache 定义一个接口，提供 memory 和 redis（兼容 Valkey）两个实现，由配置切换。
// 在线用户、验证码、令牌黑名单都走这个接口。
package cache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"nexadmin/core/config"
	"nexadmin/core/tenant"
)

// ErrNotFound 表示 key 不存在或已过期。
var ErrNotFound = errors.New("cache: key not found")

var errClosed = errors.New("cache: closed")

// Prefix 是本系统所有 key 的前缀（恢复出厂时只删 nex:*，不 FLUSHALL）。
const Prefix = "nex:"

// Cache 是缓存接口。ttl <= 0 表示不过期。
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, key string) (bool, error)
	Close() error
}

// Counter 是可选的原子计数能力（memory 与 redis 实现都提供），限流用它做固定窗口计数。
// Incr 把 key 加 1 并返回新值与剩余存活时间；key 不存在时按 ttl 新建（窗口从第一次计数开始）。
// 计数以十进制字符串存放，所以 Get 可以直接读出当前值。
type Counter interface {
	Incr(ctx context.Context, key string, ttl time.Duration) (n int64, remaining time.Duration, err error)
}

// AtomicGetDel 是可选的原子“取出并删除”能力（memory 与 redis 实现都提供），用于一次性凭据（授权码、验证码、state）
// 的消费：多实例下两个并发请求中只有一个能取到值。不直接加进 Cache 接口，避免破坏外部实现者；
// 调用方一律用 GetDel 辅助函数，它在底层不支持时回落为 Get+Del（非原子，并记一次 warn）。
type AtomicGetDel interface {
	GetDel(ctx context.Context, key string) (string, error)
}

var getDelFallbackWarn sync.Once

// GetDel 取出 key 的值并删除它；key 不存在或已过期返回 ErrNotFound。
// c 实现了 AtomicGetDel 时是原子的；否则回落为 Get 再 Del，两步之间存在并发窗口，进程内首次回落时记一条 warn。
func GetDel(c Cache, ctx context.Context, key string) (string, error) {
	if a, ok := c.(AtomicGetDel); ok {
		return a.GetDel(ctx, key)
	}
	getDelFallbackWarn.Do(func() {
		zap.L().Warn("cache: backend does not implement AtomicGetDel, falling back to non-atomic Get+Del; one-time credentials may be consumed twice across instances")
	})
	v, err := c.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if err := c.Del(ctx, key); err != nil {
		return "", err
	}
	return v, nil
}

// Key 生成带租户前缀的 key：nex:t<租户ID>:a:b；context 里没有租户时为 nex:a:b。
func Key(ctx context.Context, parts ...string) string {
	if id, ok := tenant.From(ctx); ok {
		return Prefix + "t" + strconv.FormatInt(int64(id), 10) + ":" + strings.Join(parts, ":")
	}
	return GlobalKey(parts...)
}

// GlobalKey 生成不带租户的平台级 key：nex:a:b。
func GlobalKey(parts ...string) string { return Prefix + strings.Join(parts, ":") }

// New 按配置创建缓存；所需实现不可用时返回指名原因的错误，不 panic。
func New(cfg config.Cache) (Cache, error) {
	switch cfg.Driver {
	case "memory", "":
		return newMemory(), nil
	case "redis":
		cli := redis.NewClient(&redis.Options{Addr: cfg.Addr, Password: cfg.Password})
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := cli.Ping(ctx).Err(); err != nil {
			_ = cli.Close()
			return nil, fmt.Errorf("cache: redis/valkey at %s is unavailable: %w", cfg.Addr, err)
		}
		return newRedisCache(cli), nil
	default:
		return nil, fmt.Errorf("cache: unknown driver %q (want memory or redis)", cfg.Driver)
	}
}

// ---- memory ----

type memItem struct {
	val     string
	expires time.Time // 零值 = 不过期
}

type memoryCache struct {
	mu    sync.Mutex
	data  map[string]memItem
	zsets map[string]*memZSet // 有序集合，见 ext_memory.go
	stop  chan struct{}
	once  sync.Once

	psMu sync.Mutex
	subs map[string]map[*memSub]struct{} // 进程内发布订阅
}

func newMemory() *memoryCache {
	m := &memoryCache{data: map[string]memItem{}, zsets: map[string]*memZSet{},
		subs: map[string]map[*memSub]struct{}{}, stop: make(chan struct{})}
	go m.janitor()
	return m
}

func (m *memoryCache) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			now := time.Now()
			m.mu.Lock()
			for k, it := range m.data {
				if !it.expires.IsZero() && now.After(it.expires) {
					delete(m.data, k)
				}
			}
			for k, z := range m.zsets {
				if !z.expires.IsZero() && now.After(z.expires) {
					delete(m.zsets, k)
				}
			}
			m.mu.Unlock()
		case <-m.stop:
			return
		}
	}
}

func (m *memoryCache) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.data[key]
	if !ok || (!it.expires.IsZero() && time.Now().After(it.expires)) {
		delete(m.data, key)
		return "", ErrNotFound
	}
	return it.val, nil
}

// GetDel 在同一把锁内取出并删除，并发调用只有一个能拿到值。
func (m *memoryCache) GetDel(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.data[key]
	delete(m.data, key)
	if !ok || (!it.expires.IsZero() && time.Now().After(it.expires)) {
		return "", ErrNotFound
	}
	return it.val, nil
}

func (m *memoryCache) Set(_ context.Context, key, value string, ttl time.Duration) error {
	it := memItem{val: value}
	if ttl > 0 {
		it.expires = time.Now().Add(ttl)
	}
	m.mu.Lock()
	m.data[key] = it
	m.mu.Unlock()
	return nil
}

func (m *memoryCache) Incr(_ context.Context, key string, ttl time.Duration) (int64, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	it, ok := m.data[key]
	if !ok || (!it.expires.IsZero() && now.After(it.expires)) {
		it = memItem{}
		if ttl > 0 {
			it.expires = now.Add(ttl)
		}
	}
	n, _ := strconv.ParseInt(it.val, 10, 64)
	n++
	it.val = strconv.FormatInt(n, 10)
	m.data[key] = it
	var left time.Duration
	if !it.expires.IsZero() {
		left = it.expires.Sub(now)
	}
	return n, left, nil
}

func (m *memoryCache) Del(_ context.Context, keys ...string) error {
	m.mu.Lock()
	for _, k := range keys {
		delete(m.data, k)
		delete(m.zsets, k)
	}
	m.mu.Unlock()
	return nil
}

func (m *memoryCache) Exists(ctx context.Context, key string) (bool, error) {
	_, err := m.Get(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (m *memoryCache) Close() error {
	m.once.Do(func() { close(m.stop) })
	return nil
}

// ---- redis / valkey ----

type redisCache struct {
	cli    *redis.Client
	ctx    context.Context // Close 时取消，结束所有订阅协程
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newRedisCache(cli *redis.Client) *redisCache {
	ctx, cancel := context.WithCancel(context.Background())
	return &redisCache{cli: cli, ctx: ctx, cancel: cancel}
}

func (r *redisCache) Get(ctx context.Context, key string) (string, error) {
	v, err := r.cli.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return v, err
}

func (r *redisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl < 0 {
		ttl = 0
	}
	return r.cli.Set(ctx, key, value, ttl).Err()
}

// incrScript 原子地自增并保证带 TTL（避免进程在 INCR 与 EXPIRE 之间退出留下永不过期的计数），返回 {值, 剩余毫秒}。
var incrScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
local t = redis.call('PTTL', KEYS[1])
if t < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  t = tonumber(ARGV[1])
end
return {n, t}
`)

func (r *redisCache) Incr(ctx context.Context, key string, ttl time.Duration) (int64, time.Duration, error) {
	if ttl <= 0 {
		ttl = time.Minute
	}
	res, err := incrScript.Run(ctx, r.cli, []string{key}, ttl.Milliseconds()).Slice()
	if err != nil {
		return 0, 0, err
	}
	if len(res) != 2 {
		return 0, 0, fmt.Errorf("cache: unexpected incr reply %v", res)
	}
	n, ok1 := res[0].(int64)
	ms, ok2 := res[1].(int64)
	if !ok1 || !ok2 {
		return 0, 0, fmt.Errorf("cache: unexpected incr reply types %T %T", res[0], res[1])
	}
	return n, time.Duration(ms) * time.Millisecond, nil
}

// getDelScript 在服务端原子地取出并删除（等价于 GETDEL，但不要求 Redis >= 6.2）；key 不存在时返回 nil。
var getDelScript = redis.NewScript(`
local v = redis.call('GET', KEYS[1])
if v then redis.call('DEL', KEYS[1]) end
return v
`)

func (r *redisCache) GetDel(ctx context.Context, key string) (string, error) {
	v, err := getDelScript.Run(ctx, r.cli, []string{key}).Text()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return v, err
}

func (r *redisCache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return r.cli.Del(ctx, keys...).Err()
}

func (r *redisCache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.cli.Exists(ctx, key).Result()
	return n > 0, err
}

func (r *redisCache) Close() error {
	r.cancel()
	r.wg.Wait()
	return r.cli.Close()
}
