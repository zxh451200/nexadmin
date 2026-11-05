package plugins_test

// 真引擎侧的迁移闸：把框架与每个插件的 migrations/<engine>/*.sql 在真库上按文件名顺序执行，
// 再用 information_schema 核对「模型嵌了数据基座 ⇒ 引擎里真有那几列」。
//
// 为什么还要这一条：migrations_test.go 只解析 SQL 文本，而且只比列名——
// 它比的是「三份是不是互相一致」，比不出「这份能不能在真引擎上跑」：
// postgres 迁移里写 DATETIME(3) 它不吭声（类型不在比对范围），一条执行即报「类型 datetime 不存在」，
// 设备插件那次 alarm_event 缺列也是先有三份一致的文本、后有真库插入 500。
// 本闸纵着把每引擎那一份真执行一遍，再问引擎本人「这张表到底有哪些列」。
//
// 守卫：跑哪些引擎取决于环境变量（写法见 AGENTS.md 第 3 节）——
//
//	NEXADMIN_IT_MYSQL_DSN  库名必须留空
//	NEXADMIN_IT_PG_DSN     库名必须有真值（pgx 的 keyword DSN 没有空值写法）
//
// mariadb 那份 SQL 在 MySQL 服务器上执行，与 keyword_case 用例的既有做法一致。
// 隔离：每个引擎自建一次性库，收尾按「关业务池 → 删库 → 关管理连接」的顺序，少一步就留一座库。

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormmysql "gorm.io/driver/mysql"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type liveEngine struct {
	name    string // 迁移目录名，也用于一次性库名
	dialect string // mysql | postgres：决定连法与 information_schema 写法
	env     string
}

var migLiveEngines = []liveEngine{
	{name: "mysql", dialect: "mysql", env: "NEXADMIN_IT_MYSQL_DSN"},
	{name: "mariadb", dialect: "mysql", env: "NEXADMIN_IT_MYSQL_DSN"},
	{name: "postgres", dialect: "postgres", env: "NEXADMIN_IT_PG_DSN"},
}

func TestITLiveEngineMigrationsExecute(t *testing.T) {
	configured := 0
	for _, e := range migLiveEngines {
		dsn := os.Getenv(e.env)
		if dsn == "" {
			t.Logf("%s：%s 未设置，跳过该引擎", e.name, e.env)
			continue
		}
		configured++
		e, dsn := e, dsn
		t.Run(e.name, func(t *testing.T) { runMigrationsOnLiveEngine(t, e, dsn) })
	}
	if configured == 0 {
		t.Skip("NEXADMIN_IT_MYSQL_DSN / NEXADMIN_IT_PG_DSN 都没设置，真引擎迁移闸不跑")
	}
}

func runMigrationsOnLiveEngine(t *testing.T, e liveEngine, dsn string) {
	t.Helper()

	admin, gdb, dbName := openThrowawayDB(t, e, dsn)
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		if _, err := admin.Exec(migDropDB(e.dialect, dbName)); err != nil {
			t.Errorf("删一次性库 %s 失败: %v", dbName, err)
		}
		_ = admin.Close()
	})

	frameworkDir := filepath.Join("..", "migrations", "framework", e.name)
	if err := execSQLDir(t, gdb, frameworkDir); err != nil {
		t.Fatalf("框架迁移在 %s 上没跑通：%v", e.name, err)
	}

	pluginDirs, err := filepath.Glob("*/migrations/" + e.name)
	if err != nil {
		t.Fatal(err)
	}
	if len(pluginDirs) == 0 {
		t.Fatalf("一个插件迁移目录都没找到——闸对空跑")
	}

	modelsChecked := 0
	for _, dir := range pluginDirs {
		// dir 形如 cron/migrations/mysql，插件根目录是它的第一段，不是 filepath.Dir（那会得到 cron/migrations）。
		plugin := strings.SplitN(filepath.ToSlash(dir), "/", 2)[0]
		if err := execSQLDir(t, gdb, dir); err != nil {
			t.Errorf("插件 %s 的迁移在 %s 上执行失败: %v", plugin, e.name, err)
			continue
		}
		models, err := baseModels(plugin)
		if err != nil {
			t.Errorf("插件 %s: %v", plugin, err)
			continue
		}
		for _, m := range models {
			cols, err := liveColumns(gdb, e.dialect, m.table)
			if err != nil {
				t.Errorf("插件 %s: 查 %s 的列失败: %v", plugin, m.table, err)
				continue
			}
			if len(cols) == 0 {
				t.Errorf("插件 %s: 模型 %s 的表 %s 在 %s 引擎里不存在（迁移没建它，或建表语句被跳过了）",
					plugin, m.goType, m.table, e.name)
				continue
			}
			want := []string{"id", "created_at", "updated_at"}
			if m.tenant {
				want = append(want, "tenant_id")
			}
			for _, col := range want {
				if !cols[col] {
					t.Errorf("插件 %s: %s 嵌入 %s，但 %s 引擎上表 %s 缺列 %s（文本闸只比三份互相一致，这里问的是引擎本人）",
						plugin, m.goType, m.base, e.name, m.table, col)
				}
			}
			modelsChecked++
		}
	}
	if modelsChecked == 0 {
		t.Fatalf("%s 引擎上一个模型都没检查——闸对空跑", e.name)
	}
	t.Logf("%s: %d 个插件迁移目录、%d 个模型的基座列在真引擎上核对通过", e.name, len(pluginDirs), modelsChecked)
}

