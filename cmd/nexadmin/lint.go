package main

// plugin lint 不执行插件代码。Go 使用 AST；前端只检查约定的静态 routes 对象数组、
// menu/settings.title 和 t('literal') / i18nKey="literal"。不做 TS 类型检查或运行时推导。

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"nexadmin/core/plugin"
)

func cmdLint(args []string, out io.Writer) error {
	fl := newFlags("lint")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 {
		return errors.New("用法: nexadmin plugin lint <插件目录（含 plugin.yaml）>")
	}
	dir, err := filepath.Abs(fl.Arg(0))
	if err != nil {
		return err
	}
	man, err := plugin.LoadManifest(dir)
	if err != nil {
		return err
	}
	backend := filepath.Join(dir, "backend")
	if st, err := os.Stat(backend); err != nil || !st.IsDir() {
		return errors.New("插件包缺少 backend/ 目录（安装器不支持纯前端包）")
	}
	if err := lintPluginRegistration(backend, man); err != nil {
		return err
	}
	if err := lintPluginMigrations(backend); err != nil {
		return err
	}
	if err := lintPluginWeb(dir, man); err != nil {
		return err
	}
	fmt.Fprintf(out, "校验通过：%s %s %s；Go 注册、迁移集合、路由/菜单、mock 与双语词条一致。\n", man.ID, man.Version, man.Edition)
	fmt.Fprintln(out, "边界：Go 注册需直接 Plugin 字面量（字段支持字符串常量、plugin.Free/Pro）；路由需 export const routes 的静态对象数组。i18n 检查静态 t(...)、i18nKey 和路由标题，动态写法拒绝；不验证别名翻译函数、运行时组合、TS 类型或页面样式。发布前仍须构建、测试与人工审核。")
	return nil
}

func lintPluginMigrations(backend string) error {
	root := filepath.Join(backend, "migrations")
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil // 无数据库插件无需制造迁移。
	} else if err != nil {
		return err
	}
	sets := map[string]map[string]bool{"mysql": {}, "mariadb": {}, "postgres": {}}
	all := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".sql") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 2 || sets[parts[0]] == nil || !d.Type().IsRegular() {
			return fmt.Errorf("迁移 %s 必须是 migrations/{mysql,mariadb,postgres}/<同名文件>.sql", rel)
		}
		sets[parts[0]][parts[1]], all[parts[1]] = true, true
		return nil
	})
	if err != nil {
		return err
	}
	var missing []string
	for name := range all {
		for _, engine := range []string{"mysql", "mariadb", "postgres"} {
			if !sets[engine][name] {
				missing = append(missing, engine+"/"+name)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("迁移三方言同名文件集合不一致，缺少: %s", strings.Join(missing, ", "))
	}
	return nil
}

type pluginGoSource struct {
	file      *ast.File
	aliases   map[string]bool
	constants map[string]ast.Expr
}

func lintPluginRegistration(dir string, man *plugin.Manifest) error {
	fset := token.NewFileSet()
	var sources []pluginGoSource
	// 同一 Go 包的跨文件 const 可解析；变量/函数调用不能冒充常量。
	consts := map[string]map[string]ast.Expr{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		key := filepath.Dir(path) + ":" + f.Name.Name
		if consts[key] == nil {
			consts[key] = map[string]ast.Expr{}
		}
		s := pluginGoSource{file: f, aliases: map[string]bool{}, constants: consts[key]}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p != "nexadmin/core/plugin" && !strings.HasSuffix(p, "/core/plugin") {
				continue
			}
			alias := "plugin"
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			if alias == "." {
				return fmt.Errorf("%s: 不支持 core/plugin 的点导入，请使用具名导入", path)
			}
			s.aliases[alias] = true
		}
		for _, decl := range f.Decls {
			g, ok := decl.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, spec := range g.Specs {
				v := spec.(*ast.ValueSpec)
				for i, name := range v.Names {
					if i < len(v.Values) {
						s.constants[name.Name] = v.Values[i]
					}
				}
			}
		}
		sources = append(sources, s)
		return nil
	})
	if err != nil {
		return err
	}
	count := 0
	for _, s := range sources {
		ast.Inspect(s.file, func(n ast.Node) bool {
			if err != nil {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Register" {
				return true
			}
			alias, ok := sel.X.(*ast.Ident)
			if !ok || !s.aliases[alias.Name] {
				return true
			}
			count++
			fail := func(msg string) { err = fmt.Errorf("%s: plugin.Register: %s", fset.Position(call.Pos()), msg) }
			if alias.Obj != nil && alias.Obj.Kind != ast.Pkg {
				fail("plugin 导入被局部声明遮蔽，无法静态验证")
				return false
			}
			if len(call.Args) != 1 {
				fail("需要一个 Plugin 字面量")
				return false
			}
			lit, ok := call.Args[0].(*ast.CompositeLit)
			if !ok {
				fail("无法静态验证动态注册参数；请直接传 plugin.Plugin{ID, Version, Edition, ...}")
				return false
			}
			typ, ok := lit.Type.(*ast.SelectorExpr)
			if !ok {
				fail("需要具名导入的 plugin.Plugin 字面量")
				return false
			}
			pkg, ok := typ.X.(*ast.Ident)
			if !ok || !s.aliases[pkg.Name] || typ.Sel.Name != "Plugin" {
				fail("注册参数类型不是 plugin.Plugin")
				return false
			}
			fields := map[string]ast.Expr{}
			for _, field := range lit.Elts {
				kv, ok := field.(*ast.KeyValueExpr)
				if !ok {
					fail("注册字段必须具名")
					return false
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || fields[key.Name] != nil {
					fail("注册字段无效或重复")
					return false
				}
				fields[key.Name] = kv.Value
			}
			for _, field := range []struct{ key, want string }{{"ID", man.ID}, {"Version", man.Version}, {"Edition", string(man.Edition)}} {
				got, ok := s.stringValue(fields[field.key], map[string]bool{})
				if !ok {
					fail(field.key + " 无法静态验证；仅支持字符串字面量/const 和 plugin.Free/Pro，不支持变量或函数调用")
					return false
				}
				if got != field.want {
					fail(fmt.Sprintf("%s=%q 与 plugin.yaml 的 %q 不一致", field.key, got, field.want))
					return false
				}
			}
			return true
		})
		if err != nil {
			return err
		}
	}
	if count != 1 {
		return fmt.Errorf("backend/ 必须包含且只包含一个可核对的 plugin.Register，找到 %d 个（不计 _test.go）", count)
	}
	return nil
}

