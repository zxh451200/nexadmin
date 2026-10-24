package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

var kindLabel = map[string]string{kindMissing: "后端无此路径", kindMethod: "方法不符", kindWeak: "疑似拼写"}

// writeText 输出文本报告：A / B 按严重程度排序后按模块分组；C 仅汇总提示。
func writeText(w io.Writer, rep *Report) {
	s := rep.Summary
	fmt.Fprintln(w, "== NexAdmin 前后端契约静态核对 ==")
	fmt.Fprintf(w, "后端路由 %d 条（其中动态/通配 %d 条未参与核对，权限码 %d 个）；前端调用 %d 个唯一接口（%d 处）；前端 mock 契约 %d 条\n",
		s.BackendRoutes, s.BackendDynamic, s.Perms, s.FrontCalls, s.FrontCallSites, s.MockRoutes)
	fmt.Fprintf(w, "A 前端调用了但后端没有: %d | B 前端 mock 有但后端没有: %d | C 后端有但前端从未调用: %d | 无法解析: %d\n\n", s.A, s.B, s.C, s.Unresolved)
	writeIssues(w, "A) 前端调用了但后端没有注册（联调必挂，最重要；按严重程度排序）", rep.A, false)
	writeIssues(w, "B) 前端 mock 有但后端没有（mock 契约领先于后端）", rep.B, true)
	fmt.Fprintf(w, "C) 后端有路由但前端从未调用（仅提示，不是错误）: %d 条\n", len(rep.C))
	cur := ""
	for _, r := range rep.C {
		if r.Module != cur {
			cur = r.Module
			fmt.Fprintf(w, "  [%s]\n", cur)
		}
		fmt.Fprintf(w, "    %-6s %s  (%s)\n", r.Method, r.Path, r.Loc)
	}
	fmt.Fprintln(w)
	writeFields(w, rep.Fields)
	fmt.Fprintf(w, "\n无法静态解析的条目（需人工核对）: %d\n", len(rep.Unresolved))
	us := append([]Unresolved{}, rep.Unresolved...)
	sort.SliceStable(us, func(i, j int) bool {
		if us[i].Side != us[j].Side {
			return us[i].Side < us[j].Side
		}
		if us[i].File != us[j].File {
			return us[i].File < us[j].File
		}
		return us[i].Line < us[j].Line
	})
	for _, u := range us {
		fmt.Fprintf(w, "  [%s] %s  %s\n      %s\n", u.Side, u.Loc, u.Text, u.Reason)
	}
}

func writeIssues(w io.Writer, title string, list []Issue, isB bool) {
	fmt.Fprintf(w, "%s: %d 项\n", title, len(list))
	// 模块按“最严重的一项、条目数多、名字”排序
	type grp struct {
		name  string
		worst int
		items []Issue
	}
	idx := map[string]*grp{}
	var gs []*grp
	for _, is := range list {
		g := idx[is.Module]
		if g == nil {
			g = &grp{name: is.Module, worst: is.Severity}
			idx[is.Module] = g
			gs = append(gs, g)
		}
		g.worst = min(g.worst, is.Severity)
		g.items = append(g.items, is)
	}
	sort.SliceStable(gs, func(i, j int) bool {
		if gs[i].worst != gs[j].worst {
			return gs[i].worst < gs[j].worst
		}
		if len(gs[i].items) != len(gs[j].items) {
			return len(gs[i].items) > len(gs[j].items)
		}
		return gs[i].name < gs[j].name
	})
	for _, g := range gs {
		fmt.Fprintf(w, "  [%s] %d 项\n", g.name, len(g.items))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, is := range g.items {
			fmt.Fprintf(tw, "    %s\t%s\t%s\t%s\t%s\n", kindLabel[is.Kind], is.Method, is.Path, locList(is.Locs), calledMark(is, isB))
		}
		_ = tw.Flush()
		for _, is := range g.items {
			if is.Note != "" && is.Kind != kindMissing {
				fmt.Fprintf(w, "      · %s %s: %s\n", is.Method, is.Path, is.Note)
			}
			for _, sg := range is.Suggest {
				fmt.Fprintf(w, "      · %s %s 最接近: %s\n", is.Method, is.Path, sg)
			}
		}
	}
	fmt.Fprintln(w)
}

func calledMark(is Issue, isB bool) string {
	switch {
	case !isB:
		return ""
	case is.Called:
		return "(前端代码也在调用)"
	}
	return "(前端未调用，多为 createCrudMock 自带路由)"
}

func locList(ls []Loc) string {
	var ss []string
	for i, l := range ls {
		if i == 3 {
			ss = append(ss, fmt.Sprintf("等 %d 处", len(ls)))
			break
		}
		ss = append(ss, l.String())
	}
	return strings.Join(ss, " ")
}

func writeFields(w io.Writer, f FieldReport) {
	fmt.Fprintf(w, "字段级核对（前端 api.ts 的 interface 对 后端 dto.go/model.go 的 json tag）: 已配对 %d 个，前端有而后端无 %d 个字段\n", f.Paired, len(f.Missing))
	for _, m := range f.Missing {
		opt := ""
		if m.Optional {
			opt = "?"
		}
		fmt.Fprintf(w, "  %s  %s.%s%s  -> 后端 %s (%s)\n", m.Loc, m.Interface, m.Field, opt, m.Struct, m.StructAt)
	}
	fmt.Fprintf(w, "跳过 %d 个 interface（不确定即跳过）:\n", f.Skipped)
	for _, k := range sortedKeys(f.SkipReasons) {
		fmt.Fprintf(w, "  %4d  %s\n", f.SkipReasons[k], k)
	}
}
