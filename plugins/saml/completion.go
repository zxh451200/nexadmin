package saml

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/auth"
	"nexadmin/pkg/idgen"
)

type loginCompletion struct {
	SourceID    string   `json:"sourceId"`
	NameID      string   `json:"nameId"`
	RoleCodes   []string `json:"roleCodes"`
	DefaultRole string   `json:"defaultRole"`
}

type roleInitialization struct {
	TenantID  int64     `gorm:"column:tenant_id"`
	UserID    int64     `gorm:"column:user_id"`
	SourceID  int64     `gorm:"column:source_id"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (roleInitialization) TableName() string { return "plg_saml_role_init" }

func (s *Service) registerLoginCompletion(issuer *auth.Service) error {
	return issuer.RegisterLoginCompletion("saml", s.completeLogin)
}

func (s *Service) markRoleInitializationPending(ctx context.Context, m *Source, userID idgen.ID) error {
	row := roleInitialization{TenantID: int64(m.TenantID), UserID: int64(userID), SourceID: int64(m.ID), CreatedAt: time.Now().UTC()}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func (s *Service) roleInitializationPending(ctx context.Context, m *Source, userID idgen.ID) (bool, error) {
	var row roleInitialization
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND source_id = ?",
		int64(m.TenantID), int64(userID), int64(m.ID)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) clearRoleInitialization(ctx context.Context, m *Source, userID idgen.ID) error {
	return s.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND source_id = ?",
		int64(m.TenantID), int64(userID), int64(m.ID)).Delete(&roleInitialization{}).Error
}

// completeLogin 在全部登录门通过后恢复 SLO 信息并对账角色，MFA 挑战不会绕过它。
func (s *Service) completeLogin(ctx context.Context, u *auth.User, raw json.RawMessage) {
	if u == nil {
		return
	}
	var data loginCompletion
	if err := json.Unmarshal(raw, &data); err != nil {
		s.logCompletionError(u, "invalid completion payload", err)
		return
	}
	sourceID, err := idgen.Parse(data.SourceID)
	if err != nil || sourceID == 0 {
		s.logCompletionError(u, "invalid completion source", err)
		return
	}
	m, err := s.Get(ctx, sourceID.String())
	if err != nil || m == nil || m.TenantID != u.TenantID {
		s.logCompletionError(u, "completion source unavailable", err)
		return
	}
	initial := u.ExternalCreated
	if !initial {
		initial, err = s.roleInitializationPending(ctx, m, u.ID)
		if err != nil {
			s.logCompletionError(u, "read role initialization state failed", err)
			return
		}
	}
	if initial {
		if err := s.markRoleInitializationPending(ctx, m, u.ID); err != nil {
			s.logCompletionError(u, "mark role initialization pending failed", err)
			return
		}
	}
	if s.cache != nil && data.NameID != "" {
		if err := s.cache.Set(ctx, sloKey(u.TenantID, sourceID, u.ID), data.NameID, 7*24*time.Hour); err != nil {
			s.logCompletionError(u, "store SAML NameID failed", err)
		}
	}
	desired := data.RoleCodes
	if initial {
		desired = initialCompletionRoles(desired, data.DefaultRole)
	}
	if err := s.syncRoles(ctx, m, u, desired, initial); err != nil {
		return
	}
	if initial {
		if err := s.clearRoleInitialization(ctx, m, u.ID); err != nil {
			s.logCompletionError(u, "clear role initialization state failed", err)
		}
	}
}

func initialCompletionRoles(mapped []string, defaultRole string) []string {
	if len(mapped) > 0 {
		return mapped
	}
	role := strings.TrimSpace(defaultRole)
	if role == "" {
		role = "guest"
	}
	return []string{role}
}

func (s *Service) logCompletionError(u *auth.User, message string, err error) {
	if s.log == nil {
		return
	}
	fields := []zap.Field{zap.Int64("userId", int64(u.ID)), zap.String("source", "saml")}
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	s.log.Warn("saml login completion: "+message, fields...)
}
