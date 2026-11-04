package region

// 真实引擎集成测试：sqlmock 从不执行 SQL，建表、版本单调、回滚写回这三件事必须在真库上验。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN / NEXADMIN_IT_PG_DSN 才执行，例如
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	  go test ./modules/region -run TestITMySQL -v
//
// 库名必须留空（AGENTS.md）：夹具自己换一次性库名，DSN 里写了库名就当场失败。
// 隔离：自建一次性库 nexadmin_regionit_<纳秒>，只跑本模块的 region_*.sql，跑完整库 DROP。

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/migrate"
	coreregion "nexadmin/core/region"
	"nexadmin/pkg/idgen"
)

const itDBPrefix = "nexadmin_regionit_"

// itTreePayload 造一份完整合法的三级树（内置数据改一个名字），每份内容摘要必然不同。
func itTreePayload(t *testing.T, mark string) string {
	t.Helper()
	tree, err := coreregion.Tree(3)
	if err != nil {
		t.Fatal(err)
	}
	tree[0].Name = "北京市" + mark
	raw, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func itSHA(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// itSave 走服务自己的落库路径写一份快照（覆盖前留档就发生在这条路径里）。
func itSave(t *testing.T, svc *Service, payload, mark string) {
	t.Helper()
	if err := svc.save(context.Background(), Snapshot{
		SourceURL: "https://example.com/" + mark + ".json", DataDate: "2026-01-02", License: "MIT",
		Payload: payload, SHA256: itSHA(payload),
		Provinces: 34, Prefectures: 333, Counties: 2874,
		SyncedBy: "sync-" + mark, SyncedAt: time.Now(),
	}); err != nil {
		t.Fatalf("save %s: %v", mark, err)
	}
}

// itVersions 读出全部历史版本（按版本升序），用于断言版本号与条数。
func itVersions(t *testing.T, db *gorm.DB) []SnapshotVersion {
	t.Helper()
	var rows []SnapshotVersion
	if err := db.Order("version ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func versionsBrief(rows []SnapshotVersion) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("v%d(len %d, %s)", r.Version, len(r.Payload), r.CreatedBy))
	}
	return out
}

// itCurrent 读当前快照行（id=1 那一条）。
func itCurrent(t *testing.T, db *gorm.DB) Snapshot {
	t.Helper()
	var row Snapshot
	if err := db.Where("id = ?", idgen.ID(snapshotID)).First(&row).Error; err != nil {
		t.Fatalf("读当前快照: %v", err)
	}
	return row
}

// runHistoryInvariants 是引擎无关的一组断言：真库上跑过这段，才算「历史确实落了库」。
func runHistoryInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	t.Cleanup(func() { _ = coreregion.Reset() })
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	ctx := context.Background()

	// 建表结构：迁移跑完后两张表都在，历史表带齐展示与回滚需要的列。
	if !db.Migrator().HasTable("sys_region_snapshot") || !db.Migrator().HasTable("sys_region_snapshot_version") {
		t.Fatal("迁移后快照表或历史表不存在")
	}
	for _, col := range []string{"id", "version", "payload", "sha256", "data_date", "source_url",
		"provinces", "prefectures", "counties", "created_by", "created_at", "updated_at"} {
		if !db.Migrator().HasColumn(&SnapshotVersion{}, col) {
			t.Errorf("sys_region_snapshot_version 缺列 %s", col)
		}
	}

	a, b, c, d := itTreePayload(t, "A"), itTreePayload(t, "B"), itTreePayload(t, "C"), itTreePayload(t, "D")

	// 1. 首次落库没有旧内容，不留档（内置数据不进历史表：它的版本由代码常量决定）。
	itSave(t, svc, a, "A")
	if got := itVersions(t, db); len(got) != 0 {
		t.Fatalf("首次落库不该有历史: %+v", got)
	}

	// 2. 覆盖保存留旧版：旧行 A 变成版本 1，内容与摘要就是当时那一行。
	itSave(t, svc, b, "B")
	got := itVersions(t, db)
	if len(got) != 1 || got[0].Version != 1 || got[0].Payload != a {
		t.Fatalf("覆盖后应留下版本 1（内容是 A）: %+v", got)
	}
	if got[0].SHA256 != itSHA(a) || got[0].CreatedBy != "sync-A" || got[0].Provinces != 34 {
		t.Errorf("版本 1 的摘要/操作人/计数不对: %+v", got[0])
	}

	// 3. 内容没变不重复存：再写一次同样的 B，历史条数不动。
	itSave(t, svc, b, "B2")
	if got = itVersions(t, db); len(got) != 1 {
		t.Fatalf("重复写同一内容不该新增版本: %+v", versionsBrief(got))
	}

	// 4. 版本单调：写到 C 得到版本 2，历史是 [A, B]，当前行是 C。
	itSave(t, svc, c, "C")
	if got = itVersions(t, db); len(got) != 2 || got[1].Version != 2 || got[1].Payload != b {
		t.Fatalf("应新增版本 2（内容是 B）: %+v", versionsBrief(got))
	}

	// 5. 版本号取现有最大值 +1，不是数行数：删掉最老的版本 1 后只剩版本 2，
	//    按行数会发成 2（和已存在的版本撞号），按最大值发才是 3。
	if res := db.Where("version = ?", 1).Delete(&SnapshotVersion{}); res.Error != nil {
		t.Fatalf("删掉版本 1: %v", res.Error)
	}
	itSave(t, svc, d, "D")
	got = itVersions(t, db)
	if len(got) != 2 || got[0].Version != 2 || got[1].Version != 3 || got[1].Payload != c {
		t.Fatalf("删过历史行后版本号仍应递增且不撞号（期望 2、3）: %+v", versionsBrief(got))
	}

	// 6. 回滚到版本 2（内容 B）：那份 payload 重新走完整校验与落库。
	if _, err := svc.RollbackToVersion(ctx, 2, "rollbacker"); err != nil {
		t.Fatalf("回滚到版本 2: %v", err)
	}
	cur := itCurrent(t, db)
	if cur.Payload != b {
		t.Errorf("回滚后当前内容应等于版本 2 的 payload")
	}
	if cur.SHA256 != itSHA(b) {
		t.Errorf("回滚后摘要应为版本 2 的摘要，得到 %s", cur.SHA256)
	}
	got = itVersions(t, db)
	if len(got) != 3 || got[2].Version != 4 || got[2].Payload != d {
		t.Fatalf("回滚前的 D 应留档为版本 4: %+v", versionsBrief(got))
	}
	var back SnapshotVersion
	if err := db.Where("version = ?", 2).First(&back).Error; err != nil {
		t.Fatalf("回滚后版本 2 应仍在历史里: %v", err)
	}
	if back.Payload != b {
		t.Error("版本 2 的内容被改动了")
	}
	// 内存里的生效数据也换了：回滚不是只动库。
	if name, _ := coreregion.Resolve("110000"); name != "北京市B" {
		t.Errorf("回滚后生效的数据应是版本 2 的内容，得到 %q", name)
	}

	// 7. 恢复内置：删当前行但保留历史，并且删之前把当前快照留成一个版本。
	if _, err := svc.Reset(ctx, "resetter"); err != nil {
		t.Fatalf("恢复内置: %v", err)
	}
	var left int64
	if err := db.Model(&Snapshot{}).Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("恢复内置应删掉快照行，还剩 %d 行", left)
	}
	got = itVersions(t, db)
	if len(got) != 4 || got[3].Version != 5 || got[3].Payload != b {
		t.Fatalf("恢复内置前应留档（版本 5 = 回滚后的 B）: %+v", versionsBrief(got))
	}

	// 8. 列表只给摘要字段，正文只在单版本详情里给。
	views, err := svc.ListVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 4 || views[0].Version != 5 {
		t.Fatalf("列表应倒序返回 4 条: %+v", views)
	}
	detail, err := svc.GetVersion(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Payload != b || detail.Version != 2 {
		t.Errorf("详情应带正文: %+v", detail.VersionView)
	}
	if _, err := svc.GetVersion(ctx, 999); err == nil {
		t.Error("不存在的版本应报错，不能返回空详情")
	}
	if _, err := svc.RollbackToVersion(ctx, 999, "rollbacker"); err == nil {
		t.Error("回滚不存在的版本应报错")
	}
}

// runRegionMigrations 按编号顺序跑 migrations/framework/<engine>/region_*.sql。
// 只跑本模块的迁移：同目录里还有其它会话在途的文件，拿全量迁移当夹具会把别人的问题算到这次改动上。
func runRegionMigrations(t *testing.T, engine string, exec func(string) error) {
	t.Helper()
	dir := filepath.Join("..", "..", "migrations", "framework", engine)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "region_") && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) != 3 {
		t.Fatalf("%s 侧的 region 迁移不是 3 份（001/002/003）: %v", engine, names)
	}
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range migrate.Split(string(b)) {
			if err := exec(stmt); err != nil {
				t.Fatalf("%s: %v\n语句: %s", name, err, stmt)
			}
		}
	}
	t.Logf("已执行 %s 迁移：%v", engine, names)
}

