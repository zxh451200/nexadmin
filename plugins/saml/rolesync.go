package saml

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/auth"
	"nexadmin/pkg/idgen"
)

// roleGrant 记录「本来源授予过哪些角色」以及本次授权是否由该来源实际创建。
// 仅有授予记录不足以证明 sys_user_role 的归属；历史记录因此默认不拥有授权。
type roleGrant struct {
	TenantID       int64     `gorm:"column:tenant_id"`
	UserID         int64     `gorm:"column:user_id"`
	SourceID       int64     `gorm:"column:source_id"`
	RoleID         int64     `gorm:"column:role_id"`
	RoleCode       string    `gorm:"column:role_code"`
	OwnsAssignment bool      `gorm:"column:owns_assignment"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (roleGrant) TableName() string { return "plg_saml_role_grant" }

type roleTarget struct {
	Code      string
	ID        int64
	Available bool
}

type roleChange struct {
	Code           string
	RoleID         int64
	StoredCode     string
	Effective      bool
	OwnsAssignment bool
	OtherSources   int64
	HasGrant       bool
	LegacyCode     string
}

type rolePlan struct {
	Previous    []string
	Add         []roleChange
	Remove      []roleChange
	Unavailable []string
}

func matchingRoleGrant(target roleTarget, grants []roleGrant) *roleGrant {
	for i := range grants {
		grant := &grants[i]
		if target.ID != 0 && grant.RoleID == target.ID {
			return grant
		}
		if grant.RoleID == 0 && grant.RoleCode == target.Code {
			return grant
		}
	}
	return nil
}

func planRoleGrantChanges(desired []roleTarget, prev []roleGrant, currentCodes map[int64]string, assigned map[int64]bool, revokeOnEmpty bool) (previous []string, remove []roleGrant, add []roleTarget) {
	desiredIDs := make(map[int64]bool, len(desired))
	desiredCodes := make(map[string]bool, len(desired))
	resolvedCodes := make(map[string]int64, len(desired))
	seen := make(map[string]bool, len(desired))
	for _, target := range desired {
		key := target.Code
		if target.ID != 0 {
			key = "id:" + idgen.ID(target.ID).String()
			desiredIDs[target.ID] = true
			resolvedCodes[target.Code] = target.ID
		}
		desiredCodes[target.Code] = true
		if !seen[key] {
			seen[key] = true
			add = append(add, target)
		}
	}
	for _, grant := range prev {
		code := grant.RoleCode
		if current := currentCodes[grant.RoleID]; current != "" {
			code = current
		}
		previous = append(previous, code)
		keep := desiredIDs[grant.RoleID]
		if grant.RoleID == 0 {
			keep = desiredCodes[code]
		} else if resolvedID, resolved := resolvedCodes[code]; !resolved || resolvedID == 0 {
			keep = keep || desiredCodes[code]
		}
		if !keep && (len(desired) > 0 || revokeOnEmpty) {
			remove = append(remove, grant)
		}
	}
	kept := add[:0]
	for _, target := range add {
		grant := matchingRoleGrant(target, prev)
		if grant != nil && grant.RoleID != 0 && assigned[target.ID] {
			continue
		}
		kept = append(kept, target)
	}
	return previous, remove, kept
}

func currentRoleCodes(db *gorm.DB, tenantID int64, grants []roleGrant) (map[int64]string, error) {
	ids := make([]int64, 0, len(grants))
	seen := map[int64]bool{}
	for _, grant := range grants {
		if grant.RoleID != 0 && !seen[grant.RoleID] {
			seen[grant.RoleID] = true
			ids = append(ids, grant.RoleID)
		}
	}
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID   int64
		Code string
	}
	if err := db.Table("sys_role").Select("id, code").Where("tenant_id = ? AND id IN ?", tenantID, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row.Code
	}
	return out, nil
}

func roleTargets(db *gorm.DB, tenantID int64, desired []string) ([]roleTarget, error) {
	codes := make([]string, 0, len(desired))
	seen := map[string]bool{}
	for _, code := range desired {
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return nil, nil
	}
	var rows []struct {
		ID        int64
		Code      string
		Status    string
		Builtin   bool
		DeletedAt gorm.DeletedAt
	}
	if err := db.Table("sys_role").Select("id, code, status, builtin, deleted_at").
		Where("tenant_id = ? AND code IN ? AND deleted_at IS NULL", tenantID, codes).Scan(&rows).Error; err != nil {
		return nil, err
	}
	byCode := make(map[string][]struct {
		ID        int64
		Code      string
		Status    string
		Builtin   bool
		DeletedAt gorm.DeletedAt
	}, len(rows))
	for _, row := range rows {
		byCode[row.Code] = append(byCode[row.Code], row)
	}
	out := make([]roleTarget, 0, len(codes))
	for _, code := range codes {
		target := roleTarget{Code: code}
		matches := byCode[code]
		if len(matches) == 1 {
			role := matches[0]
			target.ID = role.ID
			target.Available = !role.DeletedAt.Valid && role.Status == "1" && !role.Builtin
		}
		out = append(out, target)
	}
	return out, nil
}

func sameRoleScope(db *gorm.DB, tenantID, userID int64, grant roleGrant) *gorm.DB {
	db = db.Where("tenant_id = ? AND user_id = ?", tenantID, userID)
	if grant.RoleID != 0 {
		return db.Where("role_id = ?", grant.RoleID)
	}
	return db.Where("role_id = 0 AND role_code = ?", grant.RoleCode)
}

func planRoleChanges(db *gorm.DB, tenantID, userID, sourceID int64, desired []string, revokeOnEmpty bool) (*rolePlan, error) {
	var prev []roleGrant
	if err := db.Where("tenant_id = ? AND user_id = ? AND source_id = ?", tenantID, userID, sourceID).
		Order("role_id ASC, role_code ASC").Find(&prev).Error; err != nil {
		return nil, err
	}
	currentCodes, err := currentRoleCodes(db, tenantID, prev)
	if err != nil {
		return nil, err
	}
	targets, err := roleTargets(db, tenantID, desired)
	if err != nil {
		return nil, err
	}
	targetIDs := make([]int64, 0, len(targets))
	for _, target := range targets {
		if target.Available && target.ID != 0 {
			targetIDs = append(targetIDs, target.ID)
		}
	}
	assigned := make(map[int64]bool, len(targetIDs))
	if len(targetIDs) > 0 {
		var rows []struct{ RoleID int64 }
		if err := db.Table("sys_user_role").Select("role_id").Where("user_id = ? AND role_id IN ?", userID, targetIDs).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			assigned[row.RoleID] = true
		}
	}
	previous, removals, additions := planRoleGrantChanges(targets, prev, currentCodes, assigned, revokeOnEmpty)
	plan := &rolePlan{Previous: previous, Add: []roleChange{}, Remove: []roleChange{}, Unavailable: []string{}}
	for _, grant := range removals {
		code := grant.RoleCode
		if current := currentCodes[grant.RoleID]; current != "" {
			code = current
		}
		change := roleChange{Code: code, StoredCode: grant.RoleCode, RoleID: grant.RoleID, OwnsAssignment: grant.OwnsAssignment}
		if err := sameRoleScope(db.Model(&roleGrant{}), tenantID, userID, grant).
			Where("source_id <> ?", sourceID).Count(&change.OtherSources).Error; err != nil {
			return nil, err
		}
		change.Effective = change.OwnsAssignment && change.RoleID != 0 && change.OtherSources == 0
		plan.Remove = append(plan.Remove, change)
	}
	for _, target := range additions {
		if !target.Available {
			plan.Unavailable = append(plan.Unavailable, target.Code)
			continue
		}
		change := roleChange{Code: target.Code, RoleID: target.ID, Effective: !assigned[target.ID]}
		if grant := matchingRoleGrant(target, prev); grant != nil {
			change.HasGrant = true
			change.OwnsAssignment = grant.OwnsAssignment
			if grant.RoleID == 0 {
				change.LegacyCode = grant.RoleCode
			}
		}
		plan.Add = append(plan.Add, change)
	}
	for _, target := range targets {
		if !target.Available && !containsCode(plan.Unavailable, target.Code) {
			plan.Unavailable = append(plan.Unavailable, target.Code)
		}
	}
	return plan, nil
}

func containsCode(codes []string, code string) bool {
	for _, current := range codes {
		if current == code {
			return true
		}
	}
	return false
}

func (s *Service) revokeSourceRolesDB(ctx context.Context, db *gorm.DB, m *Source) error {
	var grants []roleGrant
	if err := db.Where("source_id = ?", int64(m.ID)).Find(&grants).Error; err != nil {
		return err
	}
	users := make(map[int64]bool, len(grants))
	for _, grant := range grants {
		users[grant.UserID] = true
	}
	revoke := *m
	revoke.RevokeRolesOnEmptyGroups = true
	for userID := range users {
		u := &auth.User{ID: idgen.ID(userID), TenantID: m.TenantID}
		if _, _, _, err := s.reconcileRolesDB(ctx, db, &revoke, u, nil); err != nil {
			return err
		}
	}
	return nil
}

// SyncRolesOnLogin 把「本次断言映射出的角色」与本地授权对齐。
// 失败不阻断登录（会话已经签发），但每次都会留一条带计数的日志，便于区分「没变化」和「没跑」。
func (s *Service) SyncRolesOnLogin(ctx context.Context, m *Source, u *auth.User, desired []string) {
	_ = s.syncRoles(ctx, m, u, desired, false)
}

func (s *Service) syncRoles(ctx context.Context, m *Source, u *auth.User, desired []string, initial bool) error {
	if m == nil || u == nil || (!initial && !m.SyncRolesOnLogin) {
		return nil
	}
	added, removed, skipped, err := s.reconcileRoles(ctx, m, u, desired)
	if err != nil {
		if s.log != nil {
			s.log.Warn("saml role sync failed",
				zap.Int64("userId", int64(u.ID)), zap.String("sourceId", m.ID.String()), zap.Error(err))
		}
		return err
	}
	if initial && len(skipped) > 0 {
		err = errors.New("saml: initial roles unavailable")
		if s.log != nil {
			s.log.Warn("saml role initialization pending",
				zap.Int64("userId", int64(u.ID)), zap.String("sourceId", m.ID.String()), zap.Strings("unavailable", skipped))
		}
		return err
	}
	if s.log != nil {
		s.log.Info("saml role sync finished",
			zap.Int64("userId", int64(u.ID)),
			zap.String("sourceId", m.ID.String()),
			zap.Int("desired", len(desired)),
			zap.Strings("added", added),
			zap.Strings("removed", removed),
			zap.Strings("skipped", skipped))
	}
	return nil
}

// reconcileRoles 在一个事务里完成补授与收回。锁住用户行，避免同一账号并发登录产生重复授权。
func (s *Service) reconcileRoles(ctx context.Context, m *Source, u *auth.User, desired []string) (added, removed, skipped []string, err error) {
	return s.reconcileRolesDB(ctx, s.db.WithContext(ctx), m, u, desired)
}

func (s *Service) reconcileRolesDB(ctx context.Context, db *gorm.DB, m *Source, u *auth.User, desired []string) (added, removed, skipped []string, err error) {
	tenantID, userID, sourceID := int64(u.TenantID), int64(u.ID), int64(m.ID)
	err = db.Transaction(func(tx *gorm.DB) error {
		var locked struct{ ID int64 }
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table("sys_user").Select("id").
			Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, userID).Take(&locked).Error; err != nil {
			return err
		}
		plan, err := planRoleChanges(tx, tenantID, userID, sourceID, desired, m.RevokeRolesOnEmptyGroups)
		if err != nil {
			return err
		}
		for _, change := range plan.Remove {
			if change.Effective {
				if err := tx.Exec("DELETE FROM sys_user_role WHERE user_id = ? AND role_id = ?", userID, change.RoleID).Error; err != nil {
					return err
				}
				removed = append(removed, change.Code)
			}
			if change.OwnsAssignment && change.OtherSources > 0 && change.RoleID != 0 {
				var next struct{ SourceID int64 }
				if err := tx.Table("plg_saml_role_grant").Select("source_id").
					Where("tenant_id = ? AND user_id = ? AND role_id = ? AND source_id <> ?", tenantID, userID, change.RoleID, sourceID).
					Order("source_id ASC").Limit(1).Scan(&next).Error; err != nil {
					return err
				}
				if err := tx.Model(&roleGrant{}).
					Where("tenant_id = ? AND user_id = ? AND role_id = ?", tenantID, userID, change.RoleID).
					Update("owns_assignment", false).Error; err != nil {
					return err
				}
				if next.SourceID != 0 {
					if err := tx.Model(&roleGrant{}).
						Where("tenant_id = ? AND user_id = ? AND role_id = ? AND source_id = ?", tenantID, userID, change.RoleID, next.SourceID).
						Update("owns_assignment", true).Error; err != nil {
						return err
					}
				}
			}
			query := tx.Where("tenant_id = ? AND user_id = ? AND source_id = ?", tenantID, userID, sourceID)
			if change.RoleID != 0 {
				query = query.Where("role_id = ?", change.RoleID)
			} else {
				query = query.Where("role_id = 0 AND role_code = ?", change.StoredCode)
			}
			if err := query.Delete(&roleGrant{}).Error; err != nil {
				return err
			}
		}
		for _, change := range plan.Add {
			if change.Effective {
				if err := tx.Model(&roleGrant{}).
					Where("tenant_id = ? AND user_id = ? AND role_id = ?", tenantID, userID, change.RoleID).
					Update("owns_assignment", false).Error; err != nil {
					return err
				}
				if err := tx.Exec("INSERT INTO sys_user_role (user_id, role_id) VALUES (?, ?)", userID, change.RoleID).Error; err != nil {
					return err
				}
				added = append(added, change.Code)
			}
			if change.HasGrant {
				query := tx.Model(&roleGrant{}).Where("tenant_id = ? AND user_id = ? AND source_id = ?", tenantID, userID, sourceID)
				if change.LegacyCode != "" {
					query = query.Where("role_id = 0 AND role_code = ?", change.LegacyCode)
				} else {
					query = query.Where("role_id = ?", change.RoleID)
				}
				if err := query.Updates(map[string]any{
					"role_id": change.RoleID, "role_code": change.Code, "owns_assignment": change.Effective,
				}).Error; err != nil {
					return err
				}
				continue
			}
			grant := roleGrant{TenantID: tenantID, UserID: userID, SourceID: sourceID, RoleID: change.RoleID, RoleCode: change.Code,
				OwnsAssignment: change.Effective, CreatedAt: time.Now().UTC()}
			if err := tx.Create(&grant).Error; err != nil {
				return err
			}
		}
		skipped = append(skipped, plan.Unavailable...)
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return added, removed, skipped, nil
}

// roleAssignmentsReplaced 由用户模块在整体替换角色时同步调用；未变化的授权保留来源记账。
func (s *Service) roleAssignmentsReplaced(ctx context.Context, tx *gorm.DB, tenantID, userID idgen.ID, previousRoles, roleIDs []idgen.ID) error {
	previous := make(map[int64]bool, len(previousRoles))
	for _, roleID := range previousRoles {
		previous[int64(roleID)] = true
	}
	selected := make(map[int64]bool, len(roleIDs))
	for _, roleID := range roleIDs {
		selected[int64(roleID)] = true
	}
	var grants []roleGrant
	if err := tx.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", int64(tenantID), int64(userID)).Find(&grants).Error; err != nil {
		return err
	}
	legacyCodes := make([]string, 0, len(grants))
	seenCodes := map[string]bool{}
	for _, grant := range grants {
		if grant.RoleID == 0 && !seenCodes[grant.RoleCode] {
			seenCodes[grant.RoleCode] = true
			legacyCodes = append(legacyCodes, grant.RoleCode)
		}
	}
	rolesByCode := make(map[string][]int64, len(legacyCodes))
	activeByCode := make(map[string][]int64, len(legacyCodes))
	if len(legacyCodes) > 0 {
		var rows []struct {
			ID        int64
			Code      string
			DeletedAt gorm.DeletedAt
		}
		if err := tx.WithContext(ctx).Table("sys_role").Select("id, code, deleted_at").
			Where("tenant_id = ? AND code IN ?", int64(tenantID), legacyCodes).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			rolesByCode[row.Code] = append(rolesByCode[row.Code], row.ID)
			if !row.DeletedAt.Valid {
				activeByCode[row.Code] = append(activeByCode[row.Code], row.ID)
			}
		}
	}
	for _, grant := range grants {
		roleID := grant.RoleID
		if roleID == 0 {
			roleID = resolveLegacyRoleID(rolesByCode[grant.RoleCode], activeByCode[grant.RoleCode], previous)
		}
		if roleID == 0 || (selected[roleID] && previous[roleID]) {
			continue
		}
		query := tx.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", int64(tenantID), int64(userID))
		if grant.RoleID != 0 {
			query = query.Where("role_id = ?", grant.RoleID)
		} else {
			query = query.Where("role_id = 0 AND role_code = ?", grant.RoleCode)
		}
		if err := query.Delete(&roleGrant{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func resolveLegacyRoleID(roleIDs, activeIDs []int64, previous map[int64]bool) int64 {
	if len(roleIDs) == 1 {
		return roleIDs[0]
	}
	var resolved int64
	for _, roleID := range roleIDs {
		if !previous[roleID] {
			continue
		}
		if resolved != 0 {
			return 0
		}
		resolved = roleID
	}
	if resolved != 0 {
		return resolved
	}
	if len(activeIDs) == 1 {
		return activeIDs[0]
	}
	return 0
}
