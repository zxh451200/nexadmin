package menu

import (
	"fmt"

	"gorm.io/gorm"

	"nexadmin/core/seed"
)

// reconcileMenuShape 修「已知的历史形状」——不是新增出厂数据，所以不进迁移：
// 本仓库的规则是迁移只做结构变更，菜单这类出厂数据由 Seeder 负责（core/migrate 会警告 DML）。
//
// 目前收敛这些形状：
//  1. 导出中心（menu:jobs）与导入中心（imports:list）曾经各占一个顶级项，现在同属 dir:/io；
//  2. 支付中台曾经是「目录 dir:/pay + 同名子项」，现在子项直接在一级；
//  3. LDAP 配置、单点登录应用、地图服务曾各占一个侧栏项，现在并进「系统设置」的分区；
//  4. 证件实名、支付中台、在线客服、工单中心、审批中心曾各占一个顶级项，现在同属 dir:/biz；
//  5. 设备监控（顶级）、报表中心（曾挂系统监控）归入 dir:/biz，AI 助手从系统监控提到顶级；
//  6. 客户主数据、AI 网关曾是「同名目录 + 唯一子项」，现在子项直接在一级；
//  7. 消息中心曾列在「内容管理」下，与顶栏铃铛重复，现在隐藏侧栏入口；
//  8. 插件/定时任务/代码生成/表单/多语言/审计与单点登录/开放平台曾在「系统管理」下，现在同属 dir:/dev。
//  9. 导入中心曾被拖进「导出中心」菜单下（拖拽没拦 menu-under-menu 的时期留下的），侧栏渲染不出来，拽回 dir:/io。
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

			// 导入中心曾被拖进「导出中心」菜单下：旧版插件清单把它种在顶级，而菜单管理的拖拽
			// 当时没拦「菜单挂菜单」。侧栏只渲染「目录 → 菜单」两层，挂在菜单下的菜单永远不可见——
			// 这个形状不可能是管理员的故意安排，所以不像目录之间的挪动，无条件拽回 dir:/io。
			// sort 跟在导出中心当前值之后（出厂值是 jobs=10、imports=20；管理员若调过 jobs 的
			// sort，按它 +10 排列，避免固定 20 与调过的 jobs 打平导致并列顺序不稳定）。
			checked++
			var underMenu int64
			if e := tx.Raw(`SELECT c.parent_id FROM sys_menu c JOIN sys_menu p ON p.id = c.parent_id
				WHERE c.code = ? AND c.type = 'menu' AND p.type = 'menu'`, "imports:list").Scan(&underMenu).Error; e != nil {
				return e
			}
			if underMenu != 0 {
				var jobsSort int
				if e := tx.Raw(`SELECT sort FROM sys_menu WHERE code = ? AND type = 'menu'`, "menu:jobs").Scan(&jobsSort).Error; e != nil {
					return e
				}
				res := tx.Exec(`UPDATE sys_menu SET parent_id = ?, sort = ? WHERE code = ? AND parent_id = ?`,
					ioID, jobsSort+10, "imports:list", underMenu)
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

		// 证件实名、支付中台、在线客服、工单中心、审批中心原先各占一个顶级项，现在同属「业务中心」。
		// 目录由出厂种子建好；这里只把「还留在顶级」的老行挂上去，管理员自己挪到别处的不动。
		checked++
		var bizDirID int64
		if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, "dir:/biz").Scan(&bizDirID).Error; e != nil {
			return e
		}
		if bizDirID != 0 {
			for _, item := range []struct {
				code string
				sort int
			}{{"realname:list", 10}, {"pay:list", 20}, {"im:list", 30}, {"ticket:list", 40}, {"workflow:list", 50}, {"devicehub:list", 60}} {
				checked++
				res := tx.Exec(`UPDATE sys_menu SET parent_id = ?, sort = ? WHERE code = ? AND parent_id = 0`, bizDirID, item.sort, item.code)
				if res.Error != nil {
					return res.Error
				}
				fixed += int(res.RowsAffected)
			}
		}

		// 报表中心曾挂在「系统监控」下（业务分析不是运维监控），AI 助手也是个人入口而不是监控项。
		// 只认「仍挂在系统监控目录」的旧形状，管理员自己挪过的不动。
		var monitorDirID int64
		if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, "dir:/monitor").Scan(&monitorDirID).Error; e != nil {
			return e
		}
		if monitorDirID != 0 {
			if bizDirID != 0 {
				checked++
				res := tx.Exec(`UPDATE sys_menu SET parent_id = ?, sort = 70 WHERE code = ? AND parent_id = ?`, bizDirID, "report:view", monitorDirID)
				if res.Error != nil {
					return res.Error
				}
				fixed += int(res.RowsAffected)
			}
			checked++
			res := tx.Exec(`UPDATE sys_menu SET parent_id = 0, sort = 25 WHERE code = ? AND parent_id = ?`, "assistant:use", monitorDirID)
			if res.Error != nil {
				return res.Error
			}
			fixed += int(res.RowsAffected)
		}

		// 客户主数据、AI 网关曾各是「同名目录 + 唯一子项」：子项提到顶级，空目录连同授权一起删。
		// 只在目录下恰好一个子项时收敛这个旧形状；管理员自己加过页面的目录不动。
		for _, item := range []struct {
			dir   string
			child string
			sort  int
		}{{"dir:/crm", "crm:list", 150}, {"dir:/ai", "aigateway:list", 65}} {
			checked++
			var dirID int64
			if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, item.dir).Scan(&dirID).Error; e != nil {
				return e
			}
			if dirID == 0 {
				continue
			}
			var kids int64
			if e := tx.Raw(`SELECT COUNT(*) FROM sys_menu WHERE parent_id = ?`, dirID).Scan(&kids).Error; e != nil {
				return e
			}
			if kids != 1 {
				continue
			}
			res := tx.Exec(`UPDATE sys_menu SET parent_id = 0, sort = ? WHERE code = ? AND parent_id = ?`, item.sort, item.child, dirID)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				continue
			}
			fixed += int(res.RowsAffected)
			if e := tx.Exec(`DELETE FROM sys_role_menu WHERE menu_id = ?`, dirID).Error; e != nil {
				return e
			}
			if e := tx.Exec(`DELETE FROM sys_menu WHERE id = ?`, dirID).Error; e != nil {
				return e
			}
			fixed++
		}

		// 消息中心是个人收件箱（顶栏铃铛已有「查看全部」），不再列在「内容管理」下。
		// 只隐藏、不删行：权限点与角色授权仍引用它。条件匹配「仍可见」的旧形状，收敛后不再写库。
		checked++
		var msgVisible int64
		if e := tx.Raw(`SELECT COUNT(*) FROM sys_menu WHERE code = ? AND hidden = false`, "message:list").Scan(&msgVisible).Error; e != nil {
			return e
		}
		if msgVisible > 0 {
			res := tx.Exec(`UPDATE sys_menu SET hidden = true WHERE code = ? AND hidden = false`, "message:list")
			if res.Error != nil {
				return res.Error
			}
			fixed += int(res.RowsAffected)
		}

		// 「开发与安全」从系统管理拆出：插件、定时任务、代码生成、表单设计、多语言、审计/身份/单点登录、
		// 开放平台曾全堆在系统管理。只挪「还挂在系统管理目录」的行，管理员自己挪过的不动。
		checked++
		var devDirID, systemDirID int64
		if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, "dir:/dev").Scan(&devDirID).Error; e != nil {
			return e
		}
		if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, "dir:/system").Scan(&systemDirID).Error; e != nil {
			return e
		}
		if devDirID != 0 && systemDirID != 0 {
			for _, item := range []struct {
				code string
				sort int
			}{
				{"plugin:list", 10}, {"cron:list", 20}, {"codegen:list", 30}, {"formbuilder:design", 40},
				{"i18n-editor:list", 50}, {"audit:list", 60}, {"security:self", 70}, {"identity:list", 80},
				{"scim:list", 90}, {"saml:list", 100}, {"openapi:list", 110},
			} {
				checked++
				res := tx.Exec(`UPDATE sys_menu SET parent_id = ?, sort = ? WHERE code = ? AND parent_id = ?`, devDirID, item.sort, item.code, systemDirID)
				if res.Error != nil {
					return res.Error
				}
				fixed += int(res.RowsAffected)
			}

			// 老库一级排序还是旧号段（系统管理 = 40），新目录按出厂值 25 会排到导入导出前面；
			// 只在「仍是出厂排序值」时贴到系统管理后面，管理员自己调过顺序就不动。
			checked++
			var sysSort int
			if e := tx.Raw(`SELECT sort FROM sys_menu WHERE id = ?`, systemDirID).Scan(&sysSort).Error; e != nil {
				return e
			}
			res := tx.Exec(`UPDATE sys_menu SET sort = ? WHERE code = ? AND sort = 25`, sysSort+1, "dir:/dev")
			if res.Error != nil {
				return res.Error
			}
			fixed += int(res.RowsAffected)
		}

		// LDAP 配置、单点登录应用、地图服务已并进「系统设置」的分区：老库里它们还是侧栏可见的独立菜单。
		// 只隐藏、不删行也不改父节点：权限点与角色授权仍引用这些行；挂到设置菜单下会让角色分配
		// 自动带上祖先节点，等于顺手授予 settings:list。条件匹配「仍可见」的旧形状，收敛后不再写库。
		checked += 3
		// hidden 在 PG 是 BOOLEAN，写 `hidden = 0` 在 PG 上直接报 42883（operator does not
		// exist: boolean = integer），MySQL/MariaDB 却照单全收——这类差异只有真跑 PG 才暴露。
		// false/true 三个字面量两边都认，所以这里不用 0/1。
		var visible int64
		if e := tx.Raw(`SELECT COUNT(*) FROM sys_menu WHERE code IN (?, ?, ?) AND hidden = false`,
			"ldap:list", "sso:list", "maps:config").Scan(&visible).Error; e != nil {
			return e
		}
		if visible > 0 {
			res := tx.Exec(`UPDATE sys_menu SET hidden = true WHERE code IN (?, ?, ?) AND hidden = false`,
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
