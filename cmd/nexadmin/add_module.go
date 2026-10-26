package main

import (
	"errors"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"nexadmin/core/plugin/market"
)

const (
	modulesBegin = "// nexadmin:modules:begin"
	modulesEnd   = "// nexadmin:modules:end"
	modulesFile  = "internal/modules/modules.go"
	mainFile     = "cmd/server/main.go"
)

var (
	migNumRe  = regexp.MustCompile(`^(\d+)_.+\.sql$`)
	engineDir = []string{"mysql", "mariadb", "postgres"}
)

// modulesGoTemplate 是 internal/modules/modules.go 的初始内容（%s 为空）。
const modulesGoTemplate = `// Package modules 汇总业务模块。每个业务模块在自己包的 init() 里调用 core.RegisterBuiltin，
// 标记行之间的空导入由 ^nexadmin add module^ 维护，请勿手改；cmd/server/main.go 已空导入本包。
package modules

import (
` + modulesBegin + `
` + modulesEnd + `
)
`

// nextMigrationNumber 返回 migrations/business 三个引擎目录里最大编号 + 1（至少 1）。
func nextMigrationNumber(root string) (int, error) {
	maxN := 0
	for _, eng := range engineDir {
		ents, err := os.ReadDir(filepath.Join(root, "migrations", "business", eng))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		for _, e := range ents {
			if m := migNumRe.FindStringSubmatch(e.Name()); m != nil {
				n, _ := strconv.Atoi(m[1])
				if n > maxN {
					maxN = n
				}
			}
		}
	}
	return maxN + 1, nil
}

func migrationFile(n int, name string) string { return fmt.Sprintf("%03d_%s.sql", n, name) }

// updateModulesFile 幂等地在标记行之间追加空导入（排序、去重）；已存在则原样返回 changed=false。
func updateModulesFile(src []byte, importPath string) (out []byte, changed bool, err error) {
	lines := strings.Split(string(src), "\n")
	begin, end := -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case modulesBegin:
			begin = i
		case modulesEnd:
			end = i
		}
	}
	if begin < 0 || end <= begin {
		return nil, false, fmt.Errorf("%s 缺少标记行 %q / %q，请手动加入: _ \"%s\"", modulesFile, modulesBegin, modulesEnd, importPath)
	}
	set := map[string]bool{}
	for _, l := range lines[begin+1 : end] {
		if m := market.ImportRe.FindStringSubmatch(l); m != nil {
			set[m[1]] = true
		}
	}
	if set[importPath] {
		return src, false, nil
	}
	set[importPath] = true
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	res := append([]string{}, lines[:begin]...)
	res = append(res, "\t"+modulesBegin)
	for _, p := range paths {
		res = append(res, "\t_ \""+p+"\"")
	}
	res = append(res, "\t"+modulesEnd)
	res = append(res, lines[end+1:]...)
	return []byte(strings.Join(res, "\n")), true, nil
}

// ensureMainImport 保证 cmd/server/main.go 空导入了 <mod>/internal/modules；已有则 changed=false。
func ensureMainImport(src []byte, importPath string) ([]byte, bool, error) {
	if strings.Contains(string(src), `"`+importPath+`"`) {
		return src, false, nil
	}
	lines := strings.Split(string(src), "\n")
	in := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "import (") {
			in = true
		} else if in && t == ")" {
			ins := "\t_ \"" + importPath + "\" // 业务模块（由 nexadmin add module 维护）"
			lines = append(lines[:i], append([]string{ins}, lines[i:]...)...)
			out, err := format.Source([]byte(strings.Join(lines, "\n")))
			return out, err == nil, err
		}
	}
	return nil, false, fmt.Errorf("%s 里没有 import ( ... ) 块，请手动加入: _ \"%s\"", mainFile, importPath)
}

// planAdd 计算要写的新文件与要改的共享文件，不落盘（写之前先全部校验）。
type addPlan struct {
	files   map[string][]byte // 新文件，相对路径
	modules []byte            // 新的 modules.go 内容（nil = 不变）
	mainGo  []byte            // 新的 main.go 内容（nil = 不变）
}

