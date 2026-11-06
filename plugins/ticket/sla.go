package ticket

// SLA 策略：一条全局策略（营业时间 + 按优先级的首响/解决时限）。
// 只存一行，读写都是整份配置；工单创建/重开时用它算到期时间，统计也用它出目标值。

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// slaDupKeyRe 认唯一索引（迁移 018 的 uk_plg_ticket_sla_code）冲突在两引擎上的驱动写法：
// MySQL/MariaDB 是 1062（Duplicate entry '...' for key '...'），PG 是 23505
// （duplicate key value violates unique constraint "..."）。框架没开 GORM 的 TranslateError，
// gorm.ErrDuplicatedKey 永远拿不到，只能按文本判——与 plugins/pay/errors.go:88 同一口径。
// 判定只回答「是不是唯一索引拦的」，不猜是哪个索引：索引名靠 Wrap 进日志留证。
var slaDupKeyRe = regexp.MustCompile(`(?i)duplicate entry|error 1062|duplicate key value|unique constraint|sqlstate 23505`)

// isSLADuplicateKeyErr 判 Create 的错误是否由 SLA 策略编码的唯一索引拦下。
// 认得太宽会把真故障（连接断了、列不存在）折成 409 业务错误，所以给反例用例钉住。
func isSLADuplicateKeyErr(err error) bool {
	return err != nil && slaDupKeyRe.MatchString(err.Error())
}

// slaDupConflict 是「同 code 的并发保存撞上了唯一索引」：预检（先查后建）在单操作者下给出
// 可读的错误，索引是并发窗口的兜底，输的那条在这里折回 4xx，而不是把驱动原文冒成 500。
func slaDupConflict(err error) *response.Error {
	return response.Conflict("ticket.sla_code_conflict").Wrap(err)
}

// SLATarget 是同一优先级下的两个时限（分钟）。
type SLATarget struct {
	FirstMinutes      int `json:"firstMinutes"`
	ResolutionMinutes int `json:"resolutionMinutes"`
}

// SLASetting 是整份 SLA 策略；Workdays 1=周一 ... 7=周日，时间用当天第几分钟表示。
// Code/Label 支持多套命名策略（Code 空串 = 默认策略），Holidays 是跳过的工作日（空 = 无节假日），
// 三者的零值都等价于旧的全局单策略行为。
type SLASetting struct {
	Code        string               `json:"code,omitempty"`
	Label       string               `json:"label,omitempty"`
	Workdays    []int                `json:"workdays"`
	StartMinute int                  `json:"startMinute"`
	EndMinute   int                  `json:"endMinute"`
	Targets     map[string]SLATarget `json:"targets"`
	Holidays    []string             `json:"holidays"`
}

// SLA 是策略表的行；targets 用 JSON 文本存，holidays 用逗号分隔存，避免三引擎 JSON 类型差异。
type SLA struct {
	database.PlatformModel
	Code        string `gorm:"size:32;not null;default:''" json:"-"`
	Label       string `gorm:"size:64;not null;default:''" json:"-"`
	Workdays    string `gorm:"size:20;not null" json:"-"`
	StartMinute int    `gorm:"not null" json:"-"`
	EndMinute   int    `gorm:"not null" json:"-"`
	Targets     string `gorm:"type:text;not null" json:"-"`
	Holidays    string `gorm:"type:text;not null" json:"-"`
}

func (SLA) TableName() string { return "plg_ticket_sla" }

// slaCodeRe 是策略编码：小写字母、数字、下划线、连字符，1~32 位（空串另作默认策略处理）。
var slaCodeRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// maxSLAHolidays 是单套策略的节假日上限，挡住误填与序列化膨胀。
const maxSLAHolidays = 200

// holidayLayout 是节假日的日期格式（YYYY-MM-DD）。
const holidayLayout = "2006-01-02"

