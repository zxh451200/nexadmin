package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	xsemver "golang.org/x/mod/semver"
)

// ---- go.mod 合并（merge_paths）----
//
// 规则：module 行以当前工程为准；go 版本取较高者；require 新包里有而当前没有的加入，
// 两边都有时取较高 semver，当前独有的（二开自己加的依赖）原样保留；replace/exclude 以当前工程为准，
// 仅补入新包里"当前没有同名 old"的模块型 replace（框架必需）。// indirect 标记不强求，升级后请 go mod tidy。

type goVer struct {
	n   [3]int
	pre string
}

func parseGoVer(s string) goVer {
	s = strings.TrimPrefix(strings.TrimSpace(s), "go")
	var v goVer
	parts := strings.SplitN(s, ".", 3)
	for i, p := range parts {
		j := 0
		for j < len(p) && p[j] >= '0' && p[j] <= '9' {
			j++
		}
		v.n[i], _ = strconv.Atoi(p[:j])
		if j < len(p) { // 如 1.25rc1：后缀视为预发布
			v.pre = p[j:]
		}
	}
	return v
}

// goVerLess 报告 a < b（预发布低于同号正式版）。
func goVerLess(a, b string) bool {
	x, y := parseGoVer(a), parseGoVer(b)
	for i := range x.n {
		if x.n[i] != y.n[i] {
			return x.n[i] < y.n[i]
		}
	}
	switch {
	case x.pre == y.pre:
		return false
	case x.pre == "":
		return false
	case y.pre == "":
		return true
	}
	return x.pre < y.pre
}

// mergeGoMod 返回合并后的内容、清单行、是否有变化；无变化时内容原样返回（不重新排版）。
func mergeGoMod(cur, nw []byte) ([]byte, []string, bool, error) {
	cf, err := modfile.Parse("go.mod(当前工程)", cur, nil)
	if err != nil {
		return nil, nil, false, err
	}
	nf, err := modfile.Parse("go.mod(新包)", nw, nil)
	if err != nil {
		return nil, nil, false, err
	}
	var rep []string
	changed := false
	if nf.Go != nil && (cf.Go == nil || goVerLess(cf.Go.Version, nf.Go.Version)) {
		old := "(无)"
		if cf.Go != nil {
			old = cf.Go.Version
		}
		if err := cf.AddGoStmt(nf.Go.Version); err != nil {
			return nil, nil, false, err
		}
		rep = append(rep, fmt.Sprintf("↑ go %s -> %s（取较高者）", old, nf.Go.Version))
		changed = true
	}
	if nf.Toolchain != nil && cf.Toolchain != nil && goVerLess(cf.Toolchain.Name, nf.Toolchain.Name) {
		if err := cf.AddToolchainStmt(nf.Toolchain.Name); err != nil {
			return nil, nil, false, err
		}
		rep = append(rep, fmt.Sprintf("↑ toolchain %s -> %s（取较高者）", cf.Toolchain.Name, nf.Toolchain.Name))
		changed = true
	}
	curReq := map[string]*modfile.Require{}
	for _, r := range cf.Require {
		curReq[r.Mod.Path] = r
	}
	nreq := append([]*modfile.Require(nil), nf.Require...)
	sort.Slice(nreq, func(i, j int) bool { return nreq[i].Mod.Path < nreq[j].Mod.Path })
	inNew := map[string]bool{}
	for _, r := range nreq {
		inNew[r.Mod.Path] = true
		c, ok := curReq[r.Mod.Path]
		switch {
		case !ok:
			cf.AddNewRequire(r.Mod.Path, r.Mod.Version, r.Indirect)
			rep = append(rep, fmt.Sprintf("+ require %s %s（框架新增）", r.Mod.Path, r.Mod.Version))
			changed = true
		case c.Mod.Version != r.Mod.Version && xsemver.Compare(r.Mod.Version, c.Mod.Version) > 0:
			oldVer := c.Mod.Version                                          // AddRequire 会原地改 c，先留旧值
			if err := cf.AddRequire(r.Mod.Path, r.Mod.Version); err != nil { // 原地改版本，保留注释
				return nil, nil, false, err
			}
			rep = append(rep, fmt.Sprintf("↑ require %s %s -> %s（取较高）", r.Mod.Path, oldVer, r.Mod.Version))
			changed = true
		}
	}
	return finishGoMod(cf, nf, cur, inNew, rep, changed)
}

