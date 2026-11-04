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
// 两类收口都在判：单条写路由挂 core.RequirePlatform()（共享配置表，见 platWriteLedgerRoutes），
// 以及整个后台挂 core.RequirePlatformFeature()（表里没有 tenant_id 的插件，读也是泄露，
// 见 platPluginWideGates——后端分组与前端 platform: true 两张脸都要在）。
// 还没收口的插件（devicehub）进挂账：它的表同样一张 tenant_id 都没有，
// 「平台自有」还是「下一个要按租户切开的洞」需要它自己的产品决定——挂账命中数为 0 时会报错，是账不是豁免。

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

// platPluginWideGates 登记「整个后台只对平台调用方开放」的插件：这类插件的共享表装的不是配置而是
// 客户数据，一次列表或一次导出就把别的租户的记录带走，所以挡写不够、读也要挡——判据从
// 「能不能改这一行」升级为「这张表根本不分租户」。两张脸都要在：后端分组挂
// core.RequirePlatformFeature()，前端该插件的每条路由标 platform: true（否则人进来是一串 403）。
var platPluginWideGates = []struct {
	plugin      string
	backendFile string
	groupPath   string // 受保护分组的注册形态，用来核对 Use 挂在路由之前
	routeFile   string
	why         string
}{
	{"im", "plugins/im/handler.go", "/plugins/im", "web/src/plugins/im/route.tsx",
		"会话/消息/附件/快捷回复/设置五张表都没有 tenant_id，convFilter 里没有任何租户谓词"},
	{"website", "plugins/website/plugin.go", "/plugins/website", "web/src/plugins/website/route.tsx",
		"内容与访客留言都没有 tenant_id，官网代表部署主体而不是某个租户"},
}

// platSharedPendingPlugins 登记「有 PlatformModel 表但既没挂闸也没整组收口」的插件，逐条写原因。
// 这些插件不在本闸判定范围（要各自的产品决定），但必须留名——静默漏过才是这条闸存在的理由。
var platSharedPendingPlugins = []struct {
	plugin, reason string
}{
	{"devicehub", "设备台账/点位/读数为单租户垂直场景设计，无 tenant_id；该插件本会话在别人在途域，只登记不代修"},
}