// minHolidayDate 是允许的节假日下界，挡 1970/0001 这类误填。
var minHolidayDate = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// DefaultSLA 是未配置时的出厂策略：7×24 营业，时限沿用旧的响应档位，解决时限按 4 倍设定。
func DefaultSLA() SLASetting {
	return SLASetting{
		Workdays:    []int{1, 2, 3, 4, 5, 6, 7},
		StartMinute: 0,
		EndMinute:   1440,
		Targets: map[string]SLATarget{
			PriorityUrgent: {FirstMinutes: 120, ResolutionMinutes: 480},
			PriorityHigh:   {FirstMinutes: 480, ResolutionMinutes: 1440},
			PriorityNormal: {FirstMinutes: 1440, ResolutionMinutes: 4320},
			PriorityLow:    {FirstMinutes: 4320, ResolutionMinutes: 10080},
		},
	}
}

// TargetFor 取某个优先级的时限；未知优先级回落 normal。
func (v SLASetting) TargetFor(priority string) SLATarget {
	if t, ok := v.Targets[priority]; ok {
		return t
	}
	if t, ok := v.Targets[PriorityNormal]; ok {
		return t
	}
	return DefaultSLA().Targets[PriorityNormal]
}

const maxSLAMinutes = 525600 // 一年，挡住溢出与误填

// normalize 校验并补齐策略：工作日去重排序、时间窗合法、四个优先级都有限时，
// 另校验策略编码/名称与节假日列表。
func (v *SLASetting) normalize() error {
	// Code 空串合法（默认策略），非空只允许 [a-z0-9_-]{1,32}。
	if v.Code != "" && !slaCodeRe.MatchString(v.Code) {
		return response.BadRequest("error.bad_request")
	}
	// Label 按字符数（rune）限制，与 DDL VARCHAR(64) 的语义对齐。
	if len([]rune(v.Label)) > 64 {
		return response.BadRequest("error.bad_request")
	}
	seen := make(map[int]bool, len(v.Workdays))
	days := make([]int, 0, 7)
	for _, d := range v.Workdays {
		if d < 1 || d > 7 {
			return response.BadRequest("error.bad_request")
		}
		if !seen[d] {
			seen[d] = true
			days = append(days, d)
		}
	}
	sort.Ints(days)
	if len(days) == 0 {
		return response.BadRequest("error.bad_request")
	}
	v.Workdays = days
	if v.StartMinute < 0 || v.StartMinute >= 1440 || v.EndMinute <= 0 || v.EndMinute > 1440 || v.StartMinute >= v.EndMinute {
		return response.BadRequest("error.bad_request")
	}
	// 补齐与删键都写在这张 map 上，而 SLASetting 虽是值传递、map 却是引用：
	// 就地改会写进调用方那张表——两个并发保存直接 fatal「concurrent map writes」，
	// 单次调用也会让调用方看见自己的入参被悄悄补全过。先换成这一份自己的副本。
	if v.Targets == nil {
		v.Targets = make(map[string]SLATarget, 4)
	} else {
		cp := make(map[string]SLATarget, len(v.Targets))
		for p, t := range v.Targets {
			cp[p] = t
		}
		v.Targets = cp
	}
	def := DefaultSLA()
	for _, p := range []string{PriorityUrgent, PriorityHigh, PriorityNormal, PriorityLow} {
		t, ok := v.Targets[p]
		if !ok || t.FirstMinutes <= 0 || t.ResolutionMinutes <= 0 {
			t = def.Targets[p]
		}
		if t.FirstMinutes > maxSLAMinutes || t.ResolutionMinutes > maxSLAMinutes || t.ResolutionMinutes < t.FirstMinutes {
			return response.BadRequest("error.bad_request")
		}
		v.Targets[p] = t
	}
	for p := range v.Targets {
		if !ValidPriority(p) {
			delete(v.Targets, p)
		}
	}
	// 节假日：空项丢弃，非空必须能按 YYYY-MM-DD 解析且不早于 2000-01-01；去重升序，超限拒绝。
	seenDay := make(map[string]bool, len(v.Holidays))
	hdays := make([]string, 0, len(v.Holidays))
	for _, raw := range v.Holidays {
		d := strings.TrimSpace(raw)
		if d == "" {
			continue
		}
		t, err := time.Parse(holidayLayout, d)
		if err != nil || t.Before(minHolidayDate) {
			return response.BadRequest("error.bad_request")
		}
		if !seenDay[d] {
			seenDay[d] = true
			hdays = append(hdays, d)
		}
	}
	if len(hdays) > maxSLAHolidays {
		return response.BadRequest("error.bad_request")
	}
	if len(hdays) == 0 {
		v.Holidays = nil
	} else {
		sort.Strings(hdays)
		v.Holidays = hdays
	}
	return nil
}

