package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/cache"
	"nexadmin/core/config"
)

func init() { gin.SetMode(gin.TestMode) }

var (
	sampleRe = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{(.*)\})?\s+(\S+)$`)
	labelRe  = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"`)
)

type sample struct {
	name   string
	labels map[string]string
	value  float64
}

// parseExposition 是最小的文本格式校验器：每个样本必须属于已声明 TYPE 的指标族，值必须能解析为浮点数。
func parseExposition(t *testing.T, text string) (samples []sample, types map[string]string) {
	t.Helper()
	types = map[string]string{}
	helped := map[string]bool{}
	for i, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		switch {
		case line == "":
			t.Fatalf("line %d: empty line", i+1)
		case strings.HasPrefix(line, "# HELP "):
			helped[strings.Fields(line)[2]] = true
		case strings.HasPrefix(line, "# TYPE "):
			f := strings.Fields(line)
			if len(f) != 4 {
				t.Fatalf("line %d: bad TYPE: %q", i+1, line)
			}
			if _, dup := types[f[2]]; dup {
				t.Fatalf("line %d: duplicate TYPE for %s", i+1, f[2])
			}
			switch f[3] {
			case "counter", "gauge", "histogram":
			default:
				t.Fatalf("line %d: bad type %q", i+1, f[3])
			}
			types[f[2]] = f[3]
		case strings.HasPrefix(line, "#"):
			t.Fatalf("line %d: unexpected comment %q", i+1, line)
		default:
			m := sampleRe.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("line %d: not a valid sample: %q", i+1, line)
			}
			v, err := strconv.ParseFloat(m[4], 64)
			if err != nil {
				t.Fatalf("line %d: bad value %q", i+1, m[4])
			}
			s := sample{name: m[1], labels: map[string]string{}, value: v}
			if m[3] != "" {
				rest := labelRe.ReplaceAllStringFunc(m[3], func(p string) string {
					sub := labelRe.FindStringSubmatch(p)
					s.labels[sub[1]] = sub[2]
					return ""
				})
				if strings.Trim(rest, ", ") != "" {
					t.Fatalf("line %d: malformed labels %q", i+1, m[3])
				}
			}
			family := s.name
			for _, suf := range []string{"_bucket", "_sum", "_count"} {
				if base := strings.TrimSuffix(s.name, suf); base != s.name && types[base] == "histogram" {
					family = base
				}
			}
			if types[family] == "" {
				t.Fatalf("line %d: sample %s has no preceding TYPE", i+1, s.name)
			}
			if !helped[family] {
				t.Fatalf("line %d: family %s has no HELP", i+1, family)
			}
			samples = append(samples, s)
		}
	}
	return samples, types
}

func newTestEngine(r *registry) *gin.Engine {
	e := gin.New()
	e.Use(r.middleware())
	e.GET("/users/:id", func(c *gin.Context) { c.String(200, "ok") })
	e.POST("/users", func(c *gin.Context) { c.String(201, "ok") })
	e.GET("/boom", func(c *gin.Context) { c.String(500, "x") })
	return e
}

func do(e *gin.Engine, method, path string) {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(method, path, nil))
}

func TestExpositionFormatValid(t *testing.T) {
	r := newRegistry()
	r.AddCollector("runtime", RuntimeCollector)
	r.AddCollector("custom", func(w *Writer) {
		w.Gauge("nexadmin_test_weird", "help with \\ and \n newline", 1.5, "name", "a\"b\\c\nd")
	})
	e := newTestEngine(r)
	for i := 0; i < 5; i++ {
		do(e, "GET", fmt.Sprintf("/users/%d", i))
	}
	do(e, "POST", "/users")
	do(e, "GET", "/boom")

	samples, types := parseExposition(t, string(r.render()))
	for _, fam := range []string{"nexadmin_http_requests_total", "nexadmin_http_request_duration_seconds", "nexadmin_http_requests_in_flight", "go_goroutines"} {
		if types[fam] == "" {
			t.Errorf("missing family %s", fam)
		}
	}
	// 直方图：桶累计单调不减，+Inf 桶等于 _count
	type hk struct{ method, route, status string }
	buckets := map[hk][]float64{}
	inf, count := map[hk]float64{}, map[hk]float64{}
	for _, s := range samples {
		k := hk{s.labels["method"], s.labels["route"], s.labels["status"]}
		switch s.name {
		case "nexadmin_http_request_duration_seconds_bucket":
			if s.labels["le"] == "+Inf" {
				inf[k] = s.value
			} else {
				buckets[k] = append(buckets[k], s.value)
			}
		case "nexadmin_http_request_duration_seconds_count":
			count[k] = s.value
		}
	}
	if len(count) != 3 {
		t.Fatalf("want 3 series, got %d: %v", len(count), count)
	}
	for k, bs := range buckets {
		for i := 1; i < len(bs); i++ {
			if bs[i] < bs[i-1] {
				t.Errorf("%v: buckets not cumulative: %v", k, bs)
			}
		}
		if len(bs) != len(Buckets) {
			t.Errorf("%v: want %d finite buckets, got %d", k, len(Buckets), len(bs))
		}
		if inf[k] != count[k] || (len(bs) > 0 && bs[len(bs)-1] > inf[k]) {
			t.Errorf("%v: +Inf=%v count=%v last=%v", k, inf[k], count[k], bs[len(bs)-1])
		}
	}
	if got := count[hk{"GET", "/users/:id", "2xx"}]; got != 5 {
		t.Errorf("GET /users/:id 2xx count = %v, want 5", got)
	}
	// 特殊字符转义后仍能被解析回原值
	for _, s := range samples {
		if s.name == "nexadmin_test_weird" && s.labels["name"] != `a\"b\\c\nd` {
			t.Errorf("label escaping = %q", s.labels["name"])
		}
	}
}

