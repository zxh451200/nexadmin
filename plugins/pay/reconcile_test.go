package pay

import (
	"testing"
	"time"
)

func TestPayDayBoundsAppTimezone(t *testing.T) {
	s := (&Service{}).WithTimezone("Asia/Shanghai")
	start, end, err := s.dayBounds("2026-10-30")
	if err != nil {
		t.Fatalf("dayBounds: %v", err)
	}
	wantStart := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 10, 30, 16, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("东八区账单日边界错误：start=%s end=%s，want %s ~ %s", start, end, wantStart, wantEnd)
	}

	utc := (&Service{}).WithTimezone("not-a-timezone")
	ustart, _, err := utc.dayBounds("2026-10-30")
	if err != nil {
		t.Fatalf("dayBounds UTC fallback: %v", err)
	}
	if !ustart.Equal(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("非法时区应退回 UTC：got %s", ustart)
	}
}
