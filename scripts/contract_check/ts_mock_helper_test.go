package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 这份用例是 createCrudMock 展开的“去重闸门”。
//
// 背景：ts_mock.go 里的 crudRoutes 是把前端 web/src/framework/request/mock.ts 的
// createCrudMock 手工抄写的一份副本，“与 mock.ts 保持一致”那句注释是唯一约束。
// 64ab086 把 PUT {base}/sort 改成只有调用点写了 sortable: true 才注册，扫描侧同步加了
// needSortable + sortablePropRe——两边各维护一份。谁改了 helper（加/删一条路由、或去掉
// sortable 守卫）而没同步扫描器，报告就静默失真，只有真树跑一遍 contract_check 才照得出来。
//
// 这里不再信任那句注释：直接从 helper 源码解析出 createCrudMock 实际注册的路由
// （method + 相对 base 的 suffix + 该路由是否坐在某个 opts.<flag> ? 三元真分支里），
// 断言它和运行时用的 crudRoutes 完全相等。helper 与扫描器的副本就此绑定成一条测试。

// routeKey 是两侧共用的规范形态：方法 + 相对 base 的后缀 + 是否只在 sortable 旗标下注册。
func routeKey(method, suffix string, sortable bool) string {
	return method + " " + suffix + " sortable=" + strconv.FormatBool(sortable)
}

// derivedRoute 是从 helper 源码抽出来的一条契约：suffix 相对 base，guard 是该路由所坐的
// opts.<guard> ? 三元旗标名（无条件时为空串）。扫描器目前只认 sortable 一个旗标。
type derivedRoute struct {
	method string
	suffix string
	guard  string
}

func (r derivedRoute) key() string {
	return routeKey(r.method, r.suffix, r.guard == "sortable")
}

