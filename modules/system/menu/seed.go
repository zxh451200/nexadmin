package menu

import (
	"time"

	"gorm.io/gorm"

	"nexadmin/core/seed"
	"nexadmin/pkg/idgen"
)

// 出厂菜单，内容与前端 menu.mock.ts 逐项一致（含出厂 ID：1850000000000000001 起按定义顺序递增）。
// 业务键 code：目录用 "dir:"+路径，菜单/按钮用权限码。已存在的 code 一律不动。

var seedTime = time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)

type seedItem struct {
	m          Menu
	parentCode string
}

type seedBuilder struct {
	seq   int64
	items []seedItem
}

func (b *seedBuilder) add(parent string, m Menu) string {
	b.seq++
	m.ID = idgen.ID(1850000000000000000 + b.seq)
	if m.Status == "" {
		m.Status = statusOn
	}
	switch {
	case m.Code != "": // 显式业务键（无权限码的顶级菜单需要唯一的 code，避免与其它无权限菜单冲突）
	case m.Type == typeDir:
		m.Code = "dir:" + m.Path
	default:
		m.Code = m.Perm
	}
	b.items = append(b.items, seedItem{m: m, parentCode: parent})
	return m.Code
}

func (b *seedBuilder) dir(title, icon, path string, sort int) string {
	return b.add("", Menu{Type: typeDir, Title: title, Icon: icon, Path: path, Sort: sort})
}

// menu 默认开启页面缓存；opts 用来覆盖（隐藏、外链、停用等）。
func (b *seedBuilder) menu(parent, title, path, component, perm, icon string, sort int, opts ...func(*Menu)) string {
	m := Menu{Type: typeMenu, Title: title, Path: path, Component: component, Perm: perm, Icon: icon, Sort: sort, KeepAlive: true}
	for _, o := range opts {
		o(&m)
	}
	return b.add(parent, m)
}

func (b *seedBuilder) btns(parent, prefix string, actions ...string) {
	for i, a := range actions {
		b.add(parent, Menu{Type: typeButton, Title: "sysMenu.btn." + a, Perm: prefix + ":" + a, Sort: (i + 1) * 10})
	}
}

func noCache(m *Menu) { m.KeepAlive = false }