func (s pluginGoSource) stringValue(expr ast.Expr, seen map[string]bool) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			v, err := strconv.Unquote(e.Value)
			return v, err == nil
		}
	case *ast.ParenExpr:
		return s.stringValue(e.X, seen)
	case *ast.Ident:
		value := s.constants[e.Name]
		// parser 的对象绑定能区分局部变量/const 与同名包级 const，不能按名字误放过动态值。
		if e.Obj != nil {
			if e.Obj.Kind != ast.Con {
				return "", false
			}
			value = nil
			if spec, ok := e.Obj.Decl.(*ast.ValueSpec); ok {
				for i, name := range spec.Names {
					if name.Name == e.Name && i < len(spec.Values) {
						value = spec.Values[i]
					}
				}
			}
		}
		if !seen[e.Name] && value != nil {
			seen[e.Name] = true
			v, ok := s.stringValue(value, seen)
			delete(seen, e.Name)
			return v, ok
		}
	case *ast.SelectorExpr:
		if p, ok := e.X.(*ast.Ident); ok && s.aliases[p.Name] && (p.Obj == nil || p.Obj.Kind == ast.Pkg) {
			switch e.Sel.Name {
			case "Free":
				return "free", true
			case "Pro":
				return "pro", true
			}
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			a, aok := s.stringValue(e.X, seen)
			b, bok := s.stringValue(e.Y, seen)
			return a + b, aok && bok
		}
	}
	return "", false
}