// platSharedErrKey / platFeatureErrKey 是两种平台闸各回的错误 key（core/router.go），双语都要有：
// 前者是「这一行大家共用，轮不到你改」，后者是「这张表根本不分租户，整个后台不对你开放」——
// 混用一条文案会让读被挡的人以为自己是配置错了。
var (
	platSharedErrKey  = "error.platform_only"
	platFeatureErrKey = "error.platform_only_feature"
)

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

	// 3b. 整组收口的插件：它的全部共享表都算覆盖（闸挂在分组上，不在单条路由上），
	// 但两张脸都要真在：后端分组的 Use，和前端每条路由的 platform: true。
	for _, w := range platPluginWideGates {
		if _, ok := pending[w.plugin]; ok {
			t.Errorf("插件 %s 同时登记成「整组收口」和「挂账」：删掉一条，别用两份说法盖同一件事", w.plugin)
		}
		if _, ok := shared[w.plugin]; !ok {
			t.Errorf("整组收口登记已失效（插件 %s 已没有 PlatformModel 表）：%s", w.plugin, w.why)
			continue
		}
		backend := platRead(t, w.backendFile)
		useAt := strings.Index(backend, "g.Use(core.RequirePlatformFeature())")
		if useAt < 0 {
			t.Errorf("插件 %s 的后台分组没整组收口：%s 里找不到 g.Use(core.RequirePlatformFeature())（%s）",
				w.plugin, w.backendFile, w.why)
		} else {
			if at := strings.Index(backend, `r.Group("`+w.groupPath+`")`); at < 0 || useAt < at {
				t.Errorf("插件 %s 的整组闸位置不对：Use 必须紧跟分组声明（%s）之后、路由之前，否则前面注册的路由不受它管",
					w.plugin, w.groupPath)
			}
			if first := strings.Index(backend, "g.GET("); first >= 0 && useAt > first {
				t.Errorf("插件 %s 的 g.Use(core.RequirePlatformFeature()) 落在第一条路由之后：gin 的分组中间件只对之后注册的路由生效，前面那些等于没挂", w.plugin)
			}
		}
		routeSrc := platRead(t, w.routeFile)
		// 只数真属性行：注释里也可能写 platform: true，strings.Count 会把说明当成登记。
		perms := len(regexp.MustCompile(`(?m)^\s*perm:`).FindAllString(routeSrc, -1))
		marked := len(regexp.MustCompile(`(?m)^\s*platform: true,$`).FindAllString(routeSrc, -1))
		if perms == 0 {
			t.Errorf("插件 %s 的前端路由表 %s 里一条 perm 都没扫到：判定基础变了", w.plugin, w.routeFile)
		} else if marked != perms {
			t.Errorf("插件 %s 有 %d 条前端路由、只有 %d 条标了 platform: true：后端整组挡了读，前端没标的那几条会让人发一串必然 403 的请求再弹错误信封（%s）",
				w.plugin, perms, marked, w.why)
		}
		if coveredByPlugin[w.plugin] == nil {
			coveredByPlugin[w.plugin] = map[string]bool{}
		}
		for tb := range shared[w.plugin] {
			coveredByPlugin[w.plugin][tb] = true
		}
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

	// 5. 前端那张脸：会话判据三处都要认，路由级判据两处都要认。
	webFiles := map[string]string{
		"web/src/framework/auth/types.ts":         "platform",
		"web/src/framework/auth/usePermission.ts": "isPlatform",
		"web/src/framework/auth/auth.mock.ts":     "platform:",
		// 平台级插件的页面守卫：判据在 ModuleRoute 上声明，在 AppRoutes 里集中过闸。
		"web/src/framework/router/types.ts":      "platform?: boolean",
		"web/src/framework/router/AppRoutes.tsx": "route.platform && platform === false",
	}
	for f, needle := range webFiles {
		if !strings.Contains(platRead(t, f), needle) {
			t.Errorf("前端平台判据缺失：%s 里没有 %q（后端挂闸、前端不认，租户管理员就是一路撞 403）", f, needle)
		}
	}

	// 6. 文案那张脸：两种 403 的 key 双语都要在（缺 key 会把 key 原文甩给用户），
	//    平台级插件的解释页文案也要双语都在——那是非平台调用方看到的第一个整页。
	for _, lang := range []string{"zh-CN", "en-US"} {
		src := platRead(t, "core/i18n/locales/"+lang+".yaml")
		for _, key := range []string{"platform_only:", "platform_only_feature:"} {
			if !strings.Contains(src, key) {
				t.Errorf("语言包 %s 缺 error.%s：撞闸的人看到的是 key 原文，不是「这一份所有租户共用/这张表不分租户」", lang, strings.TrimSuffix(key, ":"))
			}
		}
		json := platRead(t, "web/src/framework/locales/"+lang+".json")
		if !strings.Contains(json, `"platformOnly"`) {
			t.Errorf("前端语言包 %s 缺 error.platformOnly：平台级插件的解释页会显示空白标题", lang)
		}
	}
	routerSrc := platRead(t, "core/router.go")
	for _, pair := range [][2]string{
		{`RequirePlatform() gin.HandlerFunc`, `requirePlatform("` + platSharedErrKey + `")`},
		{`RequirePlatformFeature() gin.HandlerFunc`, `requirePlatform("` + platFeatureErrKey + `")`},
	} {
		if !strings.Contains(routerSrc, pair[0]) || !strings.Contains(routerSrc, pair[1]) {
			t.Errorf("core/router.go 里 %s 与它回的错误 key 对不上（找 %q / %q）：闸与文案分家，用户看到的句子就不再解释他被挡的原因",
				pair[0], pair[0], pair[1])
		}
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

	t.Logf("平台写闸：登记 %d 条路由（覆盖 %d 张共享表）+ 整组收口 %d 个插件；插件共享表清单 %d 个插件共 %d 张，挂账 %d 个插件；多挂未登记 %d 条",
		len(platWriteLedgerRoutes), len(gatedTables), len(platPluginWideGates), len(shared), platCountTables(shared), len(pending), extra)
}

func platCountTables(shared map[string]map[string]bool) int {
	n := 0
	for _, tables := range shared {
		n += len(tables)
	}
	return n
}