func encodeSLA(v SLASetting) (SLA, error) {
	parts := make([]string, 0, len(v.Workdays))
	for _, d := range v.Workdays {
		parts = append(parts, strconv.Itoa(d))
	}
	b, err := json.Marshal(v.Targets)
	if err != nil {
		return SLA{}, err
	}
	return SLA{
		Code: v.Code, Label: v.Label,
		Workdays: strings.Join(parts, ","), StartMinute: v.StartMinute, EndMinute: v.EndMinute,
		Targets: string(b), Holidays: strings.Join(v.Holidays, ","),
	}, nil
}

// parseHolidays 把逗号分隔的节假日串解析为去重升序的合法日期列表：逐项 trim、去空、
// 按 YYYY-MM-DD 校验（非法丢弃）。这是对库内脏数据的防御性收敛，不做数量/下界拒绝。
func parseHolidays(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0, 8)
	for _, part := range strings.Split(s, ",") {
		d := strings.TrimSpace(part)
		if d == "" {
			continue
		}
		if _, err := time.Parse(holidayLayout, d); err != nil {
			continue
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func decodeSLA(row SLA) SLASetting {
	out := DefaultSLA()
	out.Code = row.Code
	out.Label = row.Label
	days := make([]int, 0, 7)
	for _, part := range strings.Split(row.Workdays, ",") {
		if d, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && d >= 1 && d <= 7 {
			days = append(days, d)
		}
	}
	if len(days) > 0 {
		out.Workdays = days
	}
	if row.StartMinute >= 0 && row.EndMinute > row.StartMinute && row.EndMinute <= 1440 {
		out.StartMinute, out.EndMinute = row.StartMinute, row.EndMinute
	}
	if row.Targets != "" {
		var targets map[string]SLATarget
		if err := json.Unmarshal([]byte(row.Targets), &targets); err == nil && len(targets) > 0 {
			out.Targets = targets
		}
	}
	out.Holidays = parseHolidays(row.Holidays)
	return out
}

// CurrentSLA 读进程内缓存（插件启动时从库里载入，保存时同步更新）。
func (s *Service) CurrentSLA() SLASetting {
	if p := s.sla.Load(); p != nil {
		return *p
	}
	return DefaultSLA()
}

// SetSLA 更新进程内缓存。
func (s *Service) SetSLA(v SLASetting) {
	s.sla.Store(&v)
}

// LoadSLA 从库里读整份策略；没有配置时返回出厂策略。
func (s *Service) LoadSLA(ctx context.Context) (SLASetting, error) {
	var row SLA
	err := s.db.WithContext(ctx).Order("id ASC").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultSLA(), nil
	}
	if err != nil {
		return DefaultSLA(), err
	}
	return decodeSLA(row), nil
}

// SaveSLA 校验后整份覆盖（单行 upsert），并刷新进程内缓存。
func (s *Service) SaveSLA(ctx context.Context, in SLASetting) (SLASetting, error) {
	if err := in.normalize(); err != nil {
		return SLASetting{}, err
	}
	row, err := encodeSLA(in)
	if err != nil {
		return SLASetting{}, err
	}
	var existing SLA
	err = s.db.WithContext(ctx).Order("id ASC").Take(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		id, idErr := idgen.Next()
		if idErr != nil {
			return SLASetting{}, idErr
		}
		row.ID = id
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			// 唯一索引兜底：默认策略（code 空串）同样只允许一行，并发保存输的那条回 4xx。
			if isSLADuplicateKeyErr(err) {
				return SLASetting{}, slaDupConflict(err)
			}
			return SLASetting{}, err
		}
	case err != nil:
		return SLASetting{}, err
	default:
		if err := s.db.WithContext(ctx).Model(&SLA{}).Where("id = ?", int64(existing.ID)).
			Updates(map[string]any{
				"workdays": row.Workdays, "start_minute": row.StartMinute,
				"end_minute": row.EndMinute, "targets": row.Targets,
				"holidays": row.Holidays,
			}).Error; err != nil {
			return SLASetting{}, err
		}
	}
	s.SetSLA(in)
	return in, nil
}