func lintPluginWeb(dir string, man *plugin.Manifest) error {
	web := filepath.Join(dir, "web")
	if st, err := os.Stat(web); errors.Is(err, os.ErrNotExist) {
		for _, menu := range man.Menus {
			if menu.Type == "menu" {
				return errors.New("plugin.yaml 含页面菜单，但缺少 web/route.tsx")
			}
		}
		return nil
	} else if err != nil {
		return err
	} else if !st.IsDir() {
		return errors.New("web 必须是目录")
	}
	mock := filepath.Join(web, man.ID+".mock.ts")
	if st, err := os.Stat(mock); err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		return fmt.Errorf("缺少非空 mock 文件: %s", mock)
	}
	set := newLocaleSet()
	set.plural = false // 本命令契约要求双语的字面键集合一致。
	for _, code := range []string{"zh-CN", "en-US"} {
		path := filepath.Join(web, "locales", code+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("locale %s: %w", code, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("locale %s: %w", code, err)
		}
		var keys []string
		flattenKeys("", doc, &keys) // 复用 CLI i18n 的嵌套 key 展平约定。
		if len(keys) == 0 {
			return fmt.Errorf("locale %s 没有词条", code)
		}
		set.add(code, keys)
	}
	missing := missingKeys(set)
	for _, code := range []string{"zh-CN", "en-US"} {
		if len(missing[code]) > 0 {
			return fmt.Errorf("locale %s 缺少词条: %s", code, strings.Join(missing[code], ", "))
		}
	}
	checkKey := func(key, where string) error {
		if !set.keys["zh-CN"][key] {
			return fmt.Errorf("%s: i18n key %q 未被插件双语 locales 覆盖（共享词条也需显式提供）", where, key)
		}
		return nil
	}
	for _, menu := range man.Menus {
		if err := checkKey(menu.Title, "plugin.yaml menus.title"); err != nil {
			return err
		}
	}
	routePath := filepath.Join(web, "route.tsx")
	raw, err := os.ReadFile(routePath)
	if err != nil {
		return err
	}
	tokens, err := pluginTSTokens(string(raw))
	if err != nil {
		return fmt.Errorf("%s: %w", routePath, err)
	}
	routes, err := pluginStaticRoutes(tokens)
	if err != nil {
		return fmt.Errorf("%s: %w", routePath, err)
	}
	want := map[string]string{}
	for _, menu := range man.Menus {
		if menu.Type == "menu" {
			path := strings.TrimPrefix(menu.Path, "/")
			if _, ok := want[path]; ok {
				return fmt.Errorf("plugin.yaml 菜单 path 重复: %s", path)
			}
			want[path] = menu.Perm
		}
	}
	seen := map[string]bool{}
	for _, route := range routes {
		path, err := pluginTSLiteral(route["path"])
		if err != nil {
			return fmt.Errorf("route.tsx path: %w", err)
		}
		perm, err := pluginTSLiteral(route["perm"])
		if err != nil {
			return fmt.Errorf("route.tsx perm (%s): %w", path, err)
		}
		path = strings.TrimPrefix(path, "/")
		if seen[path] {
			return fmt.Errorf("route.tsx path 重复: %s", path)
		}
		seen[path] = true
		if expected, ok := want[path]; !ok || expected != perm {
			return fmt.Errorf("route.tsx path=%q perm=%q 与 plugin.yaml 菜单不一致", path, perm)
		}
		for _, field := range []string{"menu", "settings"} {
			if value, ok := route[field]; ok {
				props, err := pluginTSObject(value)
				if err != nil {
					return fmt.Errorf("route.tsx %s: %w", field, err)
				}
				key, err := pluginTSLiteral(props["title"])
				if err != nil {
					return fmt.Errorf("route.tsx %s.title: %w", field, err)
				}
				if err := checkKey(key, "route.tsx "+field+".title"); err != nil {
					return err
				}
			}
		}
	}
	for path := range want {
		if !seen[path] {
			return fmt.Errorf("plugin.yaml 菜单 path=%q 缺少对应 web 路由", path)
		}
	}
	return filepath.WalkDir(web, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (!strings.HasSuffix(path, ".tsx") && !strings.HasSuffix(path, ".ts")) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		ts, err := pluginTSTokens(string(raw))
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for i := 0; i+2 < len(ts); i++ {
			if ts[i].quoted {
				continue
			}
			var value []pluginTSToken
			if ts[i].text == "t" && ts[i+1].text == "(" {
				if i+3 >= len(ts) || (ts[i+3].text != "," && ts[i+3].text != ")") {
					return fmt.Errorf("%s: t(...) 动态 i18n key 无法静态验证", path)
				}
				value = ts[i+2 : i+3]
			} else if ts[i].text == "i18nKey" && ts[i+1].text == "=" {
				value = ts[i+2 : i+3]
				if ts[i+2].text == "{" && i+4 < len(ts) && ts[i+4].text == "}" {
					value = ts[i+3 : i+4]
				}
			} else {
				continue
			}
			key, err := pluginTSLiteral(value)
			if err != nil {
				return fmt.Errorf("%s: i18n key: %w", path, err)
			}
			if err := checkKey(key, path); err != nil {
				return err
			}
		}
		return nil
	})
}

// 轻量词法扫描仅用于上述声明契约：忽略注释、把字符串作为原子，避免将注释或
// lazy/component 内部的 path/perm 误认成路由。不是 TS/JSX 解析器。
type pluginTSToken struct {
	text            string
	quoted, dynamic bool
}

func pluginTSTokens(src string) ([]pluginTSToken, error) {
	var result []pluginTSToken
	for i := 0; i < len(src); {
		c := src[i]
		if unicode.IsSpace(rune(c)) {
			i++
			continue
		}
		if strings.HasPrefix(src[i:], "//") {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if strings.HasPrefix(src[i:], "/*") {
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, errors.New("未闭合注释")
			}
			i += end + 4
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			i++
			var b strings.Builder
			for i < len(src) && src[i] != c {
				if src[i] == '\\' || src[i] >= 128 {
					r, _, rest, err := strconv.UnquoteChar(src[i:], c)
					if err != nil {
						return nil, errors.New("字符串转义无法静态解析；请使用普通字符串")
					}
					i = len(src) - len(rest)
					b.WriteRune(r)
				} else {
					b.WriteByte(src[i])
					i++
				}
			}
			if i >= len(src) {
				return nil, errors.New("未闭合字符串（lint 仅支持约定的静态 TS 声明）")
			}
			i++
			result = append(result, pluginTSToken{text: b.String(), quoted: true, dynamic: c == '`' && strings.Contains(b.String(), "${")})
			continue
		}
		start := i
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '$' || (c >= '0' && c <= '9') {
			for i < len(src) && (unicode.IsLetter(rune(src[i])) || unicode.IsDigit(rune(src[i])) || src[i] == '_' || src[i] == '$') {
				i++
			}
		} else {
			i++
		}
		result = append(result, pluginTSToken{text: src[start:i]})
	}
	return result, nil
}

