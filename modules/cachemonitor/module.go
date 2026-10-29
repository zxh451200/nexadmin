// Package cachemonitor 是缓存监控。redis/valkey 驱动读 INFO 与 SCAN；
// memory 驱动（core/cache 的内存实现）没有对外统计接口，只能返回进程内存和运行时长，key 数与命中率为 0。
package cachemonitor

import (
	"context"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/cache"
	"nexadmin/core/response"
	"nexadmin/modules/internal/monitorsnap"
)

func init() { core.RegisterBuiltin(module{}) }

// PermList 与前端 route.tsx 一致。
const PermList = "cacheMonitor:list"

const (
	scanCap      = 20000            // 前缀统计最多扫描的 key 数，防止大库拖慢请求
	prefixTTL    = 30 * time.Second // 前缀统计的缓存时间
	infoTimeout  = 3 * time.Second
	topCommands  = 6
	prefixSample = 50 // 前缀列表最多展示的前缀数
)

var startedAt = time.Now()

type module struct{}

func (module) Name() string { return "cache-monitor" }

func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	m := &monitor{driver: d.Config.Cache.Driver}
	if m.driver == "redis" {
		// 监控用独立连接，不碰 core/cache 的客户端
		m.cli = redis.NewClient(&redis.Options{Addr: d.Config.Cache.Addr, Password: d.Config.Cache.Password})
		m.log = d.Log
	}
	store := monitorsnap.NewStore(d.DB)
	stop := make(chan struct{})
	if d.OnClose != nil {
		d.OnClose(func() { close(stop) })
	}
	go monitorsnap.Loop(stop, store, monitorsnap.KindCache, m.metrics, d.Log)
	r.GET("/monitor/cache", core.Perm(PermList), func(c *gin.Context) {
		response.OK(c, m.snapshot(c.Request.Context()))
	})
	r.GET("/monitor/cache/history", core.Perm(PermList), monitorsnap.HandleHistory(store, monitorsnap.KindCache))
}

// metrics 是给历史落库用的扁平采样：memory 驱动没有键数与命中率，只落进程内存，
// 不去伪造 0；valkey 驱动才补 keys/hitRate/qps/connections。
func (m *monitor) metrics() monitorsnap.Metrics {
	snap := m.snapshot(context.Background())
	m1 := monitorsnap.Metrics{"usedMemoryMb": snap.UsedMemoryMB}
	if snap.Driver != "memory" {
		m1["keys"] = float64(snap.Keys)
		m1["hitRate"] = snap.HitRate
		m1["qps"] = snap.QPS
		m1["connections"] = float64(snap.Connections)
	}
	return m1
}

// 以下结构与前端 CacheSnapshot 一致。

type Snapshot struct {
	TS           int64     `json:"ts"`
	Driver       string    `json:"driver"` // memory | valkey
	Version      string    `json:"version"`
	UptimeSec    int64     `json:"uptimeSec"`
	UsedMemoryMB float64   `json:"usedMemoryMb"`
	MaxMemoryMB  float64   `json:"maxMemoryMb"`
	Keys         int64     `json:"keys"`
	HitRate      float64   `json:"hitRate"`
	QPS          float64   `json:"qps"`
	Connections  int64     `json:"connections"`
	Commands     []Command `json:"commands"`
	Prefixes     []Prefix  `json:"prefixes"`
}

type Command struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type Prefix struct {
	Prefix string `json:"prefix"`
	Keys   int64  `json:"keys"`
	TTL    string `json:"ttl"`
	Desc   string `json:"desc"`
}

type monitor struct {
	driver string
	cli    *redis.Client
	log    *zap.Logger

	mu       sync.Mutex
	prefixes []Prefix
	prefixAt time.Time
}

