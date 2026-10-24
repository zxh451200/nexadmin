package metrics

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/cache"
)

// ---- /metrics 处理器与保护 ----

// Options 是 /metrics 的访问控制。
type Options struct {
	// Token 非空：必须带 "Authorization: Bearer <token>"（常量时间比较），任何来源地址都一样。
	// Token 为空：只允许回环地址，并且带 X-Forwarded-For / X-Real-IP 的请求（多半经过本机反向代理）一律拒绝。
	Token string
}

// Handler 返回受保护的 /metrics 处理器。
func Handler(o Options) gin.HandlerFunc { return Default.handler(o) }

func (r *registry) handler(o Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !Authorized(c.Request, peerHost(c.Request), o) {
			if o.Token != "" {
				c.Header("WWW-Authenticate", `Bearer realm="metrics"`)
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", r.render())
	}
}

// peerHost 返回 TCP 对端地址（RemoteAddr）。回环判定必须用它而不是 c.ClientIP()：
// 配置了 server.trusted_proxies 后 ClientIP 会采信转发头，转发头可以伪造。
func peerHost(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}

// Authorized 判断一个请求能否访问 /metrics（独立出来便于单测）。peerIP 必须是 TCP 对端地址，不是转发头解析出的“真实客户端 IP”。
func Authorized(req *http.Request, peerIP string, o Options) bool {
	if o.Token != "" {
		got, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
		if !ok {
			return false
		}
		a, b := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(o.Token))
		return subtle.ConstantTimeCompare(a[:], b[:]) == 1
	}
	if req.Header.Get("X-Forwarded-For") != "" || req.Header.Get("X-Real-IP") != "" {
		return false
	}
	ip := net.ParseIP(peerIP)
	return ip != nil && ip.IsLoopback()
}

// ---- Go 运行时 ----

// RuntimeCollector 输出 goroutine、内存与 GC 指标。
func RuntimeCollector(w *Writer) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	w.Gauge("go_goroutines", "Number of goroutines.", float64(runtime.NumGoroutine()))
	w.Gauge("go_memstats_alloc_bytes", "Bytes of allocated heap objects.", float64(ms.Alloc))
	w.Gauge("go_memstats_heap_inuse_bytes", "Bytes in in-use heap spans.", float64(ms.HeapInuse))
	w.Gauge("go_memstats_sys_bytes", "Bytes obtained from the system.", float64(ms.Sys))
	w.Counter("go_gc_cycles_total", "Completed GC cycles.", float64(ms.NumGC))
	w.Counter("go_gc_pause_seconds_total", "Cumulative GC stop-the-world pause in seconds.", float64(ms.PauseTotalNs)/1e9)
}

func init() { AddCollector("runtime", RuntimeCollector) }

// ---- 数据库连接池 ----

// SetDBStats 登记连接池统计来源（一般传 sqlDB.Stats）；nil 表示移除。
func SetDBStats(stats func() sql.DBStats) {
	if stats == nil {
		AddCollector("db", nil)
		return
	}
	AddCollector("db", func(w *Writer) {
		s := stats()
		w.Gauge("nexadmin_db_connections_open", "Open connections (in use + idle).", float64(s.OpenConnections))
		w.Gauge("nexadmin_db_connections_in_use", "Connections currently in use.", float64(s.InUse))
		w.Gauge("nexadmin_db_connections_idle", "Idle connections.", float64(s.Idle))
		w.Gauge("nexadmin_db_connections_max_open", "Configured maximum open connections (0 = unlimited).", float64(s.MaxOpenConnections))
		w.Counter("nexadmin_db_wait_total", "Total connections waited for.", float64(s.WaitCount))
		w.Counter("nexadmin_db_wait_seconds_total", "Total time blocked waiting for a connection, in seconds.", s.WaitDuration.Seconds())
	})
}

// ---- 缓存命中 / 错误 ----

var (
	cacheHit, cacheMiss atomic.Uint64
	cacheErr            = map[string]*atomic.Uint64{}
)

var cacheOps = []string{"get", "set", "del", "exists", "incr"}

func init() {
	for _, op := range cacheOps {
		cacheErr[op] = new(atomic.Uint64)
	}
	AddCollector("cache", func(w *Writer) {
		w.Family("nexadmin_cache_requests_total", "Cache reads by result.", "counter")
		w.Sample("nexadmin_cache_requests_total", float64(cacheHit.Load()), "result", "hit")
		w.Sample("nexadmin_cache_requests_total", float64(cacheMiss.Load()), "result", "miss")
		w.Family("nexadmin_cache_errors_total", "Cache operations that failed with an error other than not-found.", "counter")
		for _, op := range cacheOps {
			w.Sample("nexadmin_cache_errors_total", float64(cacheErr[op].Load()), "op", op)
		}
	})
}

// WrapCache 给缓存加上命中 / 错误计数；底层实现了 cache.Counter 时包装后仍然实现它（限流依赖原子计数）。
func WrapCache(c cache.Cache) cache.Cache {
	if c == nil {
		return nil
	}
	m := &meteredCache{Cache: c}
	if ctr, ok := c.(cache.Counter); ok {
		return &meteredCounterCache{meteredCache: m, ctr: ctr}
	}
	return m
}

type meteredCache struct{ cache.Cache }

// Unwrap 让 cache.As 能穿过装饰器找到底层的可选能力（Locker / SortedSet / PubSub 等）；这些操作不计入命中 / 错误指标。
func (m *meteredCache) Unwrap() cache.Cache { return m.Cache }