func TestCrudMockScannerMatchesHelper(t *testing.T) {
	src := readFrameworkHelper(t)
	if src == "" {
		// 派生项目可能把框架前端源码裁掉；参照 plugins/edition_test.go 的母版/派生判定，
		// 只在“文件根本不存在”时跳过，绝不把“文件在但内容对不上”当跳过。
		t.Skip("框架 helper 源码缺失（派生项目裁剪）：跳过 helper↔扫描器一致性核对")
	}

	derived := deriveCrudRoutes(t, src)

	// helper 里被守卫的路由只应坐在 opts.sortable ? 下：扫描器只认 sortable 一个旗标，
	// 换名字（或多加一个旗标）就意味着调用点字面量与 needSortable 的对应关系已经断了。
	// 同时要求扫描器那条 sortablePropRe 真能认出这个字面量——只改字段名不改正则，
	// 表现是扫描器少展开一条 /sort，报告静默失真，这里直接把正则钉在 helper 的字段名上。
	for _, flag := range guardFlags(derived) {
		if flag != "sortable" {
			t.Errorf("helper 用 opts.%s 守卫了路由，但扫描器的 needSortable 只认 sortable", flag)
		}
		if !sortablePropRe.MatchString(flag + ": true") {
			t.Errorf("helper 的旗标字段是 %s，可扫描器的 sortablePropRe=%q 认不出 %q 字面量——改名得两边一起同步",
				flag, sortablePropRe.String(), flag+": true")
		}
	}

	got := keySet(derived)
	want := crudRouteKeySet()
	for k := range want {
		if !got[k] {
			t.Errorf("扫描器 crudRoutes 有 %q，但 helper 的 createCrudMock 已不再注册它（scanner 领先 helper，报告虚报）", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("helper 的 createCrudMock 注册了 %q，但扫描器 crudRoutes 里没有它（helper 领先 scanner，报告漏报）", k)
		}
	}
}

// TestCrudMockDriftControls 是上面那条闸门的负控制：对 helper 源码的内存副本施加四种真实漂移，
// 逐一断言抽取结果确实和 crudRoutes 不再相等。任何一条“改了 helper 抽取结果却没变”，
// 说明抽取只是把 crudRoutes 抄回去、闸门形同虚设，这条用例会红。
//
// 不动磁盘上的 web/src/framework/request/mock.ts（那是前端真相源，本仓约定只读）：
// 四种漂移都用 strings.Replace 在内存里造，跑完无需还原。
func TestCrudMockDriftControls(t *testing.T) {
	src := readFrameworkHelper(t)
	if src == "" {
		t.Skip("框架 helper 源码缺失（派生项目裁剪）：跳过漂移负控制")
	}
	baseline := keySet(deriveCrudRoutes(t, src))
	want := crudRouteKeySet()
	if !equalKeySets(baseline, want) {
		t.Fatalf("未改动的 helper 就和 crudRoutes 对不上，负控制失去意义:\n  helper: %v\n  scanner : %v", sortedKeys(baseline), sortedKeys(want))
	}

	for _, c := range []struct {
		name     string
		old, new string
		mustGain string // 漂移后抽取结果应多出的 key
		mustLose string // 漂移后抽取结果应少掉的 key
	}{
		{
			// 漂移一：往 createCrudMock 的 defineMock 数组里加一条路由，不动扫描器。
			name:     "helper 新增 PATCH {base}/status",
			old:      "  ])\n\n  return {",
			new:      "    { method: 'PATCH', url: `${base}/status`, handler: () => null },\n  ])\n\n  return {",
			mustGain: routeKey("PATCH", "/status", false),
		},
		{
			// 漂移二：去掉 sortable 守卫，PUT {base}/sort 变成无条件注册。
			name:     "helper 去掉 opts.sortable 守卫",
			old:      "const sortRules: MockRule[] = opts.sortable ? [{",
			new:      "const sortRules: MockRule[] = [{",
			mustGain: routeKey("PUT", "/sort", false),
			mustLose: routeKey("PUT", "/sort", true),
		},
		{
			// 漂移三：helper 不再注册 DELETE {base}（改挂到 /purge），扫描器却仍留着旧的那条。
			name:     "helper 不再注册 DELETE {base}",
			old:      "    {\n      method: 'DELETE',\n      url: base,\n      handler: ({ query }) => {\n        const ids",
			new:      "    {\n      method: 'DELETE',\n      url: `${base}/purge`,\n      handler: ({ query }) => {\n        const ids",
			mustGain: routeKey("DELETE", "/purge", false),
			mustLose: routeKey("DELETE", "", false),
		},
		{
			// 漂移四：旗标改名（opts.sortable → opts.reorderable）。扫描器只在名字仍是
			// sortable 时展开 /sort，改名后调用点字面量对不上，会少报一条契约。
			name:     "helper 把 sortable 旗标改名",
			old:      "const sortRules: MockRule[] = opts.sortable ? [{",
			new:      "const sortRules: MockRule[] = opts.reorderable ? [{",
			mustGain: routeKey("PUT", "/sort", false),
			mustLose: routeKey("PUT", "/sort", true),
		},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			mutated := strings.Replace(src, c.old, c.new, 1)
			if mutated == src {
				t.Fatalf("负控制的替换没命中，锚点已失效：\n%s", c.old)
			}
			got := keySet(deriveCrudRoutes(t, mutated))
			if equalKeySets(got, want) {
				t.Errorf("helper 已被改成「%s」，抽取结果却仍等于 crudRoutes——漂移照不出来，闸门形同虚设", c.name)
			}
			if c.mustGain != "" {
				if !got[c.mustGain] {
					t.Errorf("抽取结果应多出 %q，实际: %v", c.mustGain, sortedKeys(got))
				}
				if want[c.mustGain] {
					t.Errorf("前提不成立：crudRoutes 本来就有 %q，这条漂移测不出「helper 领先 scanner」", c.mustGain)
				}
			}
			if c.mustLose != "" {
				if got[c.mustLose] {
					t.Errorf("抽取结果不应再有 %q，实际: %v", c.mustLose, sortedKeys(got))
				}
				if !want[c.mustLose] {
					t.Errorf("前提不成立：crudRoutes 本来就没有 %q，这条漂移测不出「scanner 领先 helper」", c.mustLose)
				}
			}
		})
	}
	// 基线条数与 crudRoutes 一致（上面已 Fatalf，这里只是把「抽取确实是 7 条」钉住）。
	if len(baseline) != len(want) {
		t.Errorf("基线条数不等: helper %d vs scanner %d", len(baseline), len(want))
	}
}

// crudRouteKeySet 把运行时用的 crudRoutes 折算成同一套 key，便于与 helper 抽取结果对照。
func crudRouteKeySet() map[string]bool {
	s := map[string]bool{}
	for _, r := range crudRoutes {
		s[routeKey(r.method, r.suffix, r.needSortable)] = true
	}
	return s
}

