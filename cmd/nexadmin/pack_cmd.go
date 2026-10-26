package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nexadmin/core/plugin"
	"nexadmin/core/plugin/market"
)

// cmdPack 实现 `nexadmin plugin pack <id>`：把已安装的插件（plugins/<id>/ + web/src/plugins/<id>/）
// 打成市场发行包（plugin.yaml + backend/ + web/ 结构的 zip），与 `plugin install` 的输入格式互逆。
// 市场目录（config/plugin-market.yaml）的 repo 条目指向的 GitHub 仓库内容即本命令的产物。
func cmdPack(args []string, out io.Writer) error {
	fl := newFlags("pack")
	dst := fl.String("out", "", "输出 zip 路径（默认 <id>-<version>.zip）")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 {
		return errors.New("用法: nexadmin plugin pack [--out <路径.zip>] <id>")
	}
	if *dst != "" && !strings.HasSuffix(*dst, ".zip") {
		return fmt.Errorf("输出路径 %q 不是 .zip", *dst)
	}
	id := fl.Arg(0)
	if !plugin.ValidID(id) {
		return fmt.Errorf("非法的插件 id %q", id)
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	backend := filepath.Join(root, "plugins", id)
	web := filepath.Join(root, "web", "src", "plugins", id)
	if !market.Exists(backend) && !market.Exists(web) {
		return fmt.Errorf("插件 %s 未安装", id)
	}
	man, err := plugin.LoadManifest(backend)
	if err != nil {
		return fmt.Errorf("读取清单失败: %w", err)
	}
	if *dst == "" {
		*dst = fmt.Sprintf("%s-%s.zip", id, man.Version)
	}
	if err := os.MkdirAll(filepath.Dir(*dst), 0o755); err != nil {
		return err
	}
	n, err := writePluginZip(*dst, map[string]string{
		"backend": backend,
		"web":     web, // 不存在时 addTree 跳过
	}, filepath.Join(backend, "plugin.yaml"))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "发行包已写入 %s（%d 个文件；plugin.yaml %s，结构 plugin.yaml + backend/ + web/，可直接 plugin install 或上传 GitHub 市场仓库）\n", *dst, n, man.Version)
	return nil
}

// writePluginZip 把若干源目录按包内前缀写入 zip，另把 manifestPath 指向的清单放到包根。
// 源目录不存在时跳过；符号链接等非常规文件拒绝（与安装器对称）。
func writePluginZip(dst string, trees map[string]string, manifestPath string) (int, error) {
	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck
	zw := zip.NewWriter(f)
	n := 0
	addFile := func(name string, src string) error {
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		if err == nil {
			n++
		}
		return err
	}
	addTree := func(prefix, src string) error {
		return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			name := prefix + "/" + filepath.ToSlash(rel)
			switch {
			case d.IsDir():
				return nil // zip 目录条目可省
			case d.Type().IsRegular():
				return addFile(name, p)
			}
			return fmt.Errorf("不支持的文件类型（符号链接？）: %s", p)
		})
	}
	// 清单先写：backend/ 里的同名副本会被后面拷贝的树覆盖为安装副本，保证与已安装内容一致。
	if market.Exists(manifestPath) {
		if err := addFile("plugin.yaml", manifestPath); err != nil {
			return 0, err
		}
	}
	// 固定顺序保证产物可重现。
	for _, prefix := range []string{"backend", "web"} {
		src, ok := trees[prefix]
		if !ok || !market.Exists(src) {
			continue
		}
		if err := addTree(prefix, src); err != nil {
			return 0, err
		}
	}
	if err := zw.Close(); err != nil {
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	return n, nil
}
