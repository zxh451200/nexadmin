package main

// control_plane_gate_test.go —— 框架控制面「表里没有 tenant_id」那 17 张全局行的写入口门禁。
//
// 为什么不能沿用 platform_write_gate_test.go 的扫模型判据：modules/ 里嵌 database.PlatformModel
// 却带着 tenant_id 的假阳性有（sys_tenant_usage_daily），不嵌 PlatformModel 却真的共享的漏判也有
// （plg_ldap_config / plg_maps_setting / plg_media_setting 的模型是各自手写的 key/value 结构）。
// 两头都会错，所以这里的判据取 DDL：建表语句里有没有 tenant_id 那一列，就是「这一行是不是大家共用」。
//
// 判的三件事：
//  1. 台账里每条写路由的注册语句确实带 core.RequirePlatform()（少了就红）；
//  2. 这 8 个文件里挂了闸却没登记的路由也红——控制面新增一条全局写入口必须留名，
//     不像插件那样容忍并行会话互相踩（这几个文件就是控制面本身）；
//  3. DDL 扫出的每张无 tenant_id 的框架表，要么被台账覆盖，要么在「已有更强闸 / 无 HTTP 写入口 /
//     按父行归属的关联表」清单里写明理由；清单条目若不再对应真实表同样报错。
//
// 这些洞的真实形状（本轮之前只有权限点）：租户删一条菜单会级联 DELETE sys_role_menu，把别的租户
// 角色上的菜单绑定一起清掉；租户把 security.maxAttempts 设成 1 能把所有人锁在门外；
// PUT /languages 是整表替换，一次保存删掉全站正在服务的语言；regions/import 与 sync 接受调用方
// 给的 URL，换掉的是所有地址选择器读的那一份。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// platControlPlaneWrite 是一条控制面写路由：group 是分组变量名（dict 的类型组与数据组
// method+path 完全同形，只有变量分得开），table 是它动的那张没有 tenant_id 的全局行。
type platControlPlaneWrite struct {
	file, group, method, path, table string
}

var platControlPlaneWrites = []platControlPlaneWrite{
	{"modules/system/config/module.go", "g", "PUT", "/upload", "sys_config"},
	{"modules/system/config/module.go", "g", "POST", "/refresh-cache", "sys_config"},
	{"modules/system/config/module.go", "g", "POST", "", "sys_config"},
	{"modules/system/config/module.go", "g", "PUT", "/:id", "sys_config"},
	{"modules/system/config/module.go", "g", "DELETE", "/:id", "sys_config"},
	{"modules/system/config/module.go", "g", "DELETE", "", "sys_config"},
	{"modules/system/config/module.go", "s", "PUT", "/:section", "sys_setting"},
	{"modules/system/config/module.go", "s", "POST", "/:section/test", "sys_setting"},
	{"modules/system/menu/module.go", "r", "PUT", "/menus/sort", "sys_menu"},
	{"modules/system/menu/module.go", "r", "PUT", "/menus/:id/move", "sys_menu"},
	{"modules/system/menu/module.go", "r", "POST", "/menus", "sys_menu"},
	{"modules/system/menu/module.go", "r", "PUT", "/menus/:id", "sys_menu"},
	{"modules/system/menu/module.go", "r", "DELETE", "/menus/:id", "sys_menu"},
	{"modules/system/menu/module.go", "r", "DELETE", "/menus", "sys_menu"},
	{"modules/system/dict/module.go", "t", "PUT", "/sort", "sys_dict_type"},
	{"modules/system/dict/module.go", "t", "POST", "", "sys_dict_type"},
	{"modules/system/dict/module.go", "t", "PUT", "/:id", "sys_dict_type"},
	{"modules/system/dict/module.go", "t", "DELETE", "/:id", "sys_dict_type"},
	{"modules/system/dict/module.go", "t", "DELETE", "", "sys_dict_type"},
	{"modules/system/dict/module.go", "x", "PUT", "/sort", "sys_dict_data"},
	{"modules/system/dict/module.go", "x", "POST", "", "sys_dict_data"},
	{"modules/system/dict/module.go", "x", "PUT", "/:id", "sys_dict_data"},
	{"modules/system/dict/module.go", "x", "DELETE", "/:id", "sys_dict_data"},
	{"modules/system/dict/module.go", "x", "DELETE", "", "sys_dict_data"},
	{"modules/system/language/module.go", "g", "PUT", "", "sys_language"},
	{"modules/region/module.go", "r", "POST", "/regions/preview", "sys_region_snapshot"},
	{"modules/region/module.go", "r", "POST", "/regions/import", "sys_region_snapshot"},
	{"modules/region/module.go", "r", "POST", "/regions/sync", "sys_region_snapshot"},
	{"modules/region/module.go", "r", "POST", "/regions/reset", "sys_region_snapshot"},
	{"modules/region/module.go", "r", "POST", "/regions/versions/:version/rollback", "sys_region_snapshot"},
	{"plugins/ldap/plugin.go", "g", "PUT", "/config", "plg_ldap_config"},
	{"plugins/ldap/plugin.go", "g", "POST", "/test", "plg_ldap_config"},
	{"plugins/maps/plugin.go", "g", "PUT", "/config", "plg_maps_setting"},
	{"plugins/media/plugin.go", "g", "PUT", "/video/config", "plg_media_setting"},
}

