package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"sort"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/migrate"
	"nexadmin/core/seed"
	"nexadmin/core/setup"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// migrator 装配统一迁移运行器：框架 + 业务 + 模块（已授权的插件）声明的迁移目录。
func (a *App) migrator(db *gorm.DB) (*migrate.Runner, error) {
	srcs, err := migrate.DefaultSources()
	if err != nil {
		return nil, err
	}
	extra := a.migSources
	if len(database.TenantDatabases(db)) > 0 {
		// framework 控制面只在平台库迁移；业务迁移在每个租户库独立记录。
		srcs = srcs[:1]
		extra = nil
		for _, src := range a.migSources {
			switch src.Target {
			case migrate.TargetPlatform:
				extra = append(extra, src)
			case migrate.TargetTenant:
				// Each tenant receives this source through tenantMigrators.
			default:
				return nil, fmt.Errorf("database: migration source %s requires an explicit platform or tenant target", src.Scope)
			}
		}
	}
	st, err := migrate.NewGormStore(database.Platform(db))
	if err != nil {
		return nil, err
	}
	return migrate.NewRunner(st, a.log.Sugar(), append(srcs, extra...)...), nil
}

// runMigrations 是初始化向导 / init 用的迁移入口（setup.Hooks.Migrate）：自己拿锁，执行全部待迁移文件。
func (a *App) runMigrations(db *gorm.DB) error {
	rn, err := a.migrator(db)
	if err != nil {
		return err
	}
	res, err := rn.Up()
	if err != nil {
		return err
	}
	a.logApplied(res)
	return a.migrateTenants(db, a.cfg.Database.Isolation.AutoMigrate)
}

func (a *App) tenantMigrators(db *gorm.DB) ([]*migrate.Runner, []string, error) {
	pools := database.TenantDatabases(db)
	var ids []string
	for id := range pools {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil, nil, nil
	}
	_, business, err := migrate.Roots()
	if err != nil {
		return nil, nil, err
	}
	var runners []*migrate.Runner
	sources := []migrate.Source{{Scope: migrate.ScopeBusiness, Root: business, Target: migrate.TargetTenant}}
	for _, src := range a.migSources {
		if src.Target == migrate.TargetTenant {
			sources = append(sources, src)
		}
	}
	for _, id := range ids {
		st, err := migrate.NewGormStore(pools[id])
		if err != nil {
			return nil, nil, fmt.Errorf("tenant %s migration: %w", id, err)
		}
		runners = append(runners, migrate.NewRunner(st, a.log.Sugar(), sources...))
	}
	return runners, ids, nil
}

func (a *App) migrateTenants(db *gorm.DB, auto bool) error {
	runners, ids, err := a.tenantMigrators(db)
	if err != nil {
		return err
	}
	for i, rn := range runners {
		err := rn.Exclusive(func() error {
			res, e := rn.Startup(auto)
			if e == nil {
				a.logApplied(res)
			}
			return e
		})
		if err != nil {
			return fmt.Errorf("tenant %s migration: %w", ids[i], err)
		}
	}
	return nil
}

func (a *App) logApplied(res *migrate.Result) {
	if res == nil {
		return
	}
	for _, f := range res.Applied {
		if f.Baseline {
			a.log.Info("迁移已记录（表已存在，基线）", zap.String("scope", f.Scope), zap.String("file", f.File))
		} else {
			a.log.Info("迁移已执行", zap.String("scope", f.Scope), zap.String("file", f.File))
		}
	}
}

