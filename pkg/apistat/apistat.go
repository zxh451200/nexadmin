// Package apistat 是进程内的接口调用统计：按“方法 + 路由模板”累计最近 30 秒的请求数、错误数、耗时，
// 并保留最近几天的日汇总。供 api-monitor 与 dashboard 模块共用。
// 只有能力件，没有路由也没有表；数据存在内存里，重启后清零。
package apistat

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	window     = 30 // 滑动窗口秒数，QPS = 窗口内请求数 / 秒数
	keepDays   = 8  // 日汇总保留天数
	apiPrefix  = "/api/v1"
	errorFloor = 400 // HTTP 状态码 >= 400 计为错误
)

type bucket struct {
	sec  int64
	n    int64
	errs int64
	ms   float64
}

type route struct {
	method string
	path   string
	ring   [window]bucket
}

type day struct{ requests, errors int64 }

// Stat 是单个接口的快照。
type Stat struct {
	Method    string  `json:"method"`
	Path      string  `json:"path"`
	QPS       float64 `json:"qps"`
	AvgMs     float64 `json:"avgMs"`
	ErrorRate float64 `json:"errorRate"` // 百分比 0-100
}

// Snapshot 是全局快照。
type Snapshot struct {
	QPS       float64
	AvgMs     float64
	ErrorRate float64
	APIs      []Stat
}

// Day 是某天的汇总。
type Day struct {
	Date     time.Time
	Requests int64
	Errors   int64
}

// Collector 是并发安全的统计器。
type Collector struct {
	mu      sync.Mutex
	started time.Time
	routes  map[string]*route
	days    map[string]*day
}

// New 创建统计器。
func New() *Collector {
	return &Collector{started: time.Now(), routes: map[string]*route{}, days: map[string]*day{}}
}

// Default 是进程级默认统计器。
var Default = New()

// Record 记录一次请求。只统计 GET / POST / PUT / DELETE。
func (c *Collector) Record(method, path string, status int, d time.Duration) {
	switch method {
	case "GET", "POST", "PUT", "DELETE":
	default:
		return
	}
	now := time.Now()
	sec := now.Unix()
	isErr := status >= errorFloor
	key := method + " " + path

	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.routes[key]
	if r == nil {
		r = &route{method: method, path: path}
		c.routes[key] = r
	}
	b := &r.ring[sec%window]
	if b.sec != sec {
		*b = bucket{sec: sec}
	}
	b.n++
	b.ms += float64(d.Microseconds()) / 1000
	dk := now.Format("2006-01-02")
	dd := c.days[dk]
	if dd == nil {
		dd = &day{}
		c.days[dk] = dd
		c.pruneDays(now)
	}
	dd.requests++
	if isErr {
		b.errs++
		dd.errors++
	}
}

func (c *Collector) pruneDays(now time.Time) {
	oldest := now.AddDate(0, 0, -keepDays).Format("2006-01-02")
	for k := range c.days {
		if k < oldest {
			delete(c.days, k)
		}
	}
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// Snapshot 汇总最近 window 秒的数据。
func (c *Collector) Snapshot() Snapshot {
	now := time.Now().Unix()
	span := float64(window)
	if up := time.Since(c.started).Seconds(); up < span {
		span = math.Max(up, 1)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out Snapshot
	var totalN, totalErr int64
	var totalMs float64
	for _, r := range c.routes {
		var n, errs int64
		var ms float64
		for _, b := range r.ring {
			if b.sec > now-window && b.sec <= now {
				n += b.n
				errs += b.errs
				ms += b.ms
			}
		}
		if n == 0 {
			continue
		}
		out.APIs = append(out.APIs, Stat{
			Method:    r.method,
			Path:      r.path,
			QPS:       round1(float64(n) / span),
			AvgMs:     round1(ms / float64(n)),
			ErrorRate: round2(float64(errs) / float64(n) * 100),
		})
		totalN += n
		totalErr += errs
		totalMs += ms
	}
	sort.Slice(out.APIs, func(i, j int) bool { return out.APIs[i].QPS > out.APIs[j].QPS })
	if totalN > 0 {
		out.QPS = round1(float64(totalN) / span)
		out.AvgMs = round1(totalMs / float64(totalN))
		out.ErrorRate = round2(float64(totalErr) / float64(totalN) * 100)
	}
	return out
}

// Days 返回最近 n 天（含今天，按时间升序）的日汇总，没有数据的日子为 0。
func (c *Collector) Days(n int) []Day {
	c.mu.Lock()
	defer c.mu.Unlock()
	today := time.Now()
	out := make([]Day, 0, n)
	for i := n - 1; i >= 0; i-- {
		t := today.AddDate(0, 0, -i)
		d := Day{Date: t}
		if v := c.days[t.Format("2006-01-02")]; v != nil {
			d.Requests, d.Errors = v.requests, v.errors
		}
		out = append(out, d)
	}
	return out
}

// Middleware 返回 Gin 中间件。路径用路由模板（如 /users/:id），并去掉 /api/v1 前缀。
func Middleware(c *Collector) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		start := time.Now()
		ctx.Next()
		p := ctx.FullPath()
		if p == "" {
			return
		}
		c.Record(ctx.Request.Method, strings.TrimPrefix(p, apiPrefix), ctx.Writer.Status(), time.Since(start))
	}
}

var (
	installMu sync.Mutex
	installed = map[*gin.RouterGroup]bool{}
)

// Install 给路由组挂上默认统计器（同一个组只挂一次）。
// 注意：Gin 的 Use 只对该组之后注册的路由生效，更早注册的路由（含 core 的 /auth/*）不在统计内。
func Install(groups ...*gin.RouterGroup) {
	installMu.Lock()
	defer installMu.Unlock()
	for _, g := range groups {
		if !installed[g] {
			installed[g] = true
			g.Use(Middleware(Default))
		}
	}
}
