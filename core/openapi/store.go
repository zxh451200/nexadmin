package openapi

import (
	"context"
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Owner 是令牌所有者的最小快照（仅启用且未删除的用户才会查到）。
type Owner struct {
	ID       idgen.ID `gorm:"column:id"`
	TenantID idgen.ID `gorm:"column:tenant_id"`
	Username string   `gorm:"column:username"`
	IsSuper  bool     `gorm:"column:is_super"`
}

// Store 是中间件依赖的持久化接口（单测用内存实现替换）。所有方法都是跨租户查询（调用时租户还没解析）。
type Store interface {
	PATByHash(ctx context.Context, hash string) (*PAT, error)      // 不存在返回 nil, nil
	OwnerOf(ctx context.Context, userID idgen.ID) (*Owner, error)  // 不存在/停用/已删除返回 nil, nil
	AppByKey(ctx context.Context, key string) (*App, error)        // 不存在返回 nil, nil
	AppPerms(ctx context.Context, app *App) ([]string, error)      // 应用角色集合对应的权限码
	TouchPAT(ctx context.Context, id idgen.ID, at time.Time) error // 更新 last_used_at
	TouchApp(ctx context.Context, id idgen.ID, at time.Time) error
}

// GormStore 是 Store 的 gorm 实现。
type GormStore struct{ DB *gorm.DB }

const statusOn = "1"

func (s GormStore) PATByHash(ctx context.Context, hash string) (*PAT, error) {
	var p PAT
	err := s.DB.WithContext(tenant.Skip(ctx)).Where("token_hash = ?", hash).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s GormStore) OwnerOf(ctx context.Context, userID idgen.ID) (*Owner, error) {
	var o Owner
	res := s.DB.WithContext(tenant.Skip(ctx)).Table("sys_user").Select("id, tenant_id, username, is_super").
		Where("id = ? AND status = ? AND deleted_at IS NULL", int64(userID), statusOn).Limit(1).Scan(&o)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &o, nil
}

func (s GormStore) AppByKey(ctx context.Context, key string) (*App, error) {
	var a App
	err := s.DB.WithContext(tenant.Skip(ctx)).Where("app_key = ?", key).Take(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AppPerms 取应用所绑角色（同租户、启用、非内置）下已分配的启用菜单/按钮权限码。
// 内置角色（超管角色）一律忽略：服务账号不能通过绑定内置角色获得 "*"。
func (s GormStore) AppPerms(ctx context.Context, app *App) ([]string, error) {
	ids := make([]int64, 0, len(app.RoleList()))
	for _, r := range app.RoleList() {
		n, err := strconv.ParseInt(r, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, n)
	}
	out := []string{}
	if len(ids) == 0 {
		return out, nil
	}
	err := s.DB.WithContext(tenant.Skip(ctx)).Table("sys_role AS r").Select("DISTINCT m.perm").
		Joins("JOIN sys_role_menu rm ON rm.role_id = r.id").
		Joins("JOIN sys_menu m ON m.id = rm.menu_id AND m.status = ?", statusOn).
		Where("r.id IN ? AND r.tenant_id = ? AND r.status = ? AND r.builtin = ? AND m.perm <> ''", ids, int64(app.TenantID), statusOn, false).
		Scan(&out).Error
	if out == nil {
		out = []string{}
	}
	return out, err
}

func (s GormStore) TouchPAT(ctx context.Context, id idgen.ID, at time.Time) error {
	return s.DB.WithContext(tenant.Skip(ctx)).Model(&PAT{}).Where("id = ?", int64(id)).UpdateColumn("last_used_at", at).Error
}

func (s GormStore) TouchApp(ctx context.Context, id idgen.ID, at time.Time) error {
	return s.DB.WithContext(tenant.Skip(ctx)).Model(&App{}).Where("id = ?", int64(id)).UpdateColumn("last_used_at", at).Error
}
