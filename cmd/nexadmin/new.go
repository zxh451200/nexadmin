package main

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"nexadmin/core/plugin/market"
)

const oldModule = "nexadmin"

var (
	projectNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	pkgNameRe     = regexp.MustCompile(`("name"\s*:\s*)"[^"]*"`)
)

type newOptions struct {
	Name        string
	Out         string
	Module      string
	WithPlugins bool
	Git         bool
}

// ---- 排除规则 ----

// newSkip 判断模板里的相对路径（正斜杠）是否不拷贝；isDir 表示该条目是目录。
func newSkip(rel string, isDir bool, withPlugins bool) bool {
	parts := strings.Split(rel, "/")
	base := parts[len(parts)-1]
	for _, p := range parts {
		if strings.HasPrefix(p, ".nexadmin-recovery-") || strings.HasPrefix(p, ".nexadmin-txn-") || p == ".git" || p == "node_modules" || p == "bin" || p == "__pycache__" {
			return true
		}
	}
	if !isDir && (strings.HasSuffix(base, ".log") || strings.HasSuffix(base, ".pyc")) {
		return true
	}
	if base == ".env.local" || (strings.HasPrefix(base, ".env.") && strings.HasSuffix(base, ".local")) { // 用户本地覆盖（任意目录，如 web/.env.local）：不属于任何分区，不下发
		return true
	}
	switch rel {
	case "dist", "web/dist", "backup", ".dev-keys", trashDir, filesRecord, ".env.local",
		"config/config.yaml", "scripts/derive_check", "scripts/contract_check", "scripts/gen_icons", ".github/workflows/derive.yml",
		"nexadmin-vendor":
		return true
	}
	if strings.HasPrefix(rel, "scripts/derive_check/") || strings.HasPrefix(rel, "scripts/contract_check/") || strings.HasPrefix(rel, "scripts/gen_icons/") || strings.HasPrefix(rel, "backup/") ||
		strings.HasPrefix(rel, ".dev-keys/") || strings.HasPrefix(rel, trashDir+"/") ||
		strings.HasPrefix(rel, "nexadmin-vendor/") { // 发行方私有资料（授权签发后台/文档站源/运营手册），不随框架包下发
		return true
	}
	// data/*、logs/*：只保留 .gitkeep
	for _, d := range []string{"data", "logs"} {
		if strings.HasPrefix(rel, d+"/") && rel != d+"/.gitkeep" {
			return true
		}
	}
	if !withPlugins {
		// plugins/ 下除 plugins.go 外的目录；web/src/plugins/ 下除 .gitkeep 外的目录
		if len(parts) >= 2 && parts[0] == "plugins" && (isDir || len(parts) > 2) {
			return true
		}
		if len(parts) >= 4 && parts[0] == "web" && parts[1] == "src" && parts[2] == "plugins" && (isDir || len(parts) >= 5) {
			return true
		}
	}
	return false
}

// copyTemplate 把 src 拷到 dst（保留文件权限，拒绝非常规文件），返回拷贝的文件数。
func copyTemplate(src, dst string, withPlugins bool) (int, error) {
	n := 0
	absDst, _ := filepath.Abs(dst)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if r == "." {
			return nil
		}
		rel := filepath.ToSlash(r)
		if abs, _ := filepath.Abs(p); abs == absDst {
			return filepath.SkipDir
		}
		if newSkip(rel, d.IsDir(), withPlugins) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, r)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			return fmt.Errorf("源码含非常规文件，无法复制: %s", rel)
		case d.Type().IsRegular():
			st, err := d.Info()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := market.CopyFile(p, target); err != nil {
				return err
			}
			n++
			return os.Chmod(target, st.Mode().Perm())
		}
		return fmt.Errorf("源码含非常规文件，无法复制: %s", rel)
	})
	return n, err
}

// ---- 改写 ----

// clearPluginImports 清空 plugins.go 两个标记行之间的空导入（复用 plugin install/remove 的改写函数）。
func clearPluginImports(src []byte) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	in := false
	var paths []string
	for _, l := range lines {
		switch strings.TrimSpace(l) {
		case market.MarkBegin:
			in = true
		case market.MarkEnd:
			in = false
		default:
			if in {
				if m := market.ImportRe.FindStringSubmatch(l); m != nil {
					paths = append(paths, m[1])
				}
			}
		}
	}
	out := src
	var err error
	for _, p := range paths {
		if out, err = market.UpdatePluginsFile(out, p, false); err != nil {
			return nil, err
		}
	}
	if len(paths) == 0 { // 没有导入也校验一下标记行
		return market.UpdatePluginsFile(src, "", false)
	}
	return out, nil
}

// rewriteImports 用 go/parser 只改 import 声明里以 old 开头的路径，注释与其他字符串不动。
func rewriteImports(src []byte, filename, old, nw string) ([]byte, bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, false, err
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if path != old && !strings.HasPrefix(path, old+"/") {
			continue
		}
		edits = append(edits, edit{
			start: fset.Position(imp.Path.Pos()).Offset,
			end:   fset.Position(imp.Path.End()).Offset,
			text:  strconv.Quote(nw + strings.TrimPrefix(path, old)),
		})
	}
	if len(edits) == 0 {
		return src, false, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte{}, src...)
	for _, e := range edits {
		out = append(out[:e.start], append([]byte(e.text), out[e.end:]...)...)
	}
	return out, true, nil
}

// rewriteGoMod 只改 module 行。
func rewriteGoMod(src []byte, mod string) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	for i, l := range lines {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "module" {
			lines[i] = "module " + mod
			return []byte(strings.Join(lines, "\n")), nil
		}
	}
	return nil, errors.New("go.mod 里没有 module 行")
}

func rewriteModulePath(root, mod string) (int, error) {
	n := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out, changed, err := rewriteImports(raw, p, oldModule, mod)
		if err != nil {
			return fmt.Errorf("解析 %s 失败: %w", p, err)
		}
		if !changed {
			return nil
		}
		n++
		return os.WriteFile(p, out, 0o644)
	})
	if err != nil {
		return n, err
	}
	gm := filepath.Join(root, "go.mod")
	raw, err := os.ReadFile(gm)
	if err != nil {
		return n, err
	}
	out, err := rewriteGoMod(raw, mod)
	if err != nil {
		return n, err
	}
	return n, os.WriteFile(gm, out, 0o644)
}

// rewritePackageName 改 web/package.json 的第一个 "name" 字段，保留其余排版。
func rewritePackageName(root, name string) error {
	p := filepath.Join(root, "web", "package.json")
	raw, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	done := false
	out := pkgNameRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		if done {
			return m
		}
		done = true
		return pkgNameRe.ReplaceAll(m, []byte(`${1}`+strconv.Quote(name)))
	})
	if !done {
		return errors.New("web/package.json 里没有 name 字段")
	}
	return os.WriteFile(p, out, 0o644)
}
