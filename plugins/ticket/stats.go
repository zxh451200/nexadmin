package ticket

// 工单统计：一条 SELECT 取窄投影明细，SLA 达成、首响分布、队列结构都在 Go 里算。
//
// 与日志统计同一套做法的原因一样：DATE()/百分位在 MySQL、MariaDB、Postgres 三套引擎写法不同，
// 内存聚合只依赖最基础的 SELECT；筛选条件复用 ticketFilter，保证「统计卡上的数」与
// 「坐席列表翻出来的数」同源（列表没有日期维度，统计额外支持 startDate/endDate 窗口）。

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// statsScanLimit 单次统计最多读多少张工单，超出即截断并如实标 sampled。
const statsScanLimit = 20000

// TicketStatsQuery 是统计的筛选条件；Status/Priority/BizType/Keyword/Overdue 与列表同名同义。
type TicketStatsQuery struct {
	Status    string
	Priority  string
	BizType   string
	Keyword   string
	Overdue   string
	StartDate string
	EndDate   string
}

// DayPoint 是按天分桶（新建 vs 已解决）。
type DayPoint struct {
	Date     string `json:"date"`
	Created  int64  `json:"created"`
	Resolved int64  `json:"resolved"`
}

// NameValue 是「名字 + 数量」。
type NameValue struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

// PrioritySLA 是某个优先级的 SLA 达成情况。
type PrioritySLA struct {
	Priority      string  `json:"priority"`
	TargetMinutes int64   `json:"targetMinutes"`
	Total         int64   `json:"total"`
	Breached      int64   `json:"breached"`
	BreachRate    float64 `json:"breachRate"`
}

// TicketStats 是 GET /plugins/ticket/tickets/stats 的响应。
type TicketStats struct {
	From          string        `json:"from"`
	To            string        `json:"to"`
	Total         int64         `json:"total"`
	Open          int64         `json:"open"`
	Processing    int64         `json:"processing"`
	Resolved      int64         `json:"resolved"`
	Closed        int64         `json:"closed"`
	Unassigned    int64         `json:"unassigned"`
	Overdue       int64         `json:"overdue"`
	Sampled       bool          `json:"sampled"`
	Days          []DayPoint    `json:"days"`
	ByStatus      []NameValue   `json:"byStatus"`
	ByPriority    []NameValue   `json:"byPriority"`
	ByBizType     []NameValue   `json:"byBizType"`
	ByAssignee    []NameValue   `json:"byAssignee"`
	FirstResponse FirstResponse `json:"firstResponse"`
	SlaByPrio     []PrioritySLA `json:"slaByPriority"`
}

// FirstResponse 是首次响应时长分布（分钟）。
type FirstResponse struct {
	Responded  int64 `json:"responded"`
	NoResponse int64 `json:"noResponse"`
	AvgMinutes int64 `json:"avgMinutes"`
	P50Minutes int64 `json:"p50Minutes"`
	P90Minutes int64 `json:"p90Minutes"`
}

// ticketStatRow 是统计用的窄投影：不含 content/last_preview 等正文列。
type ticketStatRow struct {
	CreatedAt    time.Time
	Status       string
	Priority     string
	BizType      string
	AssigneeName string
	AssigneeID   int64
	FirstReplyAt *time.Time
	DueAt        *time.Time
	Overdue      bool
}

// Stats 汇总工单。
func (s *Service) Stats(ctx context.Context, q TicketStatsQuery) (TicketStats, error) {
	db := s.db.WithContext(ctx).Model(&Ticket{})
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Priority != "" {
		db = db.Where("priority = ?", q.Priority)
	}
	if q.BizType != "" {
		db = db.Where("biz_type = ?", q.BizType)
	}
	if q.Overdue == "1" {
		db = db.Where("overdue = ?", true)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(kw) + "%"
		db = db.Where("ticket_no LIKE ? OR title LIKE ?", like, like)
	}
	loc := s.loc
	if loc == nil {
		loc = time.UTC
	}
	from, to := statsWindow(q, loc)
	q.StartDate, q.EndDate = from.Format("2006-01-02"), to.AddDate(0, 0, -1).Format("2006-01-02")
	db = db.Where("created_at >= ? AND created_at < ?", from.UTC(), to.UTC())

	var rows []ticketStatRow
	if err := db.Select("created_at", "status", "priority", "biz_type", "assignee_name", "assignee_id", "first_reply_at", "due_at", "overdue").
		Order("created_at DESC").Limit(statsScanLimit + 1).Find(&rows).Error; err != nil {
		return TicketStats{}, err
	}
	st := aggregateTickets(rows, loc, from, to)
	st.From, st.To = q.StartDate, q.EndDate
	return st, nil
}

const statsDefaultDays = 30
const statsMaxDays = 182

func statsWindow(q TicketStatsQuery, loc *time.Location) (time.Time, time.Time) {
	to := time.Now().In(loc)
	if e, err := time.ParseInLocation("2006-01-02", q.EndDate, loc); err == nil {
		to = e.AddDate(0, 0, 1)
	}
	from := to.AddDate(0, 0, -statsDefaultDays)
	if st, err := time.ParseInLocation("2006-01-02", q.StartDate, loc); err == nil {
		from = st
	}
	if to.Sub(from) > statsMaxDays*24*time.Hour {
		from = to.AddDate(0, 0, -statsMaxDays)
	}
	return from, to
}

