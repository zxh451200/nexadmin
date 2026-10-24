// Package metrics 是手写的 Prometheus 文本格式（exposition 0.0.4）指标：不引入 client_golang。
//
// 组成：HTTP 中间件（请求计数 / 耗时直方图 / 进行中请求数，label 用路由模板并限制基数）、
// 可插拔的 Collector（运行时、数据库连接池、缓存、迁移、插件授权、异步任务队列等由各处注册）、
// 受保护的 /metrics 处理器（默认只允许回环地址，或配置 Bearer token）。
// 本包不依赖 core / core/job / core/plugin，其他包通过 AddCollector 与 Set* 函数把数据源接进来。
package metrics

import (
	"bytes"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// 基数上限：路由模板个数超过 maxRoutes 后，新的路由统一记到 "other"。
const (
	maxRoutes    = 300
	routeOther   = "other"
	routeUnknown = "unmatched" // 没有匹配到任何路由（404 / 静态资源）
)

// Buckets 是请求耗时直方图的桶上界（秒）。
var Buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true,
	http.MethodDelete: true, http.MethodHead: true, http.MethodOptions: true,
}

// ---- Writer：文本格式输出 ----

// Writer 按 Prometheus 文本格式写指标；同一个指标族只输出一次 HELP / TYPE。
type Writer struct {
	buf  bytes.Buffer
	seen map[string]bool
}

// NewWriter 创建空的 Writer。
func NewWriter() *Writer { return &Writer{seen: map[string]bool{}} }

// Bytes 返回已写入的内容。
func (w *Writer) Bytes() []byte { return w.buf.Bytes() }

// Family 声明一个指标族（typ：counter / gauge / histogram）。重复声明被忽略。
func (w *Writer) Family(name, help, typ string) {
	if w.seen[name] {
		return
	}
	w.seen[name] = true
	w.buf.WriteString("# HELP " + name + " " + escapeHelp(help) + "\n")
	w.buf.WriteString("# TYPE " + name + " " + typ + "\n")
}

// Sample 写一条样本；labels 是 key, value 交替的列表。
func (w *Writer) Sample(name string, v float64, labels ...string) {
	w.buf.WriteString(name)
	writeLabels(&w.buf, labels)
	w.buf.WriteByte(' ')
	w.buf.WriteString(formatFloat(v))
	w.buf.WriteByte('\n')
}

// Gauge 声明并写一个 gauge 样本。
func (w *Writer) Gauge(name, help string, v float64, labels ...string) {
	w.Family(name, help, "gauge")
	w.Sample(name, v, labels...)
}

// Counter 声明并写一个 counter 样本。
func (w *Writer) Counter(name, help string, v float64, labels ...string) {
	w.Family(name, help, "counter")
	w.Sample(name, v, labels...)
}

