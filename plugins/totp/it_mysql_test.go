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
	defer admin.Close()
	name := fmt.Sprintf("nexadmin_totpit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`") })

	if !strings.Contains(dsn, "parseTime=true") {
		dsn += "?parseTime=true&loc=UTC"
	}
	db, err := gorm.Open(mysql.Open(strings.Replace(dsn, "/?", "/"+name+"?", 1)), &gorm.Config{})
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
	svc := NewService(db)
	ctx := context.Background()
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
	if err := svc.Confirm(ctx, 7, code); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !svc.IsEnrolledAndEnabled(ctx, 7) {
		t.Fatal("user should be enrolled and enabled")
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
