package database

// 真实 MySQL 集成测试：DatabaseExists 必须能把「库不存在」和「连不上/口令错」分开。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN 才执行，例如
//
//	NEXADMIN_IT_MYSQL_DSN='root@tcp(127.0.0.1:3306)/' go test ./core/database -run TestITDatabaseExists -v
//
// 隔离：只操作自建的一次性数据库 nexadmin_dbexists_<纳秒>，用完 DROP，不触碰既有库。

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"
	"gorm.io/gorm/logger"

	"nexadmin/core/config"
)

func liveMySQL(t *testing.T, name string) config.Database {
	t.Helper()
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL test")
	}
	parsed, err := mysqldrv.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse NEXADMIN_IT_MYSQL_DSN: %v", err)
	}
	host, portStr, err := net.SplitHostPort(parsed.Addr)
	if err != nil {
		t.Fatalf("parse addr %q: %v", parsed.Addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return config.Database{Type: "mysql", Host: host, Port: port, User: parsed.User, Password: parsed.Passwd, Name: name}
}

func TestITDatabaseExists(t *testing.T) {
	ctx := context.Background()
	name := fmt.Sprintf("nexadmin_dbexists_%d", time.Now().UnixNano())
	cfg := liveMySQL(t, name)

	t.Cleanup(func() {
		drop, err := openRaw(cfg, "", logger.Silent)
		if err != nil {
			t.Errorf("cleanup connect: %v", err)
			return
		}
		defer Close(drop)
		if err := drop.Exec("DROP DATABASE IF EXISTS `" + name + "`").Error; err != nil {
			t.Errorf("cleanup drop %s: %v", name, err)
		}
	})

	if got, err := DatabaseExists(ctx, cfg); err != nil || got {
		t.Fatalf("before create: exists=%v err=%v, want false/nil", got, err)
	}
	if err := EnsureDatabase(ctx, cfg); err != nil {
		t.Fatalf("EnsureDatabase: %v", err)
	}
	if got, err := DatabaseExists(ctx, cfg); err != nil || !got {
		t.Fatalf("after create: exists=%v err=%v, want true/nil", got, err)
	}
	// 名称不合法时报错，而不是当成「不存在」悄悄放过。
	if got, err := DatabaseExists(ctx, dbWith(cfg, name+"-dash")); err == nil || got {
		t.Fatalf("invalid name: exists=%v err=%v, want error", got, err)
	}
	// 口令错时必须报连接错误（调用方据此 fail-closed），不能判成「库不存在」。
	wrong := cfg
	wrong.Password += "!wrong"
	if got, err := DatabaseExists(ctx, wrong); err == nil || got {
		t.Fatalf("wrong password: exists=%v err=%v, want connection error and false", got, err)
	}
}

func dbWith(cfg config.Database, name string) config.Database {
	cfg.Name = name
	return cfg
}
