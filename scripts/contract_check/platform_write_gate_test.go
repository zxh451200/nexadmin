package main

// platform_write_gate_test.go —— 「平台共享表的写入口只认平台级调用方」门禁。
//
// 判据一句话：表里没有 tenant_id（模型嵌 database.PlatformModel），一行服务所有租户，
// 那么写它的就不只是「有没有这个模块的权限」——权限点回答不了「能不能改别人也在用的那一行」。
// 今天挂 core.RequirePlatform() 的每一条路由，明天被人当成冗余中间件删掉，就是租户之间互相改写。
//
// 一张脸不够判，所以本闸核对四处（少哪处都红）：
//  1. 后端：清单里每条写路由的注册语句确实带着 core.RequirePlatform()；
//  2. 载荷：core/auth 把同一条判据随会话给出（User.Platform，normalize 里算）；
//  3. 前端：types/usePermission/auth.mock 三处都认这个字段——缺了它，开发态的只读态判不出来，
//     线上租户管理员会一路撞 403；
//  4. 文案：RequirePlatform 回的错误 key 在双语语言包里都有，缺 key 会把 key 原文甩给用户。
//
// 判定范围保守：只认「文件 + 方法 + 路径字面量」静态能定位的注册语句（与
// public_route_gate_test.go 同一条路子）。二跳分组、运行时拼路径不判。
// 两道闸各管一种空转，别指望其中一个替另一个兜底：本闸判「中间件还在不在」，把
// RequirePlatform 换成 c.Next() 那种「挂着但什么都不做」由 core/platform_test.go 判红。
// 未决插件（website/im/devicehub）进挂账：它们的表一张都没有 tenant_id，
// 「平台自有插件」还是「下一个要修的洞」需要同样的产品决定——挂账命中数为 0 时会报错，是账不是豁免。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// platWriteLedger 是已焊死的写路由清单：file 相对仓库根，path 是注册时的路径字面量。
type platWriteLedger struct {
	file   string
	method string
	path   string
	table  string // 它写的平台共享表；非表类副作用写明是什么
}

var platWriteLedgerRoutes = []platWriteLedger{
	{"plugins/ticket/handler.go", "POST", "/canned", "plg_ticket_canned"},
	{"plugins/ticket/handler.go", "PUT", "/canned/:id", "plg_ticket_canned"},
	{"plugins/ticket/handler.go", "DELETE", "/canned/:id", "plg_ticket_canned"},
	{"plugins/ticket/handler.go", "PUT", "/sla", "plg_ticket_sla"},
	{"plugins/ticket/handler.go", "POST", "/sla/policy", "plg_ticket_sla"},
	{"plugins/ticket/handler.go", "DELETE", "/sla/policy/:code", "plg_ticket_sla"},
	{"plugins/ticket/handler.go", "POST", "/groups", "plg_ticket_group"},
	{"plugins/ticket/handler.go", "PUT", "/groups/:id", "plg_ticket_group"},
	{"plugins/ticket/handler.go", "DELETE", "/groups/:id", "plg_ticket_group"},
	{"plugins/ticket/handler.go", "PUT", "/groups/:id/members", "plg_ticket_group_member"},
	{"plugins/ticket/handler.go", "POST", "/rules", "plg_ticket_rule"},
	{"plugins/ticket/handler.go", "PUT", "/rules/:id", "plg_ticket_rule"},
	{"plugins/ticket/handler.go", "DELETE", "/rules/:id", "plg_ticket_rule"},
	{"plugins/ticket/handler.go", "POST", "/fields", "plg_ticket_field"},
	{"plugins/ticket/handler.go", "PUT", "/fields/:id", "plg_ticket_field"},
	{"plugins/ticket/handler.go", "DELETE", "/fields/:id", "plg_ticket_field"},
	{"plugins/ticket/handler.go", "PUT", "/settings", "plg_ticket_setting"},
	{"plugins/codegen/plugin.go", "POST", "/configs", "plg_codegen_config"},
	{"plugins/codegen/plugin.go", "DELETE", "/configs/:id", "plg_codegen_config"},
	{"plugins/codegen/plugin.go", "DELETE", "/configs", "plg_codegen_config"},
	// 不是共享表，但同一判据：写进项目根目录的文件不属于任何租户。
	{"plugins/codegen/plugin.go", "POST", "/install", "(写磁盘：渲染结果落进项目根目录)"},
}

// platSharedPendingPlugins 登记「有 PlatformModel 表但写路由尚未挂闸」的插件，逐条写原因。
// 这些插件不在本闸判定范围（要各自的产品决定），但必须留名——静默漏过才是这条闸存在的理由。
var platSharedPendingPlugins = []struct {
	plugin, reason string
}{
	{"website", "官网是平台自己的对外窗口（12 张表无 tenant_id）：按平台自有插件处理还是挂闸，待产品决定"},
	{"im", "在线客服的会话/消息/附件/快捷回复/设置 5 张表都没有 tenant_id，跨租户可见性同上一条决定"},
	{"devicehub", "设备台账/点位/读数为单租户垂直场景设计，无 tenant_id；该插件本会话在别人在途域，只登记不代修"},
}