func pluginTSLiteral(ts []pluginTSToken) (string, error) {
	if len(ts) != 1 || !ts[0].quoted || ts[0].dynamic {
		return "", errors.New("无法静态验证，必须使用单个字符串字面量，不能使用变量、拼接或动态模板")
	}
	return ts[0].text, nil
}

// split 只在括号深度为零处切分；同一能力用于路由对象数组和对象属性。
func pluginTSSplit(ts []pluginTSToken) ([][]pluginTSToken, error) {
	var parts [][]pluginTSToken
	var stack []string
	start := 0
	for i, t := range ts {
		if t.quoted {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			stack = append(stack, t.text)
		case ")", "]", "}":
			pairs := map[string]string{")": "(", "]": "[", "}": "{"}
			if len(stack) == 0 || stack[len(stack)-1] != pairs[t.text] {
				return nil, errors.New("静态声明括号不匹配")
			}
			stack = stack[:len(stack)-1]
		case ",":
			if len(stack) == 0 {
				parts = append(parts, ts[start:i])
				start = i + 1
			}
		}
	}
	if len(stack) != 0 {
		return nil, errors.New("静态声明括号未闭合")
	}
	if start < len(ts) {
		parts = append(parts, ts[start:])
	}
	return parts, nil
}

func pluginTSObject(ts []pluginTSToken) (map[string][]pluginTSToken, error) {
	if len(ts) < 2 || ts[0].text != "{" || ts[len(ts)-1].text != "}" {
		return nil, errors.New("仅支持静态对象字面量，不支持变量或展开")
	}
	parts, err := pluginTSSplit(ts[1 : len(ts)-1])
	if err != nil {
		return nil, err
	}
	props := map[string][]pluginTSToken{}
	for _, p := range parts {
		if len(p) < 3 || p[1].text != ":" || p[0].dynamic {
			return nil, errors.New("对象属性必须显式 key: value；不支持展开、计算属性或方法")
		}
		if _, ok := props[p[0].text]; ok {
			return nil, fmt.Errorf("重复属性 %s", p[0].text)
		}
		props[p[0].text] = p[2:]
	}
	return props, nil
}

func pluginStaticRoutes(ts []pluginTSToken) ([]map[string][]pluginTSToken, error) {
	start := -1
	for i := 0; i+2 < len(ts); i++ {
		if !ts[i].quoted && ts[i].text == "export" && ts[i+1].text == "const" && ts[i+2].text == "routes" {
			if start != -1 {
				return nil, errors.New("重复 routes 声明")
			}
			start = i + 3
		}
	}
	if start < 0 {
		return nil, errors.New("需要 export const routes: ModuleRoute[] = [...] 静态声明")
	}
	for start < len(ts) && ts[start].text != "=" {
		start++
	}
	if start+1 >= len(ts) || ts[start+1].text != "[" {
		return nil, errors.New("routes 必须是静态对象数组，不能使用函数或变量")
	}
	start += 2
	depth, end := 1, start
	for ; end < len(ts); end++ {
		if ts[end].quoted {
			continue
		}
		if ts[end].text == "[" {
			depth++
		}
		if ts[end].text == "]" {
			depth--
			if depth == 0 {
				break
			}
		}
	}
	if end == len(ts) {
		return nil, errors.New("routes 数组未闭合")
	}
	// 仅识别约定的纯类型后缀，不让 as/satisfies 掩盖后续 concat/map 等运行时表达式。
	var suffix []string
	for i := end + 1; i < len(ts) && ts[i].text != ";"; i++ {
		suffix = append(suffix, ts[i].text)
	}
	switch strings.Join(suffix, " ") {
	case "", "as const", "satisfies ModuleRoute [ ]", "as const satisfies ModuleRoute [ ]":
	default:
		return nil, errors.New("routes 数组后存在无法验证的表达式；请单独声明静态 routes")
	}
	parts, err := pluginTSSplit(ts[start:end])
	if err != nil {
		return nil, err
	}
	var routes []map[string][]pluginTSToken
	for _, part := range parts {
		props, err := pluginTSObject(part)
		if err != nil {
			return nil, err
		}
		routes = append(routes, props)
	}
	return routes, nil
}
