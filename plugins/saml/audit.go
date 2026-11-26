package saml

import (
	"context"
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/database"
)

const auditModule = "SAML 单点登录"

type auditEntry struct {
	Type, Operator, IP, Method, URL string
	Params                          map[string]any
	Err                             error
	Cost                            time.Duration
}

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

func (s *Service) record(ctx context.Context, e auditEntry) {
	if s == nil || s.db == nil {
		return
	}
	params, _ := json.Marshal(e.Params)
	row := &operLogRow{
		Module: auditModule, Type: e.Type, Operator: clipAudit(e.Operator, 64), IP: clipAudit(e.IP, 64),
		Status: "success", CostMs: int(e.Cost.Milliseconds()), Method: e.Method,
		URL: clipAudit(e.URL, 500), Params: clipAudit(string(params), 8<<10),
	}
	if row.Operator == "" {
		row.Operator = "-"
	}
	if e.Err != nil {
		row.Status, row.ErrorStack = "failed", clipAudit(e.Err.Error(), 2<<10)
	}
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Create(row).Error; err != nil && s.log != nil {
		s.log.Warn("saml: write operation audit failed", zap.Error(err))
	}
}

func (s *Service) audit(c *gin.Context, typ, sourceID string, start time.Time, params map[string]any, err error) {
	if params == nil {
		params = map[string]any{}
	}
	if sourceID != "" {
		params["sourceId"] = sourceID
	}
	e := auditEntry{Type: typ, IP: c.ClientIP(), Method: c.Request.Method, URL: c.Request.URL.Path, Params: params, Err: err, Cost: time.Since(start)}
	if claims := auth.ClaimsFrom(c); claims != nil {
		e.Operator = claims.Username
	}
	s.record(c.Request.Context(), e)
}

func clipAudit(value string, max int) string {
	if len(value) <= max {
		return value
	}
	value = value[:max]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
