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
	// 这两块访客自助页在 mock 开发态还没有规则（生产由 d.Public 提供，接口本身是好的）。
	// 已写进 docs/功能与权限清单.md 的已知边界；补上 mock 规则后这两条登记会变「失效」而报错，
	// 逼着改动的人把它删掉——不是永久豁免，是一笔挂账。
	{"/api/v1/plugins/ticket/visitor/", "开发态访客建单/查单页尚无 mock 规则（生产走 d.Public），已记入 docs 已知边界"},
	{"/api/v1/plugins/im/visitor/", "开发态在线客服访客窗尚无 mock 规则（生产走 d.Public），已记入 docs 已知边界"},
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
func pubGateFrontendCalls(t *testing.T) string {
	t.Helper()
	root := findRoot()
	var b strings.Builder
	err := filepath.Walk(filepath.Join(root, "web", "src"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return err
		}
		if info.IsDir() || !(strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx")) {
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

// pubGateCalledFromFrontend 取路由里最长的静态片段去前端源里找（:id 之前那段）。
func pubGateCalledFromFrontend(frontend, path string) bool {
	trimmed := strings.TrimPrefix(path, "/api/v1")
	segs := strings.Split(strings.Trim(trimmed, "/"), "/")
	for n := len(segs); n > 0; n-- {
		if strings.Contains(segs[n-1], ":") {
			continue
		}
		probe := strings.Join(segs[:n], "/")
		if len(probe) > 12 && strings.Contains(frontend, probe) {
			return true
		}
	}
	return false
}

func TestPublicRouteGateRepo(t *testing.T) {
	routes, unresolved := pubGatePluginRoutes(t)
	rules, prefixes := pubGateMockState(t)
	frontend := pubGateFrontendCalls(t)
	if len(routes) == 0 || len(prefixes) == 0 || len(rules) == 0 {
		t.Fatalf("闸对空跑：匿名路由 %d 条、PUBLIC_PREFIXES %d 条、mock 规则 %d 条",
			len(routes), len(prefixes), len(rules))
	}
	names := make([]string, 0, len(routes))
	byName := map[string]pubGateFound{}
	for _, r := range routes {
		names = append(names, r.full)
		byName[r.full] = r
	}
	sort.Strings(names)

	badPrefix, badRule := 0, 0
	notCalled := 0
	usedExternal := map[string]bool{}
	for _, route := range names {
		method, path, _ := strings.Cut(route, " ")
		trimmed := strings.TrimPrefix(path, "/api/v1")
		if reason, ok := pubGateExemptRoute(path); ok {
			usedExternal[reason] = true
			continue // 外部系统 / 整页跳转：mock 侧不存在是设计如此
		}
		if !pubGateCalledFromFrontend(frontend, path) {
			notCalled++ // 后端专用 / IdP 回调 / 服务端渲染的公开页：不要求 mock 侧存在
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
			dead++
			t.Errorf("匿名路由契约漂移：dead_prefix %s（没有任何 mock 规则以它开头：要么补规则要么删条目）", p)
		}
	}
	t.Logf("静态判到的插件匿名路由 %d 条（前端请求的里面 no_prefix %d、no_mock_rule %d；后端专用不判 %d）/ mock 规则 %d 条 / 白名单前缀 %d 条（死条目 %d）/ 外部触发登记 %d 条；判不了的注册形态 %d 处",
		len(names), badPrefix, badRule, notCalled, len(rules), len(prefixes), dead, len(pubGateBackendOnly), len(unresolved))
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		t.Logf("判不了（只登记不判，需要人看时按这份清单）：\n  %s", strings.Join(unresolved, "\n  "))
	}
}
