package log

// 日志统计：一条 SELECT 把明细取回来，所有分桶/分位/排行都在 Go 里算。
//
// 为什么不在 SQL 里聚合：DATE()/HOUR()/百分位在 MySQL、MariaDB、Postgres 三套引擎写法不同，
// 而本仓库要求同一份代码在三种引擎上都成立；明细扫描 + 内存聚合只依赖最基础的 SELECT，
// 也顺带保证租户与数据范围条件与列表/导出完全同源（都走 operQuery/loginQuery）。
//
// 代价是必须设扫描上限：statsScanLimit 条之外不统计，并在响应里如实标 sampled=true。

import (
	"context"
	"sort"
	"time"
)

// statsScanLimit 单次统计最多读多少条明细（按时间倒序取最近的）。
const statsScanLimit = 20000

const (
	statsDefaultDays = 7
	statsMaxDays     = 92
	statsTopN        = 8
)

// NameValue 是一个「名字 + 数量」条目，前端直接喂给图表。
type NameValue struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

// DayPoint 是按天分桶的操作日志计数。
type DayPoint struct {
	Date     string `json:"date"`
	Total    int64  `json:"total"`
	Failures int64  `json:"failures"`
}

// HourPoint 是按小时（0-23）的计数，用于时段分布。
type HourPoint struct {
	Hour  int   `json:"hour"`
	Value int64 `json:"value"`
}

// Latency 是耗时分布（毫秒）。
type Latency struct {
	Avg int64 `json:"avg"`
	P50 int64 `json:"p50"`
	P95 int64 `json:"p95"`
	P99 int64 `json:"p99"`
	Max int64 `json:"max"`
}

// URLLatency 是某个接口的平均耗时与调用次数。
type URLLatency struct {
	URL    string `json:"url"`
	Method string `json:"method"`
	AvgMs  int64  `json:"avgMs"`
	Count  int64  `json:"count"`
}

// OperStats 是 GET /oper-logs/stats 的响应。
type OperStats struct {
	From         string       `json:"from"`
	To           string       `json:"to"`
	Total        int64        `json:"total"`
	Failures     int64        `json:"failures"`
	Sampled      bool         `json:"sampled"`
	Days         []DayPoint   `json:"days"`
	Hours        []HourPoint  `json:"hours"`
	TopModules   []NameValue  `json:"topModules"`
	TopTypes     []NameValue  `json:"topTypes"`
	Methods      []NameValue  `json:"methods"`
	TopOperators []NameValue  `json:"topOperators"`
	Latency      Latency      `json:"latency"`
	Slowest      []URLLatency `json:"slowest"`
}

// LoginStats 是 GET /login-logs/stats 的响应。
type LoginStats struct {
	From        string      `json:"from"`
	To          string      `json:"to"`
	Total       int64       `json:"total"`
	Failures    int64       `json:"failures"`
	Sampled     bool        `json:"sampled"`
	Days        []DayPoint  `json:"days"`
	Hours       []HourPoint `json:"hours"`
	FailReasons []NameValue `json:"failReasons"`
	Types       []NameValue `json:"types"`
	Browsers    []NameValue `json:"browsers"`
	OS          []NameValue `json:"os"`
}

// operStatRow / loginStatRow 是统计用的窄投影：只取算聚合需要的列，
// 绝不带 params/response/error_stack 这类大字段。
type operStatRow struct {
	CreatedAt time.Time
	CostMs    int
	Status    string
	Module    string
	Type      string
	Method    string
	URL       string
	Operator  string
}

type loginStatRow struct {
	CreatedAt  time.Time
	Username   string
	LoginType  string
	Browser    string
	OS         string
	Status     string
	FailReason string
}

// statsWindow 决定统计窗口：给了日期就用给的（最多 statsMaxDays 天），否则取最近 statsDefaultDays 天。
func (s *Service) statsWindow(q Query) (from, to time.Time) {
	to = time.Now().In(s.loc)
	if e, err := time.ParseInLocation("2006-01-02", q.EndDate, s.loc); err == nil {
		to = e.AddDate(0, 0, 1)
	}
	from = to.AddDate(0, 0, -statsDefaultDays)
	if st, err := time.ParseInLocation("2006-01-02", q.StartDate, s.loc); err == nil {
		from = st
	}
	if to.Sub(from) > statsMaxDays*24*time.Hour {
		from = to.AddDate(0, 0, -statsMaxDays)
	}
	return from, to
}

