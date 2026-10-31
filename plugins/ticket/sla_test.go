package ticket

import (
	"testing"
	"time"
)

// 7×24 直接相加；工作日 09:00-18:00 要跳过下班与周末。
func TestBusinessDue(t *testing.T) {
	all := DefaultSLA()
	sat := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC) // 周六
	if got := businessDue(sat, 120, all, time.UTC); !got.Equal(sat.Add(2 * time.Hour)) {
		t.Fatalf("7x24 should add straight, got %v", got)
	}

	cal := SLASetting{
		Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 9 * 60, EndMinute: 18 * 60,
		Targets: DefaultSLA().Targets,
	}
	// 周一 16:00 + 180 分钟 = 周一 120 + 周二 60 → 周二 10:00
	mon := time.Date(2026, 10, 26, 16, 0, 0, 0, time.UTC)
	if got := businessDue(mon, 180, cal, time.UTC); !got.Equal(time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("cross-day due = %v", got)
	}
	// 周五 17:00 + 120 分钟 = 周五 60 + 周一 60 → 周一 10:00
	fri := time.Date(2026, 10, 23, 17, 0, 0, 0, time.UTC)
	if got := businessDue(fri, 120, cal, time.UTC); !got.Equal(time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("weekend skip due = %v", got)
	}
	// 上班前开始：先对齐到 09:00
	early := time.Date(2026, 10, 26, 7, 0, 0, 0, time.UTC)
	if got := businessDue(early, 60, cal, time.UTC); !got.Equal(time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("before-hours due = %v", got)
	}
	// 周六开始：先跳到周一 09:00
	if got := businessDue(sat, 60, cal, time.UTC); !got.Equal(time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("weekend start due = %v", got)
	}
}

func TestSLANormalize(t *testing.T) {
	v := SLASetting{
		Workdays: []int{5, 1, 3, 1}, StartMinute: 540, EndMinute: 1080,
		Targets: map[string]SLATarget{PriorityUrgent: {FirstMinutes: 60, ResolutionMinutes: 120}},
	}
	if err := v.normalize(); err != nil {
		t.Fatal(err)
	}
	if len(v.Workdays) != 3 || v.Workdays[0] != 1 || v.Workdays[2] != 5 {
		t.Fatalf("workdays not normalized: %+v", v.Workdays)
	}
	if v.Targets[PriorityUrgent].ResolutionMinutes != 120 {
		t.Fatalf("explicit target lost: %+v", v.Targets)
	}
	if v.Targets[PriorityNormal].FirstMinutes == 0 {
		t.Fatal("missing priorities should be filled from defaults")
	}
	if v.TargetFor("nonsense").FirstMinutes == 0 {
		t.Fatal("unknown priority should fall back to normal")
	}

	bad := []SLASetting{
		{Workdays: nil, StartMinute: 0, EndMinute: 1440},
		{Workdays: []int{1}, StartMinute: 600, EndMinute: 600},
		{Workdays: []int{1}, StartMinute: 0, EndMinute: 1440, Targets: map[string]SLATarget{PriorityHigh: {FirstMinutes: 100, ResolutionMinutes: 50}}},
		{Workdays: []int{9}, StartMinute: 0, EndMinute: 1440},
	}
	for i := range bad {
		if err := bad[i].normalize(); err == nil {
			t.Fatalf("invalid setting %d accepted", i)
		}
	}
}

// 解决时限判定：暂停时长要先补回再比。
func TestResolutionBreached(t *testing.T) {
	due := testNow.Add(-time.Hour)
	if !resolutionBreached(&due, nil, testNow) {
		t.Fatal("past due should be breached")
	}
	future := testNow.Add(time.Hour)
	if resolutionBreached(&future, nil, testNow) {
		t.Fatal("future due should not be breached")
	}
	if resolutionBreached(nil, nil, testNow) {
		t.Fatal("missing due should not be breached")
	}
	// 暂停 2 小时：到期时间后移到未来 → 不算违约
	paused := testNow.Add(-2 * time.Hour)
	if resolutionBreached(&due, &paused, testNow) {
		t.Fatal("paused duration must be credited back")
	}
}