// openThrowawayDB 建一次性库并返回管理连接与跑在库上的 gorm 句柄。
func openThrowawayDB(t *testing.T, e liveEngine, dsn string) (*sql.DB, *gorm.DB, string) {
	t.Helper()
	switch e.dialect {
	case "postgres":
		admin, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatalf("open admin conn: %v", err)
		}
		if err := admin.Ping(); err != nil {
			_ = admin.Close()
			t.Fatalf("%s 连不上: %v", e.name, err)
		}
		dbName := fmt.Sprintf("nexadmin_miglive_%s_%d", e.name, time.Now().UnixNano())
		if _, err := admin.Exec(`CREATE DATABASE ` + migQuotePG(dbName)); err != nil {
			_ = admin.Close()
			t.Fatalf("建一次性库失败: %v", err)
		}
		gdb, err := gorm.Open(gormpg.New(gormpg.Config{
			DSN: migPGWithDBName(dsn, dbName), PreferSimpleProtocol: true,
		}), silentGorm())
		if err != nil {
			_ = admin.Close()
			t.Fatalf("open gorm: %v", err)
		}
		return admin, gdb, dbName
	default:
		full := migMySQLExtra(dsn)
		admin, err := sql.Open("mysql", full)
		if err != nil {
			t.Fatalf("open admin conn: %v", err)
		}
		if err := admin.Ping(); err != nil {
			_ = admin.Close()
			t.Fatalf("%s 连不上: %v", e.name, err)
		}
		dbName := fmt.Sprintf("nexadmin_miglive_%s_%d", e.name, time.Now().UnixNano())
		if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
			_ = admin.Close()
			t.Fatalf("建一次性库失败: %v", err)
		}
		gormDSN := strings.Replace(full, ")/", ")/"+dbName+"", 1)
		if gormDSN == full {
			_ = admin.Close()
			t.Fatalf("NEXADMIN_IT_MYSQL_DSN 得是库名留空的写法（AGENTS.md 第 3 节），当前换不出一次性库：%s", full)
		}
		gdb, err := gorm.Open(gormmysql.Open(gormDSN), silentGorm())
		if err != nil {
			_ = admin.Close()
			t.Fatalf("open gorm: %v", err)
		}
		return admin, gdb, dbName
	}
}

// execSQLDir 按文件名顺序执行目录里的每个 .sql（真 runner 也按这个顺序，编号即执行序）。
func execSQLDir(t *testing.T, gdb *gorm.DB, dir string) error {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, en := range entries {
		if !en.IsDir() && strings.HasSuffix(en.Name(), ".sql") {
			names = append(names, en.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if err := gdb.Exec(string(b)).Error; err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func liveColumns(gdb *gorm.DB, dialect, table string) (map[string]bool, error) {
	var q string
	switch dialect {
	case "postgres":
		q = `SELECT column_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = lower(?)`
	default:
		q = `SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND LOWER(TABLE_NAME) = LOWER(?)`
	}
	var names []string
	if err := gdb.Raw(q, table).Scan(&names).Error; err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[strings.ToLower(n)] = true
	}
	return out, nil
}

func migDropDB(dialect, dbName string) string {
	if dialect == "postgres" {
		return `DROP DATABASE ` + migQuotePG(dbName) + ` WITH (FORCE)`
	}
	return "DROP DATABASE IF EXISTS `" + dbName + "`"
}

func migQuotePG(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func migPGWithDBName(dsn, dbName string) string {
	parts := strings.Fields(dsn)
	out := make([]string, 0, len(parts)+1)
	replaced := false
	for _, p := range parts {
		if strings.HasPrefix(p, "dbname=") {
			out = append(out, "dbname="+dbName)
			replaced = true
			continue
		}
		out = append(out, p)
	}
	if !replaced {
		out = append(out, "dbname="+dbName)
	}
	return strings.Join(out, " ")
}

// migMySQLExtra 补上多语句执行与时区：一个迁移文件里常有数条语句，单语句模式会在第二条上报错。
func migMySQLExtra(dsn string) string {
	if !strings.Contains(dsn, "multiStatements=true") {
		if strings.Contains(dsn, "?") {
			dsn += "&multiStatements=true"
		} else {
			dsn += "?multiStatements=true"
		}
	}
	if !strings.Contains(dsn, "parseTime=true") {
		dsn += "&parseTime=true&loc=UTC"
	}
	return dsn
}

func silentGorm() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
}
