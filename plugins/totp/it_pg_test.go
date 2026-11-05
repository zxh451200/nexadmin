package totp

// 真实 PostgreSQL 集成测试：与 it_mysql_test.go 同一条语义（绑定 → 闸门校验 → 防重放 → 未绑定拒绝），
// 走 postgres 那份迁移与 pgx 方言。totp 是登录链路上的一道闸，它的手写 SQL 与唯一键此前只在 MySQL 上跑过。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_PG_DSN 才执行。库名必须有真值（pgx 的 keyword DSN 没有空值写法，
// 见 AGENTS.md 第 3 节），测试自建一次性库并在结束时 DROP WITH (FORCE)。

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"nexadmin/core/migrate"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func TestITPostgresBindAndGate(t *testing.T) {
	raw := os.Getenv("NEXADMIN_IT_PG_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("postgres not reachable: %v", err)
	}
	name := fmt.Sprintf("nexadmin_totp_pg_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + pgQuoteIdent(name)); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	var db *gorm.DB
	// 收尾顺序：关业务连接池 → 删库 → 关管理连接；反了会留一座删不掉的临时库。
	t.Cleanup(func() {
		if db != nil {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if _, err := admin.Exec(`DROP DATABASE ` + pgQuoteIdent(name) + ` WITH (FORCE)`); err != nil {
			t.Errorf("drop throwaway db %s: %v", name, err)
		}
		_ = admin.Close()
	})

	db, err = gorm.Open(gormpg.New(gormpg.Config{
		DSN: pgDSNWithDB(raw, name), PreferSimpleProtocol: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// 用插件真实迁移建表（而不是 AutoMigrate）：这一句就是本文件存在的理由——
	// postgres/001_init.sql 此前从没被任何测试执行过，方言写错会一路绿到上线。
	mig, err := os.ReadFile(filepath.Join("migrations", "postgres", "001_init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range migrate.Split(string(mig)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("migration: %v", err)
		}
	}

	svc := NewService(db, nil, nil)
	ctx := tenant.With(context.Background(), 1)
	if err := idgen.Init(2); err != nil {
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
	if err := svc.VerifyForGate(ctx, 8, next); err == nil || !strings.Contains(err.Error(), "totp.not_enrolled") {
		t.Fatalf("unenrolled user must be rejected, got %v", err)
	}
	// 两处只有 PG 才照得出的形状：
	//   1. enabled 是 BOOLEAN，必须能跟 Go 的 bool 真来回一次（写成 SMALLINT 时 pgx 在扫描/启动就报类型不符）；
	//   2. uk_plg_totp_user 必须是 (tenant_id, user_id) 的复合唯一键——写成 inline UNIQUE 时 PG 会当成单列约束，
	//      文本比对闸看不出来（三引擎一致性闸不比类型与索引语义），只有真插一行冲突才知道。
	var enabled bool
	if err := db.Table("plg_totp_secret").Select("enabled").Where("user_id = ?", 7).Scan(&enabled).Error; err != nil {
		t.Fatalf("读回 enabled: %v", err)
	}
	if !enabled {
		t.Error("Confirm 之后 enabled 应为 true（BOOLEAN ↔ Go bool 往返回了 false）")
	}
	var rows int64
	if err := db.Table("plg_totp_secret").Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("绑定表期望 1 行，实得 %d 行", rows)
	}
	if err := db.Exec(`INSERT INTO plg_totp_secret (id, tenant_id, user_id, secret_enc, enabled, last_step, created_at, updated_at)` +
		` VALUES (999, 1, 7, 'dup', FALSE, 0, now(), now())`).Error; err == nil || !strings.Contains(err.Error(), "23505") {
		// 只认 SQLSTATE：这台实例的服务端消息是中文（「重复键违反唯一约束」），按英文文案判会永远判不出来。
		t.Errorf("复合唯一键没生效：同一 (tenant_id, user_id) 插进了第二行（err=%v）", err)
	}
}

func pgQuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func pgDSNWithDB(dsn, db string) string {
	parts := strings.Fields(dsn)
	out := make([]string, 0, len(parts)+1)
	replaced := false
	for _, p := range parts {
		if strings.HasPrefix(p, "dbname=") {
			out = append(out, "dbname="+db)
			replaced = true
			continue
		}
		out = append(out, p)
	}
	if !replaced {
		out = append(out, "dbname="+db)
	}
	return strings.Join(out, " ")
}
