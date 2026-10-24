package formbuilder

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 本插件只读以下内置表（只用表名常量和最少的列，不 import 内置模块）：
// 用户的角色、角色列表、用户搜索、字典项。表不存在或查询失败时返回错误，调用方一律"拒绝"而不是放行。
const (
	tableUserRole = "sys_user_role"
	tableRole     = "sys_role"
	tableUser     = "sys_user"
	tableDictType = "sys_dict_type"
	tableDictData = "sys_dict_data"
)

// RoleOption / UserOption 是可见范围选择器的候选项。
type RoleOption struct {
	ID   idgen.ID `json:"id"`
	Name string   `json:"name"`
}

type UserOption struct {
	ID       idgen.ID `json:"id"`
	Username string   `json:"username"`
	Nickname string   `json:"nickname"`
}

// Directory 抽象了对内置角色/用户/字典数据的只读访问，便于单测替换。
type Directory interface {
	// UserRoleIDs 返回用户在当前租户下已启用的角色 ID。
	UserRoleIDs(ctx context.Context, uid idgen.ID) ([]idgen.ID, error)
	Roles(ctx context.Context) ([]RoleOption, error)
	Users(ctx context.Context, keyword string, limit int) ([]UserOption, error)
	// DictItems 返回字典类型下已启用的字典项（按排序）；字典不存在返回空切片。
	DictItems(ctx context.Context, code string) ([]Option, error)
}

type gormDirectory struct{ db *gorm.DB }

// NewDirectory 返回基于内置表的实现。
func NewDirectory(db *gorm.DB) Directory { return &gormDirectory{db: db} }

func tenantOf(ctx context.Context) (idgen.ID, error) {
	tid, ok := tenant.From(ctx)
	if !ok || tid == 0 {
		return 0, database.ErrNoTenant
	}
	return tid, nil
}

func (d *gormDirectory) UserRoleIDs(ctx context.Context, uid idgen.ID) ([]idgen.ID, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var ids []int64
	err = d.db.WithContext(ctx).Table(tableUserRole+" AS ur").Select("ur.role_id").
		Joins("JOIN "+tableRole+" r ON r.id = ur.role_id AND r.tenant_id = ? AND r.status = ?", int64(tid), "1").
		Where("ur.user_id = ?", int64(uid)).Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	out := make([]idgen.ID, len(ids))
	for i, v := range ids {
		out[i] = idgen.ID(v)
	}
	return out, nil
}

func (d *gormDirectory) Roles(ctx context.Context) ([]RoleOption, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var list []RoleOption
	err = d.db.WithContext(ctx).Table(tableRole).Select("id, name").
		Where("tenant_id = ? AND status = ?", int64(tid), "1").Order("sort ASC, id ASC").Limit(500).Scan(&list).Error
	return list, err
}

func (d *gormDirectory) Users(ctx context.Context, keyword string, limit int) ([]UserOption, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	q := d.db.WithContext(ctx).Table(tableUser).Select("id, username, nickname").
		Where("tenant_id = ? AND status = ? AND deleted_at IS NULL", int64(tid), "1")
	if keyword != "" {
		like := "%" + likeEscape(keyword) + "%"
		q = q.Where("username LIKE ? OR nickname LIKE ?", like, like)
	}
	var list []UserOption
	err = q.Order("id DESC").Limit(limit).Scan(&list).Error
	return list, err
}

func (d *gormDirectory) DictItems(ctx context.Context, code string) ([]Option, error) {
	var rows []struct {
		Label string `gorm:"column:dict_label"`
		Value string `gorm:"column:dict_value"`
	}
	err := d.db.WithContext(ctx).Table(tableDictData+" AS d").Select("d.dict_label, d.dict_value").
		Joins("JOIN "+tableDictType+" t ON t.id = d.type_id AND t.status = ?", "1").
		Where("t.code = ? AND d.status = ?", code, "1").Order("d.sort ASC, d.id ASC").Limit(MaxOptions + 1).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("读取字典 %s 失败: %w", code, err)
	}
	out := make([]Option, len(rows))
	for i, r := range rows {
		out[i] = Option{Label: r.Label, Value: r.Value}
	}
	return out, nil
}
