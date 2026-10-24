package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"nexadmin/core/plugin"
)

const (
	markBegin = "// nexadmin:plugins:begin"
	markEnd   = "// nexadmin:plugins:end"

	maxZipFiles = 20000
	maxZipTotal = 256 << 20 // 解压后总大小上限，防压缩炸弹
)

// findRoot 从当前目录向上找包含 plugins/plugins.go 的项目根。
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "plugins", "plugins.go")); err == nil && !st.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("找不到项目根目录（需要 plugins/plugins.go），请在项目内运行")
		}
		dir = parent
	}
}

func modulePath(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`), nil
		}
	}
	return "", errors.New("go.mod 里没有 module 行")
}

var importRe = regexp.MustCompile(`^\s*_\s+"([^"]+)"\s*(//.*)?$`)

// updatePluginsFile 重写两个标记行之间的空导入：add=true 加入，false 移除；结果按路径排序、去重。
func updatePluginsFile(src []byte, importPath string, add bool) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	begin, end := -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case markBegin:
			if begin >= 0 {
				return nil, errors.New("plugins.go: 起始标记行重复")
			}
			begin = i
		case markEnd:
			end = i
		}
	}
	if begin < 0 || end < 0 || end <= begin {
		return nil, fmt.Errorf("plugins.go: 缺少标记行 %q / %q（或顺序不对）", markBegin, markEnd)
	}
	set := map[string]bool{}
	for _, l := range lines[begin+1 : end] {
		if m := importRe.FindStringSubmatch(l); m != nil {
			set[m[1]] = true
		}
	}
	if add {
		set[importPath] = true
	} else {
		delete(set, importPath)
	}
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	// gofmt 的规则：块内没有导入时注释顶格，有导入时全部缩进一个 tab。这里按它输出，保证文件始终是 gofmt 稳定的。
	indent := ""
	if len(paths) > 0 {
		indent = "\t"
	}
	out := append([]string{}, lines[:begin]...)
	out = append(out, indent+markBegin)
	for _, p := range paths {
		out = append(out, indent+`_ "`+p+`"`)
	}
	out = append(out, indent+markEnd)
	out = append(out, lines[end+1:]...)
	return []byte(strings.Join(out, "\n")), nil
}

func rewritePluginsFile(root, importPath string, add bool) error {
	p := filepath.Join(root, "plugins", "plugins.go")
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	nw, err := updatePluginsFile(raw, importPath, add)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, nw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// safeJoin 把 zip 条目名拼到 base 下；绝对路径、盘符、.. 穿越一律拒绝（zip slip）。
func safeJoin(base, name string) (string, error) {
	n := strings.ReplaceAll(name, "\\", "/")
	if n == "" || strings.ContainsRune(n, 0) || strings.HasPrefix(n, "/") || (len(n) >= 2 && n[1] == ':') {
		return "", fmt.Errorf("非法的压缩包路径 %q", name)
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", fmt.Errorf("压缩包路径包含 ..: %q", name)
		}
	}
	target := filepath.Join(base, filepath.FromSlash(n))
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("压缩包路径越界: %q", name)
	}
	return target, nil
}

func extractZip(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if len(zr.File) > maxZipFiles {
		return errors.New("压缩包文件数过多")
	}
	var total int64
	for _, f := range zr.File {
		target, err := safeJoin(dest, f.Name)
		if err != nil {
			return err
		}
		mode := f.Mode()
		switch {
		case mode&fs.ModeSymlink != 0:
			return fmt.Errorf("压缩包包含符号链接: %q", f.Name)
		case f.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		case !mode.IsRegular():
			return fmt.Errorf("压缩包包含不支持的文件类型: %q", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		n, err := extractFile(f, target, maxZipTotal-total)
		if err != nil {
			return err
		}
		total += n
	}
	return nil
}

func extractFile(f *zip.File, target string, budget int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	w, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(w, io.LimitReader(rc, budget+1))
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	if n > budget {
		return n, errors.New("压缩包解压后过大")
	}
	return n, nil
}

// locatePackage 找到包含 plugin.yaml 的目录：解压根目录，或唯一的顶层子目录。
func locatePackage(dir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, plugin.ManifestFile)); err == nil {
		return dir, nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range ents {
		if e.IsDir() && e.Name() != "__MACOSX" {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), plugin.ManifestFile)); err == nil {
				found = append(found, filepath.Join(dir, e.Name()))
			}
		}
	}
	if len(found) != 1 {
		return "", errors.New("压缩包里找不到唯一的 plugin.yaml")
	}
	return found[0], nil
}

