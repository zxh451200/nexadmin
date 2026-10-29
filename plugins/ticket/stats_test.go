package ticket

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	v, err := time.Parse("2006-01-02 15:04:05Z", s+"Z")
	if err != nil {
		panic(err)
	}
	return v.UTC()
}

var utcLoc = time.UTC

func TestAggregateTicketsQueueAndDays(t *testing.T) {
	from, to := at("2026-10-21 00:00:00"), at("2026-10-24 00:00:00")
	rows := []ticketStatRow{
		{CreatedAt: at("2026-10-21 02:00:00"), Status: StatusOpen, Priority: PriorityUrgent, BizType: "order", AssigneeID: 0},
		{CreatedAt: at("2026-10-22 03:00:00"), Status: StatusResolved, Priority: PriorityNormal, BizType: "order", AssigneeID: 7, AssigneeName: "坐席A", FirstReplyAt: ptr(at("2026-10-22 04:00:00"))},
		{CreatedAt: at("2026-10-22 05:00:00"), Status: StatusClosed, Priority: PriorityNormal, BizType: "refund", AssigneeID: 8, AssigneeName: "坐席B", FirstReplyAt: ptr(at("2026-10-22 09:00:00"))},
	}
	st := aggregateTickets(rows, utcLoc, from, to)
	if st.Total != 3 || st.Open != 1 || st.Resolved != 1 || st.Closed != 1 {
		t.Fatalf("状态计数错误：%+v", st)
	}
	if st.Unassigned != 1 {
		t.Errorf("未认领应为 1，得到 %d", st.Unassigned)
	}
	// 窗口 21~23 三天：22 有两张新建、两张已解决（resolved+closed 都计入解决）。
	if len(st.Days) != 3 {
		t.Fatalf("按天应补齐空白天：%+v", st.Days)
	}
	if st.Days[1].Created != 2 || st.Days[1].Resolved != 2 {
		t.Errorf("10-22 分桶错误：%+v", st.Days[1])
	}
	if st.FirstResponse.Responded != 2 || st.FirstResponse.NoResponse != 1 {
		t.Errorf("首响计数错误：%+v", st.FirstResponse)
	}
	// 首响 60 与 240 分钟 → 均值 150，p50 取最近秩 60。
	if st.FirstResponse.AvgMinutes != 150 || st.FirstResponse.P50Minutes != 60 {
		t.Errorf("首响分布错误：%+v", st.FirstResponse)
	}
	if st.ByStatus[0] != (NameValue{StatusClosed, 1}) {
		t.Errorf("状态排行错误：%+v", st.ByStatus)
	}
}

// 违约判定两条路都要覆盖：清扫器已标 overdue；以及未响应且已过 due_at。
func TestAggregateTicketsSlaBreach(t *testing.T) {
	past := time.Now().Add(-2 * time.Hour)
	future := time.Now().Add(2 * time.Hour)
	rows := []ticketStatRow{
		{CreatedAt: at("2026-10-21 01:00:00"), Status: StatusOpen, Priority: PriorityHigh, Overdue: true},
		{CreatedAt: at("2026-10-21 02:00:00"), Status: StatusOpen, Priority: PriorityHigh, DueAt: ptr(past)},
		{CreatedAt: at("2026-10-21 03:00:00"), Status: StatusOpen, Priority: PriorityHigh, DueAt: ptr(future)},
		{CreatedAt: at("2026-10-21 04:00:00"), Status: StatusProcessing, Priority: PriorityHigh, DueAt: ptr(past), FirstReplyAt: ptr(at("2026-10-21 04:30:00"))},
	}
	st := aggregateTickets(rows, utcLoc, at("2026-10-21 00:00:00"), at("2026-10-22 00:00:00"))
	if st.Overdue != 1 {
		t.Errorf("overdue 计数应只数标记位：%+v", st)
	}
	if len(st.SlaByPrio) != 1 {
		t.Fatalf("SLA 分组错误：%+v", st.SlaByPrio)
	}
	row := st.SlaByPrio[0]
	if row.Priority != PriorityHigh || row.Total != 4 || row.Breached != 2 {
		t.Fatalf("违约数应为 2（已标 overdue + 未响应且过期），得到 %+v", row)
	}
	if row.BreachRate != 50 {
		t.Errorf("违约率应为 50%%，得到 %v", row.BreachRate)
	}
	if row.TargetMinutes != int64(SLAFor(PriorityHigh).Minutes()) {
		t.Errorf("目标时限应取 SLAFor，得到 %d", row.TargetMinutes)
	}
}

