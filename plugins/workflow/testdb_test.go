package workflow

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/database"
)

// setupWorkflowDB 打开一个真实 MySQL 一次性库并建好 workflow 表。
// 需要 NEXADMIN_IT_MYSQL_DSN=user:pass@tcp(host:port)/dbname?parseTime=true；
// 库名带 it/test 时测试结束后自动 DROP（禁止指向业务库）。
func setupWorkflowDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL workflow test")
	}
	cfgDSN, err := mysqldrv.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN 非法: %v", err)
	}
	host, portStr, err := net.SplitHostPort(cfgDSN.Addr)
	if err != nil {
		t.Fatalf("DSN addr 非法: %v", err)
	}
	port, _ := strconv.Atoi(portStr)
	if cfgDSN.DBName == "" {
		// 允许服务端 DSN（不带库名）：与 ticket/pay/ldap/totp 的 IT 同一口径，
		// 自动开一个库名含 _it 的一次性库，跑完由下面的 cleanup 删掉。
		cfgDSN.DBName = fmt.Sprintf("nexadmin_workflow_it_%d", time.Now().UnixNano())
	}
	disposable := strings.Contains(cfgDSN.DBName, "_it") || strings.Contains(cfgDSN.DBName, "test")
	if disposable {
		for _, r := range cfgDSN.DBName {
			if !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
				t.Fatalf("测试库名只允许字母数字下划线: %q", cfgDSN.DBName)
			}
		}
		bootstrap := *cfgDSN
		bootstrap.DBName = ""
		admin, err := sql.Open("mysql", bootstrap.FormatDSN())
		if err != nil {
			t.Fatalf("连接 MySQL 实例失败: %v", err)
		}
		if _, err := admin.Exec("CREATE DATABASE IF NOT EXISTS `" + cfgDSN.DBName + "` CHARACTER SET utf8mb4"); err != nil {
			_ = admin.Close()
			t.Fatalf("创建测试库失败: %v", err)
		}
		_ = admin.Close()
	}
	db, err := database.Open(config.Database{
		Type: "mysql", Host: host, Port: port,
		User: cfgDSN.User, Password: cfgDSN.Passwd, Name: cfgDSN.DBName,
	})
	if err != nil {
		t.Fatalf("连接真实库失败: %v", err)
	}
	// 先关连接再删库；只清理带 it/test 的一次性库。
	t.Cleanup(func() {
		_ = database.Close(db)
		if !disposable {
			return
		}
		bootstrap := *cfgDSN
		bootstrap.DBName = ""
		admin, err := sql.Open("mysql", bootstrap.FormatDSN())
		if err != nil {
			t.Logf("清理测试库失败（连接）: %v", err)
			return
		}
		defer admin.Close()
		if _, err := admin.Exec("DROP DATABASE IF EXISTS `" + cfgDSN.DBName + "`"); err != nil {
			t.Logf("清理测试库失败: %v", err)
		}
	})

	for _, stmt := range []string{
		"DROP TABLE IF EXISTS plg_workflow_action",
		"DROP TABLE IF EXISTS plg_workflow_task",
		"DROP TABLE IF EXISTS plg_workflow_inst",
		"DROP TABLE IF EXISTS plg_workflow_def",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("清理表失败: %v", err)
		}
	}
	for _, name := range []string{"001_init.sql", "002_runtime.sql"} {
		raw, err := os.ReadFile(filepath.Join("migrations", "mysql", name))
		if err != nil {
			t.Fatalf("读取迁移 %s: %v", name, err)
		}
		for _, stmt := range splitSQL(string(raw)) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("执行迁移 %s 失败: %v\n%s", name, err, stmt)
			}
		}
	}
	return db
}

// splitSQL 去掉整行注释后按分号切分（本插件的迁移都是简单 DDL，足够用）。
func splitSQL(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		out = append(out, line)
	}
	var stmts []string
	for _, part := range strings.Split(strings.Join(out, "\n"), ";") {
		if s := strings.TrimSpace(part); s != "" {
			stmts = append(stmts, s)
		}
	}
	return stmts
}
