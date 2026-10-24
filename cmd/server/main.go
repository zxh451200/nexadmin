package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"nexadmin/core"
	"nexadmin/core/config"
	"nexadmin/core/ctl"
	"nexadmin/core/plugin"
	"nexadmin/internal/boot"      // 二开定制入口（用户区）：boot.Options() 返回要追加的 core.Option
	_ "nexadmin/internal/modules" // 业务模块（含示例模块）在各自包的 init() 里注册，这里只需引入；本文件不引用任何具体业务模块
	_ "nexadmin/modules"          // 框架内置模块在各自包的 init() 里 core.RegisterBuiltin，这里只需引入
	_ "nexadmin/plugins"          // 已安装的源码插件在各自包的 init() 里 plugin.Register
	"nexadmin/web"                // 前端产物：-tags embedweb 时嵌入二进制，否则 Dist() 为 nil（开发走 vite）
)

// version 由 scripts/build.sh 通过 -ldflags "-X main.version=..." 注入（取 .nexadmin.manifest.json 的 framework_version）。
var version = "dev"

const usage = `用法: nexadmin-server [命令]
  (无命令)            前台运行
  version             打印版本号
  start|stop|restart  后台启动 / 停止 / 重启
  status              查看运行状态
  init --config <文件> 非交互初始化（容器 / 自动化）
  migrate             执行待迁移（app.auto_migrate=false 的生产环境升级后先运行它）
  migrate --status    列出各 scope 已执行 / 待执行文件与 checksum 异常
  migrate --dry-run   只列出将执行的迁移，不改库
  migrate --mark-applied <scope>/<文件名> [--yes]
                      迁移中途失败（MySQL DDL 无法回滚）、已手工修好库后，把该文件标记为已执行；
                      不运行其中任何 SQL，需交互确认（--yes 跳过）；scope: framework | business | plugin:<id>
  migrate --unmark <scope>/<文件名> [--yes]
                      撤销手工标记（只能撤销手工标记的记录，不能撤销真实执行的记录）
  backup --output <新目录>
                      导出平台库及全部配置租户库，带 manifest 与 sha256；顺序备份，非原子快照
  restore-bundle --dir <目录> [--tenant <ID>] (--dry-run | --yes)
                      先校验全部成员及目标，必须停服务；导入前备份所有库；
                      --tenant 只恢复指定租户；失败可能部分恢复，不自动回滚
  reinit [--dry-run] [--platform-only]
                      恢复出厂（先列清单再确认）；隔离模式必须明确 --platform-only
  restore <备份文件> [--yes] [--dry-run] [--sha256 <值>] [--platform-only]
                      从备份恢复数据库（必须先 stop；先校验 sha256、列出将覆盖的库、交互确认，
                      恢复前自动再做一份“恢复前备份”；--dry-run 只列清单；
                      隔离模式必须明确 --platform-only，仅操作平台库；
                      reinit 仍会清理共享缓存、上传文件及初始化配置）`

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if cmd == "version" || cmd == "--version" {
		fmt.Println(version)
		return
	}
	if cmd == "" {
		cfg := mustConfig()
		if err := newApp(cfg).Run(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := runCommand(cmd, os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func mustConfig() *config.Config {
	cfg, err := core.LoadConfig("config/default.yaml", "config/config.yaml")
	if err != nil {
		log.Fatal(err)
	}
	return cfg
}

// newApp 装配应用；前台运行和 init 共用，保证两者看到同样的模块与插件。
func newApp(cfg *config.Config) *core.App {
	app := core.New(cfg, append([]core.Option{core.WithStatic(web.Dist())}, boot.Options()...)...)
	plugin.Mount(app, plugin.Options{FrameworkVersion: version}) // 评估插件授权，有效的挂载；业务模块已在各自包的 init() 里注册，这里不需要改
	return app
}

// runCommand 分发内置子命令；逻辑都在 core/ctl 与 core/setup，这里只做参数解析。
func runCommand(cmd string, args []string) error {
	cfg := mustConfig()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	c := ctl.New(cfg, exe, os.Stdout)
	switch cmd {
	case "start":
		return c.Start()
	case "stop":
		return c.Stop()
	case "restart":
		return c.Restart()
	case "status":
		return c.Status()
	case "init":
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		file := fs.String("config", "", "初始化参数文件（JSON 或 YAML，格式同向导请求体）")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *file == "" {
			return errors.New("init 需要 --config <文件>")
		}
		return newApp(cfg).Init(context.Background(), *file, os.Stdout)
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
		target := fs.String("tenant", "", "platform 或具体租户 ID；默认全部，独立库手工标记时必填")
		status := fs.Bool("status", false, "列出各 scope 已执行 / 待执行文件与 checksum 异常")
		dry := fs.Bool("dry-run", false, "只列出将执行的迁移，不改库")
		mark := fs.String("mark-applied", "", "不执行 SQL，把 <scope>/<文件名> 手工标记为已执行")
		unmark := fs.String("unmark", "", "撤销 <scope>/<文件名> 的手工标记")
		yes := fs.Bool("yes", false, "跳过交互确认（自动化）")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return newApp(cfg).Migrate(core.MigrateOptions{Tenant: *target, Status: *status, DryRun: *dry, MarkApplied: *mark, Unmark: *unmark, Yes: *yes, In: os.Stdin}, os.Stdout)
	case "backup":
		fs := flag.NewFlagSet("backup", flag.ContinueOnError)
		output := fs.String("output", "", "新的、不存在的输出目录（父目录必须已存在）")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *output == "" || fs.NArg() != 0 {
			return errors.New("backup requires --output <new-directory> and no positional arguments")
		}
		return ctl.BackupBundle(context.Background(), cfg, *output, os.Stdout)
	case "restore-bundle":
		fs := flag.NewFlagSet("restore-bundle", flag.ContinueOnError)
		dir := fs.String("dir", "", "完整备份 bundle 目录")
		tenant := fs.String("tenant", "", "仅恢复此规范租户 ID；默认恢复全部")
		dry := fs.Bool("dry-run", false, "完整校验及列出目标，不连接数据库")
		yes := fs.Bool("yes", false, "确认顺序覆盖目标库，失败可能部分恢复")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *dir == "" || fs.NArg() != 0 || (*dry && *yes) || (!*dry && !*yes) {
			return errors.New("restore-bundle requires --dir <directory> and exactly one of --dry-run / --yes")
		}
		return ctl.RestoreBundle(context.Background(), cfg, c, ctl.RestoreBundleOptions{Dir: *dir, Tenant: *tenant, DryRun: *dry, Yes: *yes}, os.Stdout)
	case "restore":
		fs := flag.NewFlagSet("restore", flag.ContinueOnError)
		yes := fs.Bool("yes", false, "跳过交互确认（自动化）")
		dry := fs.Bool("dry-run", false, "只列清单，不执行")
		sum := fs.String("sha256", "", "期望的 sha256（没有 .sha256 旁路文件时用）")
		platformOnly := fs.Bool("platform-only", false, "隔离模式明确仅恢复平台库")
		file, rest := "", args
		for len(rest) > 0 { // 允许 <备份文件> 出现在选项前后
			if err := fs.Parse(rest); err != nil {
				return err
			}
			if rest = fs.Args(); len(rest) > 0 {
				if file != "" {
					return errors.New("restore 只接受一个备份文件")
				}
				file, rest = rest[0], rest[1:]
			}
		}
		if file == "" {
			return errors.New("restore 需要 <备份文件>")
		}
		r, err := ctl.NewRestore(cfg, c, version, os.Stdin, os.Stdout, *platformOnly)
		if err != nil {
			return err
		}
		defer r.Close()
		return r.Run(context.Background(), ctl.RestoreOptions{File: file, SHA256: *sum, Yes: *yes, DryRun: *dry})
	case "reinit":
		fs := flag.NewFlagSet("reinit", flag.ContinueOnError)
		dry := fs.Bool("dry-run", false, "只列清单，不执行")
		platformOnly := fs.Bool("platform-only", false, "隔离模式仅重置平台库；仍清理共享文件、缓存及配置")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("reinit accepts no positional arguments")
		}
		if _, err := os.Stat(cfg.UserConfigPath); err != nil {
			return fmt.Errorf("没有 %s，说明还没有初始化过，无需恢复出厂", cfg.UserConfigPath)
		}
		r, err := ctl.NewReinit(cfg, c, version, os.Stdin, os.Stdout, *platformOnly)
		if err != nil {
			return err
		}
		defer r.Close()
		return r.Run(context.Background(), *dry)
	}
	return fmt.Errorf("未知命令 %q\n%s", cmd, usage)
}
