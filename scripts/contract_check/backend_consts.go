package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// 后端扫描范围；排除 _test.go。
var backendDirs = []string{"core", "modules", "plugins", "internal"}

type goFile struct {
	rel string // 相对仓库根
	dir string // 所在目录（相对仓库根）
	pkg string
	f   *ast.File
}

// parseBackend 解析后端所有非测试 .go；语法错误的文件跳过并记入 unres（可能是别人正在改）。
func parseBackend(root string, fset *token.FileSet) (files []*goFile, unres []Unresolved) {
	for _, d := range backendDirs {
		base := filepath.Join(root, d)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		_ = filepath.WalkDir(base, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if de.IsDir() {
				if n := de.Name(); n == "node_modules" || n == "testdata" || n == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			f, perr := parser.ParseFile(fset, p, nil, 0)
			if perr != nil {
				unres = append(unres, Unresolved{Side: "backend", Reason: "Go 文件解析失败，已跳过", Text: perr.Error(), Loc: Loc{File: rel, Line: 1}})
				return nil
			}
			files = append(files, &goFile{rel: rel, dir: path.Dir(rel), pkg: f.Name.Name, f: f})
			return nil
		})
	}
	return files, unres
}

// constTab 收集包级字符串常量，供路径与权限码里的标识符解析。
type constTab struct {
	byDir map[string]string // "目录|名字"
	byPkg map[string]string // "包名.名字"（跨包引用，如 ctl.ShutdownPath）
}

func collectConsts(files []*goFile) *constTab {
	ct := &constTab{byDir: map[string]string{}, byPkg: map[string]string{}}
	for round := 0; round < 3; round++ { // 多轮：常量之间可能互相引用
		for _, gf := range files {
			for _, decl := range gf.f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
					continue
				}
				for _, sp := range gd.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, nm := range vs.Names {
						if i >= len(vs.Values) {
							continue
						}
						if v, ok := ct.pathOf(gf, vs.Values[i]); ok {
							ct.byDir[gf.dir+"|"+nm.Name] = v
							ct.byPkg[gf.pkg+"."+nm.Name] = v
						}
					}
				}
			}
		}
	}
	return ct
}

// pathOf 把表达式求值为字符串；解析不了的片段以 "?" 占位并返回 ok=false。
func (ct *constTab) pathOf(gf *goFile, e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			if s, err := strconv.Unquote(x.Value); err == nil {
				return s, true
			}
		}
	case *ast.ParenExpr:
		return ct.pathOf(gf, x.X)
	case *ast.Ident:
		if v, ok := ct.byDir[gf.dir+"|"+x.Name]; ok {
			return v, true
		}
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if v, ok := ct.byPkg[id.Name+"."+x.Sel.Name]; ok {
				return v, true
			}
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			l, lok := ct.pathOf(gf, x.X)
			r, rok := ct.pathOf(gf, x.Y)
			return l + r, lok && rok
		}
	case *ast.CallExpr:
		return ct.sprintf(gf, x)
	}
	return "?", false
}

// sprintf 处理 fmt.Sprintf("/a/%s", x)：格式串里的动词换成 "?"。
func (ct *constTab) sprintf(gf *goFile, c *ast.CallExpr) (string, bool) {
	if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(c.Args) > 0 {
		if f, ok := ct.pathOf(gf, c.Args[0]); ok {
			var b strings.Builder
			for i := 0; i < len(f); i++ {
				if f[i] == '%' && i+1 < len(f) {
					i++
					b.WriteByte('?')
					continue
				}
				b.WriteByte(f[i])
			}
			return b.String(), false
		}
	}
	return "?", false
}