func TestITMySQLSnapshotVersions(t *testing.T) {
	runHistoryInvariants(t, itOpenMySQL(t, "mysql"))
}

// TestITMySQLVersionListCap 列表上限单独起一个库：断言的是「库里有多少版本就返回多少（截到上限）」，
// 和上面那组用例留下的版本混在一起就说不清了。
func TestITMySQLVersionListCap(t *testing.T) {
	runVersionListCapInvariant(t, itOpenMySQL(t, "mysql"))
}

// TestITMySQLMariaDBDialect 把 mariadb 目录里的 region 迁移也在真引擎上执行一遍建表。
// 本机没有独立的 MariaDB 实例，这里验的是脚本本身可执行；三份脚本的表与列是否一致，
// 由 scripts/ci/migration_parity.py 与 TestRegionMigrationsThreeEnginesMatch 兜住。
func TestITMySQLMariaDBDialect(t *testing.T) {
	db := itOpenMySQL(t, "mariadb")
	if !db.Migrator().HasTable("sys_region_snapshot_version") {
		t.Fatal("mariadb 侧迁移未建出历史表")
	}
}

// itOpenMySQL 建一次性库、跑 region 迁移，返回 GORM 句柄。
// 收尾在 t.Cleanup 里按顺序做：先关业务连接池 → 删库 → 关管理连接，
// 否则 DROP 会撞上「数据库正在被使用」或者连接已关的错误。
func itOpenMySQL(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL integration test")
	}
	d, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse NEXADMIN_IT_MYSQL_DSN: %v", err)
	}
	// AGENTS.md 那条「库名要留空」在这里是硬闸：继续跑就会往使用者命名的那个库里建表删表。
	if d.DBName != "" {
		t.Fatalf("NEXADMIN_IT_MYSQL_DSN 的库名要留空（夹具自己换一次性库），当前是 %q", d.DBName)
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("mysql not reachable: %v", err)
	}
	dbName := fmt.Sprintf("%s%d", itDBPrefix, time.Now().UnixNano())
	// 库名来自纳秒时间戳，不是请求可控的值；反引号里也不允许出现反引号。
	if strings.ContainsAny(dbName, "`\x00") {
		t.Fatalf("一次性库名不合法: %q", dbName)
	}
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		_ = admin.Close()
		t.Fatalf("create throwaway db: %v", err)
	}
	d.DBName = dbName
	pool, err := sql.Open("mysql", d.FormatDSN())
	if err != nil {
		_ = admin.Close()
		t.Fatalf("open throwaway db: %v", err)
	}
	t.Cleanup(func() {
		_ = pool.Close()
		if _, err := admin.Exec("DROP DATABASE `" + dbName + "`"); err != nil {
			t.Errorf("drop throwaway db %s: %v", dbName, err)
		}
		_ = admin.Close()
	})
	runRegionMigrations(t, engine, func(stmt string) error { _, err := pool.Exec(stmt); return err })
	gdb, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return gdb
}

