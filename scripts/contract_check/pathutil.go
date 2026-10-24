package main

import (
	"regexp"
	"strings"
)

var braceParam = regexp.MustCompile(`\{([A-Za-z_][\w]*)(?::[^}]*)?\}`)

// stripQuery 去掉前端 URL 的查询串与片段（后端路径里的 "?" 是“未解析段”标记，不能走这里）。
func stripQuery(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		return p[:i]
	}
	return p
}

// cleanPath 规范化路径：{id} 转 :id、折叠多余斜杠、去尾斜杠、补前导斜杠。
func cleanPath(p string) string {
	p = braceParam.ReplaceAllString(p, ":$1")
	var segs []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return "/" + strings.Join(segs, "/")
}

// joinPath 拼接两段路径并规范化。
func joinPath(a, b string) string {
	if b != "" && !strings.HasPrefix(b, "/") {
		b = "/" + b
	}
	return cleanPath(a + b)
}

// pathKey 是匹配用的键：所有 :name 视为同一占位 ":"。
func pathKey(p string) string {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range segs {
		if strings.HasPrefix(s, ":") {
			segs[i] = ":"
		}
	}
	return "/" + strings.Join(segs, "/")
}

func segments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// 匹配强度。
const (
	matchNone   = 0
	matchWeak   = 1 // 后端占位（:id 或 ?）吞掉了前端的静态段，疑似拼写错误
	matchStrong = 2
)

// matchPath 比较前端路径与后端路径（均已规范化）。后端含 * 通配的路由不参与匹配。
func matchPath(callPath, routePath string) int {
	cs, rs := segments(callPath), segments(routePath)
	if len(cs) != len(rs) {
		return matchNone
	}
	res := matchStrong
	for i := range rs {
		r, c := rs[i], cs[i]
		switch {
		case strings.HasPrefix(r, "*"):
			return matchNone
		case strings.HasPrefix(r, ":") || r == "?":
			if !strings.HasPrefix(c, ":") {
				res = matchWeak
			}
		case r == c:
		default:
			return matchNone
		}
	}
	return res
}

// lev 是编辑距离（按字节，路径都是 ASCII）。
func lev(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func commonPrefixSegs(a, b string) int {
	as, bs := segments(a), segments(b)
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}
