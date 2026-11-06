package report

// 同环比的纯函数闸：窗口识别、区间平移、百分比组装——都不碰数据库。

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, ok := parseTime(s)
	if !ok {
		t.Fatalf("parseTime(%q) failed", s)
	}
	return v
}

// 从 >= 下界 + < 上界推出窗口（报表 dateRange 筛选器的落法）。
func TestCurrentWindowFromRangeFilters(t *testing.T) {
	tg := &Target{cols: map[string]Column{
		"created_at": {Name: "created_at", Type: TypeTime},
		"amount":     {Name: "amount", Type: TypeNumber},
	}}
	filters := []Filter{
		{Column: "created_at", Op: ">=", Value: "2026-05-01T00:00:00Z"},
		{Column: "created_at", Op: "<", Value: "2026-06-01T00:00:00Z"},
		{Column: "amount", Op: "between", Value: []any{1.0, 100.0}},
	}
	col, win := currentWindow(tg, filters)
	if col != "created_at" || win == nil {
		t.Fatalf("want created_at window, got col=%q win=%v", col, win)
	}
	if !win.lo.Equal(mustTime(t, "2026-05-01T00:00:00Z")) || !win.hi.Equal(mustTime(t, "2026-06-01T00:00:00Z")) {
		t.Fatalf("window bounds wrong: %v..%v", win.lo, win.hi)
	}
}

// 只有一侧时间条件推不出窗口：按无对比处理。
func TestCurrentWindowNeedsBothBounds(t *testing.T) {
	tg := &Target{cols: map[string]Column{"created_at": {Name: "created_at", Type: TypeTime}}}
	filters := []Filter{{Column: "created_at", Op: ">=", Value: "2026-05-01T00:00:00Z"}}
	if _, win := currentWindow(tg, filters); win != nil {
		t.Fatal("single bound must not produce a window")
	}
}

// 数字列的 between 是金额区间，不是时间窗。
func TestCurrentWindowIgnoresNumberColumns(t *testing.T) {
	tg := &Target{cols: map[string]Column{"amount": {Name: "amount", Type: TypeNumber}}}
	filters := []Filter{{Column: "amount", Op: "between", Value: []any{1.0, 100.0}}}
	if _, win := currentWindow(tg, filters); win != nil {
		t.Fatal("numeric between must not be a time window")
	}
}

// 环比：区间整体平移一个长度；其它列的条件不动。
func TestShiftWindowPeriod(t *testing.T) {
	win := &timeWindow{lo: mustTime(t, "2026-05-01T00:00:00Z"), hi: mustTime(t, "2026-06-01T00:00:00Z")}
	filters := []Filter{
		{Column: "created_at", Op: ">=", Value: "2026-05-01T00:00:00Z"},
		{Column: "created_at", Op: "<", Value: "2026-06-01T00:00:00Z"},
		{Column: "status", Op: "=", Value: "paid"},
	}
	shifted := shiftWindowFilters(filters, "created_at", win, ComparePeriod)
	// [5-01, 6-01) 是 31 天：等长回移得到 [3-31, 5-01)（4 月只有 30 天，这就是紧挨着的上一窗）。
	if shifted[0].Value != "2026-03-31T00:00:00Z" || shifted[1].Value != "2026-05-01T00:00:00Z" {
		t.Fatalf("period shift wrong: %v .. %v", shifted[0].Value, shifted[1].Value)
	}
	if shifted[2].Value != "paid" {
		t.Fatal("non-time filter must be untouched")
	}
	// 原 spec 不被就地修改。
	if filters[0].Value != "2026-05-01T00:00:00Z" {
		t.Fatal("original filters must not be mutated")
	}
}

// 同比：去年同区间（跨闰年按日历回退一年）。
func TestShiftWindowYear(t *testing.T) {
	win := &timeWindow{lo: mustTime(t, "2026-03-01T00:00:00Z"), hi: mustTime(t, "2026-04-01T00:00:00Z")}
	filters := []Filter{
		{Column: "created_at", Op: ">=", Value: "2026-03-01T00:00:00Z"},
		{Column: "created_at", Op: "<", Value: "2026-04-01T00:00:00Z"},
	}
	shifted := shiftWindowFilters(filters, "created_at", win, CompareYear)
	if shifted[0].Value != "2025-03-01T00:00:00Z" || shifted[1].Value != "2025-04-01T00:00:00Z" {
		t.Fatalf("year shift wrong: %v .. %v", shifted[0].Value, shifted[1].Value)
	}
}

// 百分比：正增长、负增长各留一位小数；上期为 0 不给百分比；上期缺失不带 Previous。
func TestCompareInfoMath(t *testing.T) {
	delta := *compareInfo(ComparePeriod, 120, 100, true)
	if delta.Current != 120 || *delta.Previous != 100 || *delta.DeltaPct != 20 {
		t.Fatalf("delta wrong: %+v", delta)
	}
	delta = *compareInfo(ComparePeriod, 99, 132, true)
	if *delta.DeltaPct != -25 {
		t.Fatalf("negative delta wrong: %v", *delta.DeltaPct)
	}
	info := compareInfo(ComparePeriod, 50, 0, true)
	if info.Previous == nil || info.DeltaPct != nil {
		t.Fatalf("prev=0 must keep Previous but drop DeltaPct: %+v", info)
	}
	info = compareInfo(ComparePeriod, 50, 0, false)
	if info.Previous != nil || info.DeltaPct != nil {
		t.Fatalf("no previous row must omit Previous: %+v", info)
	}
	// 上期为负（如亏损转盈）分母取绝对值：-100 → 120 是 +220%。
	delta = *compareInfo(ComparePeriod, 120, -100, true)
	if *delta.DeltaPct != 220 {
		t.Fatalf("negative-base delta wrong: %v", *delta.DeltaPct)
	}
}

// metricValue 只认 m0；空结果算 0。
func TestMetricValue(t *testing.T) {
	if v := metricValue(&Result{Rows: []map[string]any{{"m0": int64(7)}}}); v != 7 {
		t.Fatalf("want 7, got %v", v)
	}
	if v := metricValue(&Result{Rows: []map[string]any{}}); v != 0 {
		t.Fatalf("empty rows must be 0, got %v", v)
	}
}