// platControlPlaneSharedBy 是「表本身没有 HTTP 路由，但被台账里别的路由一起写」的映射。
var platControlPlaneSharedBy = map[string]string{
	"sys_region_snapshot_version": "区划版本由 region 那 5 条同步 / 回滚路由一并写（台账挂在 sys_region_snapshot 上）",
}

// platControlPlaneOtherGuarded 是已有更强闸或没有 HTTP 写入口的表，逐条写清是谁在挡。
var platControlPlaneOtherGuarded = map[string]string{
	"sys_backup":           "modules/system/backup 的 superOnly()（claims.Super）比平台闸更严",
	"sys_backup_setting":   "同上：PUT /backups/config 也只认超管",
	"sys_tenant":           "分组级 middleware.RequireSuper()（modules/system/tenant/module.go）；PUT /tenant-self 钉在自己的租户行上",
	"sys_tenant_pkg":       "分组级 middleware.RequireSuper()",
	"sys_monitor_snapshot": "写侧没有 HTTP 入口（三个采样协程在写）；读侧整组收口，见 platConsoleReads——快照与实时值都是整个部署的量",
	"sys_role_menu":        "关联表：写入口是 PUT /roles/:id/menus（父角色按租户归属）与菜单删除的级联，后者已随 sys_menu 的路由一起挂闸",
	"sys_role_dept":        "关联表：随 PUT /roles/:id 一起写，父角色按租户归属",
	"sys_user_role":        "关联表：随用户创建 / 角色分配写，父用户按租户归属，角色归属另有 roleguard",
	"sys_user_post":        "关联表：同上",
}

var (
	// 设置默认值里的字段名：map 字面量的 "key": 形状。不能只锚行首——一行常写好几个键。
	keyNameRe       = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_]*)":`)
	cpMapKeysRe     = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_]*)":\s*true`)
	cpStringSliceRe = regexp.MustCompile(`(?s)secretKeyHints = \[\]string\{(.*?)\}`)
	cpQuoteRe       = regexp.MustCompile(`"([^"]+)"`)
	cpCreateTableRe = regexp.MustCompile(`(?s)CREATE TABLE(?: IF NOT EXISTS)?\s+(\w+)\s*\((.*?)\n\)`)
	cpTenantColRe   = regexp.MustCompile(`\btenant_id\b`)
	cpRouteRe       = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_]\w*)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]*)",`)
)

// cpRoutes 扫台账文件里的路由注册，返回「文件 + 分组变量 + 方法 + 路径 → 整条语句」。
func cpRoutes(t *testing.T) map[string]string {
	t.Helper()
	files := make([]string, 0, len(platControlPlaneWrites))
	for _, e := range platControlPlaneWrites {
		files = append(files, e.file)
	}
	return cpScanFiles(t, cpFilesOf(files))
}

// cpSharedTables 从 DDL 扫出没有 tenant_id 的框架表（mysql 目录为准，三引擎结构由另一道闸核对）。
func cpSharedTables(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(findRoot(), "migrations", "framework", "mysql")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读 %s 失败：%v", dir, err)
	}
	tables := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		src := platRead(t, "migrations/framework/mysql/"+e.Name())
		for _, m := range cpCreateTableRe.FindAllStringSubmatch(src, -1) {
			if !cpTenantColRe.MatchString(m[2]) {
				tables[m[1]] = e.Name()
			}
		}
	}
	if len(tables) == 0 {
		t.Fatal("闸对空跑：DDL 里一张没有 tenant_id 的框架表都没扫到，判据本身失效")
	}
	return tables
}