func keySet(rs []derivedRoute) map[string]bool {
	s := map[string]bool{}
	for _, r := range rs {
		s[r.key()] = true
	}
	return s
}

func equalKeySets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// guardFlags 返回 helper 里实际用到的守卫旗标名（去重、排序）。
func guardFlags(rs []derivedRoute) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rs {
		if r.guard != "" && !seen[r.guard] {
			seen[r.guard] = true
			out = append(out, r.guard)
		}
	}
	sort.Strings(out)
	return out
}

// readFrameworkHelper 读取真实的 web/src/framework/request/mock.ts；文件不存在返回 ""（供跳过）。
func readFrameworkHelper(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRootForTest(t), "web", "src", "framework", "request", "mock.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("读取 %s: %v", path, err)
	}
	// 锚点按 LF 书写；Windows checkout 出 CRLF 时也要能替换命中（.gitattributes 之外再兜一层）。
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// repoRootForTest 从本测试文件位置回溯仓库根：.../scripts/contract_check/ -> .../。
func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("定位测试文件失败")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// deriveCrudRoutes 解析 createCrudMock 函数体，抽出它注册的全部 { method, url } 路由，
// 并标注每条是否坐在 opts.<flag> ? [ ... ] : ... 的三元真分支里。
func deriveCrudRoutes(t *testing.T, src string) []derivedRoute {
	t.Helper()
	tt := newTS(src)
	b := tt.b
	fs, fe := crudMockBodyBounds(t, b)

	d := &crudDeriver{t: t, b: b, fs: fs, fe: fe}

	d.enumerateDefineMocks()

	if len(d.out) == 0 {
		t.Fatal("createCrudMock 里没解析出任何路由，helper 形态可能已重构")
	}
	return d.out
}

// enumerateDefineMocks 展开函数体内**每一个** defineMock([...]) 调用——helper 将来若把路由
// 拆进第二个调用（比如按旗标条件补注册），只读第一个数组会漏看，这里一次扫干净。
func (d *crudDeriver) enumerateDefineMocks() {
	for _, dm := range defineMockRe.FindAllSubmatchIndex(d.b[d.fs:d.fe], -1) {
		ai := skipWS(d.b, d.fs+dm[1]) // defineMockRe 的 dm[1] 已越过 '('，这里直接取数组的 '['
		if ai >= d.fe || d.b[ai] != '[' {
			d.t.Fatal("defineMock 的参数不是数组字面量（helper 形态已变，扫描器需要重新对齐）")
		}
		ae := matchClose(d.b, ai)
		if ae < 0 || ae > d.fe {
			d.t.Fatal("defineMock 的数组括号不配对")
		}
		d.enumerateArray(ai, ae, "")
	}
}

type crudDeriver struct {
	t   *testing.T
	b   []byte
	fs  int // createCrudMock 函数体 '{' 的下标
	fe  int // 配对 '}' 的下标
	out []derivedRoute
}

// enumerateArray 遍历 b[a:e]（一个数组字面量，a 是 '['）的直接元素：对象规则按 guard 记录，
// ...spread 引用则回溯其 const 声明解析守卫后递归展开。
func (d *crudDeriver) enumerateArray(a, e int, guard string) {
	for k := a + 1; k < e; {
		switch c := d.b[k]; {
		case c == '{':
			oe := matchClose(d.b, k)
			if oe < 0 || oe > e {
				d.t.Fatal("defineMock/sortRules 数组里的对象括号不配对")
			}
			d.out = append(d.out, d.ruleObject(k, oe, guard))
			k = oe + 1
		case c == '.' && k+2 < e && d.b[k+1] == '.' && d.b[k+2] == '.':
			name := string(identRe.Find(d.b[k+3:]))
			if name == "" {
				d.t.Fatal("defineMock 数组含无法识别的展开项（不是 ...CONST 形式）")
			}
			k += 3 + len(name)
			d.resolveSpread(name)
		case c == '\'' || c == '"':
			k = skipQuoted(d.b, k, c)
		case c == '`':
			k = skipTemplate(d.b, k)
		default:
			k++
		}
	}
}

