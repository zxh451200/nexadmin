package totp

// 真实 MySQL 集成测试：绑定 → 登录闸门校验 → 防重放 → 未绑定拒绝。
// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN 才执行，测试自建一次性库并在结束时 DROP。

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/migrate"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func TestITMySQLBindAndGate(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL integration test")
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("nexadmin_totpit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	var db *gorm.DB
	// 收尾顺序：关业务连接池 → 删库 → 关管理连接。admin.Close() 放 defer 会先于 t.Cleanup 执行，
	// DROP 报 "sql: database is closed" 且错误被吞掉，每跑一次就在实例上留一个临时库。
	t.Cleanup(func() {
		if db != nil {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if _, err := admin.Exec("DROP DATABASE IF EXISTS `" + name + "`"); err != nil {
			t.Errorf("drop throwaway db %s: %v", name, err)
		}
		_ = admin.Close()
	})

	if !strings.Contains(dsn, "parseTime=true") {
		// 分隔符按 DSN 里有没有 "?" 选：固定写 "?" 会在已带参数的 DSN 上拼出第二个 "?"，
		// 驱动把它当参数值，报 1064 而看不出是自己填错了。
		if strings.Contains(dsn, "?") {
			dsn += "&parseTime=true&loc=UTC"
		} else {
			dsn += "?parseTime=true&loc=UTC"
		}
	}
	gormDSN := strings.Replace(dsn, "/?", "/"+name+"?", 1)
	if gormDSN == dsn {
		// 换成一次性库的名字没生效，说明 DSN 里写了库名——继续跑就是往开发库建表删表。
		t.Fatalf("NEXADMIN_IT_MYSQL_DSN 的库名必须留空（user:pass@tcp(host:port)/?parseTime=true），当前 DSN 会连到命名库")
	}
	db, err = gorm.Open(mysql.Open(gormDSN), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// 用插件真实迁移建表（而不是 AutoMigrate）：同时验证迁移 SQL 在真实引擎可执行。
	raw, err := os.ReadFile(filepath.Join("migrations", "mysql", "001_init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range migrate.Split(string(raw)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("migration: %v", err)
		}
	}
	svc := NewService(db, nil, nil)
	// 表有 tenant_id：绑定与校验必须带租户上下文，否则租户过滤器会拒绝查询。
	ctx := tenant.With(context.Background(), 1)
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}

	secret, uri, err := svc.EnrollStart(ctx, 7, "admin", "NexAdmin")
	if err != nil || secret == "" || !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("enroll: %q %q %v", secret, uri, err)
	}
	now := time.Now().UTC()
	code, err := CodeAt(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Confirm(ctx, 7, code, "10.0.0.1"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if ok, err := svc.EnrolledAndEnabled(ctx, 7); err != nil || !ok {
		t.Fatalf("user should be enrolled and enabled: ok=%v err=%v", ok, err)
	}

	// 与绑定同一步长的码已被 Confirm 记为 last_step，必须按重放拒绝。
	if err := svc.VerifyForGate(ctx, 7, code); err == nil || !strings.Contains(err.Error(), "totp.code_replayed") {
		t.Fatalf("same-step code must be rejected as replay, got %v", err)
	}
	// 下一步长的码可通过，且再次使用被拒。
	next, err := CodeAt(secret, now.Add(StepSeconds*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyForGate(ctx, 7, next); err != nil {
		t.Fatalf("next-step code should pass: %v", err)
	}
	if err := svc.VerifyForGate(ctx, 7, next); err == nil || !strings.Contains(err.Error(), "totp.code_replayed") {
		t.Fatalf("reuse must be rejected, got %v", err)
	}
	// 未绑定用户不能通过闸门。
	if err := svc.VerifyForGate(ctx, 8, next); err == nil || !strings.Contains(err.Error(), "totp.not_enrolled") {
		t.Fatalf("unenrolled user must be rejected, got %v", err)
	}
}