// copyTree 递归拷贝目录；符号链接等非常规文件一律拒绝。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			return copyFile(p, target)
		}
		return fmt.Errorf("不支持的文件类型（符号链接？）: %s", p)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close() //nolint:errcheck
		return err
	}
	return out.Close()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func cmdInstall(args []string, out io.Writer) error {
	fl := newFlags("install")
	force := fl.Bool("force", false, "已安装时覆盖")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 {
		return errors.New("用法: nexadmin plugin install [--force] <目录或zip>")
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	id, err := install(root, fl.Arg(0), *force)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "插件 %s 已安装。请重新编译并重启。\n", id)
	return nil
}

// installWarn 是 install 的警告输出（测试可替换）。
var installWarn io.Writer = os.Stderr

// install 校验清单 -> 拷贝 backend/ 与 web/ -> 更新 plugins.go。返回插件 id。
func install(root, src string, force bool) (string, error) {
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	dir := src
	if !st.IsDir() {
		tmp, err := os.MkdirTemp("", "nexadmin-plugin-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp) //nolint:errcheck
		if err := extractZip(src, tmp); err != nil {
			return "", err
		}
		if dir, err = locatePackage(tmp); err != nil {
			return "", err
		}
	}
	man, err := plugin.LoadManifest(dir)
	if err != nil {
		return "", err
	}
	if err := plugin.CheckRequires(man.RequiresNexadmin, plugin.FrameworkVersion(root, "")); err != nil {
		if !force {
			return "", fmt.Errorf("%w（用 --force 跳过校验，但插件可能无法运行）", err)
		}
		fmt.Fprintf(installWarn, "警告：%v；已用 --force 跳过\n", err)
	}
	backend, web := filepath.Join(dir, "backend"), filepath.Join(dir, "web")
	if !isDir(backend) {
		return "", errors.New("插件包缺少 backend/ 目录")
	}
	mod, err := modulePath(root)
	if err != nil {
		return "", err
	}
	dstBackend := filepath.Join(root, "plugins", man.ID)
	dstWeb := filepath.Join(root, "web", "src", "plugins", man.ID)
	if !force && (exists(dstBackend) || exists(dstWeb)) {
		return "", fmt.Errorf("插件 %s 已安装（用 --force 覆盖，或先 remove）", man.ID)
	}
	// 先算出新的 plugins.go：标记行有问题就在动文件之前失败。
	goFile := filepath.Join(root, "plugins", "plugins.go")
	oldGo, err := os.ReadFile(goFile)
	if err != nil {
		return "", err
	}
	newGo, err := updatePluginsFile(oldGo, mod+"/plugins/"+man.ID, true)
	if err != nil {
		return "", err
	}

	// 先拷到同盘的临时目录，成功后再换入，避免半成品。
	stageB, err := os.MkdirTemp(filepath.Join(root, "plugins"), ".install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stageB) //nolint:errcheck
	if err := copyTree(backend, stageB); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(dir, plugin.ManifestFile), filepath.Join(stageB, plugin.ManifestFile)); err != nil {
		return "", err
	}
	var stageW string
	if isDir(web) {
		if err := os.MkdirAll(filepath.Dir(dstWeb), 0o755); err != nil {
			return "", err
		}
		if stageW, err = os.MkdirTemp(filepath.Dir(dstWeb), ".install-"); err != nil {
			return "", err
		}
		defer os.RemoveAll(stageW) //nolint:errcheck
		if err := copyTree(web, stageW); err != nil {
			return "", err
		}
	}
	if err := os.RemoveAll(dstBackend); err != nil {
		return "", err
	}
	if err := os.Rename(stageB, dstBackend); err != nil {
		return "", err
	}
	if stageW != "" {
		if err := os.RemoveAll(dstWeb); err != nil {
			return "", err
		}
		if err := os.Rename(stageW, dstWeb); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(goFile, newGo, 0o644); err != nil {
		return "", err
	}
	return man.ID, nil
}

func cmdRemove(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("用法: nexadmin plugin remove <id>")
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	if err := remove(root, args[0]); err != nil {
		return err
	}
	fmt.Fprintf(out, "插件 %s 已移除（数据表与授权文件保留）。请重新编译并重启。\n", args[0])
	return nil
}

func remove(root, id string) error {
	if !plugin.ValidID(id) {
		return fmt.Errorf("非法的插件 id %q", id)
	}
	mod, err := modulePath(root)
	if err != nil {
		return err
	}
	dstBackend := filepath.Join(root, "plugins", id)
	dstWeb := filepath.Join(root, "web", "src", "plugins", id)
	if !exists(dstBackend) && !exists(dstWeb) {
		return fmt.Errorf("插件 %s 未安装", id)
	}
	if err := rewritePluginsFile(root, mod+"/plugins/"+id, false); err != nil {
		return err
	}
	if err := os.RemoveAll(dstBackend); err != nil {
		return err
	}
	return os.RemoveAll(dstWeb)
}
