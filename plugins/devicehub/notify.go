package devicehub

// 告警收件人解析：持有 devicehub:list 的用户（平台级查询，跳过租户隔离）。
// SQL 与 website 插件的 queryHolders / ticket 的 queryAgents 同款：超管 + 显式授权。

import (
	"context"

	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// queryDevicehubHolders 返回持有 devicehub:list 的用户（含超管与内置角色），按租户分组由调用方处理。
func queryDevicehubHolders(db *gorm.DB, ctx context.Context) ([]alarmRecipient, error) {
	if db == nil {
		return nil, nil
	}
	db = db.WithContext(tenant.Skip(ctx))
	holders := db.Table("sys_user_role AS ur").Select("ur.user_id").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ?", "1").
		Joins("LEFT JOIN sys_role_menu rm ON rm.role_id = r.id").
		Joins("LEFT JOIN sys_menu m ON m.id = rm.menu_id AND m.status = ? AND m.perm = ?", "1", "devicehub:list").
		Where("r.builtin = ? OR m.id IS NOT NULL", true)
	var rows []struct {
		UserID   idgen.ID `gorm:"column:user_id"`
		TenantID idgen.ID `gorm:"column:tenant_id"`
	}
	err := db.Table("sys_user AS u").
		Select("DISTINCT u.id AS user_id, u.tenant_id AS tenant_id").
		Where("u.deleted_at IS NULL AND u.status = ?", "1").
		Where("u.is_super = ? OR u.id IN (?)", true, holders).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]alarmRecipient, 0, len(rows))
	for _, r := range rows {
		out = append(out, alarmRecipient{tenantID: r.TenantID, userID: r.UserID})
	}
	return out, nil
}
