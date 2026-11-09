package saml

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/auth"
)

// roleGrant 记录「本来源授予过哪些角色」：收回时只认这里记过的编码，
// 手工分配的角色与其它来源授予的角色都不会被这条链路碰到。
type roleGrant struct {
	TenantID  int64     `gorm:"column:tenant_id"`
	UserID    int64     `gorm:"column:user_id"`
	SourceID  int64     `gorm:"column:source_id"`
	RoleCode  string    `gorm:"column:role_code"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (roleGrant) TableName() string { return "plg_saml_role_grant" }

// roleGrantDiff 是纯计算：desired 为空表示本次断言没命中任何组，此时一个都不动，
// 避免 IdP 少发一个属性就批量收权。
func roleGrantDiff(desired, prev []string) (add, remove []string) {
	if len(desired) == 0 {
		return nil, nil
	}
	want := make(map[string]bool, len(desired))
	for _, code := range desired {
		want[code] = true
	}
	have := make(map[string]bool, len(prev))
	for _, code := range prev {
		if !want[code] {
			remove = append(remove, code)
		}
		have[code] = true
	}
	for _, code := range desired {
		if !have[code] {
			add = append(add, code)
		}
	}
	return add, remove
}

// SyncRolesOnLogin 把「本次断言映射出的角色」与本地授权对齐。
// 失败不阻断登录（会话已经签发），但每次都会留一条带计数的日志，便于区分「没变化」和「没跑」。
func (s *Service) SyncRolesOnLogin(ctx context.Context, m *Source, u *auth.User, desired []string) {
	if !m.SyncRolesOnLogin || u == nil || len(m.GroupRoleMappings) == 0 {
		return
	}
	added, removed, skipped, err := s.reconcileRoles(ctx, m, u, desired)
	if s.log == nil {
		return
	}
	if err != nil {
		s.log.Warn("saml role sync failed",
			zap.Int64("userId", int64(u.ID)), zap.String("sourceId", m.ID.String()), zap.Error(err))
		return
	}
	s.log.Info("saml role sync finished",
		zap.Int64("userId", int64(u.ID)),
		zap.String("sourceId", m.ID.String()),
		zap.Int("desired", len(desired)),
		zap.Strings("added", added),
		zap.Strings("removed", removed),
		zap.Strings("skipped", skipped))
}

// reconcileRoles 在一个事务里完成补授与收回：
//   - 收回：只删本来源记过、且没有其它来源同时在授予的角色；
//   - 补授：先删后插保证幂等（sys_user_role 是复合主键，重复插入会冲突）；
//   - 角色被改名 / 停用 / 删除时跳过并记进 skipped，下次登录会重新评估。
func (s *Service) reconcileRoles(ctx context.Context, m *Source, u *auth.User, desired []string) (added, removed, skipped []string, err error) {
	var prev []roleGrant
	if err = s.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ? AND source_id = ?", int64(u.TenantID), int64(u.ID), int64(m.ID)).
		Find(&prev).Error; err != nil {
		return nil, nil, nil, err
	}
	prevCodes := make([]string, 0, len(prev))
	for _, g := range prev {
		prevCodes = append(prevCodes, g.RoleCode)
	}
	add, remove := roleGrantDiff(desired, prevCodes)
	if len(add) == 0 && len(remove) == 0 {
		return nil, nil, nil, nil
	}
	tenantID, userID, sourceID := int64(u.TenantID), int64(u.ID), int64(m.ID)

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, code := range remove {
			var otherSources int64
			if err := tx.Model(&roleGrant{}).
				Where("tenant_id = ? AND user_id = ? AND role_code = ? AND source_id <> ?", tenantID, userID, code, sourceID).
				Count(&otherSources).Error; err != nil {
				return err
			}
			var role struct{ ID int64 }
			res := tx.Table("sys_role").Select("id").
				Where("tenant_id = ? AND code = ? AND deleted_at IS NULL", tenantID, code).
				Limit(1).Scan(&role)
			if res.Error != nil {
				return res.Error
			}
			if otherSources == 0 && role.ID != 0 {
				if err := tx.Exec("DELETE FROM sys_user_role WHERE user_id = ? AND role_id = ?", userID, role.ID).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("tenant_id = ? AND user_id = ? AND source_id = ? AND role_code = ?",
				tenantID, userID, sourceID, code).Delete(&roleGrant{}).Error; err != nil {
				return err
			}
			removed = append(removed, code)
		}
		for _, code := range add {
			var role struct{ ID int64 }
			res := tx.Table("sys_role").Select("id").
				Where("tenant_id = ? AND code = ? AND status = ? AND builtin = ? AND deleted_at IS NULL",
					tenantID, code, "1", false).
				Limit(1).Scan(&role)
			if res.Error != nil {
				return res.Error
			}
			if role.ID == 0 {
				skipped = append(skipped, code)
				continue
			}
			if err := tx.Exec("DELETE FROM sys_user_role WHERE user_id = ? AND role_id = ?", userID, role.ID).Error; err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO sys_user_role (user_id, role_id) VALUES (?, ?)", userID, role.ID).Error; err != nil {
				return err
			}
			if err := tx.Where("tenant_id = ? AND user_id = ? AND source_id = ? AND role_code = ?",
				tenantID, userID, sourceID, code).Delete(&roleGrant{}).Error; err != nil {
				return err
			}
			grant := roleGrant{TenantID: tenantID, UserID: userID, SourceID: sourceID, RoleCode: code, CreatedAt: time.Now().UTC()}
			if err := tx.Create(&grant).Error; err != nil {
				return err
			}
			added = append(added, code)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return added, removed, skipped, nil
}