// ListSLAPolicies 按 code ASC, id ASC 取全部策略（默认策略 code 为空串，也在列内）。
func (s *Service) ListSLAPolicies(ctx context.Context) ([]SLASetting, error) {
	var rows []SLA
	if err := s.db.WithContext(ctx).Order("code ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SLASetting, 0, len(rows))
	for _, r := range rows {
		out = append(out, decodeSLA(r))
	}
	return out, nil
}

// GetSLAPolicy 按 code 精确取一套策略；code 为空串取默认行。找不到回落 DefaultSLA 且不报错，
// 与 LoadSLA 的回落语义一致。
func (s *Service) GetSLAPolicy(ctx context.Context, code string) (SLASetting, error) {
	var row SLA
	err := s.db.WithContext(ctx).Where("code = ?", code).Order("id ASC").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultSLA(), nil
	}
	if err != nil {
		return DefaultSLA(), err
	}
	return decodeSLA(row), nil
}

// SaveSLAPolicy 校验后按 code upsert 一套策略；无则建行，有则覆盖六列。
// 当 code 为空串（默认策略）时同步刷新进程内缓存，保持今天 SaveSLA 的行为。
// 预检（先查后建）保留：单操作者下它给出可读错误；并发窗口由 code 上的唯一索引兜底，
// 撞索引的那条在这里折成 409 ticket.sla_code_conflict（见 slaDupConflict）。
func (s *Service) SaveSLAPolicy(ctx context.Context, v SLASetting) (SLASetting, error) {
	if err := v.normalize(); err != nil {
		return SLASetting{}, err
	}
	row, err := encodeSLA(v)
	if err != nil {
		return SLASetting{}, err
	}
	var existing SLA
	err = s.db.WithContext(ctx).Where("code = ?", v.Code).Order("id ASC").Take(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		id, idErr := idgen.Next()
		if idErr != nil {
			return SLASetting{}, idErr
		}
		row.ID = id
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			// 「先查后建」的并发窗口由迁移 018 的 uk_plg_ticket_sla_code 封口：预检放行后两条
			// INSERT 同时到达，输的那条拿到的是驱动原文，必须折成和预检同一族 4xx 业务错误——
			// 既不能冒成 500，也不能像过去那样静默多出一行被读侧掩盖。
			if isSLADuplicateKeyErr(err) {
				return SLASetting{}, slaDupConflict(err)
			}
			return SLASetting{}, err
		}
	case err != nil:
		return SLASetting{}, err
	default:
		if err := s.db.WithContext(ctx).Model(&SLA{}).Where("id = ?", int64(existing.ID)).
			Updates(map[string]any{
				"code": row.Code, "label": row.Label,
				"workdays": row.Workdays, "start_minute": row.StartMinute,
				"end_minute": row.EndMinute, "targets": row.Targets,
				"holidays": row.Holidays,
			}).Error; err != nil {
			return SLASetting{}, err
		}
	}
	if v.Code == "" {
		s.SetSLA(v)
	}
	return v, nil
}

