// Package servermonitor 是服务器监控：CPU、内存、磁盘、网络、Go 运行时。
// 不依赖 gopsutil（改动 go.mod 会影响并行开发的其他包），Linux 下直接读 /proc，其他系统只返回运行时信息。
package servermonitor

import (
	"net"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/metrics"
	"nexadmin/core/plugin"
	"nexadmin/core/response"
)

func init() { core.RegisterBuiltin(module{}) }

// PermList 与前端 route.tsx 一致。
const PermList = "serverMonitor:list"

var startedAt = time.Now()

type module struct{}

func (module) Name() string { return "server-monitor" }

func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	s := &sampler{}
	r.GET("/monitor/server", core.Perm(PermList), func(c *gin.Context) { response.OK(c, s.snapshot()) })
	h := &healthRunner{d: d}
	r.GET("/monitor/health", core.Perm(PermList), func(c *gin.Context) { response.OK(c, h.run(c.Request.Context())) })
	metrics.SetPluginSource(func() []metrics.PluginState {
		list := plugin.Default().List()
		out := make([]metrics.PluginState, 0, len(list))
		for _, p := range list {
			out = append(out, metrics.PluginState{ID: p.ID, Active: p.Status == plugin.StatusActive})
		}
		return out
	})
}

// 以下结构与前端 ServerSnapshot 一致。

type Snapshot struct {
	TS      int64        `json:"ts"`
	CPU     CPU          `json:"cpu"`
	Memory  Memory       `json:"memory"`
	Disks   []Disk       `json:"disks"`
	Network Network      `json:"network"`
	Runtime RuntimeStats `json:"runtime"`
	Info    Info         `json:"info"`
}

type CPU struct {
	Usage  float64 `json:"usage"`
	Cores  int     `json:"cores"`
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

type Memory struct {
	TotalGB float64 `json:"totalGb"`
	UsedGB  float64 `json:"usedGb"`
}

type Disk struct {
	Mount   string  `json:"mount"`
	TotalGB float64 `json:"totalGb"`
	UsedGB  float64 `json:"usedGb"`
}

type Network struct {
	InKbps  float64 `json:"inKbps"`
	OutKbps float64 `json:"outKbps"`
}

type RuntimeStats struct {
	Goroutines int     `json:"goroutines"`
	HeapMB     float64 `json:"heapMb"`
	GCCount    uint32  `json:"gcCount"`
	GCPauseMs  float64 `json:"gcPauseMs"`
}

type Info struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"goVersion"`
	Hostname  string `json:"hostname"`
	IP        string `json:"ip"`
	StartedAt string `json:"startedAt"`
	UptimeSec int64  `json:"uptimeSec"`
}

// rawSample 是一次原始采样；CPU 与网络是累计值，需要两次采样做差。
type rawSample struct {
	at                time.Time
	cpuBusy, cpuTotal uint64
	netIn, netOut     uint64
	hasCPU, hasNet    bool
}

// sampler 保存上一次采样，请求之间做差得到使用率和速率。
type sampler struct {
	mu   sync.Mutex
	last *rawSample
}

func (s *sampler) snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := readRaw()
	// 没有上一次采样，或上一次太旧（页面刚打开）：等 200ms 再采一次，保证第一次就有值
	if s.last == nil || time.Since(s.last.at) > 30*time.Second {
		s.last = &cur
		time.Sleep(200 * time.Millisecond)
		cur = readRaw()
	}
	prev := *s.last
	s.last = &cur

	snap := Snapshot{TS: time.Now().UnixMilli(), Disks: readDisks()}
	snap.CPU = CPU{Cores: runtime.NumCPU()}
	snap.CPU.Load1, snap.CPU.Load5, snap.CPU.Load15 = readLoad()
	if cur.hasCPU && prev.hasCPU && cur.cpuTotal > prev.cpuTotal {
		snap.CPU.Usage = round2(float64(cur.cpuBusy-prev.cpuBusy) / float64(cur.cpuTotal-prev.cpuTotal) * 100)
	}
	if dt := cur.at.Sub(prev.at).Seconds(); dt > 0 && cur.hasNet && prev.hasNet {
		// 字节/秒 -> kbit/s
		snap.Network.InKbps = round2(float64(sub(cur.netIn, prev.netIn)) * 8 / 1000 / dt)
		snap.Network.OutKbps = round2(float64(sub(cur.netOut, prev.netOut)) * 8 / 1000 / dt)
	}
	total, used := readMemory()
	snap.Memory = Memory{TotalGB: round2(float64(total) / gb), UsedGB: round2(float64(used) / gb)}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	var pause float64
	if ms.NumGC > 0 {
		pause = float64(ms.PauseNs[(ms.NumGC+255)%256]) / 1e6
	}
	snap.Runtime = RuntimeStats{
		Goroutines: runtime.NumGoroutine(), HeapMB: round2(float64(ms.HeapAlloc) / mb), GCCount: ms.NumGC, GCPauseMs: round2(pause),
	}
	host, _ := os.Hostname()
	snap.Info = Info{
		OS: osName(), Arch: runtime.GOARCH, GoVersion: runtime.Version(), Hostname: host, IP: localIP(),
		StartedAt: startedAt.UTC().Format(time.RFC3339), UptimeSec: int64(time.Since(startedAt).Seconds()),
	}
	return snap
}

const (
	mb = 1024 * 1024
	gb = 1024 * 1024 * 1024
)

func sub(a, b uint64) uint64 {
	if a < b { // 网卡计数器回绕或重置
		return 0
	}
	return a - b
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

// localIP 返回第一个非回环 IPv4 地址，取不到返回空串。
func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			return ipn.IP.String()
		}
	}
	return ""
}