// platConsoleReads 登记「读也整组收口」的控制台：这类页面的数据表没有 tenant_id，
// 而读出来的又是整个部署的量（监控快照与实时指标），所以不是「只挡写」而是整组只认平台调用方，
// 判据与在线客服 / 官网 / 设备中心同一条（core.RequirePlatformFeature）。
// 两张脸都要在：后端每条 GET 挂中间件，前端路由标 platform: true（否则进页面就是一串 403）。
var platConsoleReads = []struct {
	file      string
	group     string
	method    string
	path      string
	routeFile string
	why       string
}{
	{"modules/servermonitor/module.go", "r", "GET", "/monitor/server", "web/src/framework/modules/server-monitor/route.tsx", "本进程 CPU / 内存 / 磁盘"},
	{"modules/servermonitor/module.go", "r", "GET", "/monitor/server/history", "web/src/framework/modules/server-monitor/route.tsx", "sys_monitor_snapshot 没有 tenant_id"},
	{"modules/servermonitor/module.go", "r", "GET", "/monitor/health", "web/src/framework/modules/server-monitor/route.tsx", "整个部署的连通性与凭据配置状态"},
	{"modules/cachemonitor/module.go", "r", "GET", "/monitor/cache", "web/src/framework/modules/cache-monitor/route.tsx", "共享缓存的命中率与键空间"},
	{"modules/cachemonitor/module.go", "r", "GET", "/monitor/cache/history", "web/src/framework/modules/cache-monitor/route.tsx", "sys_monitor_snapshot 没有 tenant_id"},
	{"modules/apimonitor/module.go", "r", "GET", "/monitor/api", "web/src/framework/modules/api-monitor/route.tsx", "全租户混在一起的 QPS / 耗时 / 错误率与逐接口明细"},
	{"modules/apimonitor/module.go", "r", "GET", "/monitor/api/history", "web/src/framework/modules/api-monitor/route.tsx", "sys_monitor_snapshot 没有 tenant_id"},
}

// cpScanFiles 扫给定文件里的路由注册，返回「文件 + 分组变量 + 方法 + 路径 → 整条语句」。
func cpScanFiles(t *testing.T, files []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range files {
		src := platRead(t, rel)
		for _, loc := range cpRouteRe.FindAllStringSubmatchIndex(src, -1) {
			group, method := src[loc[2]:loc[3]], strings.ToUpper(src[loc[4]:loc[5]])
			key := rel + " " + group + " " + method + " " + src[loc[6]:loc[7]]
			out[key] = platStatementAt(src, loc[0])
		}
	}
	return out
}

