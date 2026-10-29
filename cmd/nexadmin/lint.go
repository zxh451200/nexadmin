package main

// plugin lint：上架前的清单与结构自检（P2-11）。不连数据库、不改文件：
// 校验 plugin.yaml 字段与约定、迁移目录三方言齐全、菜单权限码前缀。
// 校验逻辑复用 core/plugin 的 ParseManifest/Validate，与安装/挂载保持同一套规则。

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	dir := fl.Arg(0)
	if st, err := os.Stat(filepath.Join(dir, plugin.ManifestFile)); err != nil || st.IsDir() {
		if err == nil {
			return fmt.Errorf("%s 是目录，不是 %s", dir, plugin.ManifestFile)
		}
		return fmt.Errorf("%s: 找不到 %s", dir, plugin.ManifestFile)
	}
	man, err := plugin.LoadManifest(dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "清单 %s 解析通过：id=%s edition=%s version=%s\n", plugin.ManifestFile, man.ID, man.Edition, man.Version)

	// 迁移目录：backend/migrations/<engine> 三引擎（backend 目录存在时才检查；纯前端插件跳过）。
	if st, err := os.Stat(filepath.Join(dir, "backend")); err == nil && st.IsDir() {
		var missing []string
		for _, e := range []string{"mysql", "mariadb", "postgres"} {
			mig := filepath.Join(dir, "backend", "migrations", e)
			ents, rerr := os.ReadDir(mig)
			if rerr != nil || len(ents) == 0 {
				missing = append(missing, e)
				continue
			}
			foundSQL := false
			for _, f := range ents {
				if strings.HasSuffix(f.Name(), ".sql") {
					foundSQL = true
					break
				}
			}
			if !foundSQL {
				missing = append(missing, e+"（无 .sql）")
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("迁移目录不完整：缺 %s（三方言都必须有）", strings.Join(missing, ", "))
		}
		fmt.Fprintln(out, "迁移目录三方言齐全")
	}

	fmt.Fprintf(out, "校验通过。提交前请再确认：mock 文件存在、无硬编码颜色、前后端构建通过（见 plugin-development.md 第 8 节）。\n")
	return nil
}