// finishGoMod 处理 replace、汇总"保留的二开独有依赖"并输出；无变化时返回原始内容。
func finishGoMod(cf, nf *modfile.File, cur []byte, inNew map[string]bool, rep []string, changed bool) ([]byte, []string, bool, error) {
	curRep := map[string]bool{}
	for _, r := range cf.Replace {
		curRep[r.Old.Path] = true
	}
	for _, r := range nf.Replace {
		if curRep[r.Old.Path] {
			continue // 以当前工程为准
		}
		if r.New.Version == "" {
			rep = append(rep, fmt.Sprintf("! 忽略新包里指向本地路径的 replace %s => %s", r.Old.Path, r.New.Path))
			continue
		}
		if err := cf.AddReplace(r.Old.Path, r.Old.Version, r.New.Path, r.New.Version); err != nil {
			return nil, nil, false, err
		}
		rep = append(rep, fmt.Sprintf("+ replace %s => %s %s（框架必需）", r.Old.Path, r.New.Path, r.New.Version))
		changed = true
	}
	if !changed {
		return cur, nil, false, nil
	}
	var kept []string
	indirect := 0
	for _, r := range cf.Require {
		if inNew[r.Mod.Path] {
			continue
		}
		if r.Indirect {
			indirect++
		} else {
			kept = append(kept, r.Mod.Path+" "+r.Mod.Version)
		}
	}
	sort.Strings(kept)
	for _, k := range kept {
		rep = append(rep, "= 保留二开独有依赖 "+k)
	}
	if indirect > 0 {
		rep = append(rep, fmt.Sprintf("= 另有 %d 个二开独有的间接依赖原样保留", indirect))
	}
	cf.Cleanup()
	out, err := cf.Format()
	if err != nil {
		return nil, nil, false, err
	}
	return out, rep, true, nil
}

// ---- go.sum 合并：两边行的并集（去重、按模块路径+semver 排序）----

func sumFields(l string) (path, ver string) {
	f := strings.Fields(l)
	if len(f) < 2 {
		return l, ""
	}
	return f[0], f[1]
}

func sumLess(a, b string) bool {
	pa, va := sumFields(a)
	pb, vb := sumFields(b)
	if pa != pb {
		return pa < pb
	}
	ba, bb := strings.TrimSuffix(va, "/go.mod"), strings.TrimSuffix(vb, "/go.mod")
	if ba != bb {
		if c := xsemver.Compare(ba, bb); c != 0 {
			return c < 0
		}
		return ba < bb
	}
	if va != vb { // 同版本：源码哈希行在 /go.mod 行之前
		return !strings.HasSuffix(va, "/go.mod")
	}
	return a < b
}

func sumLines(b []byte) []string {
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// mergeGoSum 只在新包有当前没有的行时才改写文件。
func mergeGoSum(cur, nw []byte) ([]byte, []string, bool, error) {
	have := map[string]bool{}
	hash := map[string]string{} // "mod ver" -> 哈希，用来发现同键不同哈希的冲突
	all := sumLines(cur)
	for _, l := range all {
		have[l] = true
		if f := strings.Fields(l); len(f) >= 3 {
			hash[f[0]+" "+f[1]] = f[2]
		}
	}
	added := 0
	var rep []string
	for _, l := range sumLines(nw) {
		if have[l] {
			continue
		}
		have[l] = true
		added++
		all = append(all, l)
		if f := strings.Fields(l); len(f) >= 3 {
			if h, ok := hash[f[0]+" "+f[1]]; ok && h != f[2] {
				rep = append(rep, fmt.Sprintf("! go.sum 冲突：%s %s 两边哈希不同，两行都保留，请核对", f[0], f[1]))
			}
		}
	}
	if added == 0 {
		return cur, nil, false, nil
	}
	sort.SliceStable(all, func(i, j int) bool { return sumLess(all[i], all[j]) })
	rep = append([]string{fmt.Sprintf("+ %d 行（取并集，去重排序，共 %d 行）", added, len(all))}, rep...)
	return []byte(strings.Join(all, "\n") + "\n"), rep, true, nil
}
