package main

// public_route_gate_test.go —— 「匿名路由」三面一致性门禁（刻意保守：只判它能确定判的）。
//
// 一条匿名路由有三张脸，任何一张漏了都会静默出事，本会话真的为此踩过：
//  1. 后端注册：插件用 d.Public.Group("/auth/saml") / d.Public.GET(...)（匿名），见 plugins/saml/plugin.go:35-37；
//  2. mock 白名单：web/src/framework/request/mock.ts 的 PUBLIC_PREFIXES —— 不在表里的路径，
//     mock 适配器直接回 401 信封，而 401 会连带拆登录态并 queryClient.clear()；
//  3. mock 规则：某个 *.mock.ts 里的 { method, url }。白名单放行但没有规则，
//     适配器回「Mock not found」404，调用方各自的 .catch 把这块 UI 藏掉，开发态永远跑不到。
//
// 判定范围刻意收窄到静态能确定的三形：
//   - `d.Public.GET("/p", …)` 直挂；
//   - `x := d.Public.Group("/p")` 之后同文件内 `x.GET("/s", …)`；
//   - PUBLIC_PREFIXES 自身的「有没有对应 mock 规则」。
// 二跳分组、运行时拼路径、常量拼路径一律进 unresolved，只在末尾 t.Log 登记不判：
// 宁可少判，也不要一条常年误报的闸（误报的结局是被人加豁免，真漂移跟着一起被豁免掉）。
// 新增别的匿名注册写法时，改本文件的正则或补豁免表，别用豁免表掩盖真漂移。
// 「不判」的出口只有三类，且都绑探针：外部系统触发（pubGateBackendOnly）、页面级消费者
// （pubGatePageConsumers——登记里的探针片段在消费者源里找不到就红）、判不了的注册形态（unresolved，只登记）。
// admin SPA 一旦真去请求某条「页面级消费者」路由，豁免自动让位给 mock 要求。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	pubGatePrefixArray = regexp.MustCompile(`(?s)PUBLIC_PREFIXES\s*=\s*\[([^\]]*)\]`)
	pubGateStringLit   = regexp.MustCompile(`'([^']+)'`)
	// x := d.Public.Group("/p")
	pubGateGroupDecl = regexp.MustCompile(`(?m)^\t*(\w+)\s*:?=\s*d\.Public\.Group\(\s*"([^"]+)"`)
	// d.Public.GET("/p", …)
	pubGateDirectRoute = regexp.MustCompile(`(?m)^\s*d\.Public\.(GET|POST|PUT|PATCH|DELETE|ANY)\(\s*"([^"]+)"`)
	// x.GET("/s", …)（x 必须是上一行那种匿名分组）
	pubGateGroupRoute = regexp.MustCompile(`(?m)^\t*(\w+)\.(GET|POST|PUT|PATCH|DELETE|ANY)\(\s*"([^"]+)"`)

	pubGateMockMethod = regexp.MustCompile(`method:\s*'(GET|POST|PUT|PATCH|DELETE|ANY)'`)
	pubGateMockURL    = regexp.MustCompile(`url:\s*'([^']+)'`)
)

// pubGateBackendOnly：确实只由外部系统或整页跳转触发的公开路径前缀，逐条写原因。
// 前端源码里可能出现同样的字符串（文档、跳转链接、注释），所以「被前端请求」的粗探针会在这些
// 路径上误判；与其让探针变聪明，不如把这一类显式登记——命中数为 0 时会报错，防过期。
var pubGateBackendOnly = []struct {
	prefix, reason string
}{
	{"/api/v1/plugins/pay/notify/", "支付网关异步回调：由渠道服务器 POST，浏览器不请求"},
	{"/api/v1/plugins/ticket/inbound/email", "邮件进单：由 IMAP 轮询器/邮件网关投递"},
	{"/api/v1/plugins/identity/cas/", "CAS 的 login/callback/logout/slo 都是整页跳转或后端校验票据"},
}

