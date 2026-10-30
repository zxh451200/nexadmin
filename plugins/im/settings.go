package im

// 插件设置：工作时间、离线提示与主动邀请。存 plg_im_setting 的单行 JSON（k='general'），
// 读取时用默认值兜底，写入前做白名单校验。

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

const settingKeyGeneral = "general"

// Setting 是插件设置的键值行。
type Setting struct {
	database.PlatformModel
	Key   string `gorm:"column:k;size:64;not null;uniqueIndex:uk_plg_im_setting_k" json:"-"`
	Value string `gorm:"column:v;type:text;not null" json:"-"`
}

func (Setting) TableName() string { return "plg_im_setting" }

// Settings 是对外的设置视图。
type Settings struct {
	BusinessHours bool   `json:"businessHours"` // 是否启用工作时间（false = 7×24）
	HoursStart    string `json:"hoursStart"`    // HH:MM
	HoursEnd      string `json:"hoursEnd"`      // HH:MM
	Workdays      []int  `json:"workdays"`      // 1=周一 … 7=周日
	Timezone      string `json:"timezone"`
	OfflineText   string `json:"offlineText"`
	InviteDelay   int    `json:"inviteDelay"` // 秒；0 = 不主动邀请
	InviteText    string `json:"inviteText"`
}

// DefaultSettings 是未配置时的行为：7×24 在线、不主动邀请。
func DefaultSettings() Settings {
	return Settings{
		HoursStart: "09:00", HoursEnd: "18:00",
		Workdays: []int{1, 2, 3, 4, 5}, Timezone: "Asia/Shanghai",
	}
}

// Settings 读取设置；读失败或没配过时返回默认值（不阻断访客）。
func (s *Service) Settings(ctx context.Context) Settings {
	out := DefaultSettings()
	var row Setting
	if err := s.db.WithContext(ctx).Where("k = ?", settingKeyGeneral).Take(&row).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) && s.log != nil {
			s.log.Warn("im: load settings failed", "err", err.Error())
		}
		return out
	}
	if row.Value != "" {
		if err := json.Unmarshal([]byte(row.Value), &out); err != nil && s.log != nil {
			s.log.Warn("im: decode settings failed", "err", err.Error())
		}
	}
	normalizeSettings(&out)
	return out
}

// SaveSettings 校验并保存设置（单行覆盖）。
func (s *Service) SaveSettings(ctx context.Context, in Settings) (*Settings, error) {
	normalizeSettings(&in)
	if _, err := time.Parse("15:04", in.HoursStart); err != nil {
		return nil, response.BadRequest("im.settings_invalid")
	}
	if _, err := time.Parse("15:04", in.HoursEnd); err != nil {
		return nil, response.BadRequest("im.settings_invalid")
	}
	if in.Timezone != "" {
		if _, err := time.LoadLocation(in.Timezone); err != nil {
			return nil, response.BadRequest("im.settings_invalid")
		}
	}
	if in.BusinessHours && len(in.Workdays) == 0 {
		return nil, response.BadRequest("im.settings_invalid")
	}
	if in.InviteDelay < 0 || in.InviteDelay > 600 {
		return nil, response.BadRequest("im.settings_invalid")
	}
	in.OfflineText = truncate(strings.TrimSpace(in.OfflineText), 500)
	in.InviteText = truncate(strings.TrimSpace(in.InviteText), 500)
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var row Setting
	if err := s.db.WithContext(ctx).Where("k = ?", settingKeyGeneral).Take(&row).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		id, ierr := idgen.Next()
		if ierr != nil {
			return nil, ierr
		}
		row = Setting{Key: settingKeyGeneral, Value: string(raw)}
		row.ID = id
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, err
		}
		return &in, nil
	}
	if err := s.db.WithContext(ctx).Model(&Setting{}).Where("id = ?", int64(row.ID)).
		Update("v", string(raw)).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

// OnlineAt 判断给定时刻是否在服务时间内（未启用工作时间恒为 true）。
// 支持跨午夜（如 22:00–02:00）。
func (st Settings) OnlineAt(now time.Time) bool {
	if !st.BusinessHours {
		return true
	}
	loc := time.Local
	if st.Timezone != "" {
		if l, err := time.LoadLocation(st.Timezone); err == nil {
			loc = l
		}
	}
	t := now.In(loc)
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Go: 周日=0；这里用 1=周一 … 7=周日
	}
	if !containsWeekday(st.Workdays, weekday) {
		return false
	}
	start, ok1 := clockMinutes(st.HoursStart)
	end, ok2 := clockMinutes(st.HoursEnd)
	if !ok1 || !ok2 {
		return true
	}
	cur := t.Hour()*60 + t.Minute()
	if start <= end {
		return cur >= start && cur < end
	}
	return cur >= start || cur < end
}

func normalizeSettings(st *Settings) {
	st.HoursStart = strings.TrimSpace(st.HoursStart)
	st.HoursEnd = strings.TrimSpace(st.HoursEnd)
	st.Timezone = strings.TrimSpace(st.Timezone)
	if st.HoursStart == "" {
		st.HoursStart = "09:00"
	}
	if st.HoursEnd == "" {
		st.HoursEnd = "18:00"
	}
	if st.Timezone == "" {
		st.Timezone = "Asia/Shanghai"
	}
	seen := map[int]bool{}
	days := make([]int, 0, len(st.Workdays))
	for _, d := range st.Workdays {
		if d < 1 || d > 7 || seen[d] {
			continue
		}
		seen[d] = true
		days = append(days, d)
	}
	sort.Ints(days)
	st.Workdays = days
}

func containsWeekday(days []int, d int) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

func clockMinutes(clock string) (int, bool) {
	t, err := time.Parse("15:04", clock)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}