// platSharedErrKey 是 RequirePlatform 回的错误 key（core/router.go），双语都要有。
var platSharedErrKey = "error.platform_only"

var (
	platRouteRe = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_]\w*)\.(GET|POST|PUT|PATCH|DELETE|ANY)\(\s*"([^"]+)"`)
	// type X struct { ... database.PlatformModel ... }
	platModelStructRe = regexp.MustCompile(`(?ms)^type\s+(\w+)\s+struct\s*\{[^}]*?database\.PlatformModel`)
	platTableNameRe   = func(name string) *regexp.Regexp {
		// 接收者可能写成 (CannedReply) 或 (c CannedReply)：`\w*\s+` 那种写法会漏掉前者，整张表就扫不出来。
		return regexp.MustCompile(`(?s)func\s*\(\s*(?:\w+\s+)?` + name + `\s*\)\s*TableName\(\)\s*string\s*\{\s*return\s*"([^"]+)"`)
	}
)

// platStatementAt 从注册调用的匹配处取出整条语句（括号配平），供「这句里有没有某个中间件」判定。
func platStatementAt(src string, from int) string {
	open := strings.Index(src[from:], "(")
	if open < 0 {
		return ""
	}
	i := from + open
	depth := 0
	for ; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[from : i+1]
			}
		}
	}
	return src[from:]
}

// platRoutes 扫描插件源码里静态可确定的路由注册语句，返回「路由 → 整条语句」。
func platRoutes(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(filepath.Join(findRoot(), dir), func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("读 %s 失败：%v（这条闸依赖它，缺文件不算通过）", p, err)
		}
		src := string(b)
		for _, loc := range platRouteRe.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[loc[4]:loc[5]])
			path := src[loc[6]:loc[7]]
			rel, _ := filepath.Rel(findRoot(), p)
			// Windows 上 Rel 给反斜杠，清单里写的是正斜杠：不归一就每条登记都判成 route_missing。
			key := filepath.ToSlash(rel) + " " + method + " " + path
			out[key] = platStatementAt(src, loc[0])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败：%v", dir, err)
	}
	return out
}