func (m *monitor) snapshot(ctx context.Context) Snapshot {
	snap := Snapshot{TS: time.Now().UnixMilli(), Driver: "memory", Commands: []Command{}, Prefixes: []Prefix{}}
	if m.cli == nil {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		snap.Version = "memory"
		snap.UptimeSec = int64(time.Since(startedAt).Seconds())
		snap.UsedMemoryMB = float64(ms.HeapAlloc) / 1024 / 1024
		return snap
	}
	snap.Driver = "valkey"
	ctx, cancel := context.WithTimeout(ctx, infoTimeout)
	defer cancel()
	raw, err := m.cli.Info(ctx).Result()
	if err != nil {
		if m.log != nil {
			m.log.Warn("cache monitor: INFO failed", zap.Error(err))
		}
		snap.Version = "unavailable"
		return snap
	}
	info := parseInfo(raw)
	switch {
	case info["valkey_version"] != "":
		snap.Version = "Valkey " + info["valkey_version"]
	case info["redis_version"] != "":
		snap.Version = "Redis " + info["redis_version"]
	}
	snap.UptimeSec = atoi(info["uptime_in_seconds"])
	snap.UsedMemoryMB = float64(atoi(info["used_memory"])) / 1024 / 1024
	maxMem := atoi(info["maxmemory"])
	if maxMem == 0 { // 未设上限时用机器总内存做分母
		maxMem = atoi(info["total_system_memory"])
	}
	snap.MaxMemoryMB = float64(maxMem) / 1024 / 1024
	if n, err := m.cli.DBSize(ctx).Result(); err == nil {
		snap.Keys = n
	}
	hits, misses := atoi(info["keyspace_hits"]), atoi(info["keyspace_misses"])
	if hits+misses > 0 {
		snap.HitRate = float64(int64(float64(hits)/float64(hits+misses)*10000)) / 100
	}
	snap.QPS = float64(atoi(info["instantaneous_ops_per_sec"]))
	snap.Connections = atoi(info["connected_clients"])
	snap.Commands = topCommandStats(info)
	snap.Prefixes = m.prefixStats(ctx)
	return snap
}

func parseInfo(raw string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(raw, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if k, v, ok := strings.Cut(l, ":"); ok {
			out[k] = v
		}
	}
	return out
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// topCommandStats 取 cmdstat_xxx:calls=N,... 里调用次数最多的几个命令。
func topCommandStats(info map[string]string) []Command {
	var cmds []Command
	for k, v := range info {
		name, ok := strings.CutPrefix(k, "cmdstat_")
		if !ok {
			continue
		}
		for _, kv := range strings.Split(v, ",") {
			if n, ok := strings.CutPrefix(kv, "calls="); ok {
				cmds = append(cmds, Command{Name: strings.ToUpper(name), Count: atoi(n)})
			}
		}
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Count > cmds[j].Count })
	if len(cmds) > topCommands {
		cmds = cmds[:topCommands]
	}
	if cmds == nil {
		cmds = []Command{}
	}
	return cmds
}

var tenantSeg = regexp.MustCompile(`^t\d+$`)

// prefixOf 把 nex:t123:session:abc 归为 nex:session:*（去掉租户段，取下一段作为前缀名）。
func prefixOf(key string) (prefix, name string) {
	parts := strings.Split(strings.TrimPrefix(key, cache.Prefix), ":")
	if len(parts) > 1 && tenantSeg.MatchString(parts[0]) {
		parts = parts[1:]
	}
	name = parts[0]
	return cache.Prefix + name + ":*", name
}

// prefixStats 扫描 nex:* 按前缀分组计数，并抽样一个 key 的 TTL。结果缓存 30 秒。
func (m *monitor) prefixStats(ctx context.Context) []Prefix {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.prefixes != nil && time.Since(m.prefixAt) < prefixTTL {
		return m.prefixes
	}
	counts := map[string]int64{}
	sample := map[string]string{}
	names := map[string]string{}
	var cursor uint64
	scanned := 0
	for scanned < scanCap {
		keys, next, err := m.cli.Scan(ctx, cursor, cache.Prefix+"*", 1000).Result()
		if err != nil {
			break
		}
		for _, k := range keys {
			p, n := prefixOf(k)
			counts[p]++
			names[p] = n
			if _, ok := sample[p]; !ok {
				sample[p] = k
			}
		}
		scanned += len(keys)
		if cursor = next; cursor == 0 {
			break
		}
	}
	out := make([]Prefix, 0, len(counts))
	for p, n := range counts {
		out = append(out, Prefix{Prefix: p, Keys: n, TTL: m.ttlOf(ctx, sample[p]), Desc: names[p]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Keys > out[j].Keys })
	if len(out) > prefixSample {
		out = out[:prefixSample]
	}
	m.prefixes, m.prefixAt = out, time.Now()
	return out
}

// ttlOf 把 key 的剩余时间格式化成 2h / 5m / 30s；永不过期为 "-"。
func (m *monitor) ttlOf(ctx context.Context, key string) string {
	d, err := m.cli.TTL(ctx, key).Result()
	if err != nil || d < 0 {
		return "-"
	}
	switch {
	case d >= time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d >= time.Minute:
		return strconv.Itoa(int(d.Minutes())) + "m"
	}
	return strconv.Itoa(int(d.Seconds())) + "s"
}
