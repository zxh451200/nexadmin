package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 标准 5 段 cron 解析（分 时 日 月 周），只用标准库。
//
// 每段支持：  *   a   a,b,c   a-b   */n   a/n（a 到上限，步长 n）   a-b/n
// 周：0-7（0 和 7 都是周日）。
// 日与周都被限制（都不是 *）时按经典 cron 语义取"或"：日匹配或周匹配即可。

type field struct {
	min, max int
}

var (
	fMinute = field{0, 59}
	fHour   = field{0, 23}
	fDom    = field{1, 31}
	fMonth  = field{1, 12}
	fDow    = field{0, 7}
)

// Schedule 是解析后的 cron 表达式，位图存放各段允许的取值。
type Schedule struct {
	expr              string
	minute, hour, dom uint64
	month, dow        uint64
	domStar, dowStar  bool // 日 / 周是否为 "*"（用于"或"语义）
}

// Expr 返回规范化后的原始表达式。
func (s *Schedule) Expr() string { return s.expr }

// ParseCron 解析 5 段 cron 表达式。
func ParseCron(expr string) (*Schedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron: expression must have 5 fields (min hour dom month dow), got %d", len(parts))
	}
	s := &Schedule{expr: strings.Join(parts, " ")}
	var err error
	if s.minute, err = parseField(parts[0], fMinute, "minute"); err != nil {
		return nil, err
	}
	if s.hour, err = parseField(parts[1], fHour, "hour"); err != nil {
		return nil, err
	}
	if s.dom, err = parseField(parts[2], fDom, "day-of-month"); err != nil {
		return nil, err
	}
	if s.month, err = parseField(parts[3], fMonth, "month"); err != nil {
		return nil, err
	}
	if s.dow, err = parseField(parts[4], fDow, "day-of-week"); err != nil {
		return nil, err
	}
	if s.dow&(1<<7) != 0 { // 7 == 周日
		s.dow |= 1
		s.dow &^= 1 << 7
	}
	s.domStar = isStar(parts[2])
	s.dowStar = isStar(parts[4])
	return s, nil
}

func isStar(p string) bool { return p == "*" || p == "*/1" }

func parseField(p string, f field, name string) (uint64, error) {
	var bits uint64
	for _, item := range strings.Split(p, ",") {
		if item == "" {
			return 0, fmt.Errorf("cron: empty item in %s field", name)
		}
		rng, stepStr, hasStep := strings.Cut(item, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepStr)
			if err != nil || n < 1 {
				return 0, fmt.Errorf("cron: invalid step %q in %s field", stepStr, name)
			}
			step = n
		}
		lo, hi := f.min, f.max
		switch {
		case rng == "*":
		case strings.Contains(rng, "-"):
			a, b, _ := strings.Cut(rng, "-")
			var err error
			if lo, err = atoiIn(a, f, name); err != nil {
				return 0, err
			}
			if hi, err = atoiIn(b, f, name); err != nil {
				return 0, err
			}
			if lo > hi {
				return 0, fmt.Errorf("cron: range %q is reversed in %s field", rng, name)
			}
		default:
			v, err := atoiIn(rng, f, name)
			if err != nil {
				return 0, err
			}
			lo = v
			if hasStep { // a/n：从 a 到上限
				hi = f.max
			} else {
				hi = v
			}
		}
		for v := lo; v <= hi; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, nil
}

func atoiIn(s string, f field, name string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("cron: invalid value %q in %s field", s, name)
	}
	if v < f.min || v > f.max {
		return 0, fmt.Errorf("cron: value %d out of range %d-%d in %s field", v, f.min, f.max, name)
	}
	return v, nil
}

func has(bits uint64, v int) bool { return bits&(1<<uint(v)) != 0 }

func (s *Schedule) dayMatches(t time.Time) bool {
	domOK := has(s.dom, t.Day())
	dowOK := has(s.dow, int(t.Weekday()))
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dowOK
	case s.dowStar:
		return domOK
	default:
		return domOK || dowOK
	}
}

// Matches 判断 t（精确到分钟）是否命中该表达式。
func (s *Schedule) Matches(t time.Time) bool {
	return has(s.minute, t.Minute()) && has(s.hour, t.Hour()) && has(s.month, int(t.Month())) && s.dayMatches(t)
}

// Next 返回严格晚于 from 的下一次执行时间（精确到分钟，沿用 from 的时区）；
// 5 年内找不到（如 2 月 30 日）时返回零值。
func (s *Schedule) Next(from time.Time) time.Time {
	t := from.Truncate(time.Minute).Add(time.Minute)
	limit := from.AddDate(5, 0, 0)
	for t.Before(limit) {
		switch {
		case !has(s.month, int(t.Month())):
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
		case !s.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
		case !has(s.hour, t.Hour()):
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
		case !has(s.minute, t.Minute()):
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}
