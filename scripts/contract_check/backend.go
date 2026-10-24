package main

import (
	"go/ast"
	"go/token"
	"sort"
)

// 路由动词；Any 记为 ANY。
var routeVerbs = map[string]string{
	"GET": "GET", "POST": "POST", "PUT": "PUT", "PATCH": "PATCH", "DELETE": "DELETE",
	"HEAD": "HEAD", "OPTIONS": "OPTIONS", "Any": "ANY",
}

// BackendScan 是后端扫描结果。
type BackendScan struct {
	Routes []Route
	Perms  map[string][]Loc
	Unres  []Unresolved
}

type scope map[string]string // 变量名 -> 分组前缀

type walker struct {
	fset *token.FileSet
	ct   *constTab
	gf   *goFile
	out  *BackendScan
}

func scanBackend(root string) *BackendScan {
	fset := token.NewFileSet()
	files, unres := parseBackend(root, fset)
	out := &BackendScan{Perms: map[string][]Loc{}, Unres: unres}
	ct := collectConsts(files)
	for _, gf := range files {
		w := &walker{fset: fset, ct: ct, gf: gf, out: out}
		for _, d := range gf.f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				w.funcDecl(fd)
			}
		}
	}
	sort.SliceStable(out.Routes, func(i, j int) bool {
		a, b := out.Routes[i], out.Routes[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Method < b.Method
	})
	return out
}

func (w *walker) loc(p token.Pos) Loc {
	return Loc{File: w.gf.rel, Line: w.fset.Position(p).Line}
}

// paramScope 把 *gin.RouterGroup / *gin.Engine 形参登记为路由根。
// RouterGroup 形参一律按模块挂载点 /api/v1 处理（core/setup 是 /api/v1/setup）。
func (w *walker) paramScope(sc scope, fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		t := f.Type
		if st, ok := t.(*ast.StarExpr); ok {
			t = st.X
		}
		sel, ok := t.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		root, ok := "", false
		switch sel.Sel.Name {
		case "RouterGroup", "IRouter", "IRoutes":
			root, ok = apiPrefix, true
			if w.gf.dir == "core/setup" {
				root = apiPrefix + "/setup"
			}
		case "Engine":
			root, ok = "", true
		}
		if ok {
			for _, n := range f.Names {
				sc[n.Name] = root
			}
		}
	}
}

func (w *walker) funcDecl(fd *ast.FuncDecl) {
	sc := scope{}
	w.paramScope(sc, fd.Type.Params)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			w.paramScope(sc, x.Type.Params)
		case *ast.AssignStmt:
			w.assign(sc, x.Lhs, x.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, len(x.Names))
			for i, nm := range x.Names {
				lhs[i] = nm
			}
			w.assign(sc, lhs, x.Values)
		case *ast.CallExpr:
			w.route(x, sc)
			w.perm(x)
		}
		return true
	})
}

func (w *walker) assign(sc scope, lhs, rhs []ast.Expr) {
	if len(lhs) != len(rhs) {
		return
	}
	for i, l := range lhs {
		id, ok := l.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		if p, ok := w.router(rhs[i], sc); ok {
			sc[id.Name] = p
		} else if isEngineCtor(rhs[i]) {
			sc[id.Name] = ""
		} else {
			delete(sc, id.Name)
		}
	}
}

// isEngineCtor 识别 gin.New() / gin.Default() / x.newEngine()。
func isEngineCtor(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if id, ok := sel.X.(*ast.Ident); ok && id.Name == "gin" && (sel.Sel.Name == "New" || sel.Sel.Name == "Default") {
		return true
	}
	return sel.Sel.Name == "newEngine"
}

// router 求值“这个表达式是哪个路由分组”，返回含 /api/v1 的完整前缀。
func (w *walker) router(e ast.Expr, sc scope) (string, bool) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return w.router(x.X, sc)
	case *ast.Ident:
		p, ok := sc[x.Name]
		return p, ok
	case *ast.SelectorExpr:
		switch x.Sel.Name {
		case "Public":
			return apiPrefix, true
		case "Admin":
			return apiPrefix + "/admin", true
		}
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		base, ok := w.router(sel.X, sc)
		if !ok {
			return "", false
		}
		switch sel.Sel.Name {
		case "Group":
			rel := ""
			if len(x.Args) > 0 {
				rel, _ = w.ct.pathOf(w.gf, x.Args[0])
			}
			return joinPath(base, rel), true
		case "Use":
			return base, true
		}
	}
	return "", false
}
