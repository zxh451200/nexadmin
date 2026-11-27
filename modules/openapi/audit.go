package openapi

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/database"
	coreoa "nexadmin/core/openapi"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const auditRetention = 90 * 24 * time.Hour

// CallAudit 对应 sys_openapi_audit，只保存调用元数据。
type CallAudit struct {
	database.TenantModel
	PrincipalType   string    `gorm:"column:principal_type;size:8;not null"`
	CredentialID    idgen.ID  `gorm:"column:credential_id;not null"`
	CredentialName  string    `gorm:"column:credential_name;size:100;not null"`
	CredentialLabel string    `gorm:"column:credential_label;size:64;not null"`
	UserID          idgen.ID  `gorm:"column:user_id;not null"`
	Username        string    `gorm:"column:username;size:100;not null"`
	AuthOutcome     string    `gorm:"column:auth_outcome;size:16;not null"`
	FailureKey      string    `gorm:"column:failure_key;size:128;not null"`
	Method          string    `gorm:"column:method;size:16;not null"`
	Path            string    `gorm:"column:path;size:500;not null"`
	StatusCode      int       `gorm:"column:status_code;not null"`
	DurationMs      int       `gorm:"column:duration_ms;not null"`
	ClientIP        string    `gorm:"column:client_ip;size:64;not null"`
	UserAgent       string    `gorm:"column:user_agent;size:500;not null"`
	RequestID       string    `gorm:"column:request_id;size:64;not null"`
	RequestedAt     time.Time `gorm:"column:requested_at;not null"`
}

func (CallAudit) TableName() string { return "sys_openapi_audit" }

type auditDTO struct {
	ID              idgen.ID  `json:"id"`
	PrincipalType   string    `json:"principalType"`
	CredentialID    idgen.ID  `json:"credentialId"`
	CredentialName  string    `json:"credentialName"`
	CredentialLabel string    `json:"credentialLabel"`
	UserID          idgen.ID  `json:"userId"`
	Username        string    `json:"username"`
	AuthOutcome     string    `json:"authOutcome"`
	FailureKey      string    `json:"failureKey"`
	Method          string    `json:"method"`
	Path            string    `json:"path"`
	StatusCode      int       `json:"statusCode"`
	DurationMs      int       `json:"durationMs"`
	ClientIP        string    `json:"clientIp"`
	UserAgent       string    `json:"userAgent"`
	RequestID       string    `json:"requestId"`
	RequestedAt     time.Time `json:"requestedAt"`
	CreatedAt       time.Time `json:"createdAt"`
}

type auditStore struct {
	db  *gorm.DB
	log *zap.Logger
}

func (s *auditStore) RecordCall(ctx context.Context, e coreoa.CallAuditEvent) error {
	row := CallAudit{PrincipalType: e.PrincipalType, CredentialID: e.CredentialID, CredentialName: e.CredentialName, CredentialLabel: e.CredentialLabel, UserID: e.UserID, Username: e.Username,
		AuthOutcome: e.AuthOutcome, FailureKey: e.FailureKey, Method: e.Method, Path: e.Path, StatusCode: e.StatusCode,
		DurationMs: e.DurationMs, ClientIP: e.ClientIP, UserAgent: e.UserAgent, RequestID: e.RequestID, RequestedAt: e.RequestedAt}
	row.TenantID = e.TenantID
	if err := s.db.WithContext(tenant.Skip(ctx)).Create(&row).Error; err != nil {
		s.log.Warn("openapi: persist call audit failed", zap.String("principal", e.PrincipalType), zap.Error(err))
		return err
	}
	return nil
}

func (s *auditStore) cleanup(ctx context.Context, before time.Time) (int64, error) {
	res := s.db.WithContext(tenant.Skip(ctx)).Where("requested_at < ?", before).Delete(&CallAudit{})
	return res.RowsAffected, res.Error
}