func TestLabelCardinalityBounded(t *testing.T) {
	r := newRegistry()
	e := newTestEngine(r)
	// 1) 同一路由模板下 2000 个不同 URL -> 1 条序列
	for i := 0; i < 2000; i++ {
		do(e, "GET", "/users/"+strconv.Itoa(i))
	}
	// 2) 随机不存在的路径统一成 unmatched；随机方法统一成 OTHER
	for i := 0; i < 500; i++ {
		do(e, "GET", "/nope/"+strconv.Itoa(i))
		do(e, "X"+strconv.Itoa(i), "/nope")
	}
	r.hmu.RLock()
	n := len(r.series)
	routes := map[string]bool{}
	methods := map[string]bool{}
	for k := range r.series {
		routes[k.route] = true
		methods[k.method] = true
	}
	r.hmu.RUnlock()
	if !routes["/users/:id"] || !routes[routeUnknown] || len(routes) != 2 {
		t.Errorf("routes = %v, want only /users/:id and %s", routes, routeUnknown)
	}
	if !methods["OTHER"] || len(methods) > 2 {
		t.Errorf("methods = %v", methods)
	}
	if n > 4 {
		t.Errorf("series = %d, want <= 4", n)
	}
	// 3) 路由模板总数有上限
	for i := 0; i < maxRoutes+50; i++ {
		r.observe("GET", "/gen/"+strconv.Itoa(i), 200, time.Millisecond)
	}
	r.hmu.RLock()
	nr := len(r.routes)
	_, hasOther := r.series[seriesKey{"GET", routeOther, "2xx"}]
	r.hmu.RUnlock()
	if nr > maxRoutes || !hasOther {
		t.Errorf("routes=%d (max %d), other series=%v", nr, maxRoutes, hasOther)
	}
}

func TestStatusClass(t *testing.T) {
	for code, want := range map[int]string{200: "2xx", 302: "3xx", 404: "4xx", 503: "5xx", 0: "other", 700: "other"} {
		if got := StatusClass(code); got != want {
			t.Errorf("StatusClass(%d)=%s want %s", code, got, want)
		}
	}
}

func TestInFlight(t *testing.T) {
	r := newRegistry()
	e := gin.New()
	e.Use(r.middleware())
	started, release := make(chan struct{}), make(chan struct{})
	e.GET("/slow", func(c *gin.Context) { close(started); <-release })
	done := make(chan struct{})
	go func() { do(e, "GET", "/slow"); close(done) }()
	<-started
	if got := r.inflight.Load(); got != 1 {
		t.Errorf("in flight = %d, want 1", got)
	}
	close(release)
	<-done
	if got := r.inflight.Load(); got != 0 {
		t.Errorf("in flight after = %d, want 0", got)
	}
}

