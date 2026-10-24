package main

import (
	"regexp"
	"strings"
)

type tsField struct {
	name     string
	optional bool
	line     int
}

type tsIface struct {
	name    string
	extends []string
	fields  []tsField
	file    string
	line    int
}

var (
	ifaceRe = regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?interface\s+([A-Za-z_$][\w$]*)\s*(?:<[^{]*?>)?\s*(?:extends\s+([^{]+?))?\s*\{`)
	fieldRe = regexp.MustCompile(`(?m)(?:^|[;,])\s*(?:readonly\s+)?([A-Za-z_$][\w$]*|'[^'\n]+'|"[^"\n]+")(\?)?\s*:`)
)

// parseIfaces 解析一个文件里的 interface（只取自身这一层的属性名）。
func parseIfaces(f *tsFile) []tsIface {
	var out []tsIface
	b := f.t.b
	for _, m := range ifaceRe.FindAllSubmatchIndex(b, -1) {
		open := m[1] - 1
		end := matchClose(b, open)
		if end < 0 {
			continue
		}
		it := tsIface{name: string(b[m[2]:m[3]]), file: f.rel, line: f.t.line(m[2])}
		if m[4] >= 0 {
			for _, e := range strings.Split(string(b[m[4]:m[5]]), ",") {
				if e = strings.TrimSpace(e); e != "" {
					it.extends = append(it.extends, e)
				}
			}
		}
		body := depth0(b, open+1, end)
		for _, fm := range fieldRe.FindAllSubmatchIndex(body, -1) {
			name := strings.Trim(string(body[fm[2]:fm[3]]), `'"`)
			it.fields = append(it.fields, tsField{name: name, optional: fm[4] >= 0, line: f.t.line(open + 1 + fm[2])})
		}
		out = append(out, it)
	}
	return out
}

// moduleKey 把 modules/<dir>/api.ts 或 plugins/<id>/api.ts 映射到配对键（小写、去 - 和 _）；不是模块 api.ts 返回空。
func moduleKey(rel string) string {
	r := strings.TrimPrefix(rel, "web/src/")
	parts := strings.Split(r, "/")
	if parts[len(parts)-1] != "api.ts" {
		return ""
	}
	switch {
	case len(parts) == 4 && parts[0] == "framework" && parts[1] == "modules":
		return normName(parts[2])
	case len(parts) == 3 && parts[0] == "plugins":
		return normName(parts[1])
	}
	return ""
}

func normName(s string) string {
	return strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(s))
}

var ifaceSkip = []string{"Input", "Query", "Params", "Body", "Payload", "Request", "Filters", "Form", "Options", "Props"}
var ifaceTrim = []string{"row", "item", "info", "dto", "vo", "resp", "response", "record"}

// ifaceNorm 是 interface / struct 名的配对键：去 Sys 前缀与 Row/Item 等后缀，转小写。
func ifaceNorm(s string) string {
	s = strings.TrimPrefix(s, "Sys")
	l := strings.ToLower(s)
	for _, suf := range ifaceTrim {
		if strings.HasSuffix(l, suf) && len(l) > len(suf) {
			return strings.TrimSuffix(l, suf)
		}
	}
	return l
}

func isRequestType(name string) bool {
	for _, s := range ifaceSkip {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}
