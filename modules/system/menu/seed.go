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
	// 岗位已并进部门管理的第二个标签：不再单列菜单，权限点挂到部门下面才授得出去。
	b.btns(dept, "post", append([]string{"list"}, crud...)...)
	dict := b.menu(system, "dict.menu", "/system/dict", "dict/DictPage", "dict:list", "BookA", 60)
	b.btns(dict, "dict", crud...)
	settings := b.menu(system, "settings.menu", "/system/settings", "settings/SettingsPage", "settings:list", "Settings", 80)
	b.btns(settings, "settings", "update")
	// 语言与高级参数已并成「系统设置」里的分区：侧栏不再单列，但权限点仍要授得出去，
	// 所以它们的查看/修改按钮挂到系统设置下面（旧地址由前端路由重定向到对应分区）。
	b.btns(settings, "config", append([]string{"list"}, crud...)...)
	b.btns(settings, "language", "list", "update")
	// 省市区数据同步在「系统设置」里，权限点同样挂在设置页下面。
	b.btns(settings, "region", "sync")
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

	// 追加在末尾：已发出的 ID 不因中间插入而平移；sort 控制侧栏顺序。
	tenantSelf := b.menu(tenantDir, "tenantSelf.menu", "/tenant/self", "tenantSelf/TenantSelfPage", "tenantSelf:view", "Building2", 30)
	b.btns(tenantSelf, "tenantSelf", "update")

	// 数据备份（系统管理下）。
	backup := b.menu(system, "backup.menu", "/system/backup", "backup/BackupPage", "backup:list", "DatabaseBackup", 140, noCache)
	b.add(backup, Menu{Type: typeButton, Title: "backup.btn.edit", Perm: "backup:edit", Sort: 10})
	b.add(backup, Menu{Type: typeButton, Title: "backup.btn.download", Perm: "backup:download", Sort: 20})

	// 导入导出：导出中心（内置免费）与导入中心（plugins/imports，授权后才挂载）同组，
	// 两者原来各占一个顶级项、相距很远，看着像两个不相干的功能。
	ioDir := b.dir("sysMenu.dir.io", "ArrowLeftRight", "/io", 16)
	// 导出中心：登录即可（无权限码）；管理员“全部任务”用按钮权限 job:all。
	jobs := b.add(ioDir, Menu{Type: typeMenu, Code: "menu:jobs", Title: "jobs.menu", Icon: "Download", Path: "/jobs",
		Component: "exportcenter/ExportCenterPage", Sort: 10})
	b.add(jobs, Menu{Type: typeButton, Title: "jobs.btn.all", Perm: "job:all", Sort: 10})

	// 开放平台（系统管理下；追加在末尾，不挪动已有出厂 ID）。
	openapi := b.menu(system, "openapi.menu", "/system/openapi", "openapi/OpenApiPage", "openapi:list", "Webhook", 130)
	b.btns(openapi, "openapi", "edit")

	// 单点登录应用已并进「系统设置 → 安全与登录 → 单点登录」：侧栏不再单列，只隐藏菜单行。
	// 不能改挂到设置菜单下——角色分配会自动带上祖先节点，等于顺手授予了 settings:list。
	// 菜单行保留在原处，权限点与角色授权仍引用它（旧地址由前端路由重定向到对应分区）。
	// 必须排在所有既有菜单之后：出厂 ID 由 add 顺序决定，插在中间会顶掉后面条目的 ID。
	ssoApps := b.menu(system, "sso.menu", "/system/sso-apps", "sso/SsoAppsPage", "sso:list", "AppWindow", 110,
		func(m *Menu) { m.Hidden = true })
	b.btns(ssoApps, "sso", "edit")
	b.menu("", "sso.portal.menu", "/portal", "sso/PortalPage", "", "LayoutGrid", 15, noCache,
		func(m *Menu) { m.Code = "menu:portal" }) // 无权限码的顶级菜单必须显式业务键，否则整批出厂数据被 seed 拒绝（同 menu:jobs）

	// 语言清单是平台级真源（sys_language）：官网、字典、导出、后台界面文案都读它。
	// 管理页现在是「系统设置 → 高级 → 语言」的一个分区，权限点挂在 settings 下（见上）。
	return b.items
}

// 出厂菜单号段：新装时 ID = factoryIDBase + 定义顺序（与前端 mock、租户套餐的菜单号一致）。
const (
	factoryIDBase = int64(1850000000000000000)
	factoryIDSpan = int64(100000)
)

// reserveFreeIDs 在写库前给新条目让号：出厂 ID 由定义顺序决定，一旦有人把新菜单插在
// buildSeed 中间，后面所有条目的提议 ID 都会顺移并撞上存量库里已有的行（启动直接失败）。
// 这里只把「不在存量中、且提议 ID 已被别的 code 占用」的新条目顺延到号段尾部空号；
// 存量条目的 ID 一律不动，因此套餐与角色引用仍然有效。新装（表里没数据）时行为完全不变。
func reserveFreeIDs(db *gorm.DB, items []seedItem) error {
	type idCode struct {
		ID   int64
		Code string
	}
	var taken []idCode
	if err := db.Table("sys_menu").Select("id", "code").
		Where("id >= ? AND id < ?", factoryIDBase, factoryIDBase+factoryIDSpan).
		Scan(&taken).Error; err != nil {
		return err
	}
	owner := make(map[int64]string, len(taken))
	existing := make(map[string]bool, len(taken))
	for _, r := range taken {
		owner[r.ID] = r.Code
		existing[r.Code] = true
	}
	// 让号只用号段后半的「重定位区」：绝不去占后面条目自己的顺序号，
	// 否则一个撞号会连锁把后面所有条目都推走。
	next := factoryIDBase + factoryIDSpan/2
	for i := range items {
		it := &items[i]
		if it.m.Code == "" || existing[it.m.Code] {
			continue // 已存在：seeder 会跳过，不改它的 ID
		}
		if holder, hit := owner[int64(it.m.ID)]; hit && holder != it.m.Code {
			for {
				if _, busy := owner[next]; !busy {
					break
				}
				next++
			}
			it.m.ID = idgen.ID(next)
		}
		owner[int64(it.m.ID)] = it.m.Code
	}
	return nil
}

// seedMenus 幂等补齐出厂菜单（新装与每次启动共用）：按 code 判断，已存在（哪怕被改过）一律不动；
// 父节点按 code 解析实际 ID；已齐全时只有一次查询。返回新增条数由 seed.Report 汇总。
func seedMenus(db *gorm.DB) error {
	items := buildSeed()
	if err := reserveFreeIDs(db, items); err != nil {
		return err
	}
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
