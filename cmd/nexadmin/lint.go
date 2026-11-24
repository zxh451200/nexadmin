package main

import (
	"errors"
	"fmt"
	"io"

	"nexadmin/core/plugin"
	"nexadmin/pkg/pluginlint"
)

func cmdLint(args []string, out io.Writer) error {
	fl := newFlags("lint")
	layout := fl.String("layout", "package", "布局：package（plugin.yaml + backend/ + web/）或 source（项目源码插件 id）")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 {
		return errors.New("用法: nexadmin plugin lint [--layout package|source] <插件目录|插件 id>")
	}
	var man *plugin.Manifest
	var err error
	switch *layout {
	case "package":
		man, err = pluginlint.Lint(fl.Arg(0))
	case "source":
		var root string
		root, err = findRoot()
		if err == nil {
			man, err = pluginlint.LintSource(root, fl.Arg(0))
		}
	default:
		return fmt.Errorf("未知布局 %q（仅支持 package 或 source）", *layout)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "校验通过：%s %s %s；Go 注册、入口 init、迁移集合、路由/菜单、mock 与双语词条一致。\n", man.ID, man.Version, man.Edition)
	fmt.Fprintln(out, "边界：Go 注册需直接 Plugin 字面量（字段支持字符串常量、plugin.Free/Pro）；插件入口 init 只允许静态注册，不接受运行期订阅或任务注册。路由需 export const routes 的静态对象数组。i18n 检查静态 t(...)、i18nKey 和路由标题，动态写法拒绝；不验证别名翻译函数、运行时组合、TS 类型或页面样式。发布前仍须构建、测试与人工审核。")
	return nil
}
