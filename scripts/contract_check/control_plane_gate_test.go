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
	"sys_monitor_snapshot": "没有 HTTP 写入口，只有三个采样协程在写；读侧（监控历史）是另一条待决的产品线",
	"sys_role_menu":        "关联表：写入口是 PUT /roles/:id/menus（父角色按租户归属）与菜单删除的级联，后者已随 sys_menu 的路由一起挂闸",
	"sys_role_dept":        "关联表：随 PUT /roles/:id 一起写，父角色按租户归属",
	"sys_user_role":        "关联表：随用户创建 / 角色分配写，父用户按租户归属，角色归属另有 roleguard",
	"sys_user_post":        "关联表：同上",
}

var (
	cpCreateTableRe = regexp.MustCompile(`(?s)CREATE TABLE(?: IF NOT EXISTS)?\s+(\w+)\s*\((.*?)\n\)`)
	cpTenantColRe   = regexp.MustCompile(`\btenant_id\b`)
	cpRouteRe       = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_]\w*)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]*)",`)
)

// cpRoutes 扫这 8 个文件的路由注册，返回「文件 + 分组变量 + 方法 + 路径 → 整条语句」。
func cpRoutes(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, e := range platControlPlaneWrites {
		src := platRead(t, e.file)
		for _, loc := range cpRouteRe.FindAllStringSubmatchIndex(src, -1) {
			group, method, path := src[loc[2]:loc[3]], strings.ToUpper(src[loc[4]:loc[5]]), src[loc[6]:loc[7]]
			key := e.file + " " + group + " " + method + " " + path
			out[key] = platStatementAt(src, loc[0])
		}
	}
	return out
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
