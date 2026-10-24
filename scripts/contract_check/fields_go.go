package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type goStruct struct {
	name       string
	dir        string
	file       string
	line       int
	fields     map[string]bool // json 字段名
	jsonTagged int             // 带 json tag 的自有字段数（0 说明是 gorm 模型，响应形状在别处）
	incomplete bool            // 含无法解析的嵌入类型
}

type embedRef struct {
	owner *goStruct
	typ   string
}

// loadStructs 解析各目录下的 dto.go / model.go，返回 目录 -> 结构体名 -> 结构体。
func loadStructs(root string) map[string]map[string]*goStruct {
	fset := token.NewFileSet()
	res := map[string]map[string]*goStruct{}
	var embeds []embedRef
	for _, d := range backendDirs {
		_ = filepath.WalkDir(filepath.Join(root, d), func(p string, de fs.DirEntry, err error) error {
			if err != nil || de.IsDir() || (de.Name() != "dto.go" && de.Name() != "model.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, p, nil, 0)
			if perr != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			dir := path.Dir(rel)
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, sp := range gd.Specs {
					ts, ok := sp.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					gs := &goStruct{name: ts.Name.Name, dir: dir, file: rel, line: fset.Position(ts.Pos()).Line, fields: map[string]bool{}}
					embeds = append(embeds, gs.addFields(st)...)
					if res[dir] == nil {
						res[dir] = map[string]*goStruct{}
					}
					res[dir][gs.name] = gs
				}
			}
			return nil
		})
	}
	// 展开嵌入类型：同目录优先，其次任意目录里的同名结构（如 database.Model）。
	for round := 0; round < 3; round++ {
		for _, e := range embeds {
			src := res[e.owner.dir][e.typ]
			if src == nil {
				for _, m := range res {
					if s := m[e.typ]; s != nil {
						src = s
						break
					}
				}
			}
			if src == nil || src == e.owner {
				continue
			}
			for k := range src.fields {
				e.owner.fields[k] = true
			}
		}
	}
	for _, e := range embeds {
		found := res[e.owner.dir][e.typ] != nil
		for _, m := range res {
			found = found || m[e.typ] != nil
		}
		if !found {
			e.owner.incomplete = true
		}
	}
	return res
}

// addFields 登记结构体字段的 json 名；匿名嵌入返回待展开的引用。
func (g *goStruct) addFields(st *ast.StructType) (embeds []embedRef) {
	for _, fl := range st.Fields.List {
		tag := ""
		if fl.Tag != nil {
			if s, err := strconv.Unquote(fl.Tag.Value); err == nil {
				tag = reflect.StructTag(s).Get("json")
			}
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if name != "" {
			g.jsonTagged++
		}
		if len(fl.Names) == 0 { // 嵌入
			t := fl.Type
			if st, ok := t.(*ast.StarExpr); ok {
				t = st.X
			}
			tn := ""
			switch x := t.(type) {
			case *ast.Ident:
				tn = x.Name
			case *ast.SelectorExpr:
				tn = x.Sel.Name
			}
			if name != "" {
				g.fields[name] = true
			} else if tn != "" {
				embeds = append(embeds, embedRef{g, tn})
			}
			continue
		}
		for _, n := range fl.Names {
			if !n.IsExported() {
				continue
			}
			if name != "" {
				g.fields[name] = true
			} else {
				g.fields[n.Name] = true
			}
		}
	}
	return embeds
}
