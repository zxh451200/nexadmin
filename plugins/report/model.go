package report

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 所有表都带 tenant_id（嵌入 database.TenantModel），查询另外显式带租户条件（见 service.go 的 scoped）。
// 配置类 JSON 一律存 TEXT，三种引擎通用。

// Dataset 对应 plg_report_dataset：一张白名单表 + 选中的列（+ 可选的固定过滤）。
type Dataset struct {
	database.TenantModel
	Name      string   `gorm:"size:100;not null" json:"name"`
	Source    string   `gorm:"size:64;not null" json:"source"`
	Config    string   `gorm:"type:text" json:"-"` // DatasetConfig 的 JSON
	Remark    string   `gorm:"size:255" json:"remark"`
	CreatedBy idgen.ID `json:"createdBy"`
}

func (Dataset) TableName() string { return "plg_report_dataset" }

// Chart 对应 plg_report_chart。
type Chart struct {
	database.TenantModel
	Name      string   `gorm:"size:100;not null" json:"name"`
	DatasetID idgen.ID `gorm:"not null" json:"datasetId"`
	Type      string   `gorm:"size:16;not null" json:"type"`
	Config    string   `gorm:"type:text" json:"-"` // ChartConfig 的 JSON
	Remark    string   `gorm:"size:255" json:"remark"`
	CreatedBy idgen.ID `json:"createdBy"`
}

func (Chart) TableName() string { return "plg_report_chart" }

// ReportDef 对应 plg_report_report（报表页）。
type ReportDef struct {
	database.TenantModel
	Name      string   `gorm:"size:100;not null" json:"name"`
	GroupName string   `gorm:"size:64;not null" json:"groupName"`
	Remark    string   `gorm:"size:255" json:"remark"`
	Config    string   `gorm:"type:text" json:"-"` // ReportConfig 的 JSON
	CreatedBy idgen.ID `json:"createdBy"`
}

func (ReportDef) TableName() string { return "plg_report_report" }

// Screen 对应 plg_report_screen（数据大屏）。
type Screen struct {
	database.TenantModel
	Name      string   `gorm:"size:100;not null" json:"name"`
	Remark    string   `gorm:"size:255" json:"remark"`
	Config    string   `gorm:"type:text" json:"-"` // ScreenConfig 的 JSON
	CreatedBy idgen.ID `json:"createdBy"`
}

func (Screen) TableName() string { return "plg_report_screen" }

// ScreenRole 对应 plg_report_screen_role：大屏 -> 可见角色。
type ScreenRole struct {
	database.TenantModel
	ScreenID idgen.ID `gorm:"not null" json:"screenId"`
	RoleID   idgen.ID `gorm:"not null" json:"roleId"`
}

func (ScreenRole) TableName() string { return "plg_report_screen_role" }

// Subscription 对应 plg_report_subscription：每张报表一条订阅配置（report_id 唯一）。
// 到点给收件人列表发文本提醒邮件（附件需 mail 包扩展 multipart，另立项）。
type Subscription struct {
	database.TenantModel
	// ReportID 是被订阅的报表。
	ReportID idgen.ID `gorm:"uniqueIndex:uk_plg_report_subscription_report;not null" json:"reportId"`
	// Schedule 取 daily / weekly。
	Schedule string `gorm:"size:16;not null" json:"schedule"`
	// SendHour 是发送时刻的小时（0-23，服务器时区）。
	SendHour int `gorm:"not null;default:8" json:"sendHour"`
	// Weekday 是每周发送日（0=周日..6=周六）；仅 weekly 使用。
	Weekday int `gorm:"not null;default:1" json:"weekday"`
	// Recipients 是逗号分隔的收件人邮箱（保存前逐个校验）。
	Recipients string `gorm:"type:text;not null" json:"recipients"`
	// Enabled 沿用 001 的风格用整型承载布尔（SMALLINT 1/0），避免三库方言分叉；视图层转 boolean。
	Enabled int `gorm:"not null;default:1" json:"-"`
	// LastSentDate 是最近一次发送的日期（服务器时区当天）；NULL = 从未发送。
	LastSentDate *time.Time `gorm:"type:date" json:"lastSentDate,omitempty"`
}

func (Subscription) TableName() string { return "plg_report_subscription" }
