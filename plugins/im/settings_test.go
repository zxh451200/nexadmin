package im

import (
	"testing"
	"time"
)

func at(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
}

func TestOnlineAtDisabledIsAlwaysOn(t *testing.T) {
	st := Settings{BusinessHours: false}
	if !st.OnlineAt(at(2026, 10, 31, 3, 0)) { // 周六凌晨
		t.Fatal("business hours disabled must be online 24x7")
	}
}

func TestOnlineAtWorkdaysAndHours(t *testing.T) {
	st := Settings{
		BusinessHours: true, HoursStart: "09:00", HoursEnd: "18:00",
		Workdays: []int{1, 2, 3, 4, 5}, Timezone: "UTC",
	}
	cases := []struct {
		t    time.Time
		want bool
	}{
		{at(2026, 10, 26, 9, 0), true},   // 周一 09:00
		{at(2026, 10, 26, 17, 59), true}, // 周一 17:59
		{at(2026, 10, 26, 18, 0), false}, // 周一 18:00 打烊
		{at(2026, 10, 26, 8, 59), false}, // 周一 08:59
		{at(2026, 10, 31, 12, 0), false}, // 周六
		{at(2026, 11, 1, 12, 0), false},  // 周日
	}
	for _, c := range cases {
		if got := st.OnlineAt(c.t); got != c.want {
			t.Fatalf("%s => %v, want %v", c.t, got, c.want)
		}
	}
}

func TestOnlineAtCrossMidnight(t *testing.T) {
	st := Settings{BusinessHours: true, HoursStart: "22:00", HoursEnd: "02:00", Workdays: []int{1, 2, 3, 4, 5, 6, 7}, Timezone: "UTC"}
	if !st.OnlineAt(at(2026, 10, 26, 23, 0)) {
		t.Fatal("23:00 should be online")
	}
	if !st.OnlineAt(at(2026, 10, 26, 1, 0)) {
		t.Fatal("01:00 should be online")
	}
	if st.OnlineAt(at(2026, 10, 26, 12, 0)) {
		t.Fatal("12:00 should be offline")
	}
}

func TestOnlineAtUsesTimezone(t *testing.T) {
	st := Settings{BusinessHours: true, HoursStart: "09:00", HoursEnd: "18:00", Workdays: []int{1, 2, 3, 4, 5}, Timezone: "Asia/Shanghai"}
	// 2026-10-26 02:00 UTC = 10:00 上海（周一）
	if !st.OnlineAt(at(2026, 10, 26, 2, 0)) {
		t.Fatal("timezone conversion not applied")
	}
	// 2026-10-26 12:00 UTC = 20:00 上海
	if st.OnlineAt(at(2026, 10, 26, 12, 0)) {
		t.Fatal("20:00 Shanghai should be offline")
	}
}

func TestNormalizeSettings(t *testing.T) {
	st := Settings{Workdays: []int{5, 1, 5, 9, 0}}
	normalizeSettings(&st)
	if len(st.Workdays) != 2 || st.Workdays[0] != 1 || st.Workdays[1] != 5 {
		t.Fatalf("workdays = %v", st.Workdays)
	}
	if st.HoursStart != "09:00" || st.HoursEnd != "18:00" || st.Timezone != "Asia/Shanghai" {
		t.Fatalf("defaults not applied: %+v", st)
	}
}

// 敏感词：去空去重（大小写不敏感）、命中替换为等字符数的 *。
func TestMaskBanned(t *testing.T) {
	words := normalizeWords([]string{" 发票 ", "Invoice", "invoice", ""})
	if len(words) != 2 || words[0] != "发票" || words[1] != "Invoice" {
		t.Fatalf("normalize = %v", words)
	}
	if got := maskBanned("我要发票和 INVOICE，谢谢", words); got != "我要**和 *******，谢谢" {
		t.Fatalf("mask = %q", got)
	}
	if got := maskBanned("正常消息", words); got != "正常消息" {
		t.Fatalf("untouched = %q", got)
	}
	if got := maskBanned("发票", nil); got != "发票" {
		t.Fatalf("no words = %q", got)
	}
}
