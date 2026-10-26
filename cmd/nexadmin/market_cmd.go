package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"nexadmin/core/plugin/market"
)

// cmdMarket 实现 `nexadmin plugin market sync`：把市场目录（config/plugin-market.yaml）
// 导出为 JSON，供外部静态站/工具使用。官网与后台直接读这份 YAML，不依赖本命令。
func cmdMarket(args []string, out io.Writer) error {
	if len(args) < 1 || args[0] != "sync" {
		return errors.New("用法: nexadmin plugin market sync [--docs <目录>]（省略 --docs 打印到标准输出）")
	}
	fs := flag.NewFlagSet("market sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	docs := fs.String("docs", "", "输出目录（写入其 data/plugins.json）")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("用法: nexadmin plugin market sync [--docs <目录>]")
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	entries, err := market.LoadCatalog(root)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("市场目录为空（config/plugin-market.yaml 没有插件条目）")
	}
	raw, err := market.RenderDocsJSON(entries)
	if err != nil {
		return err
	}
	if *docs == "" {
		_, err = out.Write(raw)
		return err
	}
	dst := filepath.Join(*docs, "data", "plugins.json")
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "已导出 %d 条插件到 %s\n", len(entries), dst)
	return nil
}
