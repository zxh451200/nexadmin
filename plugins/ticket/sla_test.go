package ticket

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
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

// equalStrings 是本地比较（不改既有测试，只用在新用例里）。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 节假日算式必须用非 UTC 时区夹具，断言对 8 小时平移敏感的绝对墙上时间。
// 覆盖：周五 23:00 起算跨到周一（现有行为不回归）、把周一设为节假日后到期变晚、
// 7×24 遇节假日不走快速路径、整周皆节假日时落到兜底而不死循环。
func TestSLAHolidaysBusinessDue(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	cal := SLASetting{
		Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 540, EndMinute: 1080,
		Targets: DefaultSLA().Targets,
	}
	// 周五 23:00 起算 120 分钟：跳过周末 → 周一 09:00+120 = 周一 11:00（+08 → UTC 03:00）。
	start := time.Date(2026, 1, 2, 23, 0, 0, 0, loc) // 2026-01-02 是周五
	wantA := time.Date(2026, 1, 5, 11, 0, 0, 0, loc) // 2026-01-05 是周一
	gotA := businessDue(start, 120, cal, loc)
	if !gotA.Equal(wantA) {
		t.Fatalf("周五跨周末到期 = %v，期望 %v", gotA, wantA)
	}
	// 墙上时间必须是 +08 的周一 11:00；若实现按 UTC 计算会整体偏 8 小时 → 这条红。
	if got := gotA.UTC().Format("2006-01-02 15:04"); got != "2026-01-05 03:00" {
		t.Fatalf("到期时间的 UTC 墙上值 = %s，应为 2026-01-05 03:00（对时区偏移敏感）", got)
	}

	// 靶子：同一策略把周一(2026-01-05)设为节假日 → 到期必须变晚（顺延到周二 11:00）。
	holi := cal
	holi.Holidays = []string{"2026-01-05"}
	wantB := time.Date(2026, 1, 6, 11, 0, 0, 0, loc) // 周二
	gotB := businessDue(start, 120, holi, loc)
	if !gotB.Equal(wantB) {
		t.Fatalf("周一为节假日时到期 = %v，期望 %v", gotB, wantB)
	}
	if !gotB.After(gotA) {
		t.Fatalf("节假日必须让到期变晚：gotB=%v 未晚于 gotA=%v", gotB, gotA)
	}

	// 7×24 但当天是节假日：不能走快速路径，应顺延到次日 02:00。
	all := DefaultSLA()
	all.Holidays = []string{"2026-01-01"}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	gotC := businessDue(from, 120, all, loc)
	if wantC := time.Date(2026, 1, 2, 2, 0, 0, 0, loc); !gotC.Equal(wantC) {
		t.Fatalf("7×24 遇节假日 = %v，期望跳过当天到 %v", gotC, wantC)
	}

	// 整周都是节假日：3700 次循环耗尽后落到兜底，而不是死循环。
	// 造 3700 个连续节假日覆盖起点起的所有天，remaining 永远补不满 → 兜底 = cur + remaining。
	hol := make([]string, 3700)
	d0 := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	for i := range hol {
		hol[i] = d0.AddDate(0, 0, i).Format("2006-01-02")
	}
	week := SLASetting{Workdays: []int{1, 2, 3, 4, 5, 6, 7}, StartMinute: 0, EndMinute: 1440, Targets: DefaultSLA().Targets, Holidays: hol}
	gotD := businessDue(d0, 60, week, loc)
	// 循环 3700 次后 cur = 第 3700 天 00:00，兜底再加 60 分钟。
	wantD := d0.AddDate(0, 0, 3700).Add(60 * time.Minute)
	if !gotD.Equal(wantD) {
		t.Fatalf("全节假日兜底 = %v，期望 %v", gotD, wantD)
	}
}

