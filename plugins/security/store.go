package security

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// store 是数据访问边界：业务逻辑只依赖它，单测里用内存实现替换（沙箱没有真实数据库）。
// 所有方法都显式带租户 ID，并同时附加 tenant_id 条件（不只靠 GORM 租户 scope）。
type store interface {
	GetTOTP(ctx context.Context, tid, uid idgen.ID) (*TOTPRow, error) // 不存在返回 (nil, nil)
	SavePending(ctx context.Context, tid, uid idgen.ID, username, secretEnc string) error
	EnableTOTP(ctx context.Context, tid, uid idgen.ID, hashes []string) error
	DisableTOTP(ctx context.Context, tid, uid idgen.ID) error
	EnsureGrace(ctx context.Context, tid, uid idgen.ID, username string, until time.Time) (*TOTPRow, error)
	ListTOTP(ctx context.Context, tid idgen.ID, keyword string, page, size int) ([]TOTPRow, int64, error)
	ResetUser(ctx context.Context, tid, uid idgen.ID, lg *LogRow) error

	ReplaceRecovery(ctx context.Context, tid, uid idgen.ID, hashes []string) error
	ConsumeRecovery(ctx context.Context, tid, uid idgen.ID, hash string) (bool, error)
	CountRecovery(ctx context.Context, tid, uid idgen.ID) (int64, error)

	GetPolicy(ctx context.Context, tid idgen.ID) (*Policy, error) // 不存在返回 (nil, nil)
	SavePolicy(ctx context.Context, tid idgen.ID, p *Policy) error

	ListRules(ctx context.Context, tid idgen.ID) ([]IPRule, error)
	GetRule(ctx context.Context, tid, id idgen.ID) (*IPRule, error)
	SaveRule(ctx context.Context, tid idgen.ID, r *IPRule) error
	DeleteRule(ctx context.Context, tid, id idgen.ID) error

	AddLog(ctx context.Context, tid idgen.ID, l *LogRow) error
	ListLogs(ctx context.Context, tid idgen.ID, page, size int) ([]LogRow, int64, error)
}

// scoped 给 ctx 绑定租户；tid==0（平台级超管）时跳过租户隔离。
func scoped(ctx context.Context, tid idgen.ID) context.Context {
	if tid == 0 {
		return tenant.Skip(ctx)
	}
	return tenant.With(ctx, tid)
}

type gormStore struct{ db *gorm.DB }

func (s *gormStore) q(ctx context.Context, tid idgen.ID) *gorm.DB {
	return s.db.WithContext(scoped(ctx, tid))
}

func (s *gormStore) GetTOTP(ctx context.Context, tid, uid idgen.ID) (*TOTPRow, error) {
	var r TOTPRow
	err := s.q(ctx, tid).Where("tenant_id = ? AND user_id = ?", tid, uid).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &r, err
}

func (s *gormStore) SavePending(ctx context.Context, tid, uid idgen.ID, username, secretEnc string) error {
	return s.db.WithContext(scoped(ctx, tid)).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&TOTPRow{}).Where("tenant_id = ? AND user_id = ? AND enabled = ?", tid, uid, false).
			Updates(map[string]any{"secret_enc": secretEnc, "username": username})
		if res.Error != nil || res.RowsAffected > 0 {
			return res.Error
		}
		var n int64
		if err := tx.Model(&TOTPRow{}).Where("tenant_id = ? AND user_id = ?", tid, uid).Count(&n).Error; err != nil || n > 0 {
			return err // 已启用的不能覆盖
		}
		row := TOTPRow{UserID: uid, Username: username, SecretEnc: secretEnc}
		row.TenantID = tid
		return tx.Create(&row).Error
	})
}

func (s *gormStore) EnableTOTP(ctx context.Context, tid, uid idgen.ID, hashes []string) error {
	return s.db.WithContext(scoped(ctx, tid)).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		res := tx.Model(&TOTPRow{}).Where("tenant_id = ? AND user_id = ? AND enabled = ?", tid, uid, false).
			Updates(map[string]any{"enabled": true, "enabled_at": now, "grace_until": nil})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("security: no pending totp to enable")
		}
		return replaceRecoveryTx(tx, tid, uid, hashes)
	})
}

func (s *gormStore) DisableTOTP(ctx context.Context, tid, uid idgen.ID) error {
	return s.db.WithContext(scoped(ctx, tid)).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND user_id = ?", tid, uid).Delete(&RecoveryRow{}).Error; err != nil {
			return err
		}
		return tx.Where("tenant_id = ? AND user_id = ?", tid, uid).Delete(&TOTPRow{}).Error
	})
}

func (s *gormStore) EnsureGrace(ctx context.Context, tid, uid idgen.ID, username string, until time.Time) (*TOTPRow, error) {
	r, err := s.GetTOTP(ctx, tid, uid)
	if err != nil {
		return nil, err
	}
	if r == nil {
		row := TOTPRow{UserID: uid, Username: username, GraceUntil: &until}
		row.TenantID = tid
		if err := s.q(ctx, tid).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return nil, err
		}
		return s.GetTOTP(ctx, tid, uid) // 并发时以先写入者为准
	}
	if r.GraceUntil == nil && !r.Enabled {
		if err := s.q(ctx, tid).Model(&TOTPRow{}).Where("tenant_id = ? AND user_id = ? AND grace_until IS NULL", tid, uid).
			Update("grace_until", until).Error; err != nil {
			return nil, err
		}
		return s.GetTOTP(ctx, tid, uid)
	}
	return r, nil
}

