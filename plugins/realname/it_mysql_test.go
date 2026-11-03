package realname

// 真实引擎集成测试：用插件自己的迁移 SQL 建表，然后在真库上跑配置读写与核验落档。
// 一致性门禁（plugins/migrations_test.go）只比对三份 SQL 的结构，这里负责
// 「mysql/mariadb/postgres 方言真的能在引擎上执行、唯一索引真的生效」这一层。
//
// 守卫：默认跳过；按需设置
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC'
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=postgres dbname=postgres sslmode=disable'
//
// 两者的库名都必须留空（MySQL）或指向维护库（PG）：测试自建一次性库并在结束时整库删除。

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
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"nexadmin/core/migrate"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// itCase 是一次引擎验证的公共步骤：真库建表 → 配置读写 → 核验落档 → 明文检查。
func itCase(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	ctx := tenant.With(context.Background(), idgen.ID(1))

	// 同一租户只允许一行配置：唯一索引必须在引擎上真实拦住第二次插入。
	first := &Config{Enabled: false, Provider: "", TimeoutMs: defTimeoutMs, DailyLimit: defDailyLen}
	if err := db.WithContext(ctx).Create(first).Error; err != nil {
		t.Fatalf("insert config row: %v", err)
	}
	dup := &Config{Enabled: true, Provider: ProviderGeneric, Endpoint: "https://dup.example.com", TimeoutMs: 3000, DailyLimit: 10}
	dup.TenantID = first.TenantID
	if err := db.WithContext(ctx).Create(dup).Error; err == nil {
		t.Error("同租户第二行配置应被 uk_plg_realname_config_tenant 拒绝")
	}
	u := newUpstream(t, `{"matched":true,"message":"命中"}`)
	svc := NewService(db, memCache(t), nil)
	svc.httpClient = u.srv.Client()

	// 已有一行配置，这里走的是 UPDATE 分支。
	view, err := svc.SaveConfig(ctx, ConfigInput{
		Enabled: true, Provider: ProviderGeneric, Endpoint: u.srv.URL, AppKey: "it-key", AppSecret: "it-secret",
		TimeoutMs: 3000, DailyLimit: 0,
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	if !view.Enabled || !view.HasSecret {
		t.Fatalf("配置回显不对: %+v", view)
	}

	res, err := svc.Verify(ctx, VerifyInput{Number: validNumber, Name: fullName, Remark: "集成测试"})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.Online || res.Verification.Status != StatusMatch {
		t.Fatalf("verify = online:%v status:%s (%s), want online match", res.Online, res.Verification.Status, res.Verification.ProviderMsg)
	}
	if u.hits != 1 {
		t.Fatalf("上游应被调用一次，实际 %d 次", u.hits)
	}

	// 台账落库内容逐列拉出来看：明文号码与完整姓名不能出现在任何一列里。
	rows, err := db.WithContext(ctx).Raw("SELECT * FROM plg_realname_verification").Rows()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	dump := itScanAll(t, rows, cols)
	if strings.Contains(dump, validNumber) {
		t.Errorf("台账列里出现明文号码: %s", dump)
	}
	if strings.Contains(dump, fullName) {
		t.Errorf("台账列里出现完整姓名: %s", dump)
	}
	// 脱敏串仍保留前 6 位区划码，方便肉眼确认归属地；这一点是设计而非缺陷。
	if !strings.Contains(dump, "110101") {
		t.Errorf("台账脱敏串应保留区划前缀，实际 %s", dump)
	}

	// 同一号码重复提交：真引擎上折叠查询要只回一行，全部留痕要两行。
	if _, err := svc.verify(ctx, VerifyInput{Number: validNumber, Name: fullName}, cfgForIT(u)); err != nil {
		t.Fatalf("第二次核验: %v", err)
	}
	folded, foldedTotal, err := svc.ListVerifications(ctx, ListQuery{Latest: true, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("folded list: %v", err)
	}
	all, allTotal, err := svc.ListVerifications(ctx, ListQuery{Latest: false, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("full list: %v", err)
	}
	if allTotal != 2 || len(all) != 2 {
		t.Errorf("全部留痕应有 2 行，实际 total=%d rows=%d", allTotal, len(all))
	}
	if foldedTotal != 1 || len(folded) != 1 {
		t.Errorf("同一号码应折叠成 1 行，实际 total=%d rows=%d", foldedTotal, len(folded))
	}
	if len(folded) == 1 && folded[0].ID != all[0].ID {
		t.Errorf("折叠应保留最新一条：folded=%v all[0]=%v", folded[0].ID, all[0].ID)
	}
}

func itScanAll(t *testing.T, rows *sql.Rows, cols []string) string {
	t.Helper()
	var b strings.Builder
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan ledger: %v", err)
		}
		for _, v := range vals {
			// MySQL 把 VARCHAR 以 []byte 返回：不转成字符串的话，
			// 后面的"明文不出现"断言会永远为空过。
			if raw, ok := v.([]byte); ok {
				v = string(raw)
			}
			fmt.Fprintf(&b, "%v|", v)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate ledger: %v", err)
	}
	return b.String()
}

func itRunMigrationFiles(t *testing.T, db *gorm.DB, dir string) {
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

func TestITMySQLMigrationsAndVerify(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL integration test")
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("nexadmin_realnameit_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var db *gorm.DB
	// 收尾顺序：关业务连接池 → 删库 → 关管理连接，否则 DROP 会撞上已关闭的管理连接。
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
	itRunMigrationFiles(t, db, filepath.Join("migrations", "mysql"))
	itCase(t, db)
}

func TestITPostgresMigrationsAndVerify(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_PG_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("postgres not reachable: %v", err)
	}
	name := fmt.Sprintf("nexadmin_realnameit_%d", time.Now().UnixNano())
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

	gormDSN := itPGWithDB(dsn, name)
	db, err = gorm.Open(gormpg.New(gormpg.Config{DSN: gormDSN, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	itRunMigrationFiles(t, db, filepath.Join("migrations", "postgres"))
	itCase(t, db)
}

// itPGWithDB 把维护库换成一次性库：dbname 已存在则替换，不存在则追加。
func itPGWithDB(dsn, name string) string {
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

// cfgForIT 复用假上游地址构造已启用的配置。
func cfgForIT(u *upstream) *Config {
	return enabledConfig(u.srv.URL, "it-key", 0)
}