// 编解码往返：holidays 逗号分隔存储，读时 trim/去空/丢弃非法日期，去重升序；Code/Label 保留。
func TestSLAHolidayEncodeDecodeRoundTrip(t *testing.T) {
	// decode 侧防御性收敛：重复、空项、非法 2026-13-99 都要处理掉，保留合法项且升序。
	row := SLA{Code: "vip", Label: "VIP 通道", Holidays: "2026-01-01, 2026-01-01 ,, 2026-13-99, 2026-02-15"}
	dec := decodeSLA(row)
	if !equalStrings(dec.Holidays, []string{"2026-01-01", "2026-02-15"}) {
		t.Fatalf("decode 未去重/丢弃非法项：%+v", dec.Holidays)
	}
	if dec.Code != "vip" || dec.Label != "VIP 通道" {
		t.Fatalf("decode 丢了 Code/Label：%+v", dec)
	}

	// normalize 排序后 encode→decode 往返一致。
	v := SLASetting{
		Code: "vip", Label: "VIP", Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 540, EndMinute: 1080,
		Targets: DefaultSLA().Targets, Holidays: []string{"2026-05-01", "2026-01-01"},
	}
	if err := v.normalize(); err != nil {
		t.Fatal(err)
	}
	if !equalStrings(v.Holidays, []string{"2026-01-01", "2026-05-01"}) {
		t.Fatalf("normalize 未升序去重：%+v", v.Holidays)
	}
	enc, err := encodeSLA(v)
	if err != nil {
		t.Fatal(err)
	}
	if enc.Holidays != "2026-01-01,2026-05-01" {
		t.Fatalf("encode holidays = %q", enc.Holidays)
	}
	back := decodeSLA(enc)
	if back.Code != v.Code || back.Label != v.Label || !equalStrings(back.Holidays, v.Holidays) {
		t.Fatalf("往返不一致：enc=%+v back=%+v", v, back)
	}

	// 零值等价：无节假日行解出的 Holidays 为 nil（保持旧行为）。
	if got := decodeSLA(SLA{}).Holidays; got != nil {
		t.Fatalf("空 holidays 应为 nil，得到 %+v", got)
	}
}

// normalize 的每道闸都要「删掉守卫就变红」：Code 字符集/长度、Label rune 上限、
// 节假日非法格式/早于 2000/超限，以及合法项的去重排序。
func TestSLANormalizeGuards(t *testing.T) {
	base := func() SLASetting {
		return SLASetting{Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 540, EndMinute: 1080, Targets: DefaultSLA().Targets}
	}

	// Code：空串合法（默认策略），[a-z0-9_-]{1,32} 合法。
	empty := base()
	if err := empty.normalize(); err != nil {
		t.Fatalf("空 Code 应合法（默认策略），却报错：%v", err)
	}
	okCode := base()
	okCode.Code = "vip_eu-1"
	if err := okCode.normalize(); err != nil {
		t.Fatalf("合法 Code 被拒：%v", err)
	}
	for _, bad := range []string{"VIP", "up per", "a/b", strings.Repeat("a", 33)} {
		v := base()
		v.Code = bad
		if err := v.normalize(); err == nil {
			t.Fatalf("非法 Code %q 应被拒", bad)
		}
	}

	// Label：按 rune 限制 ≤64。
	labelOK := base()
	labelOK.Label = strings.Repeat("标", 64)
	if err := labelOK.normalize(); err != nil {
		t.Fatalf("64 rune 标签应合法：%v", err)
	}
	labelBad := base()
	labelBad.Label = strings.Repeat("标", 65)
	if err := labelBad.normalize(); err == nil {
		t.Fatal("65 rune 标签应被拒")
	}

	// Holidays：非法格式、早于 2000 都被拒；去重升序。
	badFmt := base()
	badFmt.Holidays = []string{"2026-13-99"}
	if err := badFmt.normalize(); err == nil {
		t.Fatal("非法日期 2026-13-99 应被 normalize 拒绝")
	}
	tooOld := base()
	tooOld.Holidays = []string{"1999-12-31"}
	if err := tooOld.normalize(); err == nil {
		t.Fatal("早于 2000-01-01 的节假日应被拒")
	}
	dup := base()
	dup.Holidays = []string{"2026-01-01", "2026-01-01", "2026-06-06"}
	if err := dup.normalize(); err != nil {
		t.Fatal(err)
	}
	if !equalStrings(dup.Holidays, []string{"2026-01-01", "2026-06-06"}) {
		t.Fatalf("normalize 未去重排序：%+v", dup.Holidays)
	}

	// Holidays：200 项合法，201 项超限被拒。
	mk := func(n int) []string {
		out := make([]string, n)
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := range out {
			out[i] = from.AddDate(0, 0, i).Format("2006-01-02")
		}
		return out
	}
	lim := base()
	lim.Holidays = mk(200)
	if err := lim.normalize(); err != nil {
		t.Fatalf("200 项节假日应合法：%v", err)
	}
	over := base()
	over.Holidays = mk(201)
	if err := over.normalize(); err == nil {
		t.Fatal("201 项节假日应被超限拒绝")
	}
}

