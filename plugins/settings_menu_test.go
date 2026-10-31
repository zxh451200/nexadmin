package plugins_test

import (
	"path/filepath"
	"testing"

	"nexadmin/core/plugin"
)

// LDAP 配置与地图服务是「系统设置」的分区：只隐藏菜单行，权限点仍要能被角色分配。
// 父节点保持出厂位置（顶级 / /system）——挂到 settings:list 下会让角色分配自动带上 settings:list。
func TestSettingsSectionMenusAreHiddenOnly(t *testing.T) {
	root := editionContractRoot(t)
	for _, tc := range []struct{ id, perm, parent string }{
		{"ldap", "ldap:list", ""},
		{"maps", "maps:config", "/system"},
	} {
		man, err := plugin.LoadManifest(filepath.Join(root, "plugins", tc.id))
		if err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
		found := false
		for _, m := range man.Menus {
			if m.Type != "menu" || m.Code() != tc.perm {
				continue
			}
			found = true
			if m.Parent != tc.parent {
				t.Errorf("%s 的父节点应保持 %q，实际 %q（不能挂到 settings:list 下）", tc.perm, tc.parent, m.Parent)
			}
			if !m.Hidden {
				t.Errorf("%s 的菜单应隐藏（侧栏不单列）", tc.perm)
			}
		}
		if !found {
			t.Errorf("%s 的 plugin.yaml 里找不到菜单 %s", tc.id, tc.perm)
		}
	}
}
