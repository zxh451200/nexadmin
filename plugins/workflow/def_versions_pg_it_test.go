package workflow

// 真实 PostgreSQL 集成测试：定义版本的「覆盖保存留旧版 → 回看 → 回滚」这条链在 PG 上跑一遍，
// 并且用 migrations/postgres/003_def_versions.sql 真建表（MySQL 那份用例是 AutoMigrate 自建，
// 建的是模型推导出来的表，不是交付出去的 DDL）。
//
// 与 def_versions_it_test.go 的分工：行为语义（版本递增、历史原文、回滚后再递增）以那份为准，
// 这份负责两件事——
//   1. 003 的 DDL 能在 PG 上执行、并且列够服务层写（模型与已交付 DDL 漂移时这里先红，
//      而不是等生产启动或第一次覆盖保存）；
//   2. 走的是生产构造函数 database.Open，租户回调真装着：新写的历史行必须带上 tenant_id，
//      少了这一格，夹具与实现会「错得一样」而测试全绿。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_PG_DSN 才执行，库名要有真值（见 AGENTS.md 第 3 节），
// 那条 dbname 只用于建/删一次性库的管理连接。

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/tenant"
)

func setupWorkflowPG(t *testing.T) *gorm.DB {
	t.Helper()
	raw := os.Getenv("NEXADMIN_IT_PG_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL workflow test")
	}
	kv := map[string]string{}
	for _, part := range strings.Fields(raw) {
		if k, v, ok := strings.Cut(part, "="); ok {
			kv[k] = v
		}
	}
	port, err := strconv.Atoi(kv["port"])
	if kv["host"] == "" || kv["user"] == "" || err != nil {
		t.Fatalf("NEXADMIN_IT_PG_DSN 需要 host/port/user：解析得到 %+v，port 转换 err=%v", kv, err)
	}
	name := fmt.Sprintf("nexadmin_workflow_pg_it_%d", time.Now().UnixNano())

	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatalf("连接 PG 实例失败: %v", err)
	}
	if _, err := admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		_ = admin.Close()
		t.Fatalf("创建一次性库失败: %v", err)
	}
	var db *gorm.DB
	// 收尾顺序：关业务池 → 删库 → 关管理连接；反了会留一座删不掉的库。
	t.Cleanup(func() {
		if db != nil {
			_ = database.Close(db)
		}
		if _, err := admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`); err != nil {
			t.Errorf("清理测试库 %s 失败: %v", name, err)
		}
		_ = admin.Close()
	})

	// 生产构造函数：租户回调与时区设置在它里面，裸 gorm.Open 会让跨租户类断言退化成空断言。
	db, err = database.Open(config.Database{
		Type: "postgres", Host: kv["host"], Port: port, User: kv["user"], Password: kv["password"], Name: name,
	})
	if err != nil {
		t.Fatalf("连接真实库失败: %v", err)
	}
	for _, migName := range []string{"001_init.sql", "002_runtime.sql", "003_def_versions.sql"} {
		body, err := os.ReadFile(filepath.Join("migrations", "postgres", migName))
		if err != nil {
			t.Fatalf("读取迁移 %s: %v", migName, err)
		}
		for _, stmt := range splitSQL(string(body)) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("执行迁移 %s 失败: %v\n%s", migName, err, stmt)
			}
		}
	}
	return db
}

func TestDefVersionArchiveAndRollbackPG(t *testing.T) {
	db := setupWorkflowPG(t)
	s := NewService(db, nopLogger{})
	ctx := tenant.With(context.Background(), 42)

	def, err := s.DefSave(ctx, 0, DefForm{Key: "leave", Name: "请假审批", Status: DefPublished, XML: sampleXML})
	if err != nil {
		t.Fatalf("保存定义失败: %v", err)
	}
	xmlV1, err := s.DefXML(ctx, def.ID)
	if err != nil {
		t.Fatal(err)
	}

	changed := strings.Replace(sampleXML, `name="主管审批"`, `name="主管审批（改）"`, 1)
	if _, err := s.DefSave(ctx, def.ID, DefForm{Key: def.Key, Name: def.Name, Status: DefPublished, XML: changed}); err != nil {
		t.Fatalf("第二次保存失败: %v", err)
	}
	versions, err := s.DefVersions(ctx, def.ID)
	if err != nil {
		t.Fatalf("列历史版本失败: %v", err)
	}
	if len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("历史版本 = %+v, want [v1]", versions)
	}
	gotV1, err := s.DefVersionXML(ctx, def.ID, 1)
	if err != nil {
		t.Fatalf("回看 v1 失败: %v", err)
	}
	if gotV1 != xmlV1 {
		t.Fatal("历史版本的 XML 必须是修改前的原文")
	}

	out, err := s.DefRollback(ctx, def.ID, 1)
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	if out.Version != 3 {
		t.Fatalf("回滚后 version = %d, want 3", out.Version)
	}
	if xmlNow, err := s.DefXML(ctx, def.ID); err != nil || xmlNow != xmlV1 {
		t.Fatalf("回滚后当前 XML 应与 v1 一致（got=%q err=%v）", xmlNow, err)
	}
	if versions, err := s.DefVersions(ctx, def.ID); err != nil || len(versions) != 2 {
		t.Fatalf("历史版本数 = %d, want 2（err=%v）", len(versions), err)
	}

	// 003 的 DDL 必须够服务层写：bpmn_xml 是 TEXT，中文原文要能整段读回（不是被截断或被编码吃掉）。
	body, err := s.DefVersionXML(ctx, def.ID, 2)
	if err != nil {
		t.Fatalf("回看 v2 失败: %v", err)
	}
	if body != changed {
		t.Fatal("v2 必须是改过的那一份原文")
	}
	// 走生产构造函数才装得上租户回调：历史行必须带 tenant_id，否则跨租户隔离是一句空话。
	var tid int64
	if err := db.Table("plg_workflow_def_version").
		Select("tenant_id").Where("def_id = ?", def.ID).Order("version").Limit(1).Scan(&tid).Error; err != nil {
		t.Fatalf("读历史行的 tenant_id 失败: %v", err)
	}
	if tid != 42 {
		t.Fatalf("历史行 tenant_id = %d, want 42（database.Open 的租户回调没装或写入路径绕过了它）", tid)
	}
}