func TestMetricsAccessProtection(t *testing.T) {
	r := newRegistry()
	call := func(o Options, remote string, hdr map[string]string) int {
		e := gin.New()
		_ = e.SetTrustedProxies(nil)
		e.GET("/metrics", r.handler(o))
		req := httptest.NewRequest("GET", "/metrics", nil)
		req.RemoteAddr = remote
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}
	// 默认：只允许回环
	if c := call(Options{}, "127.0.0.1:5000", nil); c != http.StatusOK {
		t.Errorf("loopback v4 = %d", c)
	}
	if c := call(Options{}, "[::1]:5000", nil); c != http.StatusOK {
		t.Errorf("loopback v6 = %d", c)
	}
	if c := call(Options{}, "10.1.2.3:5000", nil); c != http.StatusForbidden {
		t.Errorf("remote without token = %d, want 403", c)
	}
	if c := call(Options{}, "127.0.0.1:5000", map[string]string{"X-Forwarded-For": "8.8.8.8"}); c != http.StatusForbidden {
		t.Errorf("proxied loopback = %d, want 403", c)
	}
	// 配置 token：不论来源都必须带正确的 Bearer
	tok := Options{Token: "s3cret-token"}
	if c := call(tok, "10.1.2.3:5000", map[string]string{"Authorization": "Bearer s3cret-token"}); c != http.StatusOK {
		t.Errorf("good token = %d", c)
	}
	for name, h := range map[string]map[string]string{
		"none":         nil,
		"wrong":        {"Authorization": "Bearer nope"},
		"prefix":       {"Authorization": "Bearer s3cret-toke"},
		"longer":       {"Authorization": "Bearer s3cret-token-x"},
		"wrong scheme": {"Authorization": "Basic s3cret-token"},
		"raw":          {"Authorization": "s3cret-token"},
	} {
		if c := call(tok, "10.1.2.3:5000", h); c != http.StatusUnauthorized {
			t.Errorf("token case %s = %d, want 401", name, c)
		}
	}
	if c := call(tok, "127.0.0.1:5000", nil); c != http.StatusUnauthorized {
		t.Errorf("loopback without token when token configured = %d, want 401", c)
	}
}

func TestMetricsEndpointContentType(t *testing.T) {
	r := newRegistry()
	e := gin.New()
	e.GET("/metrics", r.handler(Options{}))
	req := httptest.NewRequest("GET", "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("content-type = %q", ct)
	}
	parseExposition(t, w.Body.String())
}

func TestWrapCacheCountsAndKeepsCounter(t *testing.T) {
	base, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	hit0, miss0 := cacheHit.Load(), cacheMiss.Load()
	c := WrapCache(base)
	ctx := context.Background()
	if _, ok := c.(cache.Counter); !ok {
		t.Fatal("wrapped cache lost cache.Counter")
	}
	_ = c.Set(ctx, "k", "v", time.Minute)
	if _, err := c.Get(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, "missing"); err == nil {
		t.Fatal("want not found")
	}
	if _, _, err := c.(cache.Counter).Incr(ctx, "n", time.Minute); err != nil {
		t.Fatal(err)
	}
	if cacheHit.Load()-hit0 != 1 || cacheMiss.Load()-miss0 != 1 {
		t.Errorf("hit +%d miss +%d, want +1/+1", cacheHit.Load()-hit0, cacheMiss.Load()-miss0)
	}
	w := NewWriter()
	Default.mu.RLock()
	col := Default.cols["cache"]
	Default.mu.RUnlock()
	col(w)
	parseExposition(t, string(w.Bytes()))
}

func TestOptionalSources(t *testing.T) {
	r := newRegistry()
	// 没登记任何可选来源：输出里没有队列 / 迁移 / 插件指标
	if out := string(r.render()); strings.Contains(out, "job_queue") || strings.Contains(out, "plugin_licensed") {
		t.Errorf("unexpected optional metrics: %s", out)
	}
	if _, err := PendingMigrations(); err != ErrNoSource {
		t.Errorf("PendingMigrations without source err = %v", err)
	}
	SetMigrationSource(func() (int, error) { return 3, nil })
	defer SetMigrationSource(nil)
	if n, err := PendingMigrations(); n != 3 || err != nil {
		t.Errorf("pending = %d, %v", n, err)
	}
	w := NewWriter()
	SetQueueSource(func() map[string]int64 { return map[string]int64{"queued": 4, "running": 1} })
	SetPluginSource(func() []PluginState { return []PluginState{{ID: "crm", Active: true}, {ID: "bi"}} })
	defer SetQueueSource(nil)
	defer SetPluginSource(nil)
	for _, n := range []string{"queue", "plugins", "migrations"} {
		Default.mu.RLock()
		col := Default.cols[n]
		Default.mu.RUnlock()
		col(w)
	}
	samples, _ := parseExposition(t, string(w.Bytes()))
	got := map[string]float64{}
	for _, s := range samples {
		switch s.name {
		case "nexadmin_job_queue_length":
			got["q:"+s.labels["state"]] = s.value
		case "nexadmin_plugin_licensed":
			got["p:"+s.labels["plugin"]] = s.value
		case "nexadmin_migrations_pending":
			got["m"] = s.value
		}
	}
	want := map[string]float64{"q:queued": 4, "q:running": 1, "p:crm": 1, "p:bi": 0, "m": 3}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestCollectorPanicIsolated(t *testing.T) {
	r := newRegistry()
	r.AddCollector("bad", func(*Writer) { panic("boom") })
	r.AddCollector("good", func(w *Writer) { w.Gauge("nexadmin_ok", "ok", 1) })
	if out := string(r.render()); !strings.Contains(out, "nexadmin_ok 1") {
		t.Errorf("good collector missing after panic: %s", out)
	}
}