// pubGateExemptRoute 判断某条路由是否落在「外部触发」登记里。
func pubGateExemptRoute(path string) (string, bool) {
	for _, e := range pubGateBackendOnly {
		if strings.HasPrefix(path, e.prefix) {
			return e.reason, true
		}
	}
	return "", false
}

// pubGatePageConsumers：由「插件自己渲染的无登录页面」或「官网静态挂件」直连的公开路径。
// 这条调用栈上没有 admin SPA 的 axios，mock 规则帮不上忙；但它不是永久豁免，两头都绑了探针：
//  1. 登记的 probe 片段必须还能在 files 里找到——消费者改了路径或没了，条目就红；
//  2. admin SPA 一旦真的请求同一条路由，这条登记让位，照常要求 PUBLIC_PREFIXES + mock 规则。
//     那正是「以后加了前端消费者却没人补 mock」的漂移形态。
var pubGatePageConsumers = []struct {
	prefix, probe string
	files         []string
	reason        string
}{
	{
		prefix: "/api/v1/plugins/ticket/visitor/",
		probe:  "'/api/v1/plugins/ticket/visitor'",
		files:  []string{"plugins/ticket/pages.go"},
		reason: "客户自助页由插件自己渲染（无前端构建产物），页面里的 JS 直连这些 JSON 接口",
	},
	{
		prefix: "/api/v1/plugins/im/visitor/",
		probe:  "'/api/v1/plugins/im/visitor'",
		files:  []string{"plugins/website/static/chat-widget.js"},
		reason: "在线客服挂件是官网静态 JS，用 fetch + EventSource 直连，不经 admin 的 mock 适配器",
	},
}

func pubGateIsPageConsumed(path string) bool {
	for _, e := range pubGatePageConsumers {
		if strings.HasPrefix(path, e.prefix) {
			return true
		}
	}
	return false
}

// pubGateBrowserBinding：SAML 的 IdP 绑定路由——浏览器整页跳转（login/slo/metadata）或
// 由 IdP 表单 POST 回来（acs）。前端源里确实会出现同一条路径串，但它在 `window.location.href`
// 里，不在 axios 栈上，mock 规则拦不到也不该要求。逐条登记（不是整组前缀豁免，
// 所以同组里 /sources、/exchange 这类真被 axios 调的路由照常要求 mock）；路由不见了就报错。
var pubGateBrowserBinding = []string{
	"GET /api/v1/auth/saml/:id/login",
	"GET /api/v1/auth/saml/:id/metadata",
	"POST /api/v1/auth/saml/:id/acs",
	"GET /api/v1/auth/saml/:id/slo",
	"POST /api/v1/auth/saml/:id/slo",
}

func pubGateIsBrowserBinding(route string) bool {
	for _, r := range pubGateBrowserBinding {
		if r == route {
			return true
		}
	}
	return false
}

// pubGateVerifyPageConsumers 逐条核对登记自己还成立：下面挂着匿名路由、探针片段还在消费者源里。
func pubGateVerifyPageConsumers(t *testing.T, root string, routes []pubGateFound) {
	t.Helper()
	for _, e := range pubGatePageConsumers {
		carries := false
		for _, r := range routes {
			if strings.HasPrefix(r.full, "ANY "+e.prefix) || strings.Contains(r.full, " "+e.prefix) {
				carries = true
				break
			}
		}
		if !carries {
			t.Errorf("「页面级消费者」登记已失效（没有匿名路由落在 %s 下面）：%s", e.prefix, e.reason)
			continue
		}
		probed := false
		for _, f := range e.files {
			if strings.Contains(pubGateReadFile(t, filepath.Join(root, filepath.FromSlash(f))), e.probe) {
				probed = true
				break
			}
		}
		if !probed {
			t.Errorf("登记 %s 的探针 %q 在 %v 里找不到了：消费者改道或已删，这条豁免正在掩盖漂移（要改登记，别删闸）",
				e.prefix, e.probe, e.files)
		}
	}
}

func pubGateReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败：%v（这条闸依赖它，缺文件不算通过）", path, err)
	}
	return string(b)
}

type pubGateFound struct {
	full string // 方法 + 带 /api/v1 的完整路径
	file string
}

// pubGatePluginRoutes 返回静态可确定的插件匿名路由，以及「注册了匿名路由但本闸判不了」的清单。
func pubGatePluginRoutes(t *testing.T) ([]pubGateFound, []string) {
	t.Helper()
	root := findRoot()
	entries, err := os.ReadDir(filepath.Join(root, "plugins"))
	if err != nil {
		t.Fatalf("读 plugins/ 目录失败：%v", err)
	}
	var out []pubGateFound
	var unresolved []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, "plugins", e.Name())
		err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			src := pubGateReadFile(t, p)
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			groups := map[string]string{}
			for _, m := range pubGateGroupDecl.FindAllStringSubmatch(src, -1) {
				groups[m[1]] = "/api/v1" + m[2]
			}
			for _, m := range pubGateDirectRoute.FindAllStringSubmatch(src, -1) {
				out = append(out, pubGateFound{full: strings.ToUpper(m[1]) + " /api/v1" + m[2], file: rel})
			}
			for _, m := range pubGateGroupRoute.FindAllStringSubmatch(src, -1) {
				prefix, ok := groups[m[1]]
				if !ok {
					continue // 不是匿名分组（受保护路由，或二跳分组：保守跳过）
				}
				out = append(out, pubGateFound{full: strings.ToUpper(m[2]) + " " + prefix + m[3], file: rel})
			}
			for _, line := range strings.Split(src, "\n") {
				l := strings.TrimSpace(line)
				if !strings.Contains(l, "d.Public") || strings.HasPrefix(l, "//") {
					continue
				}
				if pubGateGroupDecl.FindString(l) != "" || pubGateDirectRoute.FindString(l) != "" {
					continue
				}
				if strings.Contains(l, ".Group(") || strings.Contains(l, ".Any(") || strings.Contains(l, ".GET(") ||
					strings.Contains(l, ".POST(") || strings.Contains(l, ".PUT(") || strings.Contains(l, ".DELETE(") {
					unresolved = append(unresolved, rel+": "+l)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败：%v", dir, err)
		}
	}
	return out, unresolved
}

