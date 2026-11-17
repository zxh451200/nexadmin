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
//  6. 客户主数据、AI 网关曾是「同名目录 + 唯一子项」，先各提到顶级；2026-11 起两页分别归入
//     dir:/biz 与 dir:/system（见第 12 条前的归组规则）；
//  7. 消息中心曾列在「内容管理」下，与顶栏铃铛重复，现在隐藏侧栏入口；
//  8. 插件/定时任务/代码生成/表单/多语言/审计与单点登录/开放平台曾在「系统管理」下，现在同属 dir:/dev。
//  9. 导入中心曾被拖进「导出中心」菜单下（拖拽没拦 menu-under-menu 的时期留下的），侧栏渲染不出来，拽回 dir:/io。
//  10. 2026-11 一级菜单整体重排（常用优先，新出厂 sort 见 seed.go 与各插件 plugin.yaml）：老库的一级
//     目录/单项还挂在旧号段上，按「sort 仍是出厂旧值」贴到新号段（见下方重排收敛）；管理员调过顺序
//     的行 sort 已不是出厂值，不会被拽动。
//  11. 「内容管理」组拆散（2026-11）：通知公告/文件管理归系统管理（路径不变），媒体优化插件跟随；
//     消息中心（隐藏行）提到顶级；组清空后目录连同授权一起删（同 2/6 的空目录清理模式）。
//  12. 2026-11 归类微调：客户数据（crm）从顶级单项归入 dir:/biz，AI 网关从顶级单项归入 dir:/system；
//     知识库与报表中心在 dir:/biz 内同占 sort 70，知识库贴到 80。
//  13. 2026-11 图标去重：工作台的 LayoutDashboard 与应用中心的 LayoutGrid 在收起图标栏里都是 2×2 格子，
//     相邻两枚肉眼难分，出厂图标仍为 LayoutDashboard 的工作台收敛为 House；管理员改过图标的行不动。
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
			}{{"realname:list", 10}, {"pay:list", 20}, {"im:list", 30}, {"ticket:list", 40}, {"workflow:list", 50}, {"devicehub:list", 60}, {"crm:list", 90}} {
				checked++
				res := tx.Exec(`UPDATE sys_menu SET parent_id = ?, sort = ? WHERE code = ? AND parent_id = 0`, bizDirID, item.sort, item.code)
				if res.Error != nil {
					return res.Error
				}
				fixed += int(res.RowsAffected)
			}
			// 知识库与报表中心在组内同占 sort 70（报表由上面的挪组规则设 70，知识库出厂也是 70），
			// 顺序靠 ID 兜底不稳定；只在「仍是 70」时贴到 80，管理员调过序的不动。
			checked++
			res := tx.Exec(`UPDATE sys_menu SET sort = 80 WHERE code = ? AND parent_id = ? AND sort = 70`, "kb:list", bizDirID)
			if res.Error != nil {
				return res.Error
			}
			fixed += int(res.RowsAffected)
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
			res := tx.Exec(`UPDATE sys_menu SET parent_id = 0, sort = 80 WHERE code = ? AND parent_id = ?`, "assistant:use", monitorDirID)
			if res.Error != nil {
				return res.Error
			}
			fixed += int(res.RowsAffected)
		}

		// 客户主数据、AI 网关曾各是「同名目录 + 唯一子项」：旧规则把子项提到顶级；2026-11 起两页都归组
		// （crm → dir:/biz、aigateway → dir:/system，见各自 plugin.yaml），老库直接迁到新家。
		// 只在目录下恰好一个子项时收敛这个旧形状；管理员自己加过页面的目录不动。空目录连同授权一起删。
		for _, item := range []struct {
			dir    string
			child  string
			parent string
			sort   int
		}{{"dir:/crm", "crm:list", "dir:/biz", 90}, {"dir:/ai", "aigateway:list", "dir:/system", 85}} {
			checked++
			var dirID, parentID int64
			if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, item.dir).Scan(&dirID).Error; e != nil {
				return e
			}
			if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, item.parent).Scan(&parentID).Error; e != nil {
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
			if parentID == 0 {
				// 新家目录不存在（不该发生：两个目录都是出厂种子）：不动子项也不删旧目录，等下次收敛
				continue
			}
			res := tx.Exec(`UPDATE sys_menu SET parent_id = ?, sort = ? WHERE code = ? AND parent_id = ?`, parentID, item.sort, item.child, dirID)
			if res.Error != nil {
				return res.Error
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

			// 2026-11 一级菜单整体重排后，dir:/dev 的出厂 sort 是固定 100（见 seed.go）；
			// 旧库的排序贴合由末尾的「出厂排序收敛」统一处理，这里不再按系统管理现值就近插入。
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

		// 「内容管理」组拆散：只挪「仍挂在 dir:/content 下」的行，管理员自己挪到别处的不动。
		// 子项 sort 用系统管理组里的出厂空位（通知公告 50、文件 70、媒体优化 100），不挤既有行；
		// 消息中心是隐藏行，提到顶级（挂系统管理下会让角色分配顺手带上整个组，见 seed.go 注释）。
		// 清空后删除目录并清掉它的授权——目录行没有独立用途，留着只会让菜单管理出现幽灵节点。
		checked++
		var contentDirID, sysDirID int64
		if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, "dir:/content").Scan(&contentDirID).Error; e != nil {
			return e
		}
		if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, "dir:/system").Scan(&sysDirID).Error; e != nil {
			return e
		}
		if contentDirID != 0 {
			if sysDirID != 0 {
				for _, item := range []struct {
					code string
					sort int
				}{{"notice:list", 50}, {"file:list", 70}, {"media:optimize", 100}} {
					checked++
					res := tx.Exec(`UPDATE sys_menu SET parent_id = ?, sort = ? WHERE code = ? AND parent_id = ?`,
						sysDirID, item.sort, item.code, contentDirID)
					if res.Error != nil {
						return res.Error
					}
					fixed += int(res.RowsAffected)
				}
			}
			checked++
			res := tx.Exec(`UPDATE sys_menu SET parent_id = 0 WHERE code = ? AND parent_id = ?`, "message:list", contentDirID)
			if res.Error != nil {
				return res.Error
			}
			fixed += int(res.RowsAffected)
			var kids int64
			if e := tx.Raw(`SELECT COUNT(*) FROM sys_menu WHERE parent_id = ?`, contentDirID).Scan(&kids).Error; e != nil {
				return e
			}
			if kids == 0 {
				if e := tx.Exec(`DELETE FROM sys_role_menu WHERE menu_id = ?`, contentDirID).Error; e != nil {
					return e
				}
				if e := tx.Exec(`DELETE FROM sys_menu WHERE id = ?`, contentDirID).Error; e != nil {
					return e
				}
				fixed++
			}
		}

		// 2026-11 一级菜单重排收敛：一级项按「sort 仍是出厂旧值」贴到新号段。dir:/system 额外认 40——
		// 更老的库一级还是旧号段（系统管理 = 40）。新号段整体见 seed.go / plugin.yaml，步距 10 留插空余量。
		// aigateway 不在此列：它从顶级单项归入了 dir:/system，见下面的挪组。
		checked++
		resorts := []struct {
			code string
			olds []int
			next int
		}{
			{"dir:/website", []int{65}, 20},
			{"dir:/io", []int{16}, 40},
			{"dir:/system", []int{20, 40}, 50},
			{"dir:/monitor", []int{40}, 60},
			{"dir:/biz", []int{55}, 70},
			{"assistant:use", []int{25}, 80},
			{"dir:/tenant", []int{50}, 90},
			{"dir:/dev", []int{25}, 100},
			{"about:view", []int{65}, 110},
			{"docs:view", []int{70}, 120},
			{"totp:manage", []int{90}, 140},
		}
		for _, r := range resorts {
			res := tx.Exec(`UPDATE sys_menu SET sort = ? WHERE code = ? AND parent_id = 0 AND sort IN ?`,
				r.next, r.code, r.olds)
			if res.Error != nil {
				return res.Error
			}
			fixed += int(res.RowsAffected)
		}

		// AI 网关从顶级单项归入系统管理（模型配置与「系统设置 → AI」同域）。认两种顶级旧值：
		// 65（旧出厂）与 130（重排过渡值）。dir:/ai 下的更老形状由前面的归组规则处理。
		if sysDirID != 0 {
			checked++
			res := tx.Exec(`UPDATE sys_menu SET parent_id = ?, sort = 85 WHERE code = ? AND parent_id = 0 AND sort IN ?`,
				sysDirID, "aigateway:list", []int{65, 130})
			if res.Error != nil {
				return res.Error
			}
			fixed += int(res.RowsAffected)
		}

		// 官网插件二级同样按使用频率重排（打开网站 → 新闻/产品/页面/分类/留言 → 导航/特性/站点设置）。
		// 按行收敛有已知边界：管理员只拖过组内个别项时，其余仍停在出厂值的行会被重排——
		// 组内出厂值两两不同（1..9）且本表覆盖全组，完整调过序的组一行都不会命中。
		var websiteDirID int64
		if e := tx.Raw(`SELECT id FROM sys_menu WHERE code = ? AND type = 'dir' LIMIT 1`, "dir:/website").Scan(&websiteDirID).Error; e != nil {
			return e
		}
		if websiteDirID != 0 {
			checked++
			websiteResorts := []struct {
				code string
				old  int
				next int
			}{
				{"ext:/", 6, 1},
				{"website:post", 5, 2},
				{"website:product", 7, 3},
				{"website:product-category", 8, 5},
				{"website:message", 9, 6},
				{"website:nav", 2, 7},
				{"website:feature", 3, 8},
				{"website:setting", 1, 9},
			}
			for _, r := range websiteResorts {
				res := tx.Exec(`UPDATE sys_menu SET sort = ? WHERE code = ? AND parent_id = ? AND sort = ?`,
					r.next, r.code, websiteDirID, r.old)
				if res.Error != nil {
					return res.Error
				}
				fixed += int(res.RowsAffected)
			}
		}

		// 工作台图标去重（见注释 13）：只认出厂值 LayoutDashboard，管理员改过图标的不拽。
		checked++
		res := tx.Exec(`UPDATE sys_menu SET icon = ? WHERE code = ? AND type = 'menu' AND icon = ?`,
			"House", "dashboard:view", "LayoutDashboard")
		if res.Error != nil {
			return res.Error
		}
		fixed += int(res.RowsAffected)
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
