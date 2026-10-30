package ldap

// 真实 MySQL 迁移验收：001 建表 + 002 加 ca_cert 列。默认跳过，设置 DSN 才执行；自建库并在结束时 DROP。

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/migrate"
)

func TestITMySQLMigrations(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL migration test")
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("nexadmin_ldapit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`") })

	db, err := gorm.Open(gormmysql.Open(joinDB(dsn, name)), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"001_init.sql", "002_ca_cert.sql"} {
		raw, err := os.ReadFile(filepath.Join("migrations", "mysql", file))
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range migrate.Split(string(raw)) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("%s: %v", file, err)
			}
		}
	}

	var cols []string
	if err := db.Raw("SELECT column_name FROM information_schema.columns WHERE table_schema = ? AND table_name = 'plg_ldap_config'", name).Scan(&cols).Error; err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, c := range cols {
		has[c] = true
	}
	for _, want := range []string{"id", "bind_password", "ca_cert", "timeout_ms"} {
		if !has[want] {
			t.Errorf("missing column %s (got %v)", want, cols)
		}
	}
}

// joinDB 把 DSN 的库名换成测试库（DSN 形如 user:pass@tcp(host:port)/?params）。
func joinDB(dsn, name string) string {
	for i := len(dsn) - 1; i >= 0; i-- {
		if dsn[i] == '/' {
			return dsn[:i+1] + name + dsn[i+1:]
		}
	}
	return dsn
}
