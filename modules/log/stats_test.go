package log

import (
	"testing"
	"time"
)

func fixedTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse("2006-01-02 15:04:05Z", s+"Z")
	if err != nil {
		t.Fatalf("bad time %q: %v", s, err)
	}
	return v.UTC()
}

var utc = time.UTC

func TestPercentileKnownSample(t *testing.T) {
	costs := make([]int64, 100)
	for i := range costs {
		costs[i] = int64(i + 1) // 1..100
	}
	got := percentile(costs)
	want := Latency{Avg: 50, P50: 50, P95: 95, P99: 99, Max: 100}
	if got != want {
		t.Fatalf("percentile(1..100) = %+v, want %+v", got, want)
	}
}

func TestPercentileEmptyAndSingle(t *testing.T) {
	if got := percentile(nil); got != (Latency{}) {
		t.Errorf("空样本必须全 0，得到 %+v", got)
	}
	single := percentile([]int64{42})
	if single != (Latency{Avg: 42, P50: 42, P95: 42, P99: 42, Max: 42}) {
		t.Errorf("单样本各分位都应落在唯一样本上，得到 %+v", single)
	}
}

// 分位数必须是「输入乱序也不影响结果」：排序在函数内部完成，不依赖调用方。
// n*p 不是整数时，最近秩（ceil）与下取整必须能区分开——否则取整方向写反也照不出来。
func TestPercentileRoundsUpToNearestRank(t *testing.T) {
	got := percentile([]int64{1, 2, 3})
	if got.P50 != 2 || got.P95 != 3 || got.P99 != 3 {
		t.Errorf("三样本分位错误（应为 p50=2、p95=3）：%+v", got)
	}
}

func TestPercentileIgnoresInputOrder(t *testing.T) {
	ordered := percentile([]int64{1, 2, 3, 4, 5})
	shuffled := percentile([]int64{5, 1, 4, 2, 3})
	if ordered != shuffled {
		t.Errorf("乱序输入改变了分位：乱序 %+v，升序 %+v", shuffled, ordered)
	}
}

func TestTopNOrdersAndBreaksTiesDeterministically(t *testing.T) {
	m := map[string]int64{"b": 5, "a": 5, "c": 9, "d": 1}
	got := topN(m, 3)
	want := []NameValue{{"c", 9}, {"a", 5}, {"b", 5}}
	if len(got) != len(want) {
		t.Fatalf("topN 截断错误：%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("topN = %+v, want %+v（同数必须按名字升序）", got, want)
		}
	}
	m2 := map[string]int64{}
	inc(m2, "")
	inc(m2, "")
	if out := topN(m2, 5); len(out) != 1 || out[0] != (NameValue{"-", 2}) {
		t.Errorf("空名字必须归到占位符 -，得到 %+v", out)
	}
}

func TestAggregateOperCountsAndBuckets(t *testing.T) {
	from := fixedTime(t, "2026-10-21 00:00:00")
	to := fixedTime(t, "2026-10-24 00:00:00")
	rows := []operStatRow{
		{CreatedAt: fixedTime(t, "2026-10-21 09:15:00"), CostMs: 10, Status: StatusSuccess, Module: "user", Type: "query", Method: "GET", URL: "/a", Operator: "admin"},
		{CreatedAt: fixedTime(t, "2026-10-21 09:20:00"), CostMs: 30, Status: StatusFailed, Module: "user", Type: "update", Method: "POST", URL: "/b", Operator: "admin"},
		{CreatedAt: fixedTime(t, "2026-10-22 23:59:00"), CostMs: 90, Status: StatusSuccess, Module: "role", Type: "query", Method: "GET", URL: "/a", Operator: "auditor"},
	}
	st := aggregateOper(rows, utc, from, to)
	if st.Total != 3 || st.Failures != 1 {
		t.Fatalf("计数错误：total=%d failures=%d", st.Total, st.Failures)
	}
	if st.Sampled {
		t.Error("未超过扫描上限不该标 sampled")
	}
	// 三天窗口：21、22 有数据，23 是空白天也必须出现（折线图 x 轴连续）。
	if len(st.Days) != 3 {
		t.Fatalf("按天分桶应含空白天，得到 %+v", st.Days)
	}
	if st.Days[0].Total != 2 || st.Days[0].Failures != 1 || st.Days[1].Total != 1 || st.Days[2].Total != 0 {
		t.Errorf("按天计数错误：%+v", st.Days)
	}
	if st.Hours[9].Value != 2 || st.Hours[23].Value != 1 || st.Hours[0].Value != 0 {
		t.Errorf("时段分桶错误：%+v", st.Hours)
	}
	if st.TopModules[0] != (NameValue{"user", 2}) {
		t.Errorf("模块排行错误：%+v", st.TopModules)
	}
	// /a 两次 10+90 → 平均 50；/b 一次 30。最慢必须是 /a。
	if len(st.Slowest) != 2 || st.Slowest[0].URL != "/a" || st.Slowest[0].AvgMs != 50 || st.Slowest[0].Count != 2 {
		t.Fatalf("接口平均耗时错误：%+v", st.Slowest)
	}
	if st.Latency != (Latency{Avg: 43, P50: 30, P95: 90, P99: 90, Max: 90}) {
		t.Errorf("耗时分布错误：%+v", st.Latency)
	}
}

