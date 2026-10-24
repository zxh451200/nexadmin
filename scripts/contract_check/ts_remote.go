package main

import (
	"strings"
)

type (
	addFn func(f *tsFile, off int, method, p, kind string)
	badFn func(f *tsFile, off int, why string)
)

// dynHead 判断路径以动态段开头（如 ${base}/x 里 base 解析不出来），这种不能当作 API 路径。
func dynHead(v string) bool { return strings.HasPrefix(strings.TrimPrefix(v, "/"), ":") }

// fetches 处理原生 fetch(url, {method})：只收相对路径或 /api/v1 路径，外链忽略。
func (f *tsFile) fetches(add addFn, bad badFn) {
	b, p := f.t.b, f.parser()
	for _, m := range fetchRe.FindAllIndex(b, -1) {
		if f.t.inStr(m[0]) {
			continue
		}
		open := m[1] - 1
		e := p.parse(open + 1)
		if !e.ok {
			bad(f, m[0], "fetch 的第一个参数不是静态字符串（需人工核对）")
			continue
		}
		if strings.HasPrefix(e.val, "http") || dynHead(e.val) {
			continue
		}
		method := "GET"
		if end := matchClose(b, open); end > 0 {
			if mm := fetchMth.FindSubmatch(b[open:end]); mm != nil {
				method = strings.ToUpper(string(mm[1]))
			}
		}
		add(f, m[0], method, e.val, "fetch")
	}
}

// remotes 处理 JSX 属性 url="/xxx"：RemoteSelect / TreeSelect 是 GET，TestConnection 是 POST。
func (f *tsFile) remotes(add addFn) {
	b := f.t.b
	for _, m := range urlAttr.FindAllSubmatchIndex(b, -1) {
		if f.t.inStr(m[0]) {
			continue
		}
		v := ""
		for g := 1; g <= 3; g++ {
			if m[2*g] >= 0 {
				v = string(b[m[2*g]:m[2*g+1]])
			}
		}
		v = tmplVar.ReplaceAllString(v, ":param")
		method := "GET"
		win := b[max(0, m[0]-800):m[0]]
		if tags := tagRe.FindAllSubmatch(win, -1); len(tags) > 0 && string(tags[len(tags)-1][1]) == "TestConnection" {
			method = "POST"
		}
		add(f, m[0], method, v, "remote")
	}
}

// uploads 处理上传客户端里的固定端点常量：tus 创建 POST，以及后续的 HEAD / PATCH / DELETE /:id。
func (f *tsFile) uploads(add addFn) {
	if !strings.Contains(f.rel, "uploadClient") {
		return
	}
	for name, val := range f.consts {
		if !strings.Contains(strings.ToUpper(name), "ENDPOINT") || val == "" {
			continue
		}
		off := strings.Index(string(f.t.b), "const "+name)
		if off < 0 {
			off = 0
		}
		add(f, off, "POST", val, "upload")
		for _, m := range []string{"HEAD", "PATCH", "DELETE"} {
			add(f, off, m, strings.TrimRight(val, "/")+"/:id", "upload")
		}
	}
}