func TestITPostgresSnapshotVersions(t *testing.T) {
	runHistoryInvariants(t, itOpenPostgres(t))
}

// TestITPostgresVersionListCap 与 MySQL 侧同理：上限的成对断言要一个干净的库，
// 和历史用例留下的版本混在一起就说不清「库里到底有几条」。
func TestITPostgresVersionListCap(t *testing.T) {
	runVersionListCapInvariant(t, itOpenPostgres(t))
}

// itOpenPostgres 建一次性库、跑 postgres 侧的 region 迁移，返回 GORM 句柄。
// 收尾顺序：先关业务连接池 → 删库（WITH (FORCE) 踢掉残留连接）→ 关管理连接。
func itOpenPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("NEXADMIN_IT_PG_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("postgres not reachable: %v", err)
	}
	dbName := fmt.Sprintf("%s%d", itDBPrefix, time.Now().UnixNano())
	// 库名来自纳秒时间戳，不是请求可控的值；双引号标识符里不允许出现双引号。
	if strings.ContainsAny(dbName, "\"\x00") {
		t.Fatalf("一次性库名不合法: %q", dbName)
	}
	if _, err := admin.Exec(`CREATE DATABASE "` + dbName + `"`); err != nil {
		_ = admin.Close()
		t.Fatalf("create throwaway db: %v", err)
	}
	pool, err := sql.Open("pgx", itPGDSN(dsn, dbName))
	if err != nil {
		_ = admin.Close()
		t.Fatalf("open throwaway db: %v", err)
	}
	t.Cleanup(func() {
		_ = pool.Close()
		if _, err := admin.Exec(`DROP DATABASE "` + dbName + `" WITH (FORCE)`); err != nil {
			t.Errorf("drop throwaway db %s: %v", dbName, err)
		}
		_ = admin.Close()
	})
	runRegionMigrations(t, "postgres", func(stmt string) error { _, err := pool.Exec(stmt); return err })
	gdb, err := gorm.Open(gormpg.New(gormpg.Config{Conn: pool}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return gdb
}

// runVersionListCapInvariant 是列表上限的成对断言：正好 maxVersions 条全部返回，
// 多一条就只返回最新的 maxVersions 条（少一半说明 Limit 没生效，多一条也算错）。
// 这里直接写历史表：上限是对「已有多少版本」的截断，不需要走完整校验链路。
func runVersionListCapInvariant(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	ctx := context.Background()
	seed := func(upTo int) {
		for v := 1; v <= upTo; v++ {
			row := SnapshotVersion{Version: v, Payload: "{}", SHA256: fmt.Sprintf("%064d", v), SourceURL: "seed", CreatedBy: "seeder"}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("种入版本 %d: %v", v, err)
			}
		}
	}
	t.Cleanup(func() {
		if err := db.Where("source_url = ?", "seed").Delete(&SnapshotVersion{}).Error; err != nil {
			t.Errorf("清理种子版本: %v", err.Error())
		}
	})

	seed(maxVersions)
	views, err := svc.ListVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != maxVersions {
		t.Fatalf("正好 %d 个版本应全部返回，实际 %d", maxVersions, len(views))
	}
	if views[0].Version != maxVersions {
		t.Errorf("列表应 newest-first，第一条应是 v%d，实际 v%d", maxVersions, views[0].Version)
	}

	seed(maxVersions + 1)
	views, err = svc.ListVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != maxVersions {
		t.Fatalf("多一个版本后列表仍应是 %d 条，实际 %d", maxVersions, len(views))
	}
	if views[0].Version != maxVersions+1 {
		t.Errorf("截断要保留最新版本，第一条应是 v%d，实际 v%d", maxVersions+1, views[0].Version)
	}
}

