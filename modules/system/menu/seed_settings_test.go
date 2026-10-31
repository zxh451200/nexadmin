package menu

import "testing"

// 单点登录应用已并进「系统设置 → 安全与登录 → 单点登录」：只隐藏菜单行，权限点 sso:list 仍可分配。
// 父节点保持 /system 目录 —— 挂到设置菜单下会让角色分配自动带上 settings:list。
func TestSeedSsoMenuIsHiddenOnly(t *testing.T) {
	found := false
	for _, it := range buildSeed() {
		if it.m.Code != "sso:list" {
			continue
		}
		found = true
		if it.parentCode != "dir:/system" {
			t.Errorf("sso:list 的父节点应保持 dir:/system，实际 %q（不能挂到 settings:list 下）", it.parentCode)
		}
		if !it.m.Hidden {
			t.Error("sso:list 应隐藏（侧栏不单列）")
		}
	}
	if !found {
		t.Fatal("出厂种子里找不到 sso:list")
	}
}