// ruleObject 解析 b[s:e] 一个路由对象自身层的 method 与 url，url 里把 base 视为空串求相对后缀。
func (d *crudDeriver) ruleObject(s, e int, guard string) derivedRoute {
	props := depth0(d.b, s+1, e)
	mm := methodPropRe.FindSubmatch(props)
	ul := urlPropRe.FindIndex(props)
	if mm == nil || ul == nil {
		d.t.Fatalf("createCrudMock 展开里的对象缺字面量 method 或 url: %q", snippet(string(props)))
	}
	// base 在 helper 里是 opts 解构出来的局部名，不是字符串常量；这里钉成 "" 以求出相对后缀。
	p := &tsParser{b: props, consts: map[string]string{"base": ""}}
	v := p.parse(ul[1])
	if !v.ok {
		d.t.Fatalf("createCrudMock 的 url 不是可静态解析的字符串: %q", snippet(string(props)))
	}
	if strings.Contains(v.val, ":param") {
		d.t.Fatalf("createCrudMock 的 url 含无法解析的段: %q", snippet(string(props)))
	}
	return derivedRoute{method: strings.ToUpper(string(mm[1])), suffix: v.val, guard: guard}
}

var spreadGuardRe = regexp.MustCompile(`^\s*opts\.([A-Za-z_$][\w$]*)\s*\?`)

// resolveSpread 处理 ...NAME 展开：回溯函数体内 const NAME ... = <初值> 的声明，
// 初值为 opts.<flag> ? [ ... ] 时按该 flag 记录真分支里的路由，为普通 [ ... ] 时无条件记录。
func (d *crudDeriver) resolveSpread(name string) {
	loc := regexp.MustCompile(`(?s)\bconst\s+` + regexp.QuoteMeta(name) + `\b[^=]*=`).FindIndex(d.b[d.fs:d.fe])
	if loc == nil {
		d.t.Fatalf("createCrudMock 展开引用了函数体内未声明的常量 %q", name)
	}
	eq := d.fs + loc[1] // 等号之后的偏移
	if gm := spreadGuardRe.FindSubmatchIndex(d.b[eq:]); gm != nil {
		flag := string(d.b[eq+gm[2] : eq+gm[3]])
		open := skipWS(d.b, eq+gm[1]) // '?' 之后
		if open >= d.fe || d.b[open] != '[' {
			d.t.Fatalf("opts.%s ? 的真分支不是数组字面量", flag)
		}
		close := matchClose(d.b, open)
		if close < 0 || close > d.fe {
			d.t.Fatalf("opts.%s ? 的真分支数组不配对", flag)
		}
		d.enumerateArray(open, close, flag)
		return
	}
	if open := skipWS(d.b, eq); open < d.fe && d.b[open] == '[' {
		close := matchClose(d.b, open)
		if close < 0 || close > d.fe {
			d.t.Fatalf("常量 %q 的数组不配对", name)
		}
		// 没有 opts.<flag> ? 守卫：即“旗标被去掉”，这些路由按无条件展开。
		d.enumerateArray(open, close, "")
		return
	}
	d.t.Fatalf("无法识别常量 %q 的声明形态（既不是 opts.<flag> ? [...] 也不是 [...]）", name)
}

// crudMockBodyBounds 返回 createCrudMock 函数体大括号的绝对下标区间 [open, close]。
func crudMockBodyBounds(t *testing.T, b []byte) (int, int) {
	t.Helper()
	m := regexp.MustCompile(`\bfunction\s+createCrudMock\b`).FindIndex(b)
	if m == nil {
		t.Fatal("mock.ts 里没有 export function createCrudMock")
	}
	i := skipWS(b, skipGeneric(b, skipWS(b, m[1])))
	if i >= len(b) || b[i] != '(' {
		t.Fatal("createCrudMock 的参数列表不规整")
	}
	pe := matchClose(b, i)
	if pe < 0 {
		t.Fatal("createCrudMock 的参数括号不配对")
	}
	open := pe + 1
	for open < len(b) && b[open] != '{' { // 跳过返回类型 : CrudStore<T>
		open++
	}
	if open >= len(b) {
		t.Fatal("createCrudMock 没有函数体")
	}
	close := matchClose(b, open)
	if close < 0 {
		t.Fatal("createCrudMock 的函数体括号不配对")
	}
	return open, close
}

func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