func runAuditCleanup(ctx context.Context, s *auditStore, log *zap.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			n, err := s.cleanup(ctx, now.UTC().Add(-auditRetention))
			if err != nil {
				log.Warn("openapi: cleanup call audits failed", zap.Error(err))
			} else if n > 0 {
				log.Info("openapi: expired call audits removed", zap.Int64("count", n))
			}
		}
	}
}

func parseAuditTime(raw string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// GET /openapi/audits
func (h *handler) auditList(c *gin.Context) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	if cl.Principal != "" {
		response.Fail(c, response.Forbidden("openapi.token_manage_forbidden"))
		return
	}
	page, size := response.ParsePage(c)
	q := h.db.WithContext(c.Request.Context()).Model(&CallAudit{})
	if kind := strings.TrimSpace(c.Query("principalType")); kind != "" {
		if kind != "pat" && kind != "app" {
			response.Fail(c, response.BadRequest("openapi.audit_type_invalid"))
			return
		}
		q = q.Where("principal_type = ?", kind)
	}
	if method := strings.ToUpper(strings.TrimSpace(c.Query("method"))); method != "" {
		q = q.Where("method = ?", method)
	}
	if raw := strings.TrimSpace(c.Query("authOutcome")); raw != "" {
		if raw != "authenticated" && raw != "rejected" {
			response.Fail(c, response.BadRequest("openapi.audit_status_invalid"))
			return
		}
		q = q.Where("auth_outcome = ?", raw)
	}
	if raw := strings.TrimSpace(c.Query("status")); raw != "" {
		status, err := strconv.Atoi(raw)
		if err != nil || status < 100 || status > 599 {
			response.Fail(c, response.BadRequest("openapi.audit_status_invalid"))
			return
		}
		q = q.Where("status_code = ?", status)
	}
	if raw := strings.TrimSpace(c.Query("credentialId")); raw != "" {
		id, err := idgen.Parse(raw)
		if err != nil || id == 0 {
			response.Fail(c, response.BadRequest("openapi.invalid_id"))
			return
		}
		q = q.Where("credential_id = ?", int64(id))
	}
	if raw := strings.TrimSpace(c.Query("start")); raw != "" {
		t, ok := parseAuditTime(raw)
		if !ok {
			response.Fail(c, response.BadRequest("openapi.audit_time_invalid"))
			return
		}
		q = q.Where("requested_at >= ?", t)
	}
	if raw := strings.TrimSpace(c.Query("end")); raw != "" {
		t, ok := parseAuditTime(raw)
		if !ok {
			response.Fail(c, response.BadRequest("openapi.audit_time_invalid"))
			return
		}
		q = q.Where("requested_at <= ?", t)
	}
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("credential_name LIKE ? OR credential_label LIKE ? OR username LIKE ? OR path LIKE ? OR request_id LIKE ?", like, like, like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		response.Fail(c, err)
		return
	}
	allow := map[string]string{"requestedAt": "requested_at", "durationMs": "duration_ms", "statusCode": "status_code", "credentialName": "credential_name"}
	order := core.OrderBy(c.Query("sort"), c.Query("desc") == "true", allow, "requested_at")
	var rows []CallAudit
	if err := q.Order(order).Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]auditDTO, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		out = append(out, auditDTO{ID: r.ID, PrincipalType: r.PrincipalType, CredentialID: r.CredentialID,
			CredentialName: r.CredentialName, CredentialLabel: r.CredentialLabel, UserID: r.UserID, Username: r.Username,
			AuthOutcome: r.AuthOutcome, FailureKey: r.FailureKey, Method: r.Method, Path: r.Path, StatusCode: r.StatusCode,
			DurationMs: r.DurationMs, ClientIP: r.ClientIP, UserAgent: r.UserAgent, RequestID: r.RequestID,
			RequestedAt: r.RequestedAt, CreatedAt: r.CreatedAt})
	}
	response.Page(c, out, total, page, size)
}