// prepareData 在已初始化的进程里启动时运行：持锁 -> 迁移（auto_migrate=false 时只检查）-> 补种缺失的出厂数据。
// 多实例同时启动时只有一个在做，其余等锁后发现已完成即跳过。
func (a *App) prepareData(db *gorm.DB) error {
	rn, err := a.migrator(db)
	if err != nil {
		return err
	}
	rep := &seed.Report{}
	var res *migrate.Result
	err = rn.Exclusive(func() error {
		var e error
		if res, e = rn.Startup(a.cfg.App.AutoMigrate); e != nil {
			return e
		}
		return a.seedStartup(db, rep)
	})
	if err != nil {
		return err
	}
	a.logApplied(res)
	if n := rep.Menus(); n > 0 {
		a.log.Info(fmt.Sprintf("新增 %d 个菜单，请到角色管理分配", n))
	}
	if sk := rep.Skipped(); len(sk) > 0 {
		a.log.Warn("部分出厂菜单的父节点不存在，已跳过", zap.Strings("menus", sk))
	}
	if len(res.Applied) == 0 && rep.Menus() == 0 {
		a.log.Info("迁移与种子已是最新")
	}
	return a.migrateTenants(db, a.cfg.App.AutoMigrate && a.cfg.Database.Isolation.AutoMigrate)
}

// seedStartup 依次运行实现了 StartupSeeder 的模块种子。
func (a *App) seedStartup(db *gorm.DB, rep *seed.Report) error {
	sess := db.WithContext(seed.WithReport(tenant.Skip(context.Background()), rep))
	for _, s := range a.startupSeeders {
		if err := s.SeedOnStartup(sess); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
	}
	return nil
}

// MigrateOptions 是 `nexadmin-server migrate` 的选项。
type MigrateOptions struct {
	Tenant      string    // empty: all; platform: control plane; otherwise one configured tenant ID
	Status      bool      // 列出各 scope 已执行 / 待执行文件与 checksum 异常
	DryRun      bool      // 只列出将执行的文件，不改库
	MarkApplied string    // <scope>/<文件名>：不执行 SQL，手工标记为已执行
	Unmark      string    // <scope>/<文件名>：撤销手工标记
	Yes         bool      // 跳过交互确认（自动化）
	In          io.Reader // 交互确认的输入；nil 视为无输入（等同于不确认）
}

// confirmer 返回确认函数：--yes 直接通过；否则打印提示并读一行，只有 y / yes 才算确认（默认 N）。
func confirmer(o MigrateOptions, out io.Writer) migrate.Confirm {
	if o.Yes {
		return func(string) (bool, error) { return true, nil }
	}
	return func(prompt string) (bool, error) {
		if o.In == nil {
			return false, nil
		}
		fmt.Fprintf(out, "%s [y/N]: ", prompt)
		line, err := bufio.NewReader(o.In).ReadString('\n')
		if err != nil && line == "" { // 无输入（EOF / 非交互）视为不确认
			fmt.Fprintln(out)
			return false, nil
		}
		ans := strings.ToLower(strings.TrimSpace(line))
		return ans == "y" || ans == "yes", nil
	}
}

// operator 返回写入日志的操作者：系统用户名@主机名。
func operator() string {
	name, host := "unknown", "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		host = h
	}
	return name + "@" + host
}

// Migrate 是 `nexadmin-server migrate [--status] [--dry-run] [--mark-applied X] [--unmark X] [--yes]`：不启动服务，只处理迁移。
// 未初始化时什么都不做（初始化向导 / init 会负责建表）。
func (a *App) Migrate(o MigrateOptions, out io.Writer) error {
	log, err := newLogger(a.cfg)
	if err != nil {
		return err
	}
	defer log.Sync() //nolint:errcheck
	a.log = log
	mods, err := a.activeModules()
	if err != nil {
		return err
	}
	a.collect(mods)
	if !setup.IsInitialized(a.cfg, a.hooks.AdminChecker, log) {
		fmt.Fprintln(out, "系统尚未初始化，无需迁移（初始化向导 / init 会自动建表）")
		return nil
	}
	if err := idgen.Init(int64(a.cfg.Database.NodeID)); err != nil {
		return err
	}
	db, err := database.Open(a.cfg.Database)
	if err != nil {
		return err
	}
	defer database.Close(db)
	rn, err := a.migrator(db)
	if err != nil {
		return err
	}
	runners, ids, err := a.tenantMigrators(db)
	if err != nil {
		return err
	}
	if len(ids) > 0 && o.Tenant == "" && (o.MarkApplied != "" || o.Unmark != "") {
		return errors.New("独立库手工迁移标记必须指定 --tenant platform 或具体租户 ID")
	}
	if o.Tenant != "" && o.Tenant != "platform" {
		found := false
		for _, id := range ids {
			found = found || id == o.Tenant
		}
		if !found {
			return errors.New("--tenant 必须为 platform 或已配置的规范租户 ID")
		}
	}
	if o.Tenant == "" || o.Tenant == "platform" {
		if err := runMigrationCommand(rn, o, out); err != nil {
			return err
		}
	}
	for i, tr := range runners {
		if o.Tenant != "" && o.Tenant != ids[i] {
			continue
		}
		fmt.Fprintf(out, "租户 %s:\n", ids[i])
		if err := runMigrationCommand(tr, o, out); err != nil {
			return fmt.Errorf("tenant %s: %w", ids[i], err)
		}
	}
	return nil
}