func buildSeed() []seedItem {
	b := &seedBuilder{}
	crud := []string{"create", "update", "delete"}
	with := func(extra ...string) []string { return append(append([]string{}, crud...), extra...) }

	b.menu("", "dashboard.menu", "/dashboard", "dashboard/DashboardPage", "dashboard:view", "LayoutDashboard", 10, noCache)

	system := b.dir("sysMenu.dir.system", "Settings", "/system", 20)
	user := b.menu(system, "user.menu", "/system/user", "user/UserPage", "user:list", "Users", 10)
	b.btns(user, "user", with("export", "import", "resetPwd", "unlock")...)
	role := b.menu(system, "role.menu", "/system/role", "role/RolePage", "role:list", "ShieldCheck", 20)
	b.btns(role, "role", with("assign")...)
	sysMenu := b.menu(system, "sysMenu.menu", "/system/menu", "menu/MenuPage", "menu:list", "ListTree", 30)
	b.btns(sysMenu, "menu", crud...)
	dept := b.menu(system, "dept.menu", "/system/dept", "dept/DeptPage", "dept:list", "Network", 40)
	b.btns(dept, "dept", crud...)
	post := b.menu(system, "post.menu", "/system/post", "post/PostPage", "post:list", "BriefcaseBusiness", 50)
	b.btns(post, "post", crud...)
	dict := b.menu(system, "dict.menu", "/system/dict", "dict/DictPage", "dict:list", "BookA", 60)
	b.btns(dict, "dict", crud...)
	config := b.menu(system, "sysConfig.menu", "/system/config", "config/ConfigPage", "config:list", "Settings", 70)
	b.btns(config, "config", crud...)
	settings := b.menu(system, "settings.menu", "/system/settings", "settings/SettingsPage", "settings:list", "SlidersHorizontal", 80)
	b.btns(settings, "settings", "update")
	recycle := b.menu(system, "recycle.menu", "/system/recycle", "recycle/RecyclePage", "recycle:list", "Trash2", 90)
	b.btns(recycle, "recycle", "restore", "delete")

	content := b.dir("sysMenu.dir.content", "FileText", "/content", 30)
	notice := b.menu(content, "notice.menu", "/content/notice", "notice/NoticePage", "notice:list", "Megaphone", 10)
	b.btns(notice, "notice", with("publish", "platform")...)
	message := b.menu(content, "message.menu", "/message", "message/MessagePage", "message:list", "Bell", 20)
	b.btns(message, "message", "delete")
	file := b.menu(content, "fileMgr.menu", "/content/file", "file/FilePage", "file:list", "FolderOpen", 30)
	b.btns(file, "file", "upload", "delete")

	monitor := b.dir("sysMenu.dir.monitor", "Activity", "/monitor", 40)
	operLog := b.menu(monitor, "operLog.menu", "/monitor/oper-log", "operLog/OperLogPage", "operLog:list", "ScrollText", 10)
	b.btns(operLog, "operLog", "delete", "export")
	loginLog := b.menu(monitor, "loginLog.menu", "/monitor/login-log", "loginLog/LoginLogPage", "loginLog:list", "LogIn", 20)
	b.btns(loginLog, "loginLog", "delete", "export")
	online := b.menu(monitor, "online.menu", "/monitor/online", "online/OnlinePage", "online:list", "MonitorSmartphone", 30, noCache)
	b.btns(online, "online", "forceLogout")
	b.menu(monitor, "serverMonitor.menu", "/monitor/server", "serverMonitor/ServerMonitorPage", "serverMonitor:list", "Cpu", 40)
	b.menu(monitor, "cacheMonitor.menu", "/monitor/cache", "cacheMonitor/CacheMonitorPage", "cacheMonitor:list", "Database", 50)
	b.menu(monitor, "apiMonitor.menu", "/monitor/api", "apiMonitor/ApiMonitorPage", "apiMonitor:list", "Gauge", 60)

	tenantDir := b.dir("sysMenu.dir.tenant", "Building2", "/tenant", 50)
	tenant := b.menu(tenantDir, "tenant.menu", "/tenant/list", "tenant/TenantPage", "tenant:list", "Building2", 10)
	b.btns(tenant, "tenant", crud...)
	tenantPkg := b.menu(tenantDir, "tenantPkg.menu", "/tenant/package", "tenantPkg/TenantPkgPage", "tenantPkg:list", "Package", 20)
	b.btns(tenantPkg, "tenantPkg", crud...)

	b.menu("", "profile.menu", "/profile", "profile/ProfilePage", "profile:view", "User", 60, noCache, func(m *Menu) { m.Hidden = true })
	b.menu("", "about.menu", "/about", "about/AboutPage", "about:view", "Info", 65, noCache)
	b.menu("", "sysMenu.docs", "https://example.com/docs", "", "docs:view", "BookOpen", 70, noCache,
		func(m *Menu) { m.External, m.Status = true, "0" })

	// 单点登录（追加在末尾，避免挪动上面菜单的出厂 ID）。
	ssoApps := b.menu(system, "sso.menu", "/system/sso-apps", "sso/SsoAppsPage", "sso:list", "AppWindow", 110)
	b.btns(ssoApps, "sso", "edit")
	b.menu("", "sso.portal.menu", "/portal", "sso/PortalPage", "", "LayoutGrid", 15, noCache)

	// 数据备份（系统管理下）。
	backup := b.menu(system, "backup.menu", "/system/backup", "backup/BackupPage", "backup:list", "DatabaseBackup", 140, noCache)
	b.add(backup, Menu{Type: typeButton, Title: "backup.btn.edit", Perm: "backup:edit", Sort: 10})
	b.add(backup, Menu{Type: typeButton, Title: "backup.btn.download", Perm: "backup:download", Sort: 20})

	// 导出中心：顶级菜单，登录即可（无权限码）；管理员“全部任务”用按钮权限 job:all。
	jobs := b.add("", Menu{Type: typeMenu, Code: "menu:jobs", Title: "jobs.menu", Icon: "Download", Path: "/jobs",
		Component: "exportcenter/ExportCenterPage", Sort: 16})
	b.add(jobs, Menu{Type: typeButton, Title: "jobs.btn.all", Perm: "job:all", Sort: 10})

	// 开放平台（系统管理下；追加在末尾，不挪动已有出厂 ID）。
	openapi := b.menu(system, "openapi.menu", "/system/openapi", "openapi/OpenApiPage", "openapi:list", "Webhook", 130)
	b.btns(openapi, "openapi", "edit")
	return b.items
}

// seedMenus 幂等补齐出厂菜单（新装与每次启动共用）：按 code 判断，已存在（哪怕被改过）一律不动；
// 父节点按 code 解析实际 ID；已齐全时只有一次查询。返回新增条数由 seed.Report 汇总。
func seedMenus(db *gorm.DB) error {
	items := buildSeed()
	rows := make([]seed.MenuItem, len(items))
	for i, it := range items {
		m := it.m
		rows[i] = seed.MenuItem{
			ID: m.ID, Parent: it.parentCode, Code: m.Code, Type: m.Type, Title: m.Title, Icon: m.Icon, Path: m.Path,
			Component: m.Component, Perm: m.Perm, Sort: m.Sort, KeepAlive: m.KeepAlive, Hidden: m.Hidden,
			External: m.External, Status: m.Status, CreatedAt: seedTime,
		}
	}
	_, err := seed.Menus(db, rows)
	return err
}
