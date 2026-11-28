package saml

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/database"
	"nexadmin/core/middleware"
	"nexadmin/pkg/idgen"
)

type sourceVersion struct {
	database.TenantModel
	SourceID     idgen.ID `gorm:"index;not null"`
	Version      int      `gorm:"not null"`
	ConfigJSON   string   `gorm:"column:config_json;type:text;not null"`
	OperatorID   idgen.ID
	OperatorName string `gorm:"size:100"`
	Reason       string `gorm:"size:500"`
}

func (sourceVersion) TableName() string { return "plg_saml_source_version" }

type samlEvent struct {
	database.TenantModel
	SourceID      idgen.ID `gorm:"index;not null"`
	Type          string   `gorm:"size:64;not null;index"`
	Result        string   `gorm:"size:16;not null"`
	OperatorID    idgen.ID
	OperatorName  string `gorm:"size:100"`
	UserID        idgen.ID
	CorrelationID string `gorm:"size:100;index"`
	IP            string `gorm:"size:64"`
	Detail        string `gorm:"type:text"`
}

func (samlEvent) TableName() string { return "plg_saml_event" }

type samlSessionRecord struct {
	database.TenantModel
	SourceID     idgen.ID `gorm:"index;not null"`
	UserID       idgen.ID `gorm:"index;not null"`
	NameIDHash   string   `gorm:"size:64;index;not null"`
	SessionIndex string   `gorm:"size:500"`
	SessionKey   string   `gorm:"size:64;uniqueIndex;not null"`
	AccessJTI    string   `gorm:"size:100"`
	RefreshJTI   string   `gorm:"size:100"`
	ExpiresAt    time.Time
	LastSeenAt   time.Time
	RevokedAt    *time.Time
}

func (samlSessionRecord) TableName() string { return "plg_saml_session" }

