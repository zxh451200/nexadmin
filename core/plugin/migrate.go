package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/migrate"
	"nexadmin/core/seed"
)

// migrationSource 是 <root>/plugins/<id>/migrations/<引擎>/ 对应的迁移 scope（plugin:<id>）。
func migrationSource(root, id string) migrate.Source {
	return migrate.Source{Scope: migrate.ScopePrefix + id, Root: filepath.Join(root, "plugins", id, "migrations"), Target: migrate.TargetPlatform}
}

// RunMigrations 执行插件自己的迁移，实现全部在 core/migrate（版本表 sys_migration，scope=plugin:<id>）。
// 目录不存在视为没有迁移；升级前记录在 sys_plugin_migration 的文件按基线导入。
func RunMigrations(db *gorm.DB, root, id string) error {
	man, err := LoadManifest(filepath.Join(root, "plugins", id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	src := migrationSource(root, id)
	if man != nil && man.MigrationTarget != "" {
		src.Target = man.MigrationTarget
	}
	pools := database.TenantDatabases(db)
	targets := []*gorm.DB{database.Platform(db)}
	if len(pools) != 0 && src.Target == migrate.TargetTenant {
		targets = nil
		ids := make([]string, 0, len(pools))
		for id := range pools {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			targets = append(targets, pools[id])
		}
	}
	for _, target := range targets {
		st, err := migrate.NewGormStore(target)
		if err != nil {
			return err
		}
		if _, err := migrate.NewRunner(st, nil, src).Up(); err != nil {
			return fmt.Errorf("plugin %s migration: %w", id, err)
		}
	}
	return nil
}

// SeedMenus 幂等补齐菜单（走 core/seed 的统一入口）：按 code 判断，已存在（哪怕被改过）一律不动；
// 父节点按 code 解析，须出现在子节点之前或已存在。
func SeedMenus(db *gorm.DB, specs []MenuSpec) error {
	items := make([]seed.MenuItem, len(specs))
	for i, s := range specs {
		items[i] = seed.MenuItem{
			Parent: s.Parent, Code: s.Code(), Type: s.Type, Title: s.Title, Icon: s.Icon, Path: s.Path,
			Component: s.Component, Perm: s.Perm, Sort: s.Sort, KeepAlive: s.KeepAlive, Hidden: s.Hidden,
		}
	}
	_, err := seed.Menus(db, items)
	return err
}

// MigrationSources / SeedOnStartup 让已挂载（授权有效）的插件走和内置模块相同的启动流程：
// core 在启动时统一执行迁移、补种菜单；未授权的插件不会被挂载，也就不会迁移和种菜单。
func (w *mounted) MigrationSources() []migrate.Source {
	src := migrationSource(w.m.opts.Root, w.e.p.ID)
	if w.e.man.MigrationTarget != "" {
		src.Target = w.e.man.MigrationTarget
	}
	return []migrate.Source{src}
}

func (w *mounted) SeedOnStartup(db *gorm.DB) error { return SeedMenus(db, w.e.man.Menus) }
