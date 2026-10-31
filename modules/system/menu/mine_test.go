package menu

import (
	"testing"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 侧边栏过滤：插件菜单按 component 归属跟随挂载状态，核心模块菜单不受影响；
// 目录节点由 buildTree 的 pruneDir 剔除（过滤掉子项后为空）。
func TestMineKeepFiltersPluginMenusByMountState(t *testing.T) {
	orig := pluginMenuVisible
	t.Cleanup(func() { pluginMenuVisible = orig })
	pluginMenuVisible = func(id string) bool { return id != "website" }

	row := func(id idgen.ID, parent idgen.ID, typ, component, perm string) *Menu {
		return &Menu{
			PlatformModel: database.PlatformModel{ID: id},
			ParentID:      parent, Type: typ, Component: component, Perm: perm, Status: statusOn,
		}
	}
	dir := row(1, 0, typeDir, "", "")
	page := row(2, 1, typeMenu, "plugins/website/SettingsPage", "website:setting")
	cronPage := row(3, 0, typeMenu, "plugins/cron/CronPage", "cron:list")
	corePage := row(4, 0, typeMenu, "page/ProfilePage", "profile:view")
	btn := row(5, 4, typeButton, "", "profile:edit")
	noPerm := row(6, 0, typeMenu, "page/PublicPage", "")

	keep := mineKeep([]string{"website:setting", "cron:list", "profile:view"})
	for _, c := range []struct {
		m    *Menu
		want bool
		why  string
	}{
		{page, false, "website 未挂载"},
		{cronPage, true, "cron 可用"},
		{corePage, true, "核心模块菜单"},
		{btn, false, "按钮不进侧边栏"},
		{noPerm, true, "无权限码的门面菜单保留"},
		{dir, true, "目录本身保留，空目录由 pruneDir 剔除"},
	} {
		if got := keep(c.m); got != c.want {
			t.Errorf("keep(%q)=%v, want %v（%s）", c.m.Component+c.m.Path, got, c.want, c.why)
		}
	}

	if tree := buildTree([]Menu{*dir, *page}, keep, true); len(tree) != 0 {
		t.Fatalf("website 目录应被整体剔除，实际 %d 项", len(tree))
	}
	if tree := buildTree([]Menu{*cronPage, *corePage, *btn, *noPerm}, keep, true); len(tree) != 3 {
		t.Fatalf("应保留 3 项可见菜单，实际 %d 项", len(tree))
	}
}

func TestPluginOfComponent(t *testing.T) {
	for _, c := range []struct {
		in string
		id string
		ok bool
	}{
		{"plugins/website/SettingsPage", "website", true},
		{"plugins/i18n-editor/EditorPage", "i18n-editor", true},
		{"plugin/PluginPage", "", false},
		{"page/ProfilePage", "", false},
		{"", "", false},
	} {
		id, ok := pluginOfComponent(c.in)
		if id != c.id || ok != c.ok {
			t.Errorf("pluginOfComponent(%q)=(%q,%v), want (%q,%v)", c.in, id, ok, c.id, c.ok)
		}
	}
}