// pubGateMockState 返回 mock 规则集合（方法 + 带 /api/v1 的路径）与 PUBLIC_PREFIXES。
func pubGateMockState(t *testing.T) (map[string]bool, []string) {
	t.Helper()
	root := findRoot()
	rules := map[string]bool{}
	src := pubGateReadFile(t, filepath.Join(root, "web", "src", "framework", "request", "mock.ts"))
	var prefixes []string
	if m := pubGatePrefixArray.FindStringSubmatch(src); m != nil {
		for _, lit := range pubGateStringLit.FindAllStringSubmatch(m[1], -1) {
			prefixes = append(prefixes, lit[1])
		}
	}
	err := filepath.Walk(filepath.Join(root, "web", "src"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".mock.ts") {
			return nil
		}
		body := pubGateReadFile(t, p)
		for _, loc := range pubGateMockURL.FindAllStringSubmatchIndex(body, -1) {
			url := body[loc[2]:loc[3]]
			method := "ANY"
			if mm := pubGateMockMethod.FindStringSubmatch(body[loc[1]:]); mm != nil {
				method = mm[1]
			}
			// mock 的 url 不带 /api/v1（baseURL 拼上去），这里补同轴比较。
			rules[method+" /api/v1"+url] = true
			rules["ANY /api/v1"+url] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 web/src 失败：%v", err)
	}
	return rules, prefixes
}

// pubGateMatches 按段比较 gin 路径：`:id` 与 `*` 匹配任意一段。
func pubGateMatches(pattern, actual string) bool {
	pp := strings.Split(strings.Trim(pattern, "/"), "/")
	aa := strings.Split(strings.Trim(actual, "/"), "/")
	if len(pp) != len(aa) {
		return false
	}
	for i, seg := range pp {
		if strings.HasPrefix(seg, ":") || seg == "*" {
			continue
		}
		if seg != aa[i] {
			return false
		}
	}
	return true
}

func pubGateHasRule(rules map[string]bool, method, path string) bool {
	trimmed := strings.TrimPrefix(path, "/api/v1")
	for r := range rules {
		i := strings.Index(r, " ")
		if i < 0 {
			continue
		}
		rm, ru := r[:i], strings.TrimPrefix(r[i+1:], "/api/v1")
		if rm != "ANY" && rm != method {
			continue
		}
		if ru == trimmed || pubGateMatches(ru, trimmed) {
			return true
		}
	}
	return false
}

// pubGateFrontendCalls 把 web/src 下所有 .ts/.tsx 源拼成一个串，用于判断某条匿名路由
// 到底是不是前端在请求（后端专用/IdP 回调/服务端渲染的公开页就不该要求 mock 规则）。
// mock 自身的两份（*.mock.ts 与 request/mock.ts）不算调用者：mock 是第二份契约，
// 让它自证「前端在调用」等于用登记本身把登记里的空洞填平。
func pubGateFrontendCalls(t *testing.T) string {
	t.Helper()
	root := findRoot()
	var b strings.Builder
	err := filepath.Walk(filepath.Join(root, "web", "src"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return err
		}
		base := filepath.Base(p)
		if info.IsDir() || !(strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx")) ||
			strings.HasSuffix(base, ".mock.ts") || base == "mock.ts" {
			return nil
		}
		b.WriteString(pubGateReadFile(t, p))
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 web/src 失败：%v", err)
	}
	return b.String()
}

// pubGateCalledFromFrontend 用路由的完整骨架去前端源里找：`:no` 这类参数段换成「一段不含
// 引号/空格/?/#/斜杠的任意串」，其余段按字面，并且整条必须紧跟在一个引号之后。
// 只认引号起始是因为本闸问的是「admin 的 axios 有没有请求它」——mock 适配器只在 axios 栈上；
// `${API_BASE}/plugins/im/visitor/attachments/...` 这种拼进模板的下载链接是浏览器直连，
// 补 mock 规则也拦不到，不该由这条闸要求。
// 不用「从长到短退回」：那会把 `plugins/ticket` 这种通用片段也算成有人调用，
// 一条 URL 构造器就能让整组前缀背上它得不到的 mock 要求。
func pubGateCalledFromFrontend(frontend, path string) bool {
	segs := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/v1"), "/"), "/")
	skeleton := make([]string, 0, len(segs))
	for _, s := range segs {
		if strings.HasPrefix(s, ":") || s == "*" {
			skeleton = append(skeleton, `[^'"`+"`"+`?#\s/]+`)
			continue
		}
		skeleton = append(skeleton, regexp.QuoteMeta(s))
	}
	join := strings.Join(skeleton, "/")
	return regexp.MustCompile(`['"` + "`" + `]/(?:api/v1/)?` + join).MatchString(frontend)
}

func TestPublicRouteGateRepo(t *testing.T) {
	routes, unresolved := pubGatePluginRoutes(t)
	rules, prefixes := pubGateMockState(t)
	frontend := pubGateFrontendCalls(t)
	if len(routes) == 0 || len(prefixes) == 0 || len(rules) == 0 {
		t.Fatalf("闸对空跑：匿名路由 %d 条、PUBLIC_PREFIXES %d 条、mock 规则 %d 条",
			len(routes), len(prefixes), len(rules))
	}
	pubGateVerifyPageConsumers(t, findRoot(), routes)
	names := make([]string, 0, len(routes))
	byName := map[string]pubGateFound{}
	routeSet := map[string]bool{}
	for _, r := range routes {
		names = append(names, r.full)
		byName[r.full] = r
		routeSet[r.full] = true
	}
	sort.Strings(names)
	// 整页跳转登记也带失效探针：登记的路由不在后端注册里了（改名/删掉/换成受保护路由）就报错。
	for _, r := range pubGateBrowserBinding {
		if !routeSet[r] {
			t.Errorf("「整页跳转」登记已失效（后端没有这条匿名路由了）：%s", r)
		}
	}

	badPrefix, badRule := 0, 0
	notCalled, pageConsumed := 0, 0
	usedExternal := map[string]bool{}
	for _, route := range names {
		method, path, _ := strings.Cut(route, " ")
		trimmed := strings.TrimPrefix(path, "/api/v1")
		if reason, ok := pubGateExemptRoute(path); ok {
			usedExternal[reason] = true
			continue // 外部系统 / 整页跳转：mock 侧不存在是设计如此
		}
		if !pubGateCalledFromFrontend(frontend, path) {
			if pubGateIsPageConsumed(path) {
				pageConsumed++ // 插件自渲染页/挂件直连：admin 的 mock 层不在调用栈上
				continue
			}
			notCalled++ // 后端专用 / IdP 回调 / 服务端渲染的公开页：不要求 mock 侧存在
			continue
		}
		// 骨架能对上引号路径，但它是 window.location.href 里的整页跳转，不是 axios 请求。
		if pubGateIsBrowserBinding(route) {
			pageConsumed++
			continue
		}
		prefixOK := false
		for _, p := range prefixes {
			if trimmed == p || strings.HasPrefix(trimmed, p+"/") {
				prefixOK = true
				break
			}
		}
		if !prefixOK {
			badPrefix++
			t.Errorf("匿名路由契约漂移：no_prefix %s @%s（PUBLIC_PREFIXES 未覆盖：mock 会对匿名请求回 401 信封，并连带清全站查询缓存）",
				route, byName[route].file)
			continue
		}
		if !pubGateHasRule(rules, method, path) {
			badRule++
			t.Errorf("匿名路由契约漂移：no_mock_rule %s @%s（前缀已放行但没有 mock 规则：适配器回「Mock not found」404，调用方的 .catch 会把这块 UI 静默藏掉）",
				route, byName[route].file)
		}
	}
	for _, e := range pubGateBackendOnly {
		if !usedExternal[e.reason] {
			t.Errorf("「外部触发」登记项已失效（没有匿名路由再落在它下面）：%s", e.prefix)
		}
	}
	dead := 0
	for _, p := range prefixes {
		found := false
		for r := range rules {
			i := strings.Index(r, " ")
			if i >= 0 && strings.HasPrefix(r[i+1:], "/api/v1"+p) {
				found = true
				break
			}
		}
		if !found {
			// 挂账中的「开发态还没有规则」也算数：它同时要求这条前缀确实还挂着匿名路由，
			// 否则等于用一笔旧账掩盖一个新漂移。
			if _, ok := pubGateExemptRoute("/api/v1" + p + "/"); ok {
				continue
			}
			// 页面级消费者的前缀本就不该有 mock 规则（调用栈是插件页/挂件的 fetch）。
			if pubGateIsPageConsumed("/api/v1" + p + "/") {
				continue
			}
			dead++
			t.Errorf("匿名路由契约漂移：dead_prefix %s（没有任何 mock 规则以它开头：要么补规则要么删条目）", p)
		}
	}
	t.Logf("静态判到的插件匿名路由 %d 条（前端请求的里面 no_prefix %d、no_mock_rule %d；后端专用不判 %d、页面级消费者 %d）/ mock 规则 %d 条 / 白名单前缀 %d 条（死条目 %d）/ 外部触发登记 %d 条；判不了的注册形态 %d 处",
		len(names), badPrefix, badRule, notCalled, pageConsumed, len(rules), len(prefixes), dead, len(pubGateBackendOnly), len(unresolved))
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		t.Logf("判不了（只登记不判，需要人看时按这份清单）：\n  %s", strings.Join(unresolved, "\n  "))
	}
}
