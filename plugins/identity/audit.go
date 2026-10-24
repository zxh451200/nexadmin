package identity

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/database"
)

// 审计沿用框架的操作日志表 sys_oper_log（与「操作日志」页面同一份数据）。
// 插件不能 import 内置的 modules/log，所以这里只用表名 + 同结构的行模型写入（cron 清理日志同样按表名常量访问内置表）；
// 表不存在或写入失败只记一条 warn，不影响业务。

const auditModule = "身份源"

// auditEntry 是一条要记录的管理操作。Params 必须是已脱敏的字段（绝不放口令）。
type auditEntry struct {
	Type, Operator, IP, Method, URL string
	Params                          map[string]any
	Err                             error
	Cost                            time.Duration
}

type auditFunc func(ctx context.Context, e auditEntry)

type operLogRow struct {
	database.TenantModel
	Module     string `gorm:"size:64;not null"`
	Type       string `gorm:"size:32;not null"`
	Operator   string `gorm:"size:64;not null"`
	IP         string `gorm:"column:ip;size:64"`
	Location   string `gorm:"size:128"`
	Status     string `gorm:"size:16;not null"`
	CostMs     int
	Method     string `gorm:"size:16;not null"`
	URL        string `gorm:"column:url;size:500;not null"`
	Params     string `gorm:"type:text"`
	Response   string `gorm:"type:text"`
	ErrorStack string `gorm:"type:text"`
}

func (operLogRow) TableName() string { return "sys_oper_log" }

// dbAuditor 返回把审计写入 sys_oper_log 的实现。
func dbAuditor(db *gorm.DB, log *zap.Logger) auditFunc {
	if log == nil {
		log = zap.NewNop()
	}
	return func(ctx context.Context, e auditEntry) {
		if db == nil { // 手工构造的 Deps 没有数据库时不写
			return
		}
		params, _ := json.Marshal(e.Params)
		row := &operLogRow{Module: auditModule, Type: e.Type, Operator: clip(e.Operator, 64), IP: clip(e.IP, 64), Status: "success",
			CostMs: int(e.Cost.Milliseconds()), Method: e.Method, URL: clip(e.URL, 500), Params: clip(string(params), 8<<10)}
		if row.Operator == "" {
			row.Operator = "-"
		}
		if e.Err != nil {
			row.Status, row.ErrorStack = "failed", clip(e.Err.Error(), 2<<10)
		}
		if err := db.WithContext(context.WithoutCancel(ctx)).Create(row).Error; err != nil {
			log.Warn("identity: write oper log failed", zap.Error(err))
		}
	}
}

func (s *Service) record(ctx context.Context, e auditEntry) {
	if s.audit != nil {
		s.audit(ctx, e)
	}
}