// OperStats 汇总操作日志。筛选与数据范围沿用列表的条件，保证「看到的数」与「列表里的数」同源。
func (s *Service) OperStats(ctx context.Context, q Query) (OperStats, error) {
	from, to := s.statsWindow(q)
	if q.StartDate == "" {
		q.StartDate = from.Format("2006-01-02")
	}
	if q.EndDate == "" {
		q.EndDate = to.AddDate(0, 0, -1).Format("2006-01-02")
	}
	var rows []operStatRow
	tx := s.operQuery(ctx, q).
		Select("created_at", "cost_ms", "status", "module", "type", "method", "url", "operator").
		Order("created_at DESC").Limit(statsScanLimit + 1)
	if err := tx.Find(&rows).Error; err != nil {
		return OperStats{}, err
	}
	st := aggregateOper(rows, s.loc, from, to)
	st.From, st.To = q.StartDate, q.EndDate
	return st, nil
}

// LoginStats 汇总登录日志。
func (s *Service) LoginStats(ctx context.Context, q Query) (LoginStats, error) {
	from, to := s.statsWindow(q)
	if q.StartDate == "" {
		q.StartDate = from.Format("2006-01-02")
	}
	if q.EndDate == "" {
		q.EndDate = to.AddDate(0, 0, -1).Format("2006-01-02")
	}
	var rows []loginStatRow
	tx := s.loginQuery(ctx, q).
		Select("created_at", "username", "login_type", "browser", "os", "status", "fail_reason").
		Order("created_at DESC").Limit(statsScanLimit + 1)
	if err := tx.Find(&rows).Error; err != nil {
		return LoginStats{}, err
	}
	st := aggregateLogin(rows, s.loc, from, to)
	st.From, st.To = q.StartDate, q.EndDate
	return st, nil
}

func aggregateOper(rows []operStatRow, loc *time.Location, from, to time.Time) OperStats {
	out := OperStats{}
	out.Sampled = int64(len(rows)) > statsScanLimit
	if out.Sampled {
		rows = rows[:statsScanLimit] // 先截断，后面所有分桶都只统计被计入的样本
	}
	out.Total = int64(len(rows))
	out.Days = dayPoints(rows, loc, from, to, func(r operStatRow) bool { return r.Status != StatusSuccess })
	hours := make([]int64, 24)
	modules := map[string]int64{}
	types := map[string]int64{}
	methods := map[string]int64{}
	operators := map[string]int64{}
	costs := make([]int64, 0, len(rows))
	type urlAgg struct {
		method string
		sum    int64
		count  int64
	}
	urls := map[string]*urlAgg{}
	for _, r := range rows {
		if r.Status != StatusSuccess {
			out.Failures++
		}
		hours[r.CreatedAt.In(loc).Hour()]++
		inc(modules, r.Module)
		inc(types, r.Type)
		inc(methods, r.Method)
		inc(operators, r.Operator)
		costs = append(costs, int64(r.CostMs))
		u := urls[r.URL]
		if u == nil {
			u = &urlAgg{method: r.Method}
			urls[r.URL] = u
		}
		u.sum += int64(r.CostMs)
		u.count++
	}
	out.Hours = hourPoints(hours)
	out.TopModules = topN(modules, statsTopN)
	out.TopTypes = topN(types, statsTopN)
	out.Methods = topN(methods, statsTopN)
	out.TopOperators = topN(operators, statsTopN)
	out.Latency = percentile(costs)
	slowest := make([]URLLatency, 0, len(urls))
	for url, u := range urls {
		if u.count == 0 {
			continue
		}
		slowest = append(slowest, URLLatency{URL: url, Method: u.method, AvgMs: u.sum / u.count, Count: u.count})
	}
	sort.SliceStable(slowest, func(i, j int) bool {
		if slowest[i].AvgMs != slowest[j].AvgMs {
			return slowest[i].AvgMs > slowest[j].AvgMs
		}
		return slowest[i].URL < slowest[j].URL
	})
	if len(slowest) > statsTopN {
		slowest = slowest[:statsTopN]
	}
	out.Slowest = slowest
	return out
}