func TestAggregateOperMarksSampledAndCapsTotal(t *testing.T) {
	rows := make([]operStatRow, statsScanLimit+5)
	for i := range rows {
		rows[i] = operStatRow{CreatedAt: fixedTime(t, "2026-10-21 01:00:00"), Status: StatusSuccess, Module: "m", URL: "/u"}
	}
	st := aggregateOper(rows, utc, fixedTime(t, "2026-10-21 00:00:00"), fixedTime(t, "2026-10-22 00:00:00"))
	if !st.Sampled {
		t.Fatal("超过上限必须如实标 sampled=true，不能假装是全量")
	}
	if st.Total != statsScanLimit {
		t.Errorf("total 应被压到上限，得到 %d", st.Total)
	}
	if st.Days[0].Total != statsScanLimit {
		t.Errorf("按天计数也应只统计被计入的样本，得到 %d", st.Days[0].Total)
	}
}

func TestAggregateLoginSeparatesFailures(t *testing.T) {
	rows := []loginStatRow{
		{CreatedAt: fixedTime(t, "2026-10-21 08:00:00"), LoginType: "password", Browser: "Chrome", OS: "Mac", Status: StatusSuccess},
		{CreatedAt: fixedTime(t, "2026-10-21 08:10:00"), LoginType: "password", Browser: "Edge", OS: "Win", Status: StatusFailed, FailReason: "bad_password"},
		{CreatedAt: fixedTime(t, "2026-10-21 08:20:00"), LoginType: "oauth", Browser: "Chrome", OS: "Mac", Status: StatusFailed, FailReason: "expired_code"},
	}
	st := aggregateLogin(rows, utc, fixedTime(t, "2026-10-21 00:00:00"), fixedTime(t, "2026-10-22 00:00:00"))
	if st.Total != 3 || st.Failures != 2 {
		t.Fatalf("登录计数错误：total=%d failures=%d", st.Total, st.Failures)
	}
	// 失败原因只能统计失败行：两条各 1，且按名字升序稳定输出。
	if len(st.FailReasons) != 2 || st.FailReasons[0] != (NameValue{"bad_password", 1}) || st.FailReasons[1] != (NameValue{"expired_code", 1}) {
		t.Errorf("失败原因分布错误：%+v", st.FailReasons)
	}
	if st.Types[0] != (NameValue{"password", 2}) {
		t.Errorf("登录方式分布错误：%+v", st.Types)
	}
	if st.Browsers[0] != (NameValue{"Chrome", 2}) {
		t.Errorf("浏览器分布错误：%+v", st.Browsers)
	}
	if st.Days[0].Total != 3 || st.Days[0].Failures != 2 {
		t.Errorf("按天计数错误：%+v", st.Days)
	}
}

// 窗口外仍有数据时（例如只给了 startDate）不能丢数。
func TestFillDaysKeepsRowsOutsideWindow(t *testing.T) {
	from := fixedTime(t, "2026-10-21 00:00:00")
	to := fixedTime(t, "2026-10-21 12:00:00")
	rows := []loginStatRow{
		{CreatedAt: fixedTime(t, "2026-10-20 23:00:00"), Status: StatusSuccess},
		{CreatedAt: fixedTime(t, "2026-10-21 01:00:00"), Status: StatusSuccess},
	}
	st := aggregateLogin(rows, utc, from, to)
	if len(st.Days) != 2 {
		t.Fatalf("窗口外的数据被丢了：%+v", st.Days)
	}
	if st.Days[0].Date != "2026-10-20" || st.Days[1].Date != "2026-10-21" {
		t.Errorf("日期必须升序：%+v", st.Days)
	}
}

// 时区不是摆设：同一 UTC 时刻在东八区会落到下一个自然日与不同小时。
func TestAggregateHonoursLocation(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	rows := []loginStatRow{{CreatedAt: fixedTime(t, "2026-10-21 17:00:00"), Status: StatusSuccess}}
	st := aggregateLogin(rows, cst, fixedTime(t, "2026-10-21 00:00:00"), fixedTime(t, "2026-10-23 00:00:00"))
	var day *DayPoint
	for i := range st.Days {
		if st.Days[i].Date == "2026-10-22" {
			day = &st.Days[i]
		}
	}
	if day == nil || day.Total != 1 {
		t.Errorf("按东八区 17Z 应归到 10-22，得到 %+v", st.Days)
	}
	if st.Hours[1].Value != 1 {
		t.Errorf("17Z 在东八区是凌晨 1 点，得到 %+v", st.Hours)
	}
}

func TestStatsWindowDefaultsAndCap(t *testing.T) {
	s := &Service{loc: utc}
	from, to := s.statsWindow(Query{})
	if days := int(to.Sub(from).Hours() / 24); days != statsDefaultDays {
		t.Errorf("默认窗口应为 %d 天，得到 %d", statsDefaultDays, days)
	}
	from, to = s.statsWindow(Query{StartDate: "2020-01-01", EndDate: "2026-10-20"})
	if days := int(to.Sub(from).Hours() / 24); days > statsMaxDays {
		t.Errorf("窗口必须被压到 %d 天内，得到 %d", statsMaxDays, days)
	}
	if _, err := time.Parse("2006-01-02", from.Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
}