type sourceVersionView struct {
	Version      int        `json:"version"`
	Config       SourceForm `json:"config"`
	OperatorID   string     `json:"operatorId"`
	OperatorName string     `json:"operatorName"`
	Reason       string     `json:"reason"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type samlEventView struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Result        string    `json:"result"`
	OperatorID    string    `json:"operatorId"`
	OperatorName  string    `json:"operatorName"`
	UserID        string    `json:"userId"`
	CorrelationID string    `json:"correlationId"`
	IP            string    `json:"ip"`
	Detail        string    `json:"detail"`
	CreatedAt     time.Time `json:"createdAt"`
}

type samlSessionView struct {
	ID           string     `json:"id"`
	UserID       string     `json:"userId"`
	SessionIndex string     `json:"sessionIndex"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	LastSeenAt   time.Time  `json:"lastSeenAt"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
}

func sourceFormFrom(m *Source) SourceForm {
	return SourceForm{
		Name: m.Name, Enabled: m.Enabled, IdPMetadata: m.IdPMetadata, IdPMetadataURL: m.IdPMetadataURL,
		IDPEntityID: m.IDPEntityID, SPEntityID: m.SPEntityID, ExternalBaseURL: m.ExternalBaseURL,
		MetadataCACert: m.MetadataCACert, MetadataSign: m.MetadataSignCert,
		AttrUsername: m.AttrUsername, AttrEmail: m.AttrEmail, AttrNickname: m.AttrNickname,
		AttrGroups: m.AttrGroups, AttrPhone: m.AttrPhone, AttrExternalID: m.AttrExternalID,
		AutoCreate: m.AutoCreate, MatchExisting: m.MatchExisting, MatchProvisionedExternalID: m.MatchProvisionedExternalID,
		DefaultRole: m.DefaultRole, GroupMappings: m.GroupRoleMappings, RequireMapped: m.RequireMappedGroup,
		SyncRoles: m.SyncRolesOnLogin, RevokeEmptyRoles: m.RevokeRolesOnEmptyGroups,
		AllowIDPInit: m.AllowIDPInitiated, SignRequests: m.SignAuthnRequest, NameIDFormat: m.NameIDFormat,
		RequireEncrypted: m.RequireEncryptedAssertion, SignMetadata: m.SignMetadata, AllowSHA1: m.AllowSHA1,
		RequireSignedCBC: m.RequireSignedCBC, RejectUnknownConditions: m.RejectUnknownConditions,
		ForceAuthn: m.ForceAuthn, AuthnContextClassRef: m.AuthnContextClassRef, RequireAuthnContext: m.RequireAuthnContext,
	}
}

func (s *Service) saveVersionDB(ctx context.Context, db *gorm.DB, m *Source, reason string) error {
	raw, err := json.Marshal(sourceFormFrom(m))
	if err != nil {
		return err
	}
	var latest int
	if err := db.Model(&sourceVersion{}).Where("source_id = ?", int64(m.ID)).
		Select("COALESCE(MAX(version), 0)").Scan(&latest).Error; err != nil {
		return err
	}
	operatorID, operatorName := auditActor(ctx)
	row := sourceVersion{
		TenantModel: database.TenantModel{TenantID: m.TenantID}, SourceID: m.ID, Version: latest + 1,
		ConfigJSON: string(raw), OperatorID: operatorID, OperatorName: operatorName, Reason: clipAudit(reason, 500),
	}
	return db.Create(&row).Error
}

func (s *Service) listVersions(ctx context.Context, sourceID idgen.ID) ([]sourceVersionView, error) {
	var rows []sourceVersion
	if err := s.db.WithContext(ctx).Where("source_id = ?", sourceID).Order("version DESC").Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]sourceVersionView, 0, len(rows))
	for _, row := range rows {
		var form SourceForm
		if err := json.Unmarshal([]byte(row.ConfigJSON), &form); err != nil {
			return nil, err
		}
		out = append(out, sourceVersionView{
			Version: row.Version, Config: form, OperatorID: row.OperatorID.String(), OperatorName: row.OperatorName,
			Reason: row.Reason, CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

func (s *Service) rollback(ctx context.Context, sourceID idgen.ID, version int, reason string) (*Source, error) {
	if version <= 0 {
		return nil, errors.New("saml: 版本号无效")
	}
	var row sourceVersion
	if err := s.db.WithContext(ctx).Where("source_id = ? AND version = ?", sourceID, version).Take(&row).Error; err != nil {
		return nil, err
	}
	var form SourceForm
	if err := json.Unmarshal([]byte(row.ConfigJSON), &form); err != nil {
		return nil, err
	}
	return s.update(ctx, sourceID.String(), form, "rollback: "+reason)
}

func (s *Service) recordEvent(ctx context.Context, m *Source, typ, result, correlationID, ip, detail string) {
	if s == nil || s.db == nil || m == nil || m.ID == 0 {
		return
	}
	operatorID, operatorName := auditActor(ctx)
	row := samlEvent{
		TenantModel: database.TenantModel{TenantID: m.TenantID}, SourceID: m.ID, Type: typ, Result: result,
		OperatorID: operatorID, OperatorName: operatorName, CorrelationID: clipAudit(correlationID, 100),
		IP: clipAudit(ip, 64), Detail: clipAudit(detail, 4096),
	}
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Create(&row).Error; err != nil && s.log != nil {
		s.log.Warn("saml: write event failed", zap.Error(err))
	}
}

func (s *Service) listEvents(ctx context.Context, sourceID idgen.ID) ([]samlEventView, error) {
	var rows []samlEvent
	if err := s.db.WithContext(ctx).Where("source_id = ?", sourceID).Order("created_at DESC").Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]samlEventView, 0, len(rows))
	for _, row := range rows {
		out = append(out, samlEventView{
			ID: row.ID.String(), Type: row.Type, Result: row.Result, OperatorID: row.OperatorID.String(),
			OperatorName: row.OperatorName, UserID: row.UserID.String(), CorrelationID: row.CorrelationID,
			IP: row.IP, Detail: row.Detail, CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

func (s *Service) listSessions(ctx context.Context, sourceID idgen.ID) ([]samlSessionView, error) {
	var rows []samlSessionRecord
	if err := s.db.WithContext(ctx).Where("source_id = ?", sourceID).Order("last_seen_at DESC").Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]samlSessionView, 0, len(rows))
	for _, row := range rows {
		out = append(out, samlSessionView{
			ID: row.ID.String(), UserID: row.UserID.String(), SessionIndex: row.SessionIndex,
			ExpiresAt: row.ExpiresAt, LastSeenAt: row.LastSeenAt, RevokedAt: row.RevokedAt,
		})
	}
	return out, nil
}

func (s *Service) revokeSession(ctx context.Context, sourceID, sessionID idgen.ID) error {
	var row samlSessionRecord
	if err := s.db.WithContext(ctx).Where("source_id = ? AND id = ?", sourceID, sessionID).Take(&row).Error; err != nil {
		return err
	}
	if row.RevokedAt == nil {
		revoker, ok := s.issuer.(TokenRevoker)
		if !ok {
			return errors.New("saml: token revoker unavailable")
		}
		if err := revoker.RevokeByJTI(ctx, row.AccessJTI, row.RefreshJTI); err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := s.db.WithContext(ctx).Model(&samlSessionRecord{}).Where("id = ?", row.ID).Update("revoked_at", now).Error; err != nil {
			return err
		}
	}
	return nil
}

func auditActor(ctx context.Context) (idgen.ID, string) {
	if cl := auth.ClaimsFromContext(ctx); cl != nil {
		return cl.UserID, cl.Username
	}
	if id, name, ok := auth.UserFromContext(ctx); ok {
		return id, name
	}
	return 0, "system"
}

func recordBindingSessionDB(ctx context.Context, s *Service, cl *auth.Claims) {
	if s == nil || s.db == nil || cl == nil {
		return
	}
	sourceID, nameID, sessionIndex, ok := parseSAMLBinding(cl.SessionBinding)
	if !ok || nameID == "" {
		return
	}
	key := sessionRecordKey(sourceID, nameID, sessionIndex, cl.SessionStart)
	sum := sha256.Sum256([]byte(nameID))
	now := time.Now().UTC()
	row := samlSessionRecord{
		TenantModel: database.TenantModel{TenantID: cl.TenantID}, SourceID: sourceID, UserID: cl.UserID,
		NameIDHash: hex.EncodeToString(sum[:]), SessionIndex: sessionIndex, SessionKey: key,
		AccessJTI: cl.ID, RefreshJTI: cl.RID, ExpiresAt: cl.ExpiresAt.Time, LastSeenAt: now,
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"access_jti", "refresh_jti", "expires_at", "last_seen_at", "revoked_at"}),
	}).Create(&row).Error; err != nil && s.log != nil {
		s.log.Warn("saml: record session failed", zap.Error(err))
	}
}

func sessionRecordKey(sourceID idgen.ID, nameID, sessionIndex string, sessionStart int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%d", int64(sourceID), nameID, sessionIndex, sessionStart)))
	return hex.EncodeToString(sum[:])
}

func correlationID(c *gin.Context) string {
	return c.GetString(middleware.RequestIDKey)
}

func sanitizeDiagnosticDetail(err error) string {
	if err == nil {
		return ""
	}
	return clipAudit(strings.TrimSpace(err.Error()), 2000)
}
