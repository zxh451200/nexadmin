package main

import (
	"regexp"
	"strings"
)

var (
	defineMockRe = regexp.MustCompile(`\bdefineMock\s*\(`)
	crudMockRe   = regexp.MustCompile(`\bcreateCrudMock\b`)
	methodPropRe = regexp.MustCompile(`\bmethod\s*:\s*['"]([A-Za-z]+)['"]`)
	urlPropRe    = regexp.MustCompile(`\burl\s*:\s*`)
	basePropRe   = regexp.MustCompile(`\bbase\s*:\s*`)
)

// crudRoutes 是 createCrudMock 展开的路由，与 web/src/framework/request/mock.ts 保持一致。
var crudRoutes = []struct{ method, suffix string }{
	{"GET", ""}, {"PUT", "/sort"}, {"GET", "/:id"}, {"POST", ""},
	{"PUT", "/:id"}, {"DELETE", "/:id"}, {"DELETE", ""},
}

// frontMocks 抽取 *.mock.ts 声明的“前端 mock 契约”：defineMock 的每个元素与 createCrudMock 的展开。
func frontMocks(files []*tsFile) (mocks []Call, unres []Unresolved) {
	for _, f := range files {
		if !strings.HasSuffix(f.rel, ".mock.ts") {
			continue
		}
		b := f.t.b
		add := func(off int, method, p string) {
			mocks = append(mocks, Call{Method: method, Path: toAPIPath(p), Module: frontModule(f.rel), Kind: "mock",
				Loc: Loc{File: f.rel, Line: f.t.line(off)}})
		}
		bad := func(off int, why string) {
			end := min(off+70, len(b))
			unres = append(unres, Unresolved{Side: "mock", Reason: why, Text: strings.Join(strings.Fields(string(b[off:end])), " "),
				Loc: Loc{File: f.rel, Line: f.t.line(off)}})
		}
		for _, m := range defineMockRe.FindAllIndex(b, -1) {
			if f.t.inStr(m[0]) {
				continue
			}
			i := skipWS(b, m[1])
			if i >= len(b) || b[i] != '[' {
				bad(m[0], "defineMock 的参数不是数组字面量，未抽取")
				continue
			}
			end := matchClose(b, i)
			for k := i + 1; end > 0 && k < end; {
				switch c := b[k]; {
				case c == '{':
					e := matchClose(b, k)
					if e < 0 || e > end {
						k = end
						continue
					}
					f.mockElem(k, e, add, bad)
					k = e + 1
				case c == '.' && k+2 < end && b[k+1] == '.' && b[k+2] == '.':
					bad(k, "defineMock 数组含展开项，未抽取")
					k += 3
				case c == '\'' || c == '"':
					k = skipQuoted(b, k, c)
				case c == '`':
					k = skipTemplate(b, k)
				default:
					k++
				}
			}
		}
		for _, m := range crudMockRe.FindAllIndex(b, -1) {
			if f.t.inStr(m[0]) {
				continue
			}
			i := skipWS(b, skipGeneric(b, skipWS(b, m[1])))
			if i >= len(b) || b[i] != '(' {
				continue // import 语句或类型引用
			}
			j := skipWS(b, i+1)
			if j >= len(b) || b[j] != '{' {
				bad(m[0], "createCrudMock 的参数不是对象字面量，未展开")
				continue
			}
			props := depth0(b, j+1, max(j+1, matchClose(b, j)))
			loc := basePropRe.FindIndex(props)
			if loc == nil {
				bad(m[0], "createCrudMock 没有字面量 base，未展开")
				continue
			}
			e := (&tsParser{b: props, consts: f.consts}).parse(loc[1])
			if !e.ok {
				bad(m[0], "createCrudMock 的 base 不是静态字符串，未展开")
				continue
			}
			for _, r := range crudRoutes {
				add(m[0], r.method, strings.TrimRight(e.val, "/")+r.suffix)
			}
		}
	}
	return mocks, unres
}

// mockElem 解析 defineMock 数组里的一个 { method, url, handler } 对象（只看对象自身这一层的属性）。
func (f *tsFile) mockElem(s, e int, add func(int, string, string), bad func(int, string)) {
	props := depth0(f.t.b, s+1, e)
	mm := methodPropRe.FindSubmatch(props)
	ul := urlPropRe.FindIndex(props)
	if mm == nil && ul == nil {
		return // 不是路由对象
	}
	if mm == nil || ul == nil {
		bad(s, "mock 条目缺少字面量 method 或 url，未抽取")
		return
	}
	v := (&tsParser{b: props, consts: f.consts}).parse(ul[1])
	if !v.ok || dynHead(v.val) {
		bad(s, "mock 条目的 url 不是静态字符串，未抽取")
		return
	}
	add(s, strings.ToUpper(string(mm[1])), v.val)
}
