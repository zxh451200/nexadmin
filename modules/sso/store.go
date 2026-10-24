package sso

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// userRow 是协议层需要的用户快照（含角色 ID、角色编码、部门名）。
type userRow struct {
	ID        idgen.ID
	TenantID  idgen.ID
	Username  string
	Nickname  string
	Avatar    string
	Phone     string
	Email     string
	Dept      string
	Super     bool
	RoleIDs   []string
	RoleCodes []string
}

type refreshState int

const (
	refreshOK refreshState = iota
	refreshUnknown
	refreshReused // 已被轮换/撤销过又来用：疑似泄露
)

// store 是协议层依赖的持久化接口（单测用内存实现替换）。
type store interface {
	appByClientID(ctx context.Context, clientID string) (*App, error) // 不存在返回 nil, nil
	loadUser(ctx context.Context, id idgen.ID) (*userRow, error)      // 不存在/停用/已删除返回 nil, nil
	createRefresh(ctx context.Context, r *Refresh) error
	consumeRefresh(ctx context.Context, hash string, now time.Time) (*Refresh, refreshState, error)
	revokeFamily(ctx context.Context, family string) error
	revokeUserApp(ctx context.Context, clientID string, userID idgen.ID) error
}

type gormStore struct{ db *gorm.DB }

func (s *gormStore) appByClientID(ctx context.Context, clientID string) (*App, error) {
	var a App
	err := s.db.WithContext(tenant.Skip(ctx)).Where("client_id = ?", clientID).Take(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

type dbUser struct {
	ID       idgen.ID `gorm:"column:id"`
	TenantID idgen.ID `gorm:"column:tenant_id"`
	Username string
	Nickname string
	Avatar   string
	Phone    string
	Email    string
	DeptID   idgen.ID
	Status   string
	IsSuper  bool
}

func (dbUser) TableName() string { return "sys_user" }

func (s *gormStore) loadUser(ctx context.Context, id idgen.ID) (*userRow, error) {
	db := s.db.WithContext(tenant.Skip(ctx))
	var u dbUser
	err := db.Where("id = ? AND deleted_at IS NULL", int64(id)).Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if u.Status != "1" {
		return nil, nil
	}
	out := &userRow{ID: u.ID, TenantID: u.TenantID, Username: u.Username, Nickname: u.Nickname, Avatar: u.Avatar,
		Phone: u.Phone, Email: u.Email, Super: u.IsSuper, RoleIDs: []string{}, RoleCodes: []string{}}
	var roles []struct {
		ID   idgen.ID
		Code string
	}
	if err := db.Table("sys_user_role AS ur").Select("r.id AS id, r.code AS code").
		Joins("JOIN sys_role r ON r.id = ur.role_id").
		Where("ur.user_id = ? AND r.status = ?", int64(id), "1").Order("r.sort ASC, r.id ASC").Scan(&roles).Error; err != nil {
		return nil, err
	}
	for _, r := range roles {
		out.RoleIDs = append(out.RoleIDs, r.ID.String())
		out.RoleCodes = append(out.RoleCodes, r.Code)
	}
	if u.DeptID != 0 {
		var name string
		if err := db.Table("sys_dept").Select("name").Where("id = ?", int64(u.DeptID)).Limit(1).Scan(&name).Error; err != nil {
			return nil, err
		}
		out.Dept = name
	}
	return out, nil
}

func (s *gormStore) createRefresh(ctx context.Context, r *Refresh) error {
	ctx = tenant.Skip(ctx)
	// 顺手清理早已过期的行，避免表无限增长。
	_ = s.db.WithContext(ctx).Where("expires_at < ?", time.Now().Add(-24*time.Hour)).Delete(&Refresh{}).Error
	return s.db.WithContext(ctx).Create(r).Error
}

// consumeRefresh 原子地把未使用的 refresh 置为已使用；已使用过的返回 refreshReused。
func (s *gormStore) consumeRefresh(ctx context.Context, hash string, now time.Time) (*Refresh, refreshState, error) {
	db := s.db.WithContext(tenant.Skip(ctx))
	var r Refresh
	err := db.Where("token_hash = ?", hash).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, refreshUnknown, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if r.UsedAt != nil {
		return &r, refreshReused, nil
	}
	res := db.Model(&Refresh{}).Where("id = ? AND used_at IS NULL", int64(r.ID)).Update("used_at", now)
	if res.Error != nil {
		return nil, 0, res.Error
	}
	if res.RowsAffected != 1 {
		return &r, refreshReused, nil // 并发下被别人抢先使用
	}
	return &r, refreshOK, nil
}

func (s *gormStore) revokeFamily(ctx context.Context, family string) error {
	return s.db.WithContext(tenant.Skip(ctx)).Where("family_id = ?", family).Delete(&Refresh{}).Error
}

func (s *gormStore) revokeUserApp(ctx context.Context, clientID string, userID idgen.ID) error {
	return s.db.WithContext(tenant.Skip(ctx)).Where("client_id = ? AND user_id = ?", clientID, int64(userID)).Delete(&Refresh{}).Error
}