func (s *gormStore) ListTOTP(ctx context.Context, tid idgen.ID, keyword string, page, size int) ([]TOTPRow, int64, error) {
	q := s.q(ctx, tid).Model(&TOTPRow{}).Where("tenant_id = ?", tid)
	if keyword != "" {
		q = q.Where("username LIKE ?", "%"+escapeLike(keyword)+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []TOTPRow
	err := q.Order("updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

func (s *gormStore) ResetUser(ctx context.Context, tid, uid idgen.ID, lg *LogRow) error {
	return s.db.WithContext(scoped(ctx, tid)).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND user_id = ?", tid, uid).Delete(&RecoveryRow{}).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND user_id = ?", tid, uid).Delete(&TOTPRow{}).Error; err != nil {
			return err
		}
		lg.TenantID = tid
		return tx.Create(lg).Error // 日志与重置同事务：日志写不进去就不重置
	})
}

func replaceRecoveryTx(tx *gorm.DB, tid, uid idgen.ID, hashes []string) error {
	if err := tx.Where("tenant_id = ? AND user_id = ?", tid, uid).Delete(&RecoveryRow{}).Error; err != nil {
		return err
	}
	for _, h := range hashes {
		row := RecoveryRow{UserID: uid, CodeHash: h}
		row.TenantID = tid
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *gormStore) ReplaceRecovery(ctx context.Context, tid, uid idgen.ID, hashes []string) error {
	return s.db.WithContext(scoped(ctx, tid)).Transaction(func(tx *gorm.DB) error {
		return replaceRecoveryTx(tx, tid, uid, hashes)
	})
}

func (s *gormStore) ConsumeRecovery(ctx context.Context, tid, uid idgen.ID, hash string) (bool, error) {
	res := s.q(ctx, tid).Model(&RecoveryRow{}).
		Where("tenant_id = ? AND user_id = ? AND code_hash = ? AND used_at IS NULL", tid, uid, hash).
		Update("used_at", time.Now())
	return res.RowsAffected == 1, res.Error // 单条 UPDATE 原子：并发提交同一恢复码只有一个 RowsAffected==1
}

func (s *gormStore) CountRecovery(ctx context.Context, tid, uid idgen.ID) (int64, error) {
	var n int64
	err := s.q(ctx, tid).Model(&RecoveryRow{}).Where("tenant_id = ? AND user_id = ? AND used_at IS NULL", tid, uid).Count(&n).Error
	return n, err
}

func (s *gormStore) GetPolicy(ctx context.Context, tid idgen.ID) (*Policy, error) {
	var p Policy
	err := s.q(ctx, tid).Where("tenant_id = ?", tid).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, err
}

func (s *gormStore) SavePolicy(ctx context.Context, tid idgen.ID, p *Policy) error {
	old, err := s.GetPolicy(ctx, tid)
	if err != nil {
		return err
	}
	if old == nil {
		p.TenantID = tid
		return s.q(ctx, tid).Create(p).Error
	}
	return s.q(ctx, tid).Model(&Policy{}).Where("tenant_id = ? AND id = ?", tid, old.ID).
		Updates(map[string]any{"mode": p.Mode, "roles": p.Roles, "grace_days": p.GraceDays, "updated_by": p.UpdatedBy}).Error
}

func (s *gormStore) ListRules(ctx context.Context, tid idgen.ID) ([]IPRule, error) {
	var rows []IPRule
	err := s.q(ctx, tid).Where("tenant_id = ?", tid).Order("created_at ASC, id ASC").Find(&rows).Error
	return rows, err
}

func (s *gormStore) GetRule(ctx context.Context, tid, id idgen.ID) (*IPRule, error) {
	var r IPRule
	err := s.q(ctx, tid).Where("tenant_id = ? AND id = ?", tid, id).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &r, err
}

func (s *gormStore) SaveRule(ctx context.Context, tid idgen.ID, r *IPRule) error {
	if r.ID == 0 {
		r.TenantID = tid
		return s.q(ctx, tid).Create(r).Error
	}
	return s.q(ctx, tid).Model(&IPRule{}).Where("tenant_id = ? AND id = ?", tid, r.ID).
		Updates(map[string]any{"action": r.Action, "cidr": r.CIDR, "scope": r.Scope, "remark": r.Remark, "enabled": r.Enabled}).Error
}

func (s *gormStore) DeleteRule(ctx context.Context, tid, id idgen.ID) error {
	return s.q(ctx, tid).Where("tenant_id = ? AND id = ?", tid, id).Delete(&IPRule{}).Error
}

func (s *gormStore) AddLog(ctx context.Context, tid idgen.ID, l *LogRow) error {
	l.TenantID = tid
	return s.q(ctx, tid).Create(l).Error
}

func (s *gormStore) ListLogs(ctx context.Context, tid idgen.ID, page, size int) ([]LogRow, int64, error) {
	q := s.q(ctx, tid).Model(&LogRow{}).Where("tenant_id = ?", tid)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []LogRow
	err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

func escapeLike(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' || s[i] == '%' || s[i] == '_' {
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	return string(out)
}
