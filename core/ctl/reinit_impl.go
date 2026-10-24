package ctl

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/database"
)

// NewReinit 用真实的数据库、缓存和备份工具装配 Reinit。version 是框架版本，写进备份文件名。连不上库或缓存会直接返回错误（读不到就中止）。
func NewReinit(cfg *config.Config, svc Service, version string, in io.Reader, out io.Writer, platformOnly ...bool) (*Reinit, error) {
	var err error
	cfg, err = legacyPlatformConfig(cfg, platformOnly)
	if err != nil {
		return nil, err
	}
	db, err := database.Open(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("连不上数据库，已中止: %w", err)
	}
	r := &Reinit{
		Cfg: cfg, Svc: svc, In: in, Out: out, Now: time.Now,
		DB:     &gormDB{db: db, cfg: cfg.Database},
		Backup: &gzipBackup{cfg: cfg.Database, version: version, kind: "reinit"}, // 与备份页同格式（gzip + sha256），restore 可直接恢复
	}
	if cfg.Cache.Driver == "redis" {
		cli := redis.NewClient(&redis.Options{Addr: cfg.Cache.Addr, Password: cfg.Cache.Password})
		pctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := cli.Ping(pctx).Err(); err != nil {
			_ = database.Close(db)
			return nil, fmt.Errorf("连不上缓存 %s，已中止: %w", cfg.Cache.Addr, err)
		}
		r.Cache = &redisPurger{cli: cli}
	}
	return r, nil
}

// Close 释放连接。
func (r *Reinit) Close() {
	_ = r.DB.Close()
	if r.Cache != nil {
		_ = r.Cache.Close()
	}
}

// ---- 数据库 ----

type gormDB struct {
	db  *gorm.DB
	cfg config.Database
}

func (g *gormDB) kind() string { k, _ := database.Normalize(g.cfg.Type); return k }

func (g *gormDB) Inspect(ctx context.Context) (DBInfo, error) {
	db := g.db.WithContext(ctx)
	nameSQL, tableSQL := "SELECT DATABASE()", `SELECT TABLE_NAME AS name, COALESCE(TABLE_ROWS, 0) AS n_rows, COALESCE(DATA_LENGTH + INDEX_LENGTH, 0) AS n_bytes
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`
	if g.kind() == "postgres" {
		nameSQL = "SELECT current_database()"
		tableSQL = `SELECT c.relname AS name, GREATEST(c.reltuples, 0)::bigint AS n_rows, pg_total_relation_size(c.oid)::bigint AS n_bytes
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p') ORDER BY c.relname`
	}
	var name sql.NullString
	if err := db.Raw(nameSQL).Row().Scan(&name); err != nil {
		return DBInfo{}, err
	}
	var rows []struct {
		Name   string
		NRows  int64
		NBytes int64
	}
	if err := db.Raw(tableSQL).Scan(&rows).Error; err != nil {
		return DBInfo{}, err
	}
	info := DBInfo{Engine: g.kind(), Host: g.cfg.Host, Port: g.cfg.Port, Name: name.String}
	for _, t := range rows {
		info.Tables = append(info.Tables, t.Name)
		info.Rows += t.NRows
		info.Bytes += t.NBytes
	}
	return info, nil
}

func quoteIdent(kind, s string) string {
	if kind == "postgres" {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// DropTables 删除库里所有表（库本身保留）。MySQL 临时关外键检查，Postgres 用 CASCADE。
func (g *gormDB) DropTables(ctx context.Context, tables []string) error {
	if len(tables) == 0 {
		return nil
	}
	quoted := make([]string, len(tables))
	for i, t := range tables {
		quoted[i] = quoteIdent(g.kind(), t)
	}
	list := strings.Join(quoted, ", ")
	if g.kind() == "postgres" {
		return g.db.WithContext(ctx).Exec("DROP TABLE IF EXISTS " + list + " CASCADE").Error
	}
	return g.db.WithContext(ctx).Connection(func(tx *gorm.DB) error { // 同一条连接上开关外键检查
		if err := tx.Exec("SET FOREIGN_KEY_CHECKS = 0").Error; err != nil {
			return err
		}
		defer tx.Exec("SET FOREIGN_KEY_CHECKS = 1")
		return tx.Exec("DROP TABLE IF EXISTS " + list).Error
	})
}

func (g *gormDB) Close() error { return database.Close(g.db) }

// ---- Redis / Valkey：只处理 nex:* ----

type redisPurger struct{ cli *redis.Client }

func (p *redisPurger) keys(ctx context.Context) ([]string, error) {
	var out []string
	it := p.cli.Scan(ctx, 0, cache.Prefix+"*", 500).Iterator()
	for it.Next(ctx) {
		out = append(out, it.Val())
	}
	return out, it.Err()
}

func (p *redisPurger) CountKeys(ctx context.Context) (int, error) {
	ks, err := p.keys(ctx)
	return len(ks), err
}

func (p *redisPurger) Purge(ctx context.Context) (int, error) {
	ks, err := p.keys(ctx)
	if err != nil {
		return 0, err
	}
	for i := 0; i < len(ks); i += 500 {
		if err := p.cli.Del(ctx, ks[i:min(i+500, len(ks))]...).Err(); err != nil {
			return i, err
		}
	}
	return len(ks), nil
}

func (p *redisPurger) Close() error { return p.cli.Close() }
