package tnote

import (
	"gorm.io/gorm"

	"nexadmin/core/seed"
)

// seedMenus 补齐菜单与按钮权限：按业务键 code 匹配，不存在才插入，已存在（哪怕被改过）一律不动。
// 新增的菜单超管默认可见，普通角色需要到「角色管理」里分配。
func seedMenus(db *gorm.DB) error {
	_, err := seed.Menus(db, []seed.MenuItem{
		{Code: "tnote:list", Type: "menu", Title: "tnote.menu", Icon: "FileText", Path: "/tnote",
			Component: "tnote/TnotePage", Perm: "tnote:list", Sort: 100, KeepAlive: true},
		{Parent: "tnote:list", Code: "tnote:edit", Type: "button", Title: "common.edit", Perm: "tnote:edit", Sort: 10},
		{Parent: "tnote:list", Code: "tnote:delete", Type: "button", Title: "common.delete", Perm: "tnote:delete", Sort: 20},
	})
	return err
}
