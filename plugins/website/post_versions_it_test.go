package website

// 新闻版本历史的真引擎集成测试：用插件自己的迁移 SQL（021_post_versions.sql 在内）
// 在一次性库里建表，然后走真实 Service 链路验证：覆盖保存留旧版、内容相同不重复存、
// 列表不带正文、回滚写回内容且版本继续递增、删除级联清版本。
//
// 守卫：默认跳过；按需设置（库名必须留空，测试自建一次性库并在结束时整库删除）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC'
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=postgres sslmode=disable'
//
// mariadb 的 SQL 与 mysql 方言同源，由结构门禁（plugins/migrations_test.go）与
// MySQL 侧执行共同覆盖，这里不单独连 MariaDB 实例。

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"nexadmin/core/migrate"
	"nexadmin/pkg/idgen"
)

func itNewsRunMigrationFiles(t *testing.T, db *gorm.DB, dir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("find migration files in %s: %v (%d)", dir, err, len(files))
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range migrate.Split(string(raw)) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("run %s: %v", filepath.Base(f), err)
			}
		}
	}
}

// itNewsPrepareFrameworkTables 先建出官网迁移依赖的两张框架表（sys_language、sys_menu）。
// 官网插件的 010/012 迁移会 INSERT INTO sys_language / UPDATE sys_menu——生产里它们由框架
// 迁移先行创建；一次性库里单跑插件迁移必须自己补齐，否则 010 起就断表。列形状照抄
// migrations/framework/{mysql,postgres}/system_language.sql，sys_menu 只建被引用的列。
func itNewsPrepareFrameworkTables(t *testing.T, db *gorm.DB, pg bool) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE sys_language (
  id BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  code VARCHAR(16) NOT NULL,
  name VARCHAR(64) NOT NULL,
  short VARCHAR(8) NOT NULL DEFAULT '',
  dir VARCHAR(3) NOT NULL DEFAULT 'ltr',
  html_lang VARCHAR(16) NOT NULL,
  og_locale VARCHAR(32) NOT NULL DEFAULT '',
  prefix VARCHAR(16) NOT NULL DEFAULT '',
  date_format VARCHAR(32) NOT NULL DEFAULT '',
  is_default TINYINT(1) NOT NULL DEFAULT 0,
  sort INT NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_sys_language_code (code)
)`,
		`CREATE TABLE sys_menu (
  id BIGINT NOT NULL,
  code VARCHAR(128) NOT NULL DEFAULT '',
  path VARCHAR(255) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
)`,
	}
	if pg {
		stmts = []string{
			`CREATE TABLE sys_language (
  id BIGINT PRIMARY KEY,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  code VARCHAR(16) NOT NULL UNIQUE,
  name VARCHAR(64) NOT NULL,
  short VARCHAR(8) NOT NULL DEFAULT '',
  dir VARCHAR(3) NOT NULL DEFAULT 'ltr',
  html_lang VARCHAR(16) NOT NULL,
  og_locale VARCHAR(32) NOT NULL DEFAULT '',
  prefix VARCHAR(16) NOT NULL DEFAULT '',
  date_format VARCHAR(32) NOT NULL DEFAULT '',
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  sort INT NOT NULL DEFAULT 0
)`,
			`CREATE TABLE sys_menu (
  id BIGINT PRIMARY KEY,
  code VARCHAR(128) NOT NULL DEFAULT '',
  path VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
)`,
		}
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("prepare framework table: %v", err)
		}
	}
}

func itNewsVersionCount(t *testing.T, db *gorm.DB, postID int64) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&PostVersion{}).Where("post_id = ?", postID).Count(&n).Error; err != nil {
		t.Fatalf("count versions: %v", err)
	}
	return n
}

// itNewsVersionsCase 在真引擎上跑完整版本历史链路。
func itNewsVersionsCase(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWithEditor("editor-it")
	svc := NewService(db)

	form := func(content, title string) PostForm {
		return PostForm{Slug: "it-news", Title: title, Summary: "IT 摘要", Content: content, Published: true, Tags: []string{"it"}}
	}

	created, err := svc.SavePost(ctx, 0, form("<p>第一版正文</p>", "IT 新闻"))
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	// 新建不留档：历史表此时应为空。
	if n := itNewsVersionCount(t, db, int64(created.ID)); n != 0 {
		t.Fatalf("新建新闻不该有历史版本，got %d", n)
	}

	// 覆盖保存改正文：旧行归档为 v1。
	if _, err := svc.SavePost(ctx, created.ID, form("<p>第二版正文</p>", "IT 新闻")); err != nil {
		t.Fatalf("second save: %v", err)
	}
	vers, err := svc.ListPostVersions(ctx, created.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(vers) != 1 || vers[0].Version != 1 || vers[0].CreatedBy != "editor-it" {
		t.Fatalf("覆盖保存应留旧版，got %+v", vers)
	}
	raw, _ := json.Marshal(vers)
	if strings.Contains(string(raw), "content") {
		t.Errorf("版本列表响应不应带正文: %s", raw)
	}

	// 只改标题、正文没变：守卫生效，不重复存。
	if _, err := svc.SavePost(ctx, created.ID, form("<p>第二版正文</p>", "IT 新闻改标题")); err != nil {
		t.Fatalf("title-only save: %v", err)
	}
	if n := itNewsVersionCount(t, db, int64(created.ID)); n != 1 {
		t.Fatalf("正文未变不该新增版本，got %d", n)
	}

	// 快照详情带回旧正文与字数。
	detail, err := svc.GetPostVersion(ctx, created.ID, 1)
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	if detail.Content != "<p>第一版正文</p>" {
		t.Fatalf("v1 正文 = %q, want <p>第一版正文</p>", detail.Content)
	}
	if detail.Words != len([]rune("第一版正文")) {
		t.Errorf("words = %d, want %d", detail.Words, len([]rune("第一版正文")))
	}

	// 回滚到 v1：走一次正常保存——回滚前当前内容先归档（版本继续递增），正文写回 v1。
	if _, err := svc.RollbackPostVersion(ctx, created.ID, 1); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	row, err := svc.PostByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("reload post: %v", err)
	}
	if row.Content != "<p>第一版正文</p>" {
		t.Errorf("回滚后正文 = %q, want 第一版正文", row.Content)
	}
	if row.Slug != "it-news" {
		t.Errorf("回滚不该改 slug: %q", row.Slug)
	}
	// 回滚不拆栏目/标签：快照外的字段沿用当前行。
	if cur, err := svc.postWithTaxonomy(ctx, created.ID); err != nil {
		t.Fatalf("reload taxonomy: %v", err)
	} else if len(cur.Tags) != 1 || cur.Tags[0].Name != "it" {
		t.Errorf("回滚后标签应原样保留，got %+v", cur.Tags)
	}
	vers, err = svc.ListPostVersions(ctx, created.ID)
	if err != nil {
		t.Fatalf("list after rollback: %v", err)
	}
	// 版本单调新→旧：[2]=回滚前被顶掉的内容，[1]=最初正文；回滚本身可再回滚。
	if len(vers) != 2 || vers[0].Version != 2 || vers[1].Version != 1 {
		t.Fatalf("回滚后版本应递增为 [2,1]，got %+v", vers)
	}
	v2, err := svc.GetPostVersion(ctx, created.ID, 2)
	if err != nil {
		t.Fatalf("get v2: %v", err)
	}
	if v2.Content != "<p>第二版正文</p>" || v2.Title != "IT 新闻改标题" {
		t.Errorf("v2 应为回滚前的当前内容: %+v", v2)
	}

	// 单条删除：级联清掉版本，不留孤儿快照。
	if err := svc.DeletePost(ctx, created.ID); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	if n := itNewsVersionCount(t, db, int64(created.ID)); n != 0 {
		t.Errorf("删除新闻应级联清版本，got %d", n)
	}

	// 批量删除：同样级联。
	p2, err := svc.SavePost(ctx, 0, PostForm{Slug: "it-news-2", Title: "IT 新闻二", Content: "<p>A</p>", Published: false})
	if err != nil {
		t.Fatalf("create second post: %v", err)
	}
	for _, c := range []string{"<p>B</p>", "<p>C</p>"} {
		if _, err := svc.SavePost(ctx, p2.ID, PostForm{Slug: "it-news-2", Title: "IT 新闻二", Content: c}); err != nil {
			t.Fatalf("save second post: %v", err)
		}
	}
	if n := itNewsVersionCount(t, db, int64(p2.ID)); n != 2 {
		t.Fatalf("两条覆盖保存应有 2 个版本，got %d", n)
	}
	deleted, err := svc.DeletePosts(ctx, []idgen.ID{p2.ID})
	if err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if n := itNewsVersionCount(t, db, int64(p2.ID)); n != 0 {
		t.Errorf("批量删除应级联清版本，got %d", n)
	}
	// 列表接口对已删新闻回 404，而不是空列表。
	if _, err := svc.ListPostVersions(ctx, p2.ID); err == nil {
		t.Error("已删新闻的版本列表应报错")
	}
}

func TestITMySQLNewsVersions(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL news version test")
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("nexadmin_newsit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var db *gorm.DB
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
		if strings.Contains(dsn, "?") {
			dsn += "&parseTime=true&loc=UTC"
		} else {
			dsn += "?parseTime=true&loc=UTC"
		}
	}
	gormDSN := strings.Replace(dsn, "/?", "/"+name+"?", 1)
	if gormDSN == dsn {
		t.Fatal("NEXADMIN_IT_MYSQL_DSN 的库名必须留空，当前 DSN 会连到命名库")
	}
	db, err = gorm.Open(mysql.Open(gormDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	itNewsPrepareFrameworkTables(t, db, false)
	itNewsRunMigrationFiles(t, db, filepath.Join("migrations", "mysql"))
	itNewsVersionsCase(t, db)
}

func TestITPostgresNewsVersions(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_PG_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL news version test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("postgres not reachable: %v", err)
	}
	name := fmt.Sprintf("nexadmin_newsit_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var db *gorm.DB
	t.Cleanup(func() {
		if db != nil {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if _, err := admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`); err != nil {
			t.Errorf("drop throwaway db %s: %v", name, err)
		}
		_ = admin.Close()
	})

	gormDSN := itNewsPGWithDB(dsn, name)
	db, err = gorm.Open(gormpg.New(gormpg.Config{DSN: gormDSN, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	itNewsPrepareFrameworkTables(t, db, true)
	itNewsRunMigrationFiles(t, db, filepath.Join("migrations", "postgres"))
	itNewsVersionsCase(t, db)
}

// itNewsPGWithDB 把维护库换成一次性库：dbname 已存在则替换，不存在则追加。
func itNewsPGWithDB(dsn, name string) string {
	fields := strings.Fields(dsn)
	out := make([]string, 0, len(fields)+1)
	replaced := false
	for _, f := range fields {
		if strings.HasPrefix(f, "dbname=") {
			out = append(out, "dbname="+name)
			replaced = true
			continue
		}
		out = append(out, f)
	}
	if !replaced {
		out = append(out, "dbname="+name)
	}
	return strings.Join(out, " ")
}
