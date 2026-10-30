package imports

import (
	"nexadmin/core/database"
)

// Job 是一次导入的台账(租户级)。
// ≤ SyncMaxRows 的文件同步执行,请求返回时已完成;
// > SyncMaxRows 的文件创建 pending 任务由后台协程异步处理,前端靠列表轮询感知状态迁移。
type Job struct {
	database.TenantModel
	Source     string `gorm:"column:source;index" json:"source"`
	FileName   string `gorm:"column:file_name" json:"fileName"`
	TotalRows  int    `gorm:"column:total_rows" json:"totalRows"`
	OkRows     int    `gorm:"column:ok_rows" json:"okRows"`
	FailRows   int    `gorm:"column:fail_rows" json:"failRows"`
	Status     string `gorm:"column:status" json:"status"` // pending | running | done | failed
	DurationMs int64  `gorm:"column:duration_ms" json:"durationMs"`
	// ErrorJSON 是最多 500 条 RowError 的 JSON;超出 MaxErrors 的错误只计数不存明细。
	ErrorJSON string `gorm:"column:error_json;type:text" json:"-"`
	CreatedBy string `gorm:"column:created_by" json:"createdBy"`
	// Lang 是提交时的界面语言：异步执行没有请求上下文，行错误文案按它翻译。
	Lang string `gorm:"column:lang;size:16;not null" json:"-"`
}

func (Job) TableName() string { return "plg_imports_job" }

// 任务状态：pending(已受理) → running(后台执行中) → done | failed。
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// 上限：小文件同步、大文件异步(后台逐行,仍受 AsyncMaxRows 硬顶)。
const (
	SyncMaxRows  = 5000   // ≤ 此行数同步执行(请求内完成)
	AsyncMaxRows = 100000 // > 此行数直接拒绝(让用户拆文件)
	MaxBytes     = 20 << 20
	MaxErrors    = 500 // 明细错误最多存 500 条,超出只计数
)
