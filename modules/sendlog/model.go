package sendlog

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 权限点。
const (
	PermList   = "sendlog:list"
	PermDelete = "sendlog:delete"
)

// 渠道与状态取值（与 core/notify 的 Record 一致，这里复述一份用于入参校验）。
var (
	channels = []string{"sms", "email", "robot"}
	statuses = []string{"success", "fail", "skipped"}
)

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

// 字段长度上限，与迁移里的列宽一致。
const (
	MaxRecipient = 128
	MaxSubject   = 200
	MaxError     = 500
	MaxScene     = 32
)

// SendLog 对应 sys_send_log。
//
// 红线：不存验证码、正文、密钥。Recipient 是脱敏后的值（138****1234、a***@x.com，机器人为渠道名）；
// Subject 只存模板名或标题；Error 已脱敏并截断到 MaxError 个字符。
type SendLog struct {
	database.TenantModel
	Channel    string `gorm:"size:16;not null"`
	Scene      string `gorm:"size:32;not null"`
	Recipient  string `gorm:"size:128;not null"`
	Subject    string `gorm:"size:200;not null"`
	Status     string `gorm:"size:16;not null"`
	Error      string `gorm:"size:500;not null"`
	DurationMs int    `gorm:"not null"`
}

func (SendLog) TableName() string { return "sys_send_log" }

// View 与前端 SendLog 一致。
type View struct {
	ID         idgen.ID  `json:"id"`
	Channel    string    `json:"channel"`
	Scene      string    `json:"scene"`
	Recipient  string    `json:"recipient"`
	Subject    string    `json:"subject"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	DurationMs int       `json:"durationMs"`
	CreatedAt  time.Time `json:"createdAt"`
}

func toView(l SendLog) View {
	return View{ID: l.ID, Channel: l.Channel, Scene: l.Scene, Recipient: l.Recipient, Subject: l.Subject,
		Status: l.Status, Error: l.Error, DurationMs: l.DurationMs, CreatedAt: l.CreatedAt}
}

// Query 是列表查询条件。
type Query struct {
	Channel   string
	Status    string
	Recipient string // 对（已脱敏的）收件人做模糊匹配
	StartDate string // YYYY-MM-DD，按应用时区，含当天
	EndDate   string
	Page      int
	PageSize  int
}
