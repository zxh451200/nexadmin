package tenant

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	ctenant "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 参数键（sys_config）与默认值。
const (
	cfgGraceDays = "tenant.grace_days"       // 过期后的宽限天数
	cfgWarnDays  = "tenant.expire_warn_days" // 到期前几天开始提醒，0 = 不提醒

	defaultGraceDays = 0
	defaultWarnDays  = 15
)

// configInt 读取 sys_config 里的非负整数参数；缺失、非法、为负或表不存在都返回默认值。
func (s *Service) configInt(ctx context.Context, key string, def int) int {
	var v string
	if err := s.db.WithContext(ctx).Table("sys_config").Select("config_value").Where("config_key = ?", key).Limit(1).Scan(&v).Error; err != nil {
		return def
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return n
	}
	return def
}

// TenantInfo 实现 core/tenant.Source：返回租户的有效额度（租户覆盖优先，其次套餐）、套餐菜单、过期信息。
// 租户记录不存在（如默认租户）返回 (nil, nil)。
func (s *Service) TenantInfo(ctx context.Context, id idgen.ID) (*ctenant.Info, error) {
	var t Tenant
	if err := s.db.WithContext(ctx).Where("id = ?", int64(id)).Take(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	info := &ctenant.Info{
		ID: t.ID, Name: t.Name, Status: t.Status, ExpireAt: t.ExpireAt,
		GraceDays: s.configInt(ctx, cfgGraceDays, defaultGraceDays),
		WarnDays:  s.configInt(ctx, cfgWarnDays, defaultWarnDays),
	}
	var p Package
	if t.PackageID != 0 {
		err := s.db.WithContext(ctx).Where("id = ?", int64(t.PackageID)).Take(&p).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	info.MaxUsers, info.MaxRoles, info.MaxStorageMB = p.MaxUsers, p.MaxRoles, p.MaxStorageMb
	info.AIDisabled, info.AIDailyQuota = !t.AiEnabled, p.AiDailyQuota
	if len(p.MenuIDs) > 0 { // 套餐没勾选任何菜单按“不限制菜单”处理，向后兼容
		info.MenuIDs = []idgen.ID(p.MenuIDs)
	}
	if t.MaxUsers != nil {
		info.MaxUsers = *t.MaxUsers
	}
	if t.MaxRoles != nil {
		info.MaxRoles = *t.MaxRoles
	}
	if t.MaxStorageMb != nil {
		info.MaxStorageMB = *t.MaxStorageMb
	}
	if t.AiDailyQuota != nil {
		info.AIDailyQuota = *t.AiDailyQuota
	}
	return info, nil
}

// TenantByDomain 实现 core/tenant.Source：按域名（大小写不敏感）找租户 ID。
func (s *Service) TenantByDomain(ctx context.Context, host string) (idgen.ID, bool, error) {
	var ids []int64
	err := s.db.WithContext(ctx).Model(&Tenant{}).Where("LOWER(domain) = ?", strings.ToLower(host)).Limit(1).Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, false, err
	}
	return idgen.ID(ids[0]), true, nil
}

// Usage 返回租户当前用量与有效额度。已用存储走 core/tenant.StorageMeter（与文件模块共用，缓存约 30 秒）。
func (s *Service) Usage(ctx context.Context, id idgen.ID) (*Usage, error) {
	info, err := s.TenantInfo(ctx, id)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, notFound()
	}
	db := s.db.WithContext(ctx)
	out := &Usage{MaxUsers: info.MaxUsers, MaxRoles: info.MaxRoles, MaxStorageMB: info.MaxStorageMB, ExpireAt: info.ExpireAt}
	if err := db.Table("sys_user").Where("tenant_id = ? AND deleted_at IS NULL", int64(id)).Count(&out.Users).Error; err != nil {
		return nil, err
	}
	if err := db.Table("sys_role").Where("tenant_id = ?", int64(id)).Count(&out.Roles).Error; err != nil {
		return nil, err
	}
	bytes, err := ctenant.StorageMeter.Get(id, func() (int64, error) { return s.storageBytes(ctx, id) })
	if err != nil {
		return nil, err
	}
	out.StorageMB = (bytes + 1<<20 - 1) >> 20 // 向上取整：有文件就不显示 0
	out.DaysLeft, _ = info.DaysLeft(time.Now())
	return out, nil
}

// storageBytes 汇总租户已存储字节数（仅本地/对象存储里的文件，外链不计）；文件模块未启用（没有 sys_file 表）按 0 处理。
func (s *Service) storageBytes(ctx context.Context, id idgen.ID) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Table("sys_file").Select("COALESCE(SUM(size), 0)").
		Where("tenant_id = ? AND storage_path <> ''", int64(id)).Scan(&n).Error
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "does not exist") || strings.Contains(msg, "no such table") {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}