// platSharedTables 返回每个插件里嵌 database.PlatformModel 的表（插件 → 表名集合）。
func platSharedTables(t *testing.T) map[string]map[string]bool {
	t.Helper()
	res := map[string]map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(findRoot(), "plugins"))
	if err != nil {
		t.Fatalf("读 plugins/ 失败：%v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tables := map[string]bool{}
		err := filepath.Walk(filepath.Join(findRoot(), "plugins", e.Name()), func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("读 %s 失败：%v", p, err)
			}
			src := string(b)
			for _, m := range platModelStructRe.FindAllStringSubmatch(src, -1) {
				if tm := platTableNameRe(m[1]).FindStringSubmatch(src); tm != nil {
					tables[tm[1]] = true
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 plugins/%s 失败：%v", e.Name(), err)
		}
		if len(tables) > 0 {
			res[e.Name()] = tables
		}
	}
	return res
}

// platRead 读仓库内文件（缺文件即红：闸不许在缺件时空转）。
func platRead(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(findRoot(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("读 %s 失败：%v（这条闸依赖它，缺文件不算通过）", rel, err)
	}
	return string(b)
}

func TestPlatformWriteGate(t *testing.T) {
	routes := platRoutes(t, "plugins")
	shared := platSharedTables(t)
	if len(shared) == 0 {
		t.Fatal("闸对空跑：一个嵌 database.PlatformModel 的插件模型都没扫到，判定本身失效")
	}

	// 1. 清单逐条：路由要在，且必须挂着闸。
	gatedTables := map[string]bool{}
	for _, e := range platWriteLedgerRoutes {
		key := e.file + " " + e.method + " " + e.path
		stmt, ok := routes[key]
		if !ok {
			t.Errorf("平台写闸清单漂移：route_missing %s %s @%s（清单里登记的路由在源码里找不到了：改名/删除/换文件都要回来改这条清单）",
				e.method, e.path, e.file)
			continue
		}
		if !strings.Contains(stmt, "core.RequirePlatform()") {
			t.Errorf("平台共享表写入口没挂闸：%s %s @%s 写 %s（表里没有 tenant_id，一行服务所有租户；权限点挡不住别的租户改写这一行）",
				e.method, e.path, e.file, e.table)
		}
		if !strings.HasPrefix(e.table, "(") {
			gatedTables[e.table] = true
		}
	}

	// 2. 表要还在：清单点名的表如果已经不再是 PlatformModel，说明判定基础变了。
	for _, e := range platWriteLedgerRoutes {
		if strings.HasPrefix(e.table, "(") {
			continue
		}
		found := false
		for _, tables := range shared {
			if tables[e.table] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("平台写闸清单里的表 %s 已不再嵌 database.PlatformModel（@%s %s %s）：要么它已按租户建表（那就删掉这条登记），要么模型改形让这条判据失效了",
				e.table, e.file, e.method, e.path)
		}
	}

	// 3. 每个有共享表的插件：要么全部表都被清单覆盖，要么在挂账里留名——不许静默漏过。
	// 台账里的文件路径形如 plugins/ticket/handler.go，插件名取第二段。
	plugin := func(file string) string {
		segs := strings.Split(filepath.ToSlash(file), "/")
		if len(segs) < 2 {
			return file
		}
		return segs[1]
	}
	coveredByPlugin := map[string]map[string]bool{}
	for _, e := range platWriteLedgerRoutes {
		if strings.HasPrefix(e.table, "(") {
			continue
		}
		p := plugin(e.file)
		if coveredByPlugin[p] == nil {
			coveredByPlugin[p] = map[string]bool{}
		}
		coveredByPlugin[p][e.table] = true
	}
	pending := map[string]string{}
	for _, p := range platSharedPendingPlugins {
		pending[p.plugin] = p.reason
	}
	for plug, tables := range shared {
		if _, ok := pending[plug]; ok {
			continue
		}
		var missing []string
		for tb := range tables {
			if !coveredByPlugin[plug][tb] {
				missing = append(missing, tb)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("插件 %s 有 %d 张平台共享表没进平台写闸台账：%s（新加的 PlatformModel 表必须选一条：挂闸进清单，或写明「平台自有/待决定」进挂账）",
				plug, len(missing), strings.Join(missing, ", "))
		}
	}
	for plug, reason := range pending {
		if _, ok := shared[plug]; !ok {
			t.Errorf("挂账项已失效（插件 %s 已没有 PlatformModel 表）：%s", plug, reason)
		}
		if len(coveredByPlugin[plug]) > 0 {
			t.Errorf("挂账与清单同时登记了插件 %s：它的表已经挂闸了，删掉挂账项", plug)
		}
	}

	// 4. 载荷那张脸：判据要随会话给到前端，否则前端只能拿 perms.includes('*') 近似，
	//    平台租户里非超管的坐席会被误伤成只读（后端其实允许他写）。
	userSrc := platRead(t, "core/auth/auth.go")
	if !regexp.MustCompile(`(?m)^\s*Platform\s+bool\s+` + "`json:\"platform\"`").MatchString(userSrc) {
		t.Error("auth.User 缺 Platform bool `json:\"platform\"`：前端算不出「本次调用方是否平台级」，只读态只能靠猜")
	}
	if !strings.Contains(userSrc, "func (u *User) IsPlatform() bool") {
		t.Error("core/auth/auth.go 缺 IsPlatform()：判据没有单源，前端与后端会各写一份并各自漂移")
	}
	if !strings.Contains(platRead(t, "core/auth/handler.go"), "u.Platform = u.IsPlatform()") {
		t.Error("normalize() 没填 Platform：登录与 /auth/me 都走它，漏了就等于载荷里永远没有这个字段")
	}

	// 5. 前端那张脸：三处都要认这个字段。
	webFiles := map[string]string{
		"web/src/framework/auth/types.ts":         "platform",
		"web/src/framework/auth/usePermission.ts": "isPlatform",
		"web/src/framework/auth/auth.mock.ts":     "platform:",
	}
	for f, needle := range webFiles {
		if !strings.Contains(platRead(t, f), needle) {
			t.Errorf("前端平台判据缺失：%s 里没有 %q（后端挂闸、前端不认，租户管理员就是一路撞 403）", f, needle)
		}
	}

	// 6. 文案那张脸：403 的错误 key 双语都要在（缺 key 会把 key 原文甩给用户）。
	for _, lang := range []string{"zh-CN", "en-US"} {
		src := platRead(t, "core/i18n/locales/"+lang+".yaml")
		if !strings.Contains(src, "platform_only:") {
			t.Errorf("语言包 %s 缺 error.platform_only：租户用户撞 403 时看到的是 key 原文，不是「这是平台共享配置，请联系平台管理员」", lang)
		}
	}
	routerSrc := platRead(t, "core/router.go")
	if !strings.Contains(routerSrc, `response.Forbidden("`+platSharedErrKey+`")`) {
		t.Errorf("RequirePlatform 没有回 %s：回笼统的 forbidden 会让人以为是自己权限配错，转头去翻角色", platSharedErrKey)
	}

	// 7. 多挂的闸只登记不判红：并行会话里别人也在收自己的共享表，判红就成了互相踩。
	extra := 0
	for key, stmt := range routes {
		if !strings.Contains(stmt, "core.RequirePlatform()") {
			continue
		}
		listed := false
		for _, e := range platWriteLedgerRoutes {
			if key == e.file+" "+e.method+" "+e.path {
				listed = true
				break
			}
		}
		if !listed {
			extra++
			t.Logf("挂了闸但未登记（不判红，请补清单或说明）：%s", key)
		}
	}

	t.Logf("平台写闸：登记 %d 条路由（覆盖 %d 张共享表）；插件共享表清单 %d 个插件共 %d 张，挂账 %d 个插件；多挂未登记 %d 条",
		len(platWriteLedgerRoutes), len(gatedTables), len(shared), platCountTables(shared), len(pending), extra)
}

func platCountTables(shared map[string]map[string]bool) int {
	n := 0
	for _, tables := range shared {
		n += len(tables)
	}
	return n
}