// DeleteSLAPolicy 按 code 删除一套策略；默认策略（code 空串）不可删。删不存在的行不报错。
// WHERE code = ? 不带 limit 是有意的：迁移 018 之后一个 code 至多一行，这条语句就是「删那一行」。
func (s *Service) DeleteSLAPolicy(ctx context.Context, code string) error {
	if code == "" {
		return response.BadRequest("error.bad_request")
	}
	return s.db.WithContext(ctx).Where("code = ?", code).Delete(&SLA{}).Error
}

// SLAForCode 解析某套策略：code 空串走进程内缓存（默认策略），否则查行；
// 未命中回落缓存。分组→策略的绑定解析应由调用方（如 handler/service）拿 Group.SLACode
// 后调本方法，本文件不感知 plg_ticket_group。
func (s *Service) SLAForCode(ctx context.Context, code string) (SLASetting, error) {
	if code == "" {
		return s.CurrentSLA(), nil
	}
	var row SLA
	err := s.db.WithContext(ctx).Where("code = ?", code).Order("id ASC").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.CurrentSLA(), nil
	}
	if err != nil {
		return SLASetting{}, err
	}
	return decodeSLA(row), nil
}

// businessDue 在营业时间内累加 minutes；默认 7×24 时退化为直接相加。
func businessDue(start time.Time, minutes int, v SLASetting, loc *time.Location) time.Time {
	if minutes <= 0 {
		return start
	}
	if loc == nil {
		loc = time.UTC
	}
	if len(v.Workdays) == 7 && v.StartMinute == 0 && v.EndMinute == 1440 && len(v.Holidays) == 0 {
		return start.Add(time.Duration(minutes) * time.Minute)
	}
	work := make(map[int]bool, len(v.Workdays))
	for _, d := range v.Workdays {
		work[d] = true
	}
	holiday := make(map[string]bool, len(v.Holidays))
	for _, h := range v.Holidays {
		holiday[h] = true
	}
	dayBase := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	}
	cur := start.In(loc)
	remaining := minutes
	for i := 0; i < 3700 && remaining > 0; i++ {
		base := dayBase(cur)
		dayStart := base.Add(time.Duration(v.StartMinute) * time.Minute)
		dayEnd := base.Add(time.Duration(v.EndMinute) * time.Minute)
		wd := int(cur.Weekday())
		if wd == 0 {
			wd = 7
		}
		// 落在节假日的整天按非工作日跳过，即便它同时在 Workdays 内。
		if !work[wd] || holiday[base.Format(holidayLayout)] || !cur.Before(dayEnd) {
			cur = base.AddDate(0, 0, 1).Add(time.Duration(v.StartMinute) * time.Minute)
			continue
		}
		if cur.Before(dayStart) {
			cur = dayStart
		}
		avail := int(dayEnd.Sub(cur).Minutes())
		if avail >= remaining {
			return cur.Add(time.Duration(remaining) * time.Minute)
		}
		remaining -= avail
		cur = base.AddDate(0, 0, 1).Add(time.Duration(v.StartMinute) * time.Minute)
	}
	// 策略异常（例如一年里全是非工作日）时兜底，避免死循环。
	return cur.Add(time.Duration(remaining) * time.Minute)
}

// slaFor 取某个队列实际生效的策略：分组绑了 code 就用那套，没绑、查不到或查库出错
// 都回落默认策略——SLA 解析失败不该把建单整条链打挂。
func (s *Service) slaFor(ctx context.Context, groupID int64) SLASetting {
	if groupID == 0 {
		return s.CurrentSLA()
	}
	var g Group
	if err := s.db.WithContext(ctx).Select("sla_code").Where("id = ?", groupID).Take(&g).Error; err != nil {
		return s.CurrentSLA()
	}
	v, err := s.SLAForCode(ctx, g.SLACode)
	if err != nil {
		return s.CurrentSLA()
	}
	return v
}
