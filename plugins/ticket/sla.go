package ticket

// SLA 策略：一条全局策略（营业时间 + 按优先级的首响/解决时限）。
// 只存一行，读写都是整份配置；工单创建/重开时用它算到期时间，统计也用它出目标值。

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// SLATarget 是同一优先级下的两个时限（分钟）。
type SLATarget struct {
	FirstMinutes      int `json:"firstMinutes"`
	ResolutionMinutes int `json:"resolutionMinutes"`
}

// SLASetting 是整份 SLA 策略；Workdays 1=周一 ... 7=周日，时间用当天第几分钟表示。
type SLASetting struct {
	Workdays    []int                `json:"workdays"`
	StartMinute int                  `json:"startMinute"`
	EndMinute   int                  `json:"endMinute"`
	Targets     map[string]SLATarget `json:"targets"`
}

// SLA 是策略表的行；targets 用 JSON 文本存，避免三引擎 JSON 类型差异。
type SLA struct {
	database.PlatformModel
	Workdays    string `gorm:"size:20;not null" json:"-"`
	StartMinute int    `gorm:"not null" json:"-"`
	EndMinute   int    `gorm:"not null" json:"-"`
	Targets     string `gorm:"type:text;not null" json:"-"`
}

func (SLA) TableName() string { return "plg_ticket_sla" }

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

// normalize 校验并补齐策略：工作日去重排序、时间窗合法、四个优先级都有限时。
func (v *SLASetting) normalize() error {
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
	if v.Targets == nil {
		v.Targets = map[string]SLATarget{}
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
	return SLA{Workdays: strings.Join(parts, ","), StartMinute: v.StartMinute, EndMinute: v.EndMinute, Targets: string(b)}, nil
}

func decodeSLA(row SLA) SLASetting {
	out := DefaultSLA()
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
			return SLASetting{}, err
		}
	case err != nil:
		return SLASetting{}, err
	default:
		if err := s.db.WithContext(ctx).Model(&SLA{}).Where("id = ?", int64(existing.ID)).
			Updates(map[string]any{
				"workdays": row.Workdays, "start_minute": row.StartMinute,
				"end_minute": row.EndMinute, "targets": row.Targets,
			}).Error; err != nil {
			return SLASetting{}, err
		}
	}
	s.SetSLA(in)
	return in, nil
}

// businessDue 在营业时间内累加 minutes；默认 7×24 时退化为直接相加。
func businessDue(start time.Time, minutes int, v SLASetting, loc *time.Location) time.Time {
	if minutes <= 0 {
		return start
	}
	if loc == nil {
		loc = time.UTC
	}
	if len(v.Workdays) == 7 && v.StartMinute == 0 && v.EndMinute == 1440 {
		return start.Add(time.Duration(minutes) * time.Minute)
	}
	work := make(map[int]bool, len(v.Workdays))
	for _, d := range v.Workdays {
		work[d] = true
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
		if !work[wd] || !cur.Before(dayEnd) {
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
