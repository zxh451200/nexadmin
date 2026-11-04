package main

import (
	"fmt"
	"sort"
	"strings"
)

// classify 把前端调用（或 mock）与后端路由逐一对照，返回不一致项（按 METHOD+路径合并）
// 与「静态判不出来」的那一批。
//
// 判不出来的意思是：调用路径里有 ":param"（运行时拼出来的段，如 test-${key}），
// 后端那条路由的对应段是静态字面量，两边谁也不吞谁。把它当成「后端没有这条路由」，
// 这条闸会恒红，反而照不出真的缺路由，所以单列出去，只计数不阻断。
func classify(calls []Call, routes []Route, used []bool, called map[string]bool) ([]Issue, []Issue) {
	byKey := map[string]*Issue{}
	var order []string
	undecidedByKey := map[string]*Issue{}
	var undecided []string
	for _, c := range calls {
		kind, note, hit := classifyOne(c, routes)
		for _, i := range hit {
			used[i] = true
		}
		if kind == "" {
			continue
		}
		key := c.Method + " " + pathKey(c.Path)
		if kind == kindMissing && strings.Contains(c.Path, ":param") {
			is := undecidedByKey[key]
			if is == nil {
				is = &Issue{Method: c.Method, Path: c.Path, Module: c.Module, Kind: kind,
					Severity: kindSeverity[kind], Note: "路径含运行时拼的段，静态判不出是否与某条路由同一条", Suggest: suggest(c, routes, hit)}
				undecidedByKey[key] = is
				undecided = append(undecided, key)
			}
			is.Locs = append(is.Locs, c.Loc)
			continue
		}
		is := byKey[key]
		if is == nil {
			is = &Issue{Method: c.Method, Path: c.Path, Module: c.Module, Kind: kind, Severity: kindSeverity[kind], Note: note,
				Suggest: suggest(c, routes, hit), Called: called[key]}
			byKey[key] = is
			order = append(order, key)
		}
		is.Locs = append(is.Locs, c.Loc)
	}
	out := make([]Issue, 0, len(order))
	for _, k := range order {
		is := byKey[k]
		sort.Slice(is.Locs, func(a, b int) bool {
			if is.Locs[a].File != is.Locs[b].File {
				return is.Locs[a].File < is.Locs[b].File
			}
			return is.Locs[a].Line < is.Locs[b].Line
		})
		out = append(out, *is)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		if a.Called != b.Called {
			return a.Called
		}
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		return a.Path < b.Path
	})
	sort.Strings(undecided)
	und := make([]Issue, 0, len(undecided))
	for _, k := range undecided {
		und = append(und, *undecidedByKey[k])
	}
	return out, und
}

// classifyOne 返回 (问题种类, 说明, 命中的后端路由下标)；种类为空表示对得上。
func classifyOne(c Call, routes []Route) (string, string, []int) {
	var strong, weak, other []int
	for i, r := range routes {
		if strings.Contains(r.Path, "*") {
			continue
		}
		s := matchPath(c.Path, r.Path)
		if s == matchNone {
			continue
		}
		if r.Method != c.Method && r.Method != "ANY" {
			if s == matchStrong {
				other = append(other, i)
			}
			continue
		}
		if s == matchStrong {
			strong = append(strong, i)
		} else {
			weak = append(weak, i)
		}
	}
	switch {
	case len(strong) > 0:
		return "", "", strong
	case len(weak) > 0:
		r := routes[weak[0]]
		return kindWeak, fmt.Sprintf("仅被后端占位吞掉静态段才对上：%s %s（%s），疑似路径拼写错误", r.Method, r.Path, r.Loc), weak
	case len(other) > 0:
		var ms []string
		for _, i := range other {
			ms = append(ms, routes[i].Method)
		}
		return kindMethod, "后端该路径只注册了 " + strings.Join(ms, ",") + "，方法对不上", nil
	}
	return kindMissing, "后端没有注册该路径", nil
}

// suggest 给出最接近的后端路由：同前缀段多、编辑距离小、方法一致者优先。
func suggest(c Call, routes []Route, hit []int) []string {
	if len(hit) > 0 {
		return nil // 弱匹配本身已在说明里指出了对应路由
	}
	type cand struct {
		score int
		text  string
	}
	var cs []cand
	ck := pathKey(c.Path)
	for _, r := range routes {
		if r.Dynamic || strings.Contains(r.Path, "*") {
			continue
		}
		rk := pathKey(r.Path)
		score := lev(ck, rk) - 2*commonPrefixSegs(ck, rk)
		if r.Method != c.Method {
			score += 2
		}
		cs = append(cs, cand{score, fmt.Sprintf("%s %s (%s)", r.Method, r.Path, r.Loc)})
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].score < cs[j].score })
	var out []string
	for i := 0; i < len(cs) && i < 2; i++ {
		out = append(out, cs[i].text)
	}
	return out
}