func aggregateLogin(rows []loginStatRow, loc *time.Location, from, to time.Time) LoginStats {
	out := LoginStats{}
	out.Sampled = int64(len(rows)) > statsScanLimit
	if out.Sampled {
		rows = rows[:statsScanLimit]
	}
	out.Total = int64(len(rows))
	hours := make([]int64, 24)
	reasons := map[string]int64{}
	ltypes := map[string]int64{}
	browsers := map[string]int64{}
	oses := map[string]int64{}
	days := map[string]*DayPoint{}
	dayKeys := []string{}
	for _, r := range rows {
		fail := r.Status != StatusSuccess
		if fail {
			out.Failures++
		}
		local := r.CreatedAt.In(loc)
		hours[local.Hour()]++
		key := local.Format("2006-01-02")
		dp := days[key]
		if dp == nil {
			dp = &DayPoint{Date: key}
			days[key] = dp
			dayKeys = append(dayKeys, key)
		}
		dp.Total++
		if fail {
			dp.Failures++
		}
		if fail {
			inc(reasons, r.FailReason)
		}
		inc(ltypes, r.LoginType)
		inc(browsers, r.Browser)
		inc(oses, r.OS)
	}
	out.Days = fillDays(days, dayKeys, from, to, loc)
	out.Hours = hourPoints(hours)
	out.FailReasons = topN(reasons, statsTopN)
	out.Types = topN(ltypes, statsTopN)
	out.Browsers = topN(browsers, statsTopN)
	out.OS = topN(oses, statsTopN)
	return out
}

// dayPoints 按天分桶（操作日志版），并补齐窗口内没有数据的日期，让折线图 x 轴连续。
func dayPoints(rows []operStatRow, loc *time.Location, from, to time.Time, isFail func(operStatRow) bool) []DayPoint {
	bucket := map[string]*DayPoint{}
	keys := []string{}
	for _, r := range rows {
		key := r.CreatedAt.In(loc).Format("2006-01-02")
		dp := bucket[key]
		if dp == nil {
			dp = &DayPoint{Date: key}
			bucket[key] = dp
			keys = append(keys, key)
		}
		dp.Total++
		if isFail(r) {
			dp.Failures++
		}
	}
	return fillDays(bucket, keys, from, to, loc)
}

// fillDays 输出升序的按天序列，并补上窗口内空白天（最多 statsMaxDays 天，避免异常区间撑爆响应）。
func fillDays(bucket map[string]*DayPoint, keys []string, from, to time.Time, loc *time.Location) []DayPoint {
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	out := make([]DayPoint, 0, len(keys)+1)
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	for d := start; d.Before(to) && len(out) <= statsMaxDays; d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if dp := bucket[key]; dp != nil {
			out = append(out, *dp)
			delete(seen, key)
			continue
		}
		out = append(out, DayPoint{Date: key})
	}
	// 窗口外仍有数据（例如只给了 start_date）时，把它们按日期顺序补在尾部，不丢数。
	extra := make([]string, 0, len(seen))
	for k := range seen {
		extra = append(extra, k)
	}
	sort.Strings(extra)
	for _, k := range extra {
		out = append(out, *bucket[k])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func hourPoints(counts []int64) []HourPoint {
	out := make([]HourPoint, 24)
	for h, v := range counts {
		out[h] = HourPoint{Hour: h, Value: v}
	}
	return out
}

func inc(m map[string]int64, key string) {
	if key == "" {
		key = "-"
	}
	m[key]++
}

// topN 取数量前 N；数量相同的按名字排序，保证同一份数据每次输出一致。
func topN(m map[string]int64, n int) []NameValue {
	items := make([]NameValue, 0, len(m))
	for name, value := range m {
		items = append(items, NameValue{Name: name, Value: value})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Value != items[j].Value {
			return items[i].Value > items[j].Value
		}
		return items[i].Name < items[j].Name
	})
	if len(items) > n {
		items = items[:n]
	}
	return items
}

// percentile 计算平均/p50/p95/p99/最大。空样本返回全 0，而不是编一个数。
func percentile(costs []int64) Latency {
	if len(costs) == 0 {
		return Latency{}
	}
	sorted := make([]int64, len(costs))
	copy(sorted, costs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum int64
	for _, c := range sorted {
		sum += c
	}
	return Latency{
		Avg: sum / int64(len(sorted)),
		P50: pick(sorted, 0.50),
		P95: pick(sorted, 0.95),
		P99: pick(sorted, 0.99),
		Max: sorted[len(sorted)-1],
	}
}

// pick 取分位数：升序第 ceil(p*n) 个（最近秩法）。n=1 时任何分位都落在唯一样本上。
func pick(sorted []int64, p float64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(float64(n)*p + 0.999999) // ceil
	if idx < 1 {
		idx = 1
	}
	if idx > n {
		idx = n
	}
	return sorted[idx-1]
}
