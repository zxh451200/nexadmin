package cron

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.UTC)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseCronInvalid(t *testing.T) {
	bad := []string{
		"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8",
		"5-1 * * * *", "*/0 * * * *", "a * * * *", "1,,2 * * * *", "*/x * * * *", "-1 * * * *",
	}
	for _, e := range bad {
		if _, err := ParseCron(e); err == nil {
			t.Errorf("ParseCron(%q) should fail", e)
		}
	}
}

func TestParseCronValid(t *testing.T) {
	ok := []string{"* * * * *", "*/5 * * * *", "0 3 * * 1", "0 0 1,15 * *", "10-20/2 8-18 * 1-6 1-5", "0 0 * * 7", "5/15 * * * *"}
	for _, e := range ok {
		if _, err := ParseCron(e); err != nil {
			t.Errorf("ParseCron(%q): %v", e, err)
		}
	}
}

func TestNext(t *testing.T) {
	cases := []struct{ expr, from, want string }{
		{"* * * * *", "2026-01-01 00:00", "2026-01-01 00:01"},
		{"*/15 * * * *", "2026-01-01 10:07", "2026-01-01 10:15"},
		{"*/15 * * * *", "2026-01-01 10:45", "2026-01-01 11:00"},
		{"0 3 * * *", "2026-01-01 03:00", "2026-01-02 03:00"}, // 严格晚于 from
		{"30 2 * * *", "2026-01-01 23:59", "2026-01-02 02:30"},
		{"0 0 1 * *", "2026-01-15 12:00", "2026-02-01 00:00"},
		{"0 0 1 1 *", "2026-01-01 00:00", "2027-01-01 00:00"},
		{"0 9 * * 1", "2026-10-17 10:00", "2026-10-19 09:00"},   // 2026-10-17 是周六，下个周一
		{"0 9 * * 7", "2026-10-17 10:00", "2026-10-18 09:00"},   // 7 = 周日
		{"0 9 * * 0", "2026-10-17 10:00", "2026-10-18 09:00"},   // 0 = 周日
		{"0 9 * * 1-5", "2026-10-16 10:00", "2026-10-19 09:00"}, // 周五之后是周一
		{"0 0 29 2 *", "2026-03-01 00:00", "2028-02-29 00:00"},  // 跨闰年
		{"5/20 * * * *", "2026-01-01 00:00", "2026-01-01 00:05"},
		{"5/20 * * * *", "2026-01-01 00:26", "2026-01-01 00:45"},
		{"0 0 1,15 * *", "2026-01-02 00:00", "2026-01-15 00:00"},
		// 日与周都限制时取"或"：每月 13 日，或者周五
		{"0 0 13 * 5", "2026-10-01 00:00", "2026-10-02 00:00"},
		{"0 0 13 * 5", "2026-10-10 00:00", "2026-10-13 00:00"},
	}
	for _, c := range cases {
		s, err := ParseCron(c.expr)
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		if got := s.Next(at(c.from)); !got.Equal(at(c.want)) {
			t.Errorf("Next(%q, %s) = %s, want %s", c.expr, c.from, got.Format("2006-01-02 15:04"), c.want)
		}
	}
}

func TestNextImpossible(t *testing.T) {
	s, err := ParseCron("0 0 30 2 *") // 2 月 30 日永不存在
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Next(at("2026-01-01 00:00")); !got.IsZero() {
		t.Errorf("expected zero time, got %s", got)
	}
}

func TestMatches(t *testing.T) {
	s, _ := ParseCron("*/10 8-9 * * 1-5")
	if !s.Matches(at("2026-10-19 08:20")) {
		t.Error("Monday 08:20 should match")
	}
	if s.Matches(at("2026-10-19 08:25")) {
		t.Error("08:25 should not match")
	}
	if s.Matches(at("2026-10-18 08:20")) {
		t.Error("Sunday should not match")
	}
}
