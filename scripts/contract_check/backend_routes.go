package main

import (
	"go/ast"
	"go/token"
	"path"
	"sort"
	"strings"
)

// route 处理一次 X.GET/POST/.../Any/Handle(path, ...) 调用。
func (w *walker) route(call *ast.CallExpr, sc scope) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	method, isVerb := routeVerbs[sel.Sel.Name]
	args := call.Args
	if sel.Sel.Name == "Handle" && len(args) >= 3 {
		m, _ := w.ct.pathOf(w.gf, args[0])
		method, isVerb, args = strings.ToUpper(m), true, args[1:]
	}
	if !isVerb || len(args) < 1 {
		return
	}
	base, ok := w.router(sel.X, sc)
	if !ok {
		// 看起来像注册路由（字面量路径 + 处理器），但接收者不是已知分组：不能漏，列入无法解析。
		if lit, isLit := args[0].(*ast.BasicLit); isLit && lit.Kind == token.STRING && len(args) >= 2 && (strings.HasPrefix(lit.Value, `"/`) || lit.Value == `""`) {
			w.out.Unres = append(w.out.Unres, Unresolved{Side: "backend", Reason: "路由接收者不是可识别的分组，未纳入核对",
				Text: sel.Sel.Name + "(" + lit.Value + ")", Loc: w.loc(call.Pos())})
		}
		return
	}
	rel, resolved := w.ct.pathOf(w.gf, args[0])
	full := joinPath(base, rel)
	rt := Route{Method: method, Path: full, Module: backendModule(w.gf.rel), Loc: w.loc(call.Pos())}
	switch {
	case !resolved || strings.Contains(full, "?"):
		rt.Dynamic = true
		w.out.Unres = append(w.out.Unres, Unresolved{Side: "backend", Reason: "动态拼接路径（? 为无法静态解析的段），不参与核对",
			Text: method + " " + full, Loc: rt.Loc})
	case strings.Contains(full, "*"):
		rt.Dynamic = true
		w.out.Unres = append(w.out.Unres, Unresolved{Side: "backend", Reason: "通配路由，不参与核对",
			Text: method + " " + full, Loc: rt.Loc})
	}
	var perms []string
	for _, a := range args[1:] {
		if pc, ok := a.(*ast.CallExpr); ok {
			perms = append(perms, w.permArgs(pc)...)
		}
	}
	rt.Perm = strings.Join(perms, "|")
	w.out.Routes = append(w.out.Routes, rt)
}

// permArgs 提取 core.Perm("x") / anyPerm("a","b") 的权限码；未知的调用返回 nil。
func (w *walker) permArgs(c *ast.CallExpr) []string {
	name := ""
	switch f := c.Fun.(type) {
	case *ast.SelectorExpr:
		name = f.Sel.Name
	case *ast.Ident:
		name = f.Name
	}
	if name != "Perm" && name != "anyPerm" {
		return nil
	}
	var out []string
	for _, a := range c.Args {
		v, _ := w.ct.pathOf(w.gf, a)
		out = append(out, v)
	}
	return out
}

// perm 登记全仓库出现的 core.Perm(...) 权限码；解析不了的用 "?" 并列入无法解析。
func (w *walker) perm(c *ast.CallExpr) {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Perm" {
		return
	}
	if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "core" {
		return
	}
	for _, code := range w.permArgs(c) {
		if strings.Contains(code, "?") {
			w.out.Unres = append(w.out.Unres, Unresolved{Side: "backend", Reason: "权限码无法静态解析", Text: "core.Perm(...)", Loc: w.loc(c.Pos())})
			continue
		}
		w.out.Perms[code] = append(w.out.Perms[code], w.loc(c.Pos()))
	}
}

// backendModule 把源文件目录转成报告里的模块名：modules/system/user -> system/user。
func backendModule(rel string) string {
	d := path.Dir(rel)
	switch {
	case strings.HasPrefix(d, "modules/"):
		return strings.TrimPrefix(d, "modules/")
	case strings.HasPrefix(d, "internal/modules/"):
		return "internal/" + strings.TrimPrefix(d, "internal/modules/")
	}
	return d
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