func aggregateTickets(rows []ticketStatRow, loc *time.Location, from, to time.Time) TicketStats {
	out := TicketStats{}
	out.Sampled = int64(len(rows)) > statsScanLimit
	if out.Sampled {
		rows = rows[:statsScanLimit] // 先截断：所有分桶只统计被计入的工单
	}
	out.Total = int64(len(rows))
	hourless := map[string]*DayPoint{}
	status := map[string]int64{}
	priority := map[string]int64{}
	biz := map[string]int64{}
	assignee := map[string]int64{}
	respMinutes := make([]int64, 0, len(rows))
	type prioAgg struct{ total, breached int64 }
	prios := map[string]*prioAgg{}
	now := time.Now()
	for _, r := range rows {
		switch r.Status {
		case StatusOpen:
			out.Open++
		case StatusProcessing:
			out.Processing++
		case StatusResolved:
			out.Resolved++
		case StatusClosed:
			out.Closed++
		}
		if r.AssigneeID == 0 {
			out.Unassigned++
		}
		if r.Overdue {
			out.Overdue++
		}
		key := r.CreatedAt.In(loc).Format("2006-01-02")
		dp := hourless[key]
		if dp == nil {
			dp = &DayPoint{Date: key}
			hourless[key] = dp
		}
		dp.Created++
		if r.Status == StatusResolved || r.Status == StatusClosed {
			dp.Resolved++
		}
		inc(status, r.Status)
		inc(priority, r.Priority)
		inc(biz, r.BizType)
		inc(assignee, r.AssigneeName)
		if r.FirstReplyAt != nil {
			respMinutes = append(respMinutes, int64(r.FirstReplyAt.Sub(r.CreatedAt).Minutes()))
		} else {
			out.FirstResponse.NoResponse++
		}
		p := prios[r.Priority]
		if p == nil {
			p = &prioAgg{}
			prios[r.Priority] = p
		}
		p.total++
		// 违约判定：清扫器已标 overdue，或未响应且已过 SLA 时限（两种都算没达成）
		breached := r.Overdue
		if !breached && r.DueAt != nil && r.FirstReplyAt == nil && now.After(*r.DueAt) {
			breached = true
		}
		if breached {
			p.breached++
		}
	}
	out.Days = fillTicketDays(hourless, from, to, loc)
	out.ByStatus = topN(status, 8)
	out.ByPriority = topN(priority, 8)
	out.ByBizType = topN(biz, 8)
	out.ByAssignee = topN(assignee, 8)
	out.FirstResponse.Responded = int64(len(respMinutes))
	out.FirstResponse.AvgMinutes = avg(respMinutes)
	out.FirstResponse.P50Minutes = pick(respMinutes, 0.50)
	out.FirstResponse.P90Minutes = pick(respMinutes, 0.90)
	names := make([]string, 0, len(prios))
	for k := range prios {
		names = append(names, k)
	}
	sort.Strings(names)
	// 空结果必须是 []，不能让 nil 切片序列化成 null：前端直接 slaByPriority.map。
	out.SlaByPrio = make([]PrioritySLA, 0, len(names))
	for _, name := range names {
		p := prios[name]
		rate := 0.0
		if p.total > 0 {
			rate = round1(float64(p.breached) / float64(p.total) * 100)
		}
		out.SlaByPrio = append(out.SlaByPrio, PrioritySLA{
			Priority: name, TargetMinutes: int64(SLAFor(name).Minutes()),
			Total: p.total, Breached: p.breached, BreachRate: rate,
		})
	}
	return out
}

// fillTicketDays 补齐窗口内没有新建工单的日期，让折线 x 轴连续。
func fillTicketDays(bucket map[string]*DayPoint, from, to time.Time, loc *time.Location) []DayPoint {
	seen := map[string]bool{}
	for k := range bucket {
		seen[k] = true
	}
	out := make([]DayPoint, 0, len(bucket)+1)
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

func inc(m map[string]int64, key string) {
	if key == "" {
		key = "-"
	}
	m[key]++
}

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

func avg(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	var sum int64
	for _, v := range xs {
		sum += v
	}
	return sum / int64(len(xs))
}

// pick 取升序第 ceil(p*n) 个（最近秩）；空样本给 0，不编数。
func pick(xs []int64, p float64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := make([]int64, len(xs))
	copy(s, xs)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(float64(len(s))*p + 0.999999)
	if idx < 1 {
		idx = 1
	}
	if idx > len(s) {
		idx = len(s)
	}
	return s[idx-1]
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}

// Stats GET /plugins/ticket/tickets/stats：坐席队列与 SLA 概览。
func (h *handler) Stats(c *gin.Context) {
	q := TicketStatsQuery{
		Status:    c.Query("status"),
		Priority:  c.Query("priority"),
		BizType:   c.Query("bizType"),
		Keyword:   c.Query("keyword"),
		Overdue:   c.Query("overdue"),
		StartDate: c.Query("startDate"),
		EndDate:   c.Query("endDate"),
	}
	st, err := h.svc.Stats(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, st)
}
