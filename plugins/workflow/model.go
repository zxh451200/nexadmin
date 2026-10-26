package workflow

import (
	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// Def 是流程定义（表 plg_workflow_def，租户级）。definition 以 JSON 文本存储，
// 结构见本包 bpmn.Definition，便于随 BPMN 子集演进而不改表结构。
type Def struct {
	database.TenantModel
	DefKey     string `gorm:"column:def_key" json:"defKey"`
	Name       string `gorm:"column:name" json:"name"`
	Status     string `gorm:"column:status" json:"status"`
	Version    int    `gorm:"column:version" json:"version"`
	Definition string `gorm:"column:definition;type:text" json:"-"`
}

func (Def) TableName() string { return "plg_workflow_def" }

// 定义状态。
const (
	DefDraft     = "draft"
	DefPublished = "published"
	DefDisabled  = "disabled"
)

// Inst 是流程实例（表 plg_workflow_inst，租户级）。state_json 存 bpmn.Instance 的完整快照，
// 表上的 status / def_key / title 等列只用于列表检索与展示。
type Inst struct {
	database.TenantModel
	DefID       idgen.ID `gorm:"column:def_id;index" json:"defId"`
	DefKey      string   `gorm:"column:def_key;index" json:"defKey"`
	Title       string   `gorm:"column:title" json:"title"`
	BizType     string   `gorm:"column:biz_type;index" json:"bizType"`
	BusinessKey string   `gorm:"column:business_key;index" json:"businessKey"`
	Submitter   string   `gorm:"column:submitter" json:"submitter"`
	Status      string   `gorm:"column:status;index" json:"status"`
	StateJSON   string   `gorm:"column:state_json;type:text" json:"-"`
}

func (Inst) TableName() string { return "plg_workflow_inst" }
