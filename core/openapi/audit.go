package openapi

import (
	"context"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/middleware"
	"nexadmin/pkg/idgen"
)

// CallAuditEvent 只包含调用元数据，绝不包含凭证、签名、查询串或请求响应正文。
type CallAuditEvent struct {
	RequestedAt     time.Time
	TenantID        idgen.ID
	PrincipalType   string
	CredentialID    idgen.ID
	CredentialName  string
	CredentialLabel string
	UserID          idgen.ID
	Username        string
	AuthOutcome     string
	FailureKey      string
	Method          string
	Path            string
	StatusCode      int
	DurationMs      int
	ClientIP        string
	UserAgent       string
	RequestID       string
}

type CallAuditSink interface {
	RecordCall(ctx context.Context, event CallAuditEvent) error
}

var callAuditSink atomic.Pointer[CallAuditSink]

func SetCallAuditSink(sink CallAuditSink) { callAuditSink.Store(&sink) }

const (
	auditCredentialKey = "openapi_audit_credential"
	auditFailureKey    = "openapi_audit_failure"
)

type auditCredential struct {
	tenantID idgen.ID
	id       idgen.ID
	name     string
	label    string
}

func setCallAuditCredential(c *gin.Context, tenantID, id idgen.ID, name, label string) {
	c.Set(auditCredentialKey, auditCredential{tenantID: tenantID, id: id, name: name, label: label})
}

func setCallAuditFailure(c *gin.Context, key string) { c.Set(auditFailureKey, key) }

// CallAudit 包在 Authenticate 外，覆盖认证失败；普通会话、匿名请求与预检不记录。
func CallAudit() gin.HandlerFunc {
	return func(c *gin.Context) {
		kind := ""
		switch {
		case strings.HasPrefix(c.GetHeader("Authorization"), "Bearer "+TokenPrefix):
			kind = "pat"
		case c.GetHeader("X-App-Key") != "":
			kind = "app"
		}
		if kind == "" || c.Request.Method == "OPTIONS" || auth.ClaimsFrom(c) != nil {
			c.Next()
			return
		}
		start := time.Now()
		event := CallAuditEvent{RequestedAt: start, PrincipalType: kind, AuthOutcome: "rejected"}
		defer func() {
			if r := recover(); r != nil {
				event.StatusCode = 500
				event.FailureKey = "error.internal"
				writeCallAudit(c, event, start)
				panic(r)
			}
			event.StatusCode = c.Writer.Status()
			writeCallAudit(c, event, start)
		}()
		c.Next()
	}
}

func writeCallAudit(c *gin.Context, event CallAuditEvent, start time.Time) {
	if cred, ok := c.Get(auditCredentialKey); ok {
		v := cred.(auditCredential)
		event.TenantID, event.CredentialID = v.tenantID, v.id
		event.CredentialName, event.CredentialLabel = clipAudit(v.name, 100), clipAudit(v.label, 64)
	}
	if cl := auth.ClaimsFrom(c); cl != nil && cl.Principal == event.PrincipalType {
		event.AuthOutcome = "authenticated"
		event.TenantID = cl.TenantID
		if event.PrincipalType == "pat" {
			event.UserID = cl.UserID
			event.Username = clipAudit(cl.Username, 100)
		}
	}
	if key, _ := c.Get("response_failure_key"); key != nil {
		event.FailureKey, _ = key.(string)
	}
	if event.FailureKey == "" {
		if key, _ := c.Get(auditFailureKey); key != nil {
			event.FailureKey, _ = key.(string)
		}
	}
	if event.FailureKey == "" && event.StatusCode >= 400 {
		event.FailureKey = "error.request_failed"
	}
	if event.CredentialID == 0 {
		return // 未知凭证不落库，避免未认证请求刷爆审计表；仍由访问日志记录。
	}
	event.Method = clipAudit(c.Request.Method, 16)
	p := c.FullPath()
	if p == "" {
		p = c.Request.URL.Path
	}
	event.Path = clipAudit(p, 500)
	event.DurationMs = int(time.Since(start) / time.Millisecond)
	event.ClientIP = clipAudit(c.ClientIP(), 64)
	event.UserAgent = clipAudit(c.Request.UserAgent(), 500)
	event.RequestID = clipAudit(c.GetString(middleware.RequestIDKey), 64)
	sink := callAuditSink.Load()
	if sink == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 2*time.Second)
	defer cancel()
	if err := (*sink).RecordCall(ctx, event); err != nil {
		// 审计失败不能改写已经完成的业务响应；由请求日志与持久化告警保留可观测性。
		_ = err
	}
}

func clipAudit(s string, n int) string {
	if len(s) <= n {
		return strings.ToValidUTF8(s, "")
	}
	s = strings.ToValidUTF8(s[:n], "")
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