func TestAggregateTicketsSampledCapsEveryBucket(t *testing.T) {
	rows := make([]ticketStatRow, statsScanLimit+3)
	for i := range rows {
		rows[i] = ticketStatRow{CreatedAt: at("2026-10-21 01:00:00"), Status: StatusOpen, Priority: PriorityLow}
	}
	st := aggregateTickets(rows, utcLoc, at("2026-10-21 00:00:00"), at("2026-10-22 00:00:00"))
	if !st.Sampled {
		t.Fatal("超过上限必须标 sampled")
	}
	if st.Total != statsScanLimit || st.Open != statsScanLimit {
		t.Errorf("total 与分桶都必须被压到上限：total=%d open=%d", st.Total, st.Open)
	}
	if st.Days[0].Created != statsScanLimit {
		t.Errorf("按天计数不能统计被丢弃的样本，得到 %d", st.Days[0].Created)
	}
}

func TestTicketPickAndTopN(t *testing.T) {
	xs := make([]int64, 100)
	for i := range xs {
		xs[i] = int64(i + 1)
	}
	if pick(xs, 0.5) != 50 || pick(xs, 0.9) != 90 || pick(nil, 0.9) != 0 {
		t.Errorf("分位数错误：p50=%d p90=%d 空=%d", pick(xs, 0.5), pick(xs, 0.9), pick(nil, 0.9))
	}
	if pick([]int64{7}, 0.9) != 7 {
		t.Error("单样本各分位必须落在唯一样本上")
	}
	// 乱序输入不得改变结果。
	if pick([]int64{1, 2, 3}, 0.5) != 2 || pick([]int64{1, 2, 3}, 0.9) != 3 {
		t.Error("n*p 非整数时必须按最近秩向上取整")
	}
	if pick([]int64{3, 1, 2}, 0.5) != pick([]int64{1, 2, 3}, 0.5) {
		t.Error("pick 依赖调用方排序")
	}
	items := topN(map[string]int64{"b": 5, "a": 5, "c": 9}, 2)
	if len(items) != 2 || items[0] != (NameValue{"c", 9}) || items[1] != (NameValue{"a", 5}) {
		t.Errorf("topN 排序/截断错误：%+v", items)
	}
	m := map[string]int64{}
	inc(m, "")
	if topN(m, 1)[0].Name != "-" {
		t.Error("空名字必须归到占位符")
	}
}

// 时区不是摆设：同一 UTC 时刻在东八区落到下一个自然日。
func TestAggregateTicketsHonoursLocation(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	rows := []ticketStatRow{{CreatedAt: at("2026-10-21 17:00:00"), Status: StatusOpen, Priority: PriorityNormal}}
	st := aggregateTickets(rows, cst, at("2026-10-21 00:00:00"), at("2026-10-23 00:00:00"))
	var found bool
	for _, d := range st.Days {
		if d.Date == "2026-10-22" && d.Created == 1 {
			found = true
		}
		if d.Date == "2026-10-21" && d.Created != 0 {
			t.Errorf("17Z 在东八区不该算进 10-21：%+v", st.Days)
		}
	}
	if !found {
		t.Errorf("17Z 在东八区应归到 10-22：%+v", st.Days)
	}
}

func TestStatsWindowDefaultAndCap(t *testing.T) {
	from, to := statsWindow(TicketStatsQuery{}, utcLoc)
	if d := int(to.Sub(from).Hours() / 24); d != statsDefaultDays {
		t.Errorf("默认窗口 %d 天，期望 %d", d, statsDefaultDays)
	}
	from, to = statsWindow(TicketStatsQuery{StartDate: "2000-01-01", EndDate: "2026-10-20"}, utcLoc)
	if d := int(to.Sub(from).Hours() / 24); d > statsMaxDays {
		t.Errorf("窗口必须压到 %d 天内，得到 %d", statsMaxDays, d)
	}
}

// 同 pay：空聚合要给出 []，否则前端的 slaByPriority.map / RankBars 直接读到 null 崩页。
func TestAggregateTicketsEmptyMarshalsAsArrays(t *testing.T) {
	from, to := at("2026-10-21 00:00:00"), at("2026-10-24 00:00:00")
	raw, err := json.Marshal(aggregateTickets(nil, utcLoc, from, to))
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	s := string(raw)
	for _, want := range []string{`"slaByPriority":[]`, `"byStatus":[]`, `"byAssignee":[]`, `"days":[`} {
		if !strings.Contains(s, want) {
			t.Errorf("空聚合应为 %s，实际：%s", want, s)
		}
	}
	if strings.Contains(s, "null") {
		t.Errorf("空聚合不应出现 null：%s", s)
	}
}
