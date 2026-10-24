package cron

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 执行状态、触发方式。
const (
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"

	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// Job 对应 plg_cron_job（租户表：每个租户管理自己的任务）。
type Job struct {
	database.TenantModel
	Name       string     `gorm:"size:100;not null" json:"name"`
	CronExpr   string     `gorm:"size:100;not null" json:"cronExpr"`
	Handler    string     `gorm:"size:64;not null" json:"handler"`
	Params     string     `gorm:"type:text" json:"params"`
	Enabled    bool       `json:"enabled"` // 注意：不要加 gorm default 标签，否则 false 会被默认值覆盖
	RetryCount int        `json:"retryCount"`
	LastRunAt  *time.Time `json:"lastRunAt"`
	LastStatus string     `gorm:"size:16" json:"lastStatus"`
	// LastSlot 是最近一次被某个实例"抢占"的调度分钟（unix 分钟数），多实例防重复执行用，见 service.go。
	LastSlot int64 `gorm:"not null" json:"-"`
}

func (Job) TableName() string { return "plg_cron_job" }

// Log 对应 plg_cron_log。一次触发（含其全部重试）一行。
type Log struct {
	database.TenantModel
	JobID       idgen.ID   `gorm:"not null;index" json:"jobId"`
	JobName     string     `gorm:"size:100;not null" json:"jobName"`
	Handler     string     `gorm:"size:64;not null" json:"handler"`
	TriggerType string     `gorm:"size:16;not null" json:"trigger"`
	Status      string     `gorm:"size:16;not null" json:"status"`
	Attempts    int        `json:"attempts"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	CostMs      int        `json:"costMs"`
	Output      string     `gorm:"type:text" json:"output"`
	Error       string     `gorm:"type:text" json:"error"`
}

func (Log) TableName() string { return "plg_cron_log" }
