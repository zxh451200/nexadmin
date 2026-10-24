package report

import (
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