func writeLabels(b *bytes.Buffer, labels []string) {
	if len(labels) < 2 {
		return
	}
	b.WriteByte('{')
	for i := 0; i+1 < len(labels); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(sanitizeName(labels[i]))
		b.WriteString(`="`)
		b.WriteString(escapeLabel(labels[i+1]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
}

func escapeLabel(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func escapeHelp(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`).Replace(s)
}

// sanitizeName 把 label 名里不合法的字符替换成下划线。
func sanitizeName(s string) string {
	var b strings.Builder
	for i, r := range s {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

func formatFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// ---- Collector 注册 ----

// Collector 在每次抓取时把当前值写进 Writer。
type Collector func(w *Writer)

type registry struct {
	mu   sync.RWMutex
	cols map[string]Collector

	inflight atomic.Int64
	hmu      sync.RWMutex
	series   map[seriesKey]*histogram
	routes   map[string]struct{}
}

type seriesKey struct{ method, route, class string }

func newRegistry() *registry {
	return &registry{cols: map[string]Collector{}, series: map[seriesKey]*histogram{}, routes: map[string]struct{}{}}
}

// Default 是进程级注册表。
var Default = newRegistry()

// AddCollector 登记（或按名字替换）一个采集器。
func AddCollector(name string, c Collector) { Default.AddCollector(name, c) }

// AddCollector 登记（或按名字替换）一个采集器；c 为 nil 表示移除。
func (r *registry) AddCollector(name string, c Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c == nil {
		delete(r.cols, name)
		return
	}
	r.cols[name] = c
}

// ---- HTTP 中间件 ----

type histogram struct {
	buckets []atomic.Uint64 // 非累计计数，输出时再累加
	count   atomic.Uint64
	sumNs   atomic.Int64
}

func newHistogram() *histogram { return &histogram{buckets: make([]atomic.Uint64, len(Buckets))} }

func (h *histogram) observe(d time.Duration) {
	sec := d.Seconds()
	for i, ub := range Buckets {
		if sec <= ub {
			h.buckets[i].Add(1)
			break
		}
	}
	h.count.Add(1)
	h.sumNs.Add(int64(d))
}

// StatusClass 把状态码分组成 1xx..5xx；非法值归为 "other"。
func StatusClass(code int) string {
	if code < 100 || code > 599 {
		return "other"
	}
	return strconv.Itoa(code/100) + "xx"
}

// normMethod 非常见方法统一成 OTHER，防止攻击者用随机方法名撑大基数。
func normMethod(m string) string {
	if knownMethods[m] {
		return m
	}
	return "OTHER"
}

// routeLabel 返回 label 用的路由：优先用路由模板；没匹配到用固定值；模板数超过上限归入 other。
func (r *registry) routeLabel(full string) string {
	if full == "" {
		return routeUnknown
	}
	r.hmu.RLock()
	_, ok := r.routes[full]
	n := len(r.routes)
	r.hmu.RUnlock()
	if ok {
		return full
	}
	if n >= maxRoutes {
		return routeOther
	}
	r.hmu.Lock()
	defer r.hmu.Unlock()
	if _, ok := r.routes[full]; !ok {
		if len(r.routes) >= maxRoutes {
			return routeOther
		}
		r.routes[full] = struct{}{}
	}
	return full
}

func (r *registry) observe(method, full string, status int, d time.Duration) {
	k := seriesKey{method: normMethod(method), route: r.routeLabel(full), class: StatusClass(status)}
	r.hmu.RLock()
	h := r.series[k]
	r.hmu.RUnlock()
	if h == nil {
		r.hmu.Lock()
		if h = r.series[k]; h == nil {
			h = newHistogram()
			r.series[k] = h
		}
		r.hmu.Unlock()
	}
	h.observe(d)
}

// Middleware 统计请求数、耗时与进行中请求数。放在 Recovery 之前，才能看到 panic 转成的 500。
func Middleware() gin.HandlerFunc { return Default.middleware() }

func (r *registry) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		r.inflight.Add(1)
		defer func() {
			r.inflight.Add(-1)
			r.observe(c.Request.Method, c.FullPath(), c.Writer.Status(), time.Since(start))
		}()
		c.Next()
	}
}

// ---- 渲染 ----

// Render 输出全部指标的文本。
func Render() []byte { return Default.render() }

func (r *registry) render() []byte {
	w := NewWriter()
	r.writeHTTP(w)

	r.mu.RLock()
	names := make([]string, 0, len(r.cols))
	for n := range r.cols {
		names = append(names, n)
	}
	sort.Strings(names)
	cols := make([]Collector, 0, len(names))
	for _, n := range names {
		cols = append(cols, r.cols[n])
	}
	r.mu.RUnlock()
	for _, c := range cols {
		safeCollect(c, w)
	}
	return w.Bytes()
}

// safeCollect 隔离单个采集器的 panic，不影响其他指标。
func safeCollect(c Collector, w *Writer) {
	defer func() { _ = recover() }()
	c(w)
}

func (r *registry) writeHTTP(w *Writer) {
	r.hmu.RLock()
	keys := make([]seriesKey, 0, len(r.series))
	for k := range r.series {
		keys = append(keys, k)
	}
	r.hmu.RUnlock()
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.route != b.route {
			return a.route < b.route
		}
		if a.method != b.method {
			return a.method < b.method
		}
		return a.class < b.class
	})

	w.Family("nexadmin_http_requests_total", "Total HTTP requests by method, route template and status class.", "counter")
	for _, k := range keys {
		r.hmu.RLock()
		h := r.series[k]
		r.hmu.RUnlock()
		w.Sample("nexadmin_http_requests_total", float64(h.count.Load()), "method", k.method, "route", k.route, "status", k.class)
	}
	w.Family("nexadmin_http_request_duration_seconds", "HTTP request latency in seconds.", "histogram")
	for _, k := range keys {
		r.hmu.RLock()
		h := r.series[k]
		r.hmu.RUnlock()
		base := []string{"method", k.method, "route", k.route, "status", k.class}
		var cum uint64
		for i, ub := range Buckets {
			cum += h.buckets[i].Load()
			w.Sample("nexadmin_http_request_duration_seconds_bucket", float64(cum), append(base[:6:6], "le", formatFloat(ub))...)
		}
		cnt := h.count.Load()
		w.Sample("nexadmin_http_request_duration_seconds_bucket", float64(cnt), append(base[:6:6], "le", "+Inf")...)
		w.Sample("nexadmin_http_request_duration_seconds_sum", float64(h.sumNs.Load())/1e9, base...)
		w.Sample("nexadmin_http_request_duration_seconds_count", float64(cnt), base...)
	}
	w.Gauge("nexadmin_http_requests_in_flight", "HTTP requests currently being served.", float64(r.inflight.Load()))
}
