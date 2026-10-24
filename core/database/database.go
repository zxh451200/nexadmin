// Package database 打开 GORM（MySQL 8.4 / MariaDB 10.11 / PostgreSQL 16），并提供模型基座与租户 scope。
package database

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/config"
)

// Normalize 把类型名归一为 mysql（含 MariaDB）或 postgres。向导里的 postgresql 也接受。
func Normalize(t string) (string, error) {
	switch t {
	case "mysql", "mariadb":
		return "mysql", nil
	case "postgres", "postgresql":
		return "postgres", nil
	}
	return "", fmt.Errorf("database: unsupported type %q (want mysql, mariadb or postgres)", t)
}

// Open 连接配置里的库，带租户 scope 插件，并检查连通性。
func Open(cfg config.Database) (*gorm.DB, error) {
	db, err := openRaw(cfg, cfg.Name, logger.Warn)
	if err != nil {
		return nil, err
	}
	if err := db.Use(tenantPlugin{}); err != nil {
		_ = Close(db)
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	if err := configureIsolation(db, cfg); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Close 关闭连接池。
func Close(db *gorm.DB) error {
	if p, ok := db.Config.Plugins["nexadmin:isolation"].(*isolation); ok {
		var first error
		for _, td := range p.tenants {
			if err := Close(td); err != nil && first == nil {
				first = err
			}
		}
		if err := p.pool.Close(); err != nil && first == nil {
			first = err
		}
		return first
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func openRaw(cfg config.Database, dbName string, level logger.LogLevel) (*gorm.DB, error) {
	kind, err := Normalize(cfg.Type)
	if err != nil {
		return nil, err
	}
	hostPort := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	var dial gorm.Dialector
	if kind == "mysql" {
		mc := mysqldrv.NewConfig()
		mc.User, mc.Passwd, mc.Net, mc.Addr, mc.DBName = cfg.User, cfg.Password, "tcp", hostPort, dbName
		mc.ParseTime, mc.Loc, mc.Timeout = true, time.UTC, 5*time.Second
		mc.Params = map[string]string{"charset": "utf8mb4"}
		dial = gormmysql.New(gormmysql.Config{DSN: mc.FormatDSN()})
	} else {
		if dbName == "" {
			dbName = "postgres"
		}
		u := url.URL{
			Scheme:   "postgres",
			User:     url.UserPassword(cfg.User, cfg.Password),
			Host:     hostPort,
			Path:     "/" + dbName,
			RawQuery: "sslmode=disable&connect_timeout=5&TimeZone=UTC",
		}
		dial = gormpg.Open(u.String())
	}
	db, err := gorm.Open(dial, &gorm.Config{
		Logger:  logger.Default.LogMode(level),
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("database: connect %s %s: %w", kind, hostPort, err)
	}
	return db, nil
}

var identRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// EnsureDatabase 在库不存在时创建（utf8mb4 / UTF8），供向导“库不存在则自动创建”使用。
func EnsureDatabase(ctx context.Context, cfg config.Database) error {
	if !identRe.MatchString(cfg.Name) {
		return fmt.Errorf("database: invalid database name %q (letters, digits, underscore only)", cfg.Name)
	}
	kind, err := Normalize(cfg.Type)
	if err != nil {
		return err
	}
	db, err := openRaw(cfg, "", logger.Silent)
	if err != nil {
		return err
	}
	defer Close(db)
	db = db.WithContext(ctx)
	if kind == "mysql" {
		return db.Exec("CREATE DATABASE IF NOT EXISTS `" + cfg.Name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci").Error
	}
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", cfg.Name).Scan(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return db.Exec(`CREATE DATABASE "` + cfg.Name + `" ENCODING 'UTF8'`).Error
}

// Probe 测试连接并返回服务器版本。serverLevel=true 时不连具体库（库可能还不存在）。
func Probe(ctx context.Context, cfg config.Database, serverLevel bool) (string, error) {
	name := cfg.Name
	if serverLevel {
		name = ""
	}
	db, err := openRaw(cfg, name, logger.Silent)
	if err != nil {
		return "", err
	}
	defer Close(db)
	kind, _ := Normalize(cfg.Type)
	q := "SELECT VERSION()"
	if kind == "postgres" {
		q = "SHOW server_version"
	}
	var v string
	if err := db.WithContext(ctx).Raw(q).Scan(&v).Error; err != nil {
		return "", err
	}
	return v, nil
}
