package menu

import (
	"fmt"

	"gorm.io/gorm"

	"nexadmin/core/seed"
)

// reconcileMenuShape 修「已知的历史形状」——不是新增出厂数据，所以不进迁移：
// 本仓库的规则是迁移只做结构变更，菜单这类出厂数据由 Seeder 负责（core/migrate 会警告 DML）。
//
// 三处形状：
//  1. 导出中心（menu:jobs）与导入中心（imports:list）曾经各占一个顶级项，现在同属 dir:/io；
//  2. 支付中台曾经是「目录 dir:/pay + 同名子项」，现在子项直接在一级；
//  3. LDAP 配置、单点登录应用、地图服务曾各占一个侧栏项，现在并进「系统设置」的分区。
//
// 播种只补不改（core/seed 按 code 判断，已存在的一律不动），所以老库必须在这里收敛一次。
// 条件只匹配「坏形状」本身（顶级 / 挂在旧目录下），管理员自己挪过位置的菜单不会被拽回来；
// 收敛之后每次启动都是空操作。返回值分别用于启动日志：检查了几项、实际改了几项。
func reconcileMenuShape(db *gorm.DB) (checked, fixed int, err error) {
	err = db.Transaction(func(tx *gorm.DB) error {
		var ioID int64
		if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, "dir:/io").Scan(&ioID).Error; e != nil {
			return e
		}

		checked++
		if ioID != 0 {
			for _, item := range []struct {
				code string
				sort int
			}{{"menu:jobs", 10}, {"imports:list", 20}} {
				checked++
				res := tx.Exec(`UPDATE sys_menu SET parent_id = ?, sort = ? WHERE code = ? AND parent_id = 0`, ioID, item.sort, item.code)
				if res.Error != nil {
					return res.Error
				}
				fixed += int(res.RowsAffected)
			}
		}

		checked++
		var payDirID int64
		if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, "dir:/pay").Scan(&payDirID).Error; e != nil {
			return e
		}
		if payDirID != 0 {
			res := tx.Exec(`UPDATE sys_menu SET parent_id = 0, sort = 60 WHERE code = ? AND parent_id = ?`, "pay:list", payDirID)
			if res.Error != nil {
				return res.Error
			}
			fixed += int(res.RowsAffected)

			var kids int64
			if e := tx.Raw(`SELECT COUNT(*) FROM sys_menu WHERE parent_id = ?`, payDirID).Scan(&kids).Error; e != nil {
				return e
			}
			if kids == 0 {
				if e := tx.Exec(`DELETE FROM sys_role_menu WHERE menu_id = ?`, payDirID).Error; e != nil {
					return e
				}
				if e := tx.Exec(`DELETE FROM sys_menu WHERE id = ?`, payDirID).Error; e != nil {
					return e
				}
				fixed++
			}
		}

		// LDAP 配置、单点登录应用、地图服务已并进「系统设置」的分区：老库里它们还是侧栏可见的独立菜单。
		// 只隐藏、不删行也不改父节点：权限点与角色授权仍引用这些行；挂到设置菜单下会让角色分配
		// 自动带上祖先节点，等于顺手授予 settings:list。条件匹配「仍可见」的旧形状，收敛后不再写库。
		checked += 3
		var visible int64
		if e := tx.Raw(`SELECT COUNT(*) FROM sys_menu WHERE code IN (?, ?, ?) AND hidden = 0`,
			"ldap:list", "sso:list", "maps:config").Scan(&visible).Error; e != nil {
			return e
		}
		if visible > 0 {
			res := tx.Exec(`UPDATE sys_menu SET hidden = 1 WHERE code IN (?, ?, ?) AND hidden = 0`,
				"ldap:list", "sso:list", "maps:config")
			if res.Error != nil {
				return res.Error
			}
			fixed += int(res.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return checked, fixed, fmt.Errorf("菜单结构对账: %w", err)
	}
	// 交给启动日志统一打：空操作与「没跑」必须能区分（幂等钩子的固定要求）。
	// 走 seed.Report 而不是 db.Logger——GORM 这里是默认 logger，Info 是空实现。
	seed.ReportFrom(db.Statement.Context).AddReconcile(checked, fixed)
	return checked, fixed, nil
}
