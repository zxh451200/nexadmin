// Package backup 是数据库备份（免费内置模块）：调用本机 mysqldump / pg_dump 导出并 gzip 压缩到备份目录，
// 记录元数据（sys_backup），支持保留策略与每天定时。恢复不提供网页入口，用命令行
// `nexadmin-server restore <备份文件>`（见 core/ctl/restore.go）。找不到导出工具时只报告
// available=false 与原因，不做纯 Go 的“半吊子”导出。
package backup

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 备份来源与状态。
const (
	SourceManual   = "manual"
	SourceSchedule = "schedule"

	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// Record 对应 sys_backup：一次备份的元数据。平台级表（整库备份，不属于某个租户）。
type Record struct {
	database.PlatformModel
	FileName   string   `gorm:"size:200;not null" json:"fileName"`
	Engine     string   `gorm:"size:16;not null" json:"engine"`
	Version    string   `gorm:"size:32;not null" json:"version"` // 备份时的框架版本
	Size       int64    `gorm:"not null" json:"size"`
	SHA256     string   `gorm:"column:sha256;size:64;not null" json:"sha256"`
	Source     string   `gorm:"size:16;not null" json:"source"` // manual | schedule
	DurationMs int64    `gorm:"not null" json:"durationMs"`
	Status     string   `gorm:"size:16;not null" json:"status"` // running | success | failed
	Error      string   `gorm:"size:1000;not null" json:"error"`
	CreatedBy  idgen.ID `json:"createdBy"`
}

func (Record) TableName() string { return "sys_backup" }

// Setting 对应 sys_backup_setting（只有 id=1 一行）：保留策略与定时，多实例共享。
// LastRunDate 是“定时任务今天已被某个实例抢占”的标记（乐观抢占，防多实例重复备份）。
type Setting struct {
	ID          int64     `gorm:"primaryKey;autoIncrement:false" json:"-"`
	Schedule    string    `gorm:"size:5;not null" json:"schedule"`
	KeepDays    int       `gorm:"not null" json:"keepDays"`
	KeepCount   int       `gorm:"not null" json:"keepCount"`
	LastRunDate string    `gorm:"size:10" json:"-"`
	UpdatedAt   time.Time `json:"-"`
}

func (Setting) TableName() string { return "sys_backup_setting" }

const settingID = 1