func runMigrationCommand(rn *migrate.Runner, o MigrateOptions, out io.Writer) error {
	switch {
	case o.MarkApplied != "" && o.Unmark != "":
		return errors.New("--mark-applied 与 --unmark 不能同时使用")
	case o.MarkApplied != "":
		if err := rn.MarkApplied(o.MarkApplied, operator(), confirmer(o, out)); err != nil {
			return err
		}
		fmt.Fprintf(out, "已把 %s 标记为已执行（手工标记，未运行 SQL）\n", o.MarkApplied)
		return nil
	case o.Unmark != "":
		if err := rn.Unmark(o.Unmark, operator(), confirmer(o, out)); err != nil {
			return err
		}
		fmt.Fprintf(out, "已撤销 %s 的手工标记，它重新变为待执行\n", o.Unmark)
		return nil
	case o.Status:
		return printStatus(rn, out)
	case o.DryRun:
		return printDryRun(rn, out)
	}
	res, err := rn.Up()
	if err != nil {
		return err
	}
	if len(res.Applied) == 0 {
		fmt.Fprintln(out, "迁移已是最新")
		return nil
	}
	for _, f := range res.Applied {
		note := ""
		if f.Baseline {
			note = "（表已存在，仅记录）"
		}
		fmt.Fprintf(out, "已执行 %s/%s%s\n", f.Scope, f.File, note)
	}
	return nil
}

func printStatus(rn *migrate.Runner, out io.Writer) error {
	st, err := rn.Status()
	if err != nil {
		return err
	}
	var applied, pending, bad int
	scope := ""
	for _, s := range st {
		if s.Scope != scope {
			scope = s.Scope
			fmt.Fprintf(out, "[%s]\n", scope)
		}
		switch s.State {
		case migrate.StateApplied:
			applied++
			if s.Manual {
				fmt.Fprintf(out, "  手工标记  %s  %.12s\n", s.Name, s.Checksum)
			} else {
				fmt.Fprintf(out, "  已执行    %s  %.12s\n", s.Name, s.Checksum)
			}
		case migrate.StatePending:
			pending++
			fmt.Fprintf(out, "  待执行    %s  %.12s\n", s.Name, s.Checksum)
		default:
			bad++
			fmt.Fprintf(out, "  CHECKSUM 异常  %s  记录 %.12s，现在 %.12s\n", s.Name, s.Recorded, s.Checksum)
		}
	}
	fmt.Fprintf(out, "已执行 %d，待执行 %d，checksum 异常 %d\n", applied, pending, bad)
	if bad > 0 {
		return errors.New("存在 checksum 异常的迁移文件（违反“只增不改”）")
	}
	return nil
}

func printDryRun(rn *migrate.Runner, out io.Writer) error {
	items, err := rn.Plan()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "没有待执行的迁移")
		return nil
	}
	for _, it := range items {
		if it.Baseline {
			fmt.Fprintf(out, "将记录 %s/%s（表已存在，不执行）\n", it.Scope, it.Name)
		} else {
			fmt.Fprintf(out, "将执行 %s/%s（%d 条语句）\n", it.Scope, it.Name, len(it.Stmts))
		}
		for _, w := range it.Warnings {
			fmt.Fprintln(out, "  WARN:", strings.TrimSpace(w))
		}
	}
	fmt.Fprintf(out, "共 %d 个待执行迁移（dry-run，未改动数据库）\n", len(items))
	return nil
}
