package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"nexadmin/core/plugin/market"
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
	id, err := market.Install(root, fl.Arg(0), *force)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "插件 %s 已安装。请重新编译并重启。\n", id)
	return nil
}

func cmdRemove(args []string, out io.Writer) error {
	fs := newFlags("remove")
	purge := fs.Bool("purge", false, "同时清理数据库：DROP plg_<id>_% 表并删除该插件的菜单（不可恢复）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("用法: nexadmin plugin remove [--purge] <id>")
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	return market.Remove(root, fs.Arg(0), *purge, out)
}