// itPGDSN 把 key=value 形式的 DSN 里的 dbname 换成一次性库名（覆盖已有值）。
func itPGDSN(dsn, dbName string) string {
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

// TestRegionMigrationsThreeEnginesMatch 不连库也能兜住「漏写一个引擎 / 漏一列」这类漂移：
// 三份 region_003 必须建同一张表、同一组列（类型名各引擎本来就不同，不比对）。
// 真引擎侧另有 TestITMySQLSnapshotVersions / TestITPostgresSnapshotVersions 与
// scripts/ci/migration_parity.py。
func TestRegionMigrationsThreeEnginesMatch(t *testing.T) {
	const name = "region_003_snapshot_version.sql"
	colsByEngine := map[string][]string{}
	for _, engine := range []string{"mysql", "mariadb", "postgres"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "migrations", "framework", engine, name))
		if err != nil {
			t.Fatalf("%s: %v", engine, err)
		}
		colsByEngine[engine] = createTableColumns(string(b))
	}
	want := colsByEngine["mysql"]
	if len(want) == 0 {
		t.Fatal("没解析出列，脚本或解析规则不对")
	}
	for engine, cols := range colsByEngine {
		if strings.Join(cols, ",") != strings.Join(want, ",") {
			t.Errorf("%s 侧的列集合与 mysql 不一致: %v vs %v", engine, cols, want)
		}
	}
}

// createTableColumns 取 CREATE TABLE 块里的列名（跳过约束行），只够发现漏列/漏表。
func createTableColumns(sql string) []string {
	start := strings.Index(strings.ToUpper(sql), "CREATE TABLE")
	if start < 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(sql[start:], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		if strings.HasPrefix(line, ")") || strings.HasPrefix(line, "PRIMARY") ||
			strings.HasPrefix(line, "UNIQUE") || strings.HasPrefix(line, "KEY") ||
			strings.HasPrefix(line, "INDEX") || strings.HasPrefix(line, "CONSTRAINT") {
			continue
		}
		field := strings.Trim(strings.Fields(line)[0], "`\"")
		if field == "CREATE" {
			continue
		}
		out = append(out, strings.ToLower(field))
		if strings.Contains(line, ")") {
			break
		}
	}
	return out
}
