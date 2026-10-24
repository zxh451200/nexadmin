package sendlog

import (
	"time"

	"gorm.io/gorm"

	"nexadmin/core/seed"
)

// 菜单挂在「系统监控」目录（与操作日志、登录日志同组）。出厂菜单每次启动幂等补齐，按业务键判断，已存在的一律不动。
// 业务键：菜单用权限码，按钮用权限码；与前端 menu.mock.ts 逐项一致。
const parentDir = "/monitor"

var seedTime = time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)

func menuSeed() []seed.MenuItem {
	return []seed.MenuItem{
		{Parent: parentDir, Code: PermList, Type: "menu", Title: "sendlog.menu", Icon: "Send", Path: "/monitor/send-log",
			Component: "sendlog/SendLogPage", Perm: PermList, Sort: 70, KeepAlive: true, CreatedAt: seedTime},
		{Parent: PermList, Code: PermDelete, Type: "button", Title: "sysMenu.btn.delete", Perm: PermDelete, Sort: 10, CreatedAt: seedTime},
	}
}

// seedMenus 幂等补齐菜单与按钮。
func seedMenus(db *gorm.DB) error {
	_, err := seed.Menus(db, menuSeed())
	return err
}
