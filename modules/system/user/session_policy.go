package user

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/auth"
)

// 会话时限默认值（管理员没在「设置 → 安全」改过时生效）。
// 参照行业基线：OWASP 建议特权后台闲置 30 分钟以内；Keycloak 默认 idle 30m / max 10h；
// AWS 控制台最长 12h。这里给「闲置 2 小时 + 最长 12 小时」：一天工作够用，也不会一直在线到忘记退出。
const (
	defaultIdleMinutes = 120
	defaultMaxHours    = 12
)

// sessionPolicy 读设置页「安全设置」里的会话时限；字段缺失或非法时用默认值。
// 数值 0 表示该维度不限制（闲置 0 = 沿用配置 auth.refresh_ttl 的滑动窗口，最长 0 = 不设绝对上限）。
func sessionPolicy(db *gorm.DB) auth.SessionPolicy {
	idleMin, maxHour := defaultIdleMinutes, defaultMaxHours
	var content string
	if err := db.Table("sys_setting").Select("content").Where("section = ?", "security").Limit(1).Scan(&content).Error; err == nil && content != "" {
		var sec struct {
			IdleMinutes json.Number `json:"idleMinutes"`
			MaxHours    json.Number `json:"maxHours"`
		}
		dec := json.NewDecoder(strings.NewReader(content))
		dec.UseNumber()
		if dec.Decode(&sec) == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(sec.IdleMinutes.String())); err == nil && n >= 0 {
				idleMin = n
			}
			if n, err := strconv.Atoi(strings.TrimSpace(sec.MaxHours.String())); err == nil && n >= 0 {
				maxHour = n
			}
		}
	}
	return auth.SessionPolicy{
		IdleTTL:     time.Duration(idleMin) * time.Minute,
		MaxLifetime: time.Duration(maxHour) * time.Hour,
	}
}