// cpFilesOf 从台账里取出涉及的文件清单（去重、排序，保证扫描顺序稳定）。
func cpFilesOf(files []string) []string {
	set := map[string]bool{}
	for _, f := range files {
		set[f] = true
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func TestControlPlaneWriteGate(t *testing.T) {
	routes := cpRoutes(t)
	shared := cpSharedTables(t)

	// 1. 台账逐条：路由要在，且必须挂着闸。
	covered := map[string]bool{}
	for _, e := range platControlPlaneWrites {
		key := e.file + " " + e.group + " " + e.method + " " + e.path
		stmt, ok := routes[key]
		if !ok {
			t.Errorf("控制面写闸台账漂移：route_missing %s %s %s @%s（登记的路由在源码里找不到了：改名、换分组变量、挪文件都要回来改这条台账）",
				e.method, e.path, e.group, e.file)
			continue
		}
		if !strings.Contains(stmt, "core.RequirePlatform()") {
			t.Errorf("控制面全局行写入口没挂闸：%s %s @%s 写 %s（这张表没有 tenant_id，一行服务所有租户；权限点挡不住别的租户改写它）",
				e.method, e.path, e.file, e.table)
		}
		covered[e.table] = true
	}

	// 2. 这 8 个文件里挂了闸却没登记的：红（控制面新增全局写入口必须留名）。
	ledger := map[string]bool{}
	for _, e := range platControlPlaneWrites {
		ledger[e.file+" "+e.group+" "+e.method+" "+e.path] = true
	}
	for key, stmt := range routes {
		if ledger[key] || !strings.Contains(stmt, "core.RequirePlatform()") {
			continue
		}
		t.Errorf("控制面挂了闸但没登记：%s（新增一条全局写入口就把它进台账，并写清动的是哪张没有 tenant_id 的表）", key)
		covered["(未登记)"] = true
	}

	// 3. DDL 的每张共享表：台账覆盖 / 被台账路由一并写 / 已有更强闸或无 HTTP 写入口，三选一。
	var leaked []string
	for table, file := range shared {
		if covered[table] || platControlPlaneSharedBy[table] != "" || platControlPlaneOtherGuarded[table] != "" {
			continue
		}
		leaked = append(leaked, table+"（"+file+"）")
	}
	if len(leaked) > 0 {
		sort.Strings(leaked)
		t.Errorf("%d 张框架表没有 tenant_id 却没被任何一条判据解释：%s（要么进台账，要么在「已有更强闸」清单里写明是谁在挡）",
			len(leaked), strings.Join(leaked, ", "))
	}
	for table, reason := range platControlPlaneOtherGuarded {
		if _, ok := shared[table]; !ok {
			t.Errorf("「已有更强闸」登记失效：框架 DDL 里已经没有 %s 这张表了（理由：%s）", table, reason)
		}
		if covered[table] {
			t.Errorf("%s 同时登记成「台账覆盖」和「已有更强闸」：删掉一条", table)
		}
	}
	for table, reason := range platControlPlaneSharedBy {
		if _, ok := shared[table]; !ok {
			t.Errorf("「由别的路由一并写」登记失效：框架 DDL 里已经没有 %s（理由：%s）", table, reason)
		}
	}

	// 4. 回收站是字典这类全局行的第二入口：还原 / 彻底删除必须挡住非平台调用方。
	recycle := platRead(t, "modules/recycle/service.go")
	if !strings.Contains(recycle, "recbin.Platform[e.Type] && !platform") {
		t.Error("回收站没有按调用方收口平台级源表：租户能借 recycle:restore / recycle:delete 绕过字典写路由上的平台闸")
	}
	if !strings.Contains(recycle, `"error.platform_only"`) {
		t.Error("回收站的这条 403 没有专属文案 key：被挡的人只会看到 error.forbidden，不知道自己是动了全局行")
	}

	// 5. 设置页的凭据不许随 GET 下发给非平台调用方（smtp 密码、短信 access key、SSO secret）。
	cfgHandler := platRead(t, "modules/system/config/handler.go")
	if !strings.Contains(cfgHandler, "restrictSecrets(v)") {
		t.Error("GET /settings 没有对非平台调用方抹掉凭据字段：sys_setting 里的邮件 / 短信 / SSO 密钥会原样下发")
	}
	if !strings.Contains(cfgHandler, "func platformCaller(c *gin.Context) bool { return core.IsPlatformCaller(c) }") {
		t.Error("modules/system/config 自己重写了一份平台判据：判据必须单源（tenant.IsPlatformCallerID）")
	}

	t.Logf("控制面写闸：台账 %d 条路由（覆盖 %d 张全局行）；DDL 扫出无 tenant_id 的框架表 %d 张，"+
		"其中已有更强闸或无 HTTP 写入口 %d 张、由台账路由一并写 %d 张；回收站与设置页凭据两处第二入口已核对",
		len(platControlPlaneWrites), len(covered), len(shared), len(platControlPlaneOtherGuarded), len(platControlPlaneSharedBy))
}

// TestConsoleReadGate 判「读也整组收口」的那三个监控台：后端每条 GET 挂
// core.RequirePlatformFeature()，前端路由标 platform: true。两张脸缺任一张都会红——
// 只挂后端不标前端，租户进页面是一串 403；只标前端不挂后端，界面好看但数据照样泄露。
func TestConsoleReadGate(t *testing.T) {
	files := make([]string, 0, len(platConsoleReads))
	for _, e := range platConsoleReads {
		files = append(files, e.file)
	}
	routes := cpScanFiles(t, cpFilesOf(files))
	if len(routes) == 0 {
		t.Fatal("闸对空跑：监控模块里一条路由都没扫到")
	}
	byRouteFile := map[string]int{}
	for _, e := range platConsoleReads {
		key := e.file + " " + e.group + " " + e.method + " " + e.path
		stmt, ok := routes[key]
		if !ok {
			t.Errorf("整组收口台账漂移：route_missing %s %s @%s（登记的路由在源码里找不到了）", e.method, e.path, e.file)
			continue
		}
		if !strings.Contains(stmt, "core.RequirePlatformFeature()") {
			t.Errorf("监控台读入口没整组收口：%s %s @%s（%s——快照表没有 tenant_id，读的是整个部署的量）",
				e.method, e.path, e.file, e.why)
		}
		byRouteFile[e.routeFile]++
	}
	// 一个监控页可能同时打几条后端接口（实时 + 历史 + 健康），所以这里不比条数对等，
	// 只要求该路由文件里的每条路由都标了 platform: true。
	for routeFile := range byRouteFile {
		src := platRead(t, routeFile)
		perms := len(regexp.MustCompile(`(?m)^\s*perm:`).FindAllString(src, -1))
		// 行尾要容 \r：Windows 检出会把 .tsx 变成 CRLF，用 $ 直接锚死会在新克隆 / 新 worktree 上
		// 误报「没标 platform」（core/testdata 的 golden 当年就是同一个坑，见 AGENTS.md 第 3 节）。
		marked := len(regexp.MustCompile(`(?m)^\s*platform: true,\r?$`).FindAllString(src, -1))
		if perms == 0 {
			t.Errorf("%s 一条 perm 都没扫到：判定基础变了", routeFile)
		}
		if marked != perms {
			t.Errorf("%s 有 %d 条路由、只有 %d 条标了 platform: true：后端挡住读之后，没标的页面就是发一串必然 403 的请求",
				routeFile, perms, marked)
		}
	}
	// 控制面页面收成只读的那张脸：这些页面的写按钮必须按 isPlatform 置灰，
	// 否则租户管理员一路点到 403 才知道自己动不了全局行。
	for _, page := range []string{
		"web/src/framework/modules/config/SysConfigPage.tsx",
		"web/src/framework/modules/config/UploadLimitCard.tsx",
		"web/src/framework/modules/settings/SectionCard.tsx",
		"web/src/framework/modules/menu/MenuPage.tsx",
		"web/src/framework/modules/dict/DictTypePanel.tsx",
		"web/src/framework/modules/dict/DictDataPanel.tsx",
		"web/src/framework/modules/language/LanguagePage.tsx",
		"web/src/framework/modules/region/RegionSection.tsx",
		"web/src/framework/modules/recycle/RecyclePage.tsx",
		"web/src/plugins/ldap/LdapPage.tsx",
		"web/src/plugins/maps/MapSettingsPage.tsx",
		"web/src/plugins/media/VideoTab.tsx",
	} {
		src := platRead(t, page)
		if !strings.Contains(src, "isPlatform") {
			t.Errorf("%s 没有按 isPlatform 收写入口：后端挂闸、界面照旧，租户管理员只会反复点同一个 403", page)
		}
	}
	t.Logf("整组收口的读入口 %d 条（3 个监控台）；控制面只读页 %d 个已核对", len(platConsoleReads), 12)
}

// TestSecretFieldNamesAreCovered 把「密钥名单」从口头约定变成核对：
// 上一轮我在文档里写过「GET /settings 原先把 sms / notify / auth 的凭据原样下发」，那是错的——
// 服务层的 maskSecrets 早就对所有人生效（password / secretKey / apiKey / clientSecret）。
// 真正漏的是两类：名字不在表里的（accessKey、token 类），以及按名字表永远抓不到的
// notify.robots[].url（webhook 地址自带 access_token）。所以这里逐名核对，新增字段名不许掉队。
func TestSecretFieldNamesAreCovered(t *testing.T) {
	settingsSrc := platRead(t, "modules/system/config/settings.go")
	notifySrc := platRead(t, "modules/system/config/notify.go")
	handlerSrc := platRead(t, "modules/system/config/handler.go")

	// 1. 分区默认值里出现的字段名（只取三个构造函数体内、行首的 "key": 形状，避免抓到代码里的字符串）
	names := map[string]bool{}
	// 三个构造函数分家两处：defaultSettings / provider 在 settings.go，notifyDefaults 在 notify.go，
	// 所以按函数名去两个文件里找，找到哪个算哪个——只扫一个文件会误报「判据基础变了」。
	for _, fn := range []string{"func defaultSettings()", "func notifyDefaults()", "func provider()"} {
		body := ""
		for _, src := range []string{settingsSrc, notifySrc} {
			if i := strings.Index(src, fn); i >= 0 {
				body = src[i:]
				if end := strings.Index(body, "\n}\n"); end > 0 {
					body = body[:end]
				}
				break
			}
		}
		if body == "" {
			t.Fatalf("两个文件里都找不到 %s：判据基础变了", fn)
		}
		for _, m := range keyNameRe.FindAllStringSubmatch(body, -1) {
			names[m[1]] = true
		}
	}
	if len(names) < 20 {
		t.Fatalf("闸对空跑：只扫到 %d 个设置字段名", len(names))
	}

	// 2. 两份名单：服务层对所有人生效的 secretFields，与平台边界用的 secretKeyHints（按片段匹配）
	serviceMask := map[string]bool{}
	for _, m := range cpMapKeysRe.FindAllStringSubmatch(settingsSrc, -1) {
		serviceMask[m[1]] = true
	}
	hints := cpStringSliceRe.FindStringSubmatch(handlerSrc)
	if hints == nil {
		t.Fatal("handler.go 里找不到 secretKeyHints 名单")
	}
	var hintList []string
	for _, m := range cpQuoteRe.FindAllStringSubmatch(hints[1], -1) {
		hintList = append(hintList, m[1])
	}
	norm := func(s string) string {
		return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(s))
	}
	covered := func(name string) bool {
		if serviceMask[name] {
			return true
		}
		n := norm(name)
		for _, h := range hintList {
			if strings.Contains(n, norm(h)) {
				return true
			}
		}
		return false
	}
	secretish := func(name string) bool {
		n := norm(name)
		// 这张表要宽于两份名单：它的职责是「像密钥就得有人解释」，解释不了就该红，
		// 而不是「只有我认识的拼法才算」。publickey / signature 这类公开值刻意不在表里。
		for _, k := range []string{"password", "passwd", "passphrase", "secret", "apikey", "accesskey",
			"privatekey", "authkey", "appkey", "signkey", "sessionkey", "token", "credential", "bearer", "webhook"} {
			if strings.Contains(n, k) {
				return true
			}
		}
		return false
	}

	var missing []string
	for name := range names {
		if secretish(name) && !covered(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d 个设置字段名看着就是密钥，却不在任何一份名单里：%s（要么进 modules/system/config 的 secretFields，要么进 handler.go 的 secretKeyHints；漏一个就是把它明文发给每个租户）",
			len(missing), strings.Join(missing, ", "))
	}

	// 3. 按名字抓不到的那一类：机器人 webhook 的 url 必须按上下文码掉；这条规则不许悄悄消失
	if !strings.Contains(handlerSrc, `robotURL && k == "url"`) {
		t.Error("handler.go 没有「notify.robots[].url 按上下文打码」这条规则：webhook 地址自带 access_token，字段名却是 url，按名单永远漏")
	}
	// url 不在分区默认值里（默认是空列表），它是保存后落库的机器人条目字段——所以去 notify.go 的
	// robots 映射里找；找不到就说明那条上下文规则成了死代码。
	if !strings.Contains(notifySrc, `"url":`) {
		t.Error(`notify.go 的机器人条目里已经没有 "url" 字段了：那条 robots 规则该跟着删或改，别留死代码`)
	}

	// 4. 服务层那份对所有人生效的打码不许掉
	if !strings.Contains(settingsSrc, "maskSecrets(v)") || len(serviceMask) == 0 {
		t.Errorf("maskSecrets 或其名单不见了（名单 %d 项）：那是所有人——包括平台自己——读设置页的第一道", len(serviceMask))
	}

	t.Logf("设置字段名 %d 个，其中密钥形状 %d 个全部有名单覆盖；服务层 secretFields %d 项、平台边界 secretKeyHints %d 项；robots url 按上下文打码在位",
		len(names), countSecretish(names, secretish), len(serviceMask), len(hintList))
}

func countSecretish(names map[string]bool, pred func(string) bool) int {
	n := 0
	for k := range names {
		if pred(k) {
			n++
		}
	}
	return n
}