func planAdd(root, mod, name string, fields []Field, sortable bool) (*addPlan, *Spec, error) {
	if err := validModuleName(name); err != nil {
		return nil, nil, err
	}
	for _, d := range []string{filepath.Join("internal", "modules", name), filepath.Join("web", "src", "pages", name), filepath.Join("modules", name)} {
		if market.Exists(filepath.Join(root, d)) {
			return nil, nil, fmt.Errorf("%s 已存在，拒绝覆盖", filepath.ToSlash(d))
		}
	}
	n, err := nextMigrationNumber(root)
	if err != nil {
		return nil, nil, err
	}
	spec, err := buildSpec(mod, name, fields, sortable, migrationFile(n, name))
	if err != nil {
		return nil, nil, err
	}
	files, err := renderModule(spec)
	if err != nil {
		return nil, nil, err
	}
	for rel := range files {
		if market.Exists(filepath.Join(root, filepath.FromSlash(rel))) {
			return nil, nil, fmt.Errorf("%s 已存在，拒绝覆盖", rel)
		}
	}
	p := &addPlan{files: files}
	imp := mod + "/internal/modules/" + name
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(modulesFile)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		src = []byte(strings.ReplaceAll(modulesGoTemplate, "^", "`"))
	case err != nil:
		return nil, nil, err
	}
	nw, changed, err := updateModulesFile(src, imp)
	if err != nil {
		return nil, nil, err
	}
	if changed {
		p.modules = nw
	}
	if raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(mainFile))); err == nil {
		out, ch, err := ensureMainImport(raw, mod+"/internal/modules")
		if err != nil {
			return nil, nil, err
		}
		if ch {
			p.mainGo = out
		}
	}
	return p, spec, nil
}

func writeRel(root, rel string, data []byte) error {
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// addModule 生成模块并更新汇总文件，返回新增文件的相对路径（已排序）。
func addModule(root, mod, name string, fields []Field, sortable bool) ([]string, error) {
	p, _, err := planAdd(root, mod, name, fields, sortable)
	if err != nil {
		return nil, err
	}
	var rels []string
	for rel := range p.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		if err := writeRel(root, rel, p.files[rel]); err != nil {
			return nil, err
		}
	}
	if p.modules != nil {
		if err := writeRel(root, modulesFile, p.modules); err != nil {
			return nil, err
		}
	}
	if p.mainGo != nil { // 共享文件只在必要时改，且只加一行 import
		if err := writeRel(root, mainFile, p.mainGo); err != nil {
			return nil, err
		}
	}
	return rels, nil
}

const addUsage = `用法: nexadmin add module <名称> [--fields "title:string,status:dict(sys_status),sort:int,cover:image"] [--sortable]
字段类型: string text int float bool date datetime dict(字典编码) ref(目标资源) image file`

func cmdAdd(args []string, out io.Writer) error {
	if len(args) < 1 || args[0] != "module" {
		return errors.New(addUsage)
	}
	fl := newFlags("add module")
	fieldsArg := fl.String("fields", "", "字段列表，逗号分隔 name:type")
	sortable := fl.Bool("sortable", false, "生成排序接口与拖动手柄")
	var pos []string
	rest := args[1:]
	for len(rest) > 0 {
		if err := fl.Parse(rest); err != nil {
			return fmt.Errorf("%w\n%s", err, addUsage)
		}
		if fl.NArg() == 0 {
			break
		}
		pos = append(pos, fl.Arg(0))
		rest = fl.Args()[1:]
	}
	if len(pos) != 1 {
		return errors.New(addUsage)
	}
	fields, err := parseFields(*fieldsArg)
	if err != nil {
		return err
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	mod, err := market.ModulePath(root)
	if err != nil {
		return err
	}
	rels, err := addModule(root, mod, pos[0], fields, *sortable)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "已生成模块 %s（%d 个文件）:\n", pos[0], len(rels))
	for _, r := range rels {
		fmt.Fprintln(out, "  +", r)
	}
	fmt.Fprintf(out, "已在 %s 追加空导入（幂等）。\n后端: go build ./... ；前端: cd web && npx tsc --noEmit\n"+
		"提示: 重新编译后重启服务即自动执行迁移（migrations/business/）并补上菜单，无需手动跑 SQL 或加菜单；"+
		"新菜单超管默认可见，其他角色到「角色管理」分配。\n", modulesFile)
	return nil
}
