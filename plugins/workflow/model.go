package workflow

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 表前缀 plg_workflow_（租户级）。引擎状态与定义快照都以 JSON 文本存储：
// 引擎版本升级时不需要随之改表结构，历史实例也能原样回放。

// Def 是流程定义（表 plg_workflow_def）。
type Def struct {
	database.TenantModel
	DefKey     string `gorm:"column:def_key" json:"defKey"`
	Name       string `gorm:"column:name" json:"name"`
	Status     string `gorm:"column:status" json:"status"`
	Version    int    `gorm:"column:version" json:"version"`
	Definition string `gorm:"column:definition;type:text" json:"-"`
	BpmnXML    string `gorm:"column:bpmn_xml;type:text" json:"-"`
}

func (Def) TableName() string { return "plg_workflow_def" }

// 定义状态。
const (
	DefDraft     = "draft"
	DefPublished = "published"
	DefDisabled  = "disabled"
)

// Inst 是流程实例（表 plg_workflow_inst）。state_json 是引擎状态快照，
// def_snapshot 是发起时的定义 JSON——在途实例永远按自己的快照执行。
type Inst struct {
	database.TenantModel
	DefID       idgen.ID `gorm:"column:def_id;index" json:"defId"`
	DefKey      string   `gorm:"column:def_key;index" json:"defKey"`
	DefVersion  int      `gorm:"column:def_version" json:"defVersion"`
	Title       string   `gorm:"column:title" json:"title"`
	BizType     string   `gorm:"column:biz_type;index" json:"bizType"`
	BusinessKey string   `gorm:"column:business_key;index" json:"businessKey"`
	Submitter   string   `gorm:"column:submitter" json:"submitter"`
	Status      string   `gorm:"column:status;index" json:"status"`
	Revision    int64    `gorm:"column:revision" json:"revision"`
	DefSnapshot string   `gorm:"column:def_snapshot;type:text" json:"-"`
	StateJSON   string   `gorm:"column:state_json;type:text" json:"-"`
}

func (Inst) TableName() string { return "plg_workflow_inst" }

// Task 是物化的待办（表 plg_workflow_task）。Assignee 为空表示公开任务。
type Task struct {
	database.TenantModel
	InstID   idgen.ID `gorm:"column:inst_id;index" json:"instId"`
	DefKey   string   `gorm:"column:def_key" json:"defKey"`
	NodeKey  string   `gorm:"column:node_key" json:"nodeKey"`
	NodeName string   `gorm:"column:node_name" json:"nodeName"`
	Assignee string   `gorm:"column:assignee;index" json:"assignee"`
	Status   string   `gorm:"column:status;index" json:"status"`
}

func (Task) TableName() string { return "plg_workflow_task" }

// 待办状态。
const (
	TaskPending = "pending"
	TaskClosed  = "closed"
)

// Action 是不可变的流转动作记录（表 plg_workflow_action），用于审计与「我已办」查询。
type Action struct {
	database.TenantModel
	InstID     idgen.ID  `gorm:"column:inst_id;index" json:"instId"`
	DefKey     string    `gorm:"column:def_key" json:"defKey"`
	NodeKey    string    `gorm:"column:node_key" json:"nodeKey"`
	Actor      string    `gorm:"column:actor;index" json:"actor"`
	Action     string    `gorm:"column:action" json:"action"`
	Comment    string    `gorm:"column:comment" json:"comment"`
	OccurredAt time.Time `gorm:"column:occurred_at;index" json:"occurredAt"`
}

func (Action) TableName() string { return "plg_workflow_action" }