// 多策略 CRUD 的 sqlmock 期望：表列名对齐 DDL（code/label/holidays）。
var slaColumns = []string{"id", "created_at", "updated_at", "code", "label", "workdays", "start_minute", "end_minute", "targets", "holidays"}

func slaPolicyRows() *sqlmock.Rows {
	r := sqlmock.NewRows(slaColumns)
	// 默认策略：code 空串。
	r.AddRow(int64(1), testNow, testNow, "", "默认", "1,2,3,4,5,6,7", 0, 1440,
		`{"normal":{"firstMinutes":1440,"resolutionMinutes":4320}}`, "")
	// 命名策略：含 holidays。
	r.AddRow(int64(2), testNow, testNow, "vip", "VIP", "1,2,3,4,5", 540, 1080,
		`{"normal":{"firstMinutes":60,"resolutionMinutes":120}}`, "2026-01-01,2026-05-01")
	return r
}

func TestSLAListPolicies(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_sla` ORDER BY code ASC").
		WillReturnRows(slaPolicyRows())

	list, err := s.ListSLAPolicies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("应列出 2 套策略，得到 %d", len(list))
	}
	if list[0].Code != "" || list[0].Label != "默认" {
		t.Fatalf("默认策略解析错：%+v", list[0])
	}
	if list[1].Code != "vip" || !equalStrings(list[1].Holidays, []string{"2026-01-01", "2026-05-01"}) {
		t.Fatalf("vip 策略解析错：%+v", list[1])
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// SaveSLAPolicy insert 分支：Take 未命中 → Create（Begin/INSERT/Commit）。
func TestSLASavePolicyInsert(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_sla` WHERE code = \\? ORDER BY id ASC").
		WillReturnRows(sqlmock.NewRows(slaColumns)) // 空结果 → 未找到
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_ticket_sla`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	in := SLASetting{
		Code: "vip", Label: "VIP", Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 540, EndMinute: 1080,
		Targets: DefaultSLA().Targets, Holidays: []string{"2026-01-01"},
	}
	saved, err := s.SaveSLAPolicy(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Code != "vip" {
		t.Fatalf("saved code = %q", saved.Code)
	}
	// 命名策略（code 非空）不得污染进程内默认缓存。
	if s.CurrentSLA().Code != "" {
		t.Fatalf("非默认策略不应刷新缓存：%+v", s.CurrentSLA())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// SaveSLAPolicy update 分支：Take 命中 → Updates（Begin/UPDATE/Commit）；code 空串时同步刷新缓存。
func TestSLASavePolicyUpdateDefault(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	defRow := sqlmock.NewRows(slaColumns).
		AddRow(int64(1), testNow, testNow, "", "旧默认", "1,2,3,4,5,6,7", 0, 1440, "{}", "")
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_sla` WHERE code = \\? ORDER BY id ASC").
		WillReturnRows(defRow)
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_ticket_sla` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	in := SLASetting{
		Code: "", Label: "新默认", Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 540, EndMinute: 1080,
		Targets: DefaultSLA().Targets, Holidays: []string{"2026-01-01"},
	}
	if _, err := s.SaveSLAPolicy(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	// code 为空串 → 必须刷新进程内缓存（保持今天 SaveSLA 的行为）；删掉守卫这里就红。
	if s.CurrentSLA().Label != "新默认" || !equalStrings(s.CurrentSLA().Holidays, []string{"2026-01-01"}) {
		t.Fatalf("默认策略未刷新缓存：%+v", s.CurrentSLA())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// DeleteSLAPolicy：命名策略按 code 删除；删不存在的行不报错。
func TestSLADeletePolicy(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_ticket_sla` WHERE code = \\?").
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 行受影响 = 不存在，也不报错
	m.ExpectCommit()

	if err := s.DeleteSLAPolicy(context.Background(), "ghost"); err != nil {
		t.Fatalf("删不存在的策略不应报错：%v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// DeleteSLAPolicy("") 必须在发出任何 SQL 前被拒：断言错误是 400 业务错误而非 sqlmock 报错。
func TestSLADeleteDefaultRejected(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	// 不登记任何期望：若守卫被删，DELETE 会发出 SQL 并返回 sqlmock 的 not-expected 错误（非业务错误）。
	err := s.DeleteSLAPolicy(context.Background(), "")
	if err == nil {
		t.Fatal("默认策略删除应被拒")
	}
	var e *response.Error
	if !errors.As(err, &e) || e.Status != 400 || e.Key != "error.bad_request" {
		t.Fatalf("删除默认策略应返回 400 业务错误，得到 %#v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// SLAForCode：空串走缓存、命中返回解码值、未命中回落缓存，都不报错。
func TestSLAForCode(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	s.SetSLA(SLASetting{
		Workdays: []int{1, 2, 3, 4, 5, 6, 7}, StartMinute: 0, EndMinute: 1440,
		Targets: DefaultSLA().Targets,
	})

	// 空串直接走缓存，不发 SQL。
	if got, err := s.SLAForCode(context.Background(), ""); err != nil || got.Code != "" {
		t.Fatalf("空 code 应走缓存：%+v err=%v", got, err)
	}

	// 命中 vip 行。
	vipRow := sqlmock.NewRows(slaColumns).
		AddRow(int64(2), testNow, testNow, "vip", "VIP", "1,2,3,4,5", 540, 1080, "{}", "2026-01-01")
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_sla` WHERE code = \\? ORDER BY id ASC").
		WillReturnRows(vipRow)
	got, err := s.SLAForCode(context.Background(), "vip")
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "vip" || !equalStrings(got.Holidays, []string{"2026-01-01"}) {
		t.Fatalf("vip 解析错：%+v", got)
	}

	// 未命中回落缓存。
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_sla` WHERE code = \\? ORDER BY id ASC").
		WillReturnRows(sqlmock.NewRows(slaColumns))
	fallback, err := s.SLAForCode(context.Background(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Code != "" {
		t.Fatalf("未命中应回落默认缓存，得到 %+v", fallback)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// GetSLAPolicy：找不到 → 返回 DefaultSLA 且不报错（与 LoadSLA 回落一致）。
func TestSLAGetPolicyFallback(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_sla` WHERE code = \\? ORDER BY id ASC").
		WillReturnRows(sqlmock.NewRows(slaColumns))
	got, err := s.GetSLAPolicy(context.Background(), "nope")
	if err != nil {
		t.Fatalf("找不到不应报错：%v", err)
	}
	if got.StartMinute != 0 || got.EndMinute != 1440 || len(got.Workdays) != 7 {
		t.Fatalf("应回落 DefaultSLA，得到 %+v", got)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