func countErr(op string, err error) {
	if err != nil {
		cacheErr[op].Add(1)
	}
}

func (m *meteredCache) Get(ctx context.Context, key string) (string, error) {
	v, err := m.Cache.Get(ctx, key)
	switch {
	case err == nil:
		cacheHit.Add(1)
	case isNotFound(err):
		cacheMiss.Add(1)
	default:
		cacheErr["get"].Add(1)
	}
	return v, err
}

func (m *meteredCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	err := m.Cache.Set(ctx, key, value, ttl)
	countErr("set", err)
	return err
}

func (m *meteredCache) Del(ctx context.Context, keys ...string) error {
	err := m.Cache.Del(ctx, keys...)
	countErr("del", err)
	return err
}

func (m *meteredCache) Exists(ctx context.Context, key string) (bool, error) {
	ok, err := m.Cache.Exists(ctx, key)
	countErr("exists", err)
	return ok, err
}

// GetDel 转发给底层的原子实现（不支持时由 cache.GetDel 回落并告警）；嵌入接口不会自动带上可选能力，必须显式转发。
func (m *meteredCache) GetDel(ctx context.Context, key string) (string, error) {
	v, err := cache.GetDel(m.Cache, ctx, key)
	switch {
	case err == nil:
		cacheHit.Add(1)
	case isNotFound(err):
		cacheMiss.Add(1)
	default:
		cacheErr["get"].Add(1)
	}
	return v, err
}

type meteredCounterCache struct {
	*meteredCache
	ctr cache.Counter
}

func (m *meteredCounterCache) Incr(ctx context.Context, key string, ttl time.Duration) (int64, time.Duration, error) {
	n, left, err := m.ctr.Incr(ctx, key, ttl)
	countErr("incr", err)
	return n, left, err
}

func isNotFound(err error) bool { return errors.Is(err, cache.ErrNotFound) }

// ---- 可选数据源：任务队列 / 迁移 / 插件授权 ----

// SetQueueSource 登记异步任务队列长度来源：返回 状态 -> 数量（如 queued / running）。
// core/job 不被本包依赖，由它（或装配代码）在启动时调用本函数；没人调用就不输出该指标。状态数最多取 16 个。
func SetQueueSource(src func() map[string]int64) {
	if src == nil {
		AddCollector("queue", nil)
		return
	}
	AddCollector("queue", func(w *Writer) {
		m := src()
		names := sortedKeys(m)
		if len(names) > 16 {
			names = names[:16]
		}
		w.Family("nexadmin_job_queue_length", "Async jobs by state.", "gauge")
		for _, n := range names {
			w.Sample("nexadmin_job_queue_length", float64(m[n]), "state", n)
		}
	})
}

// PluginState 是一个插件的授权状态。
type PluginState struct {
	ID     string
	Active bool // 授权有效、已挂载
}

// SetPluginSource 登记插件授权来源；每个插件一条 nexadmin_plugin_licensed{plugin="id"}，有效 1 无效 0。最多输出 200 个插件。
func SetPluginSource(src func() []PluginState) {
	if src == nil {
		AddCollector("plugins", nil)
		return
	}
	AddCollector("plugins", func(w *Writer) {
		list := src()
		if len(list) > 200 {
			list = list[:200]
		}
		w.Family("nexadmin_plugin_licensed", "Plugin license state: 1 = active, 0 = not usable.", "gauge")
		for _, p := range list {
			v := 0.0
			if p.Active {
				v = 1
			}
			w.Sample("nexadmin_plugin_licensed", v, "plugin", p.ID)
		}
	})
}

var (
	pendingMu  sync.Mutex
	pendingFn  func() (int, error)
	pendingVal int
	pendingErr error
	pendingAt  time.Time
	pendingTTL = 30 * time.Second
)

// SetMigrationSource 登记“待执行迁移数”的计算函数。结果缓存 30 秒，避免每次抓取都扫库。
func SetMigrationSource(fn func() (int, error)) {
	pendingMu.Lock()
	pendingFn, pendingAt = fn, time.Time{}
	pendingMu.Unlock()
	if fn == nil {
		AddCollector("migrations", nil)
		return
	}
	AddCollector("migrations", func(w *Writer) {
		n, err := PendingMigrations()
		if err != nil {
			w.Gauge("nexadmin_migrations_check_ok", "1 if the pending-migration check succeeded.", 0)
			return
		}
		w.Gauge("nexadmin_migrations_check_ok", "1 if the pending-migration check succeeded.", 1)
		w.Gauge("nexadmin_migrations_pending", "Migration files not yet applied.", float64(n))
	})
}

// PendingMigrations 返回待执行迁移数（带 30 秒缓存）；没有登记数据源时返回 (0, ErrNoSource)。
func PendingMigrations() (int, error) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	if pendingFn == nil {
		return 0, ErrNoSource
	}
	if !pendingAt.IsZero() && time.Since(pendingAt) < pendingTTL {
		return pendingVal, pendingErr
	}
	pendingVal, pendingErr = pendingFn()
	pendingAt = time.Now()
	return pendingVal, pendingErr
}

type noSourceError struct{}

func (noSourceError) Error() string { return "metrics: no source registered" }

// ErrNoSource 表示对应的数据源没有登记。
var ErrNoSource error = noSourceError{}

func sortedKeys(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// 简单插入排序，元素极少
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
