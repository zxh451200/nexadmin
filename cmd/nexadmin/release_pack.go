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
	"strings"
)

// release pack：在模板根目录生成干净的框架源码包（zip，根目录 nexadmin-<framework_version>/），
// 供 `nexadmin upgrade --from <zip>` 直接使用。排除规则与 `nexadmin new` 共用 newSkip，保证一致。

var packVersionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]*$`)

func cmdReleasePack(args []string, out io.Writer) error {
	fl := newFlags("release pack")
	var outPath string
	var force bool
	fl.StringVar(&outPath, "out", "", "输出 zip 路径（默认 <模板根>/../nexadmin-<版本>.zip）")
	fl.BoolVar(&force, "force", false, "存在 FAIL 项也打包（仅用于内部测试）")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 0 {
		return errors.New("用法: nexadmin release pack [--out <路径.zip>] [--force]")
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	return releasePack(root, outPath, force, out)
}

func releasePack(root, outPath string, force bool, out io.Writer) error {
	man, err := loadUpManifest(filepath.Join(root, frameworkManifest))
	if err != nil {
		return fmt.Errorf("读取框架清单失败: %w", err)
	}
	if man.isProduct() {
		return errors.New("当前工程的清单是产品清单，release pack 只能在框架模板根运行")
	}
	if !packVersionRe.MatchString(man.FrameworkVersion) {
		return fmt.Errorf("framework_version %q 不能用作目录名", man.FrameworkVersion)
	}
	var fails []relItem
	for _, it := range releaseChecks(root) {
		if it.level == "FAIL" {
			fails = append(fails, it)
		}
	}
	if len(fails) > 0 {
		for _, f := range fails {
			fmt.Fprintf(out, "[FAIL] %s\n", f.msg)
		}
		if !force {
			return fmt.Errorf("发布前检查有 %d 项 FAIL，拒绝打包（--force 仅用于内部测试）", len(fails))
		}
		fmt.Fprintf(out, "警告: --force 忽略了 %d 项 FAIL，该包不得对外发布。\n", len(fails))
	}
	top := "nexadmin-" + man.FrameworkVersion
	if outPath == "" {
		outPath = filepath.Join(filepath.Dir(root), top+".zip")
	}
	if !strings.HasSuffix(strings.ToLower(outPath), ".zip") {
		return fmt.Errorf("--out 必须是 .zip 路径: %s", outPath)
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	if st, err := os.Stat(absOut); err == nil && !st.Mode().IsRegular() {
		return fmt.Errorf("输出路径已存在且不是普通文件: %s", absOut)
	}
	if err := os.MkdirAll(filepath.Dir(absOut), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(absOut), ".nexadmin-pack-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // 成功后已 rename，失败时清理
	n, total, err := writePackZip(tmp, root, top, absOut, tmp.Name())
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), absOut); err != nil {
		return err
	}
	sum, err := upHashFile(absOut)
	if err != nil {
		return err
	}
	st, err := os.Stat(absOut)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "已生成 %s\n  根目录 %s/，文件数 %d，源码 %d 字节，zip %d 字节\n  sha256 %s\n", absOut, top, n, total, st.Size(), sum)
	return nil
}

// writePackZip 把 root 按 newSkip 的排除集合写成 zip；skipAbs 是输出文件及其临时文件（防止把自己打进去）。
func writePackZip(w io.Writer, root, top string, skipAbs ...string) (int, int64, error) {
	zw := zip.NewWriter(w)
	n := 0
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, err := filepath.Rel(root, p)
		if err != nil || r == "." {
			return err
		}
		rel := filepath.ToSlash(r)
		if newSkip(rel, d.IsDir(), false) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if abs, _ := filepath.Abs(p); containsStr(skipAbs, abs) {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("源码含非常规文件（符号链接？），无法打包: %s", rel)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if rel == "plugins/plugins.go" { // 与 new 一致：清空已安装插件的空导入，保留标记行
			if raw, err = clearPluginImports(raw); err != nil {
				return err
			}
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = top + "/" + rel
		h.Method = zip.Deflate
		h.SetMode(info.Mode().Perm())
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := fw.Write(raw); err != nil {
			return err
		}
		n++
		total += int64(len(raw))
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	if n > maxZipFiles || total > maxZipTotal {
		return 0, 0, fmt.Errorf("源码包过大（%d 个文件 / %d 字节），超过 upgrade 解压上限", n, total)
	}
	return n, total, zw.Close()
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
