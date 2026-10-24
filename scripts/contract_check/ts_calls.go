package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type tsFile struct {
	rel    string // 相对仓库根，如 web/src/framework/modules/user/api.ts
	t      *tsText
	consts map[string]string
}

func (f *tsFile) parser() *tsParser { return &tsParser{b: f.t.b, consts: f.consts} }

// loadFront 读取 web/src 下的 .ts/.tsx（排除 .d.ts 与测试文件）。
func loadFront(root string) ([]*tsFile, error) {
	base := filepath.Join(root, "web", "src")
	if _, err := os.Stat(base); err != nil {
		return nil, err
	}
	var out []*tsFile
	err := filepath.WalkDir(base, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if de.IsDir() {
			if n := de.Name(); n == "node_modules" || n == "__tests__" {
				return filepath.SkipDir
			}
			return nil
		}
		n := de.Name()
		if !(strings.HasSuffix(n, ".ts") || strings.HasSuffix(n, ".tsx")) || strings.HasSuffix(n, ".d.ts") ||
			strings.Contains(n, ".test.") || strings.Contains(n, ".spec.") {
			return nil
		}
		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		t := newTS(string(src))
		out = append(out, &tsFile{rel: filepath.ToSlash(rel), t: t, consts: collectTSConsts(t)})
		return nil
	})
	return out, err
}

// frontModule 把前端文件路径归到模块名。
func frontModule(rel string) string {
	r := strings.TrimPrefix(rel, "web/src/")
	parts := strings.Split(r, "/")
	switch {
	case len(parts) > 3 && parts[0] == "framework" && parts[1] == "modules":
		return parts[2]
	case len(parts) > 2 && parts[0] == "plugins":
		return "plugins/" + parts[1]
	case len(parts) > 2 && parts[0] == "framework":
		return "framework/" + parts[1]
	}
	return "src"
}

var (
	callRe   = regexp.MustCompile(`\b(api|http|apiClient|httpClient|axios)\s*\.\s*(get|post|put|patch|delete|del|head|getBlob|postBlob)\b`)
	fetchRe  = regexp.MustCompile(`\bfetch\s*\(`)
	fetchMth = regexp.MustCompile(`\bmethod\s*:\s*['"]([A-Za-z]+)['"]`)
	urlAttr  = regexp.MustCompile("\\burl=(?:\"(/[^\"]*)\"|\\{\\s*(?:'(/[^']*)'|`(/[^`]*)`)\\s*\\})")
	tagRe    = regexp.MustCompile(`<([A-Z][A-Za-z0-9_.]*)`)
	tmplVar  = regexp.MustCompile(`\$\{[^}]*\}`)
	httpVerb = map[string]string{"get": "GET", "post": "POST", "put": "PUT", "patch": "PATCH", "delete": "DELETE",
		"del": "DELETE", "head": "HEAD", "getBlob": "GET", "postBlob": "POST"}
)

// toAPIPath 把前端相对 baseURL 的路径变成完整 /api/v1 路径。
func toAPIPath(v string) string {
	v = cleanPath(stripQuery(v))
	if v == apiPrefix || strings.HasPrefix(v, apiPrefix+"/") {
		return v
	}
	return joinPath(apiPrefix, v)
}

// frontCalls 抽取前端接口调用：api/http.xxx(...)、fetch(...)、<RemoteSelect url=...>、上传固定路径。
func frontCalls(files []*tsFile) (calls []Call, unres []Unresolved) {
	seen := map[string]bool{}
	add := func(f *tsFile, off int, method, p, kind string) {
		c := Call{Method: method, Path: toAPIPath(p), Module: frontModule(f.rel), Kind: kind, Loc: Loc{File: f.rel, Line: f.t.line(off)}}
		k := c.String() + method + c.Path
		if !seen[k] {
			seen[k] = true
			calls = append(calls, c)
		}
	}
	bad := func(f *tsFile, off int, why string) {
		end := min(off+70, len(f.t.b))
		txt := strings.Join(strings.Fields(string(f.t.b[off:end])), " ")
		unres = append(unres, Unresolved{Side: "frontend", Reason: why, Text: txt, Loc: Loc{File: f.rel, Line: f.t.line(off)}})
	}
	for _, f := range files {
		if strings.HasSuffix(f.rel, ".mock.ts") {
			continue
		}
		b, p := f.t.b, f.parser()
		for _, m := range callRe.FindAllSubmatchIndex(b, -1) {
			if f.t.inStr(m[0]) || m[0] > 0 && b[m[0]-1] == '.' {
				continue
			}
			j := skipWS(b, skipGeneric(b, skipWS(b, m[1])))
			if j >= len(b) || b[j] != '(' {
				continue
			}
			e := p.parse(j + 1)
			if !e.ok || dynHead(e.val) {
				if strings.HasSuffix(f.rel, "framework/request/http.ts") {
					continue // api 封装自身的透传调用
				}
				bad(f, m[0], "第一个参数不是静态字符串（动态 URL，需人工核对）")
				continue
			}
			add(f, m[0], httpVerb[string(b[m[4]:m[5]])], e.val, "api")
		}
		f.fetches(add, bad)
		f.remotes(add)
		f.uploads(add)
	}
	return calls, unres
}
