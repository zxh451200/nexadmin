package ticket

// 真三引擎集成测试：018 迁移的「先清洗重复 code、再建唯一索引」在 MySQL / MariaDB / PostgreSQL
// 上真跑一遍，外加同 code 并发保存的实际落库结果与错误口径。
//
// 为什么不能只有 sqlmock：sqlmock 只匹配语句文本、从不执行 SQL——清洗语句在多引擎上的
// 自连接写法、唯一索引是否真的建成、并发下驱动抛回的是不是 1062/23505，全都只有真库能证。
//
// 守卫（缺哪个 DSN 就 SKIP 哪个，SKIP 不算通过）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC'
//	NEXADMIN_IT_MARIADB_DSN='root:pass@tcp(127.0.0.1:3307)/?charset=utf8mb4&parseTime=true&loc=UTC'
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable'
//	go test ./plugins/ticket -run 'TestIT.*(SLAPolicyCodeUnique|SLAPolicyConcurrentSave)' -count=1 -v
//
// 隔离：一次性库前缀 nexadmin_tk_sla_（带引擎名与纳秒），雪花节点固定 42（与其它泳道错开），
// 跑完只 DROP 自己建的库。MySQL DSN 的库名必须留空（AGENTS.md 第 3 节），写了库名当场失败。

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/migrate"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

const (
	// itSLANode 是本泳道的雪花节点号：与 notice/recycle 的 31、ticket 既有单测的 1 都不同，
	// 三条泳道共用同一台库时主键不会撞车。
	itSLANode = 42
	// slaUniqueMigration 是本批新增的 018（三引擎同名，按编号累加）。
	slaUniqueMigration = "018_ticket_sla_code_unique.sql"
)

// itSLAMySQLLane 起一条 MySQL/MariaDB 泳道：建一次性库 → 跑插件迁移。
// before018=true 时跳过 018（清洗用例要的就是「封口前」的世界），false 时跑全部（并发用例要真索引在库上）。
// 句柄必须走 database.Open（生产构造器，装租户回调与 UTC 时区），裸 gorm.Open 会让断言空跑。
func itSLAMySQLLane(t *testing.T, engine string, before018 bool) *gorm.DB {
	t.Helper()
	env := "NEXADMIN_IT_MYSQL_DSN"
	if engine == "mariadb" {
		env = "NEXADMIN_IT_MARIADB_DSN"
	}
	raw := os.Getenv(env)
	if raw == "" {
		t.Skipf("%s not set; skipping real-%s integration test", env, engine)
	}
	d, err := gomysql.ParseDSN(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", env, err)
	}
	if d.DBName != "" {
		t.Fatalf("%s 的库名要留空（夹具自建一次性库），实得 %q", env, d.DBName)
	}
	dbName := fmt.Sprintf("nexadmin_tk_sla_%s_%d", engine, time.Now().UnixNano())
	admin, err := sql.Open("mysql", d.FormatDSN())
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("%s 连不上: %v", env, err)
	}
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		_ = admin.Close()
		t.Fatalf("建一次性库 %s 失败: %v", dbName, err)
	}
	cfg := config.Database{Type: engine, User: d.User, Password: d.Passwd, Name: dbName, NodeID: itSLANode, Host: "127.0.0.1", Port: 3306}
	if host, portStr, e := net.SplitHostPort(d.Addr); e == nil {
		cfg.Host = host
		if p, e2 := strconv.Atoi(portStr); e2 == nil {
			cfg.Port = p
		}
	}
	gdb, err := database.Open(cfg)
	if err != nil {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`")
		_ = admin.Close()
		t.Fatalf("database.Open: %v", err)
	}
	// 收尾顺序：关业务连接池 → 删库 → 关管理连接。反过来的话 DROP 会撞 "database is closed"，
	// 一次性库就永远留在共享实例上。
	t.Cleanup(func() {
		if sqlDB, e := gdb.DB(); e == nil {
			_ = sqlDB.Close()
		}
		if _, e := admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`"); e != nil {
			t.Errorf("drop throwaway db %s: %v", dbName, e)
		}
		_ = admin.Close()
	})
	exclude := ""
	if before018 {
		exclude = slaUniqueMigration
	}
	itSLARunMigrations(t, gdb, engine, exclude)
	return gdb
}

// itSLAPGConfig 把 key=value 形式的 PG DSN 拆成 config.Database（dbname 换成一次性库）。
// 本机要用 host=localhost：pgx 连 127.0.0.1 会超时（AGENTS.md 第 3 节）。
func itSLAPGConfig(raw, dbName string) (config.Database, error) {
	cfg := config.Database{Type: "postgres", Name: dbName, NodeID: itSLANode, Host: "localhost", Port: 5432, User: "postgres"}
	for _, field := range strings.Fields(raw) {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch k {
		case "host":
			cfg.Host = v
		case "port":
			p, err := strconv.Atoi(v)
			if err != nil {
				return config.Database{}, fmt.Errorf("NEXADMIN_IT_PG_DSN 的 port 不是数字: %q", v)
			}
			cfg.Port = p
		case "user":
			cfg.User = v
		case "password":
			cfg.Password = v
		case "sslmode", "connect_timeout", "TimeZone":
			// database.Open 自己拼连接串（sslmode=disable、TimeZone=UTC），这里不接受覆盖。
		case "dbname":
			// 管理连接用的库；业务句柄固定连一次性库。
		default:
			return config.Database{}, fmt.Errorf("NEXADMIN_IT_PG_DSN 有夹具不认的字段 %s=%s（别静默丢掉）", k, v)
		}
	}
	return cfg, nil
}

// itSLAPGLane 是 PG 泳道：管理连接建一次性库，业务句柄走 database.Open。
// before018 的含义与 MySQL 泳道一致（true = 停在封口之前，false = 连 018 一起跑完）。
func itSLAPGLane(t *testing.T, before018 bool) *gorm.DB {
	t.Helper()
	raw := os.Getenv("NEXADMIN_IT_PG_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("postgres 连不上: %v", err)
	}
	dbName := fmt.Sprintf("nexadmin_tk_sla_pg_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + pgQuoteIdent(dbName)); err != nil {
		_ = admin.Close()
		t.Fatalf("建一次性库 %s 失败: %v", dbName, err)
	}
	cfg, err := itSLAPGConfig(raw, dbName)
	if err != nil {
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + pgQuoteIdent(dbName))
		_ = admin.Close()
		t.Fatalf("%v", err)
	}
	gdb, err := database.Open(cfg)
	if err != nil {
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + pgQuoteIdent(dbName))
		_ = admin.Close()
		t.Fatalf("database.Open: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, e := gdb.DB(); e == nil {
			_ = sqlDB.Close()
		}
		// WITH (FORCE)：PG 在有会话时删库直接报 55006。
		if _, e := admin.Exec(`DROP DATABASE IF EXISTS ` + pgQuoteIdent(dbName) + ` WITH (FORCE)`); e != nil {
			t.Errorf("drop throwaway db %s: %v", dbName, e)
		}
		_ = admin.Close()
	})
	exclude := ""
	if before018 {
		exclude = slaUniqueMigration
	}
	itSLARunMigrations(t, gdb, "postgres", exclude)
	return gdb
}

// itSLARunMigrations 按文件名升序执行 migrations/<engine>/ 下全部 SQL，跳过 exclude（可空）。
// 逐条执行与生产 runner 一致：迁移文件一个分号一条语句，不靠驱动的 multiStatements。
func itSLARunMigrations(t *testing.T, gdb *gorm.DB, engine, exclude string) {
	t.Helper()
	names := itSLAMigrationNames(t, engine, exclude)
	if len(names) == 0 {
		t.Fatalf("migrations/%s 下没有待执行的迁移文件", engine)
	}
	for _, name := range names {
		itSLARunMigrationFile(t, gdb, engine, name)
	}
}

func itSLAMigrationNames(t *testing.T, engine, exclude string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join("migrations", engine))
	if err != nil {
		t.Fatalf("读 migrations/%s: %v", engine, err)
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") || e.Name() == exclude {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// itSLARunMigrationFile 执行一份迁移文件（逐条语句），失败时报出文件与语句。
func itSLARunMigrationFile(t *testing.T, gdb *gorm.DB, engine, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("migrations", engine, name))
	if err != nil {
		t.Fatalf("读 %s/%s: %v", engine, name, err)
	}
	for _, stmt := range migrate.Split(string(b)) {
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("%s/%s: %v\n语句：%s", engine, name, err, stmt)
		}
	}
}

// itSLAInsertRow 用裸 SQL 插一行策略：绕开服务层，模拟「并发窗口留下的脏数据」。
// 值一律走占位符（夹具里的 code/label 都是常量，但同一套写法能直接搬到真用例）。
func itSLAInsertRow(gdb *gorm.DB, id int64, code, label string) error {
	return gdb.Exec(`INSERT INTO plg_ticket_sla (id, created_at, updated_at, code, label, workdays, start_minute, end_minute, targets, holidays)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "2026-01-01 08:00:00", "2026-01-01 08:00:00", code, label, "1,2,3,4,5", 540, 1080, "{}", "").Error
}

func itSLASeedRow(t *testing.T, gdb *gorm.DB, id int64, code, label string) {
	t.Helper()
	if err := itSLAInsertRow(gdb, id, code, label); err != nil {
		t.Fatalf("铺脏数据 id=%d code=%q 失败: %v", id, code, err)
	}
}

func itSLARowCount(t *testing.T, gdb *gorm.DB, code string) int64 {
	t.Helper()
	var n int64
	if err := gdb.Raw("SELECT COUNT(*) FROM plg_ticket_sla WHERE code = ?", code).Scan(&n).Error; err != nil {
		t.Fatalf("数 code=%q 的行: %v", code, err)
	}
	return n
}

func itSLASurvivor(t *testing.T, gdb *gorm.DB, code string) (int64, string) {
	t.Helper()
	var row struct {
		ID    int64
		Label string
	}
	if err := gdb.Raw("SELECT id, label FROM plg_ticket_sla WHERE code = ? ORDER BY id ASC LIMIT 1", code).Scan(&row).Error; err != nil {
		t.Fatalf("读 code=%q 的幸存行: %v", code, err)
	}
	return row.ID, row.Label
}

// itSLAIndexFlags 查某个索引在真库里存不存在、唯不唯一（形状照 modules/recycle/soft_delete_it_test.go
// 的 itUsernameIndexNotUnique：MySQL/MariaDB 走 information_schema.statistics，PG 走 pg_index.indisunique）。
func itSLAIndexFlags(t *testing.T, gdb *gorm.DB, index string) (exists, unique bool) {
	t.Helper()
	var n int64
	var err error
	switch gdb.Dialector.Name() {
	case "postgres":
		if err = gdb.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'plg_ticket_sla' AND indexname = ?`, index).Scan(&n).Error; err != nil {
			t.Fatalf("查 PG 索引 %s: %v", index, err)
		}
		exists = n > 0
		if err = gdb.Raw(`SELECT COUNT(*)
			FROM pg_index ix
			JOIN pg_class i ON i.oid = ix.indexrelid
			JOIN pg_class t ON t.oid = ix.indrelid
			WHERE t.relname = 'plg_ticket_sla' AND i.relname = ? AND ix.indisunique`, index).Scan(&n).Error; err != nil {
			t.Fatalf("查 PG 索引唯一性 %s: %v", index, err)
		}
		unique = n > 0
	default: // mysql / mariadb
		if err = gdb.Raw("SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'plg_ticket_sla' AND index_name = ?", index).Scan(&n).Error; err != nil {
			t.Fatalf("查 %s 索引: %v", index, err)
		}
		exists = n > 0
		if err = gdb.Raw("SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'plg_ticket_sla' AND index_name = ? AND non_unique = 0", index).Scan(&n).Error; err != nil {
			t.Fatalf("查 %s 索引唯一性: %v", index, err)
		}
		unique = n > 0
	}
	return exists, unique
}

// runSLACodeUniqueMigration 是三引擎共用的清洗+加约束断言：
//  1. 铺同 code 的重复行，并确认迁移前确实挡不住（唯一轴不存在，重复插得进去）；
//  2. 跑 018；
//  3. 只剩 id 最小的一行（幸存的是读侧一直在用的那一行，不是任意一行），其余 code 与默认行不受影响；
//  4. 唯一索引 uk_plg_ticket_sla_code 真的在库上、且真的是唯一；
//  5. 再插同 code 被驱动拦下，且拦下的文本能被应用层的判据认出来（这是并发用例里 409 的前提）。
func runSLACodeUniqueMigration(t *testing.T, gdb *gorm.DB, engine string) {
	t.Helper()
	if err := idgen.Init(itSLANode); err != nil {
		t.Fatal(err)
	}
	itSLASeedRow(t, gdb, 500001, "dup-a", "A1")
	itSLASeedRow(t, gdb, 500002, "dup-a", "A2")
	itSLASeedRow(t, gdb, 500003, "dup-a", "A3")
	itSLASeedRow(t, gdb, 500010, "dup-b", "B1")
	itSLASeedRow(t, gdb, 500011, "dup-b", "B2")
	itSLASeedRow(t, gdb, 500020, "", "默认")
	itSLASeedRow(t, gdb, 500030, "solo", "S")

	// 迁移前：013 那个非唯一索引就是本用例的前提，它要已经唯一了这条用例在测别的东西。
	if _, wasUnique := itSLAIndexFlags(t, gdb, "idx_plg_ticket_sla_code"); wasUnique {
		t.Fatal("idx_plg_ticket_sla_code 已唯一：013 的前提变了，清洗用例失去意义")
	}
	// 迁移前重复 code 插得进去（这就是并发窗口留下的形态）。
	itSLASeedRow(t, gdb, 500004, "dup-a", "A4")
	if n := itSLARowCount(t, gdb, "dup-a"); n != 4 {
		t.Fatalf("018 之前 dup-a 应有 4 行（真库没把脏数据铺上？）：%d", n)
	}

	itSLARunMigrationFile(t, gdb, engine, slaUniqueMigration)

	if n := itSLARowCount(t, gdb, "dup-a"); n != 1 {
		t.Fatalf("清洗后 dup-a 应只剩 1 行，实得 %d", n)
	}
	if id, label := itSLASurvivor(t, gdb, "dup-a"); id != 500001 || label != "A1" {
		t.Fatalf("dup-a 幸存行应是 id 最小的 500001/A1（读侧一直在用的那行），实得 %d/%q", id, label)
	}
	if n := itSLARowCount(t, gdb, "dup-b"); n != 1 {
		t.Fatalf("清洗后 dup-b 应只剩 1 行，实得 %d", n)
	}
	if id, _ := itSLASurvivor(t, gdb, "dup-b"); id != 500010 {
		t.Fatalf("dup-b 幸存行应是 500010，实得 %d", id)
	}
	// 没有重复的 code 一律不动：默认策略行与 solo 都还在原 id 上。
	for code, want := range map[string]int64{"": 500020, "solo": 500030} {
		if n := itSLARowCount(t, gdb, code); n != 1 {
			t.Fatalf("code=%q 不该被动过，实得 %d 行", code, n)
		}
		if id, _ := itSLASurvivor(t, gdb, code); id != want {
			t.Fatalf("code=%q 的行应是 %d，实得 %d", code, want, id)
		}
	}
	var total int64
	if err := gdb.Raw("SELECT COUNT(*) FROM plg_ticket_sla").Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("清洗后全表应剩 4 行（dup-a、dup-b、默认、solo），实得 %d", total)
	}

	exists, unique := itSLAIndexFlags(t, gdb, "uk_plg_ticket_sla_code")
	if !exists {
		t.Fatalf("%s 上没建出 uk_plg_ticket_sla_code", engine)
	}
	if !unique {
		t.Fatalf("uk_plg_ticket_sla_code 建成了普通索引")
	}

	// 封口之后同 code 必须被驱动拦下，而且拦下的文本要能被应用层认出来（sla.go 的 slaDupKeyRe）。
	dupErr := itSLAInsertRow(gdb, 599999, "dup-a", "A5")
	if dupErr == nil {
		t.Fatal("加了唯一索引还能插入同 code：索引没生效")
	}
	if !isSLADuplicateKeyErr(dupErr) {
		t.Fatalf("应用层判据认不出 %s 的真实重复键文本：%v", engine, dupErr)
	}
	marker := "1062"
	if engine == "postgres" {
		marker = "23505"
	}
	if !strings.Contains(dupErr.Error(), marker) {
		t.Fatalf("%s 的重复键错误里没有 %s：%v", engine, marker, dupErr)
	}
}

func TestITMySQLSLAPolicyCodeUniqueMigration(t *testing.T) {
	runSLACodeUniqueMigration(t, itSLAMySQLLane(t, "mysql", true), "mysql")
}

func TestITMariaDBSLAPolicyCodeUniqueMigration(t *testing.T) {
	runSLACodeUniqueMigration(t, itSLAMySQLLane(t, "mariadb", true), "mariadb")
}

func TestITPostgresSLAPolicyCodeUniqueMigration(t *testing.T) {
	runSLACodeUniqueMigration(t, itSLAPGLane(t, true), "postgres")
}

// runSLAPolicyConcurrentSave 是同 code 并发保存的断言集：
//   - 表上只剩一行（封口前这里是 N 行）；
//   - 至少一条赢（拿到成功），至少一条输在唯一索引上（拿到 409 ticket.sla_code_conflict）；
//   - 没有任何一条以 500/裸驱动错误出去。
//
// 有效性靠变异反证：018 的唯一索引改成普通索引 → 行数断言红；
// 删掉 sla.go 的撞码分支 → 「至少一条 409」与「无其他错误」红。
func runSLAPolicyConcurrentSave(t *testing.T, gdb *gorm.DB, engine string) {
	t.Helper()
	if err := idgen.Init(itSLANode); err != nil {
		t.Fatal(err)
	}
	// 并发用例的仪器先自查：池只有 1 条连接的话，goroutine 会在连接上排队，
	// 删掉唯一索引也照样全绿（AGENTS.md：这类用例必须真重叠）。
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	if maxOpen := sqlDB.Stats().MaxOpenConnections; maxOpen < 2 {
		t.Fatalf("连接池上限 %d，并发用例不会真重叠", maxOpen)
	}
	ctx := itCtx()
	svc := NewService(gdb, nopLogger{}).WithTimezone("UTC")
	setting := SLASetting{
		Code: "race-desk", Label: "撞码策略",
		Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 540, EndMinute: 1080,
		Targets: DefaultSLA().Targets, Holidays: []string{"2026-05-01"},
	}
	const racers = 8
	errs := make([]error, racers)
	start := make(chan struct{})
	// 起跑前先把连接池暖到 racers 条：冷池下八条 goroutine 会排在「建连接」上串行化，
	// PG 上实测变成一条插入 + 七条更新、全都成功——那是空跑，不是并发。
	var warm sync.WaitGroup
	for i := 0; i < racers; i++ {
		warm.Add(1)
		go func() {
			defer warm.Done()
			var one int64
			if err := gdb.Raw("SELECT 1").Scan(&one).Error; err != nil {
				t.Errorf("暖连接失败（%s）: %v", engine, err)
			}
		}()
	}
	warm.Wait()
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // 起跑线对齐，八条一起冲出去
			_, errs[i] = svc.SaveSLAPolicy(ctx, setting)
		}(i)
	}
	close(start)
	wg.Wait()

	if n := itSLARowCount(t, gdb, "race-desk"); n != 1 {
		t.Fatalf("%s 上同 code 并发保存落了 %d 行，应为 1 行（唯一索引没兜住）", engine, n)
	}
	var ok, conflict int
	var others []string
	marker := "1062"
	if engine == "postgres" {
		marker = "23505"
	}
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		default:
			var e *response.Error
			if !errors.As(err, &e) {
				others = append(others, fmt.Sprintf("%T %v", err, err))
				continue
			}
			if e.Status != 409 || e.Key != "ticket.sla_code_conflict" {
				others = append(others, fmt.Sprintf("%d %q", e.Status, e.Key))
				continue
			}
			// 索引名是「谁拦的」唯一取证：驱动原文要随 Wrap 留住，否则排障时看不见。
			if e.Err == nil || !strings.Contains(e.Err.Error(), marker) {
				others = append(others, fmt.Sprintf("409 但没带驱动原文: %v", e.Err))
				continue
			}
			conflict++
		}
	}
	if ok == 0 {
		t.Fatalf("八条并发保存没有一条成功：%+v", errs)
	}
	if len(others) > 0 {
		t.Fatalf("并发保存里有非 4xx 业务的错误（500/裸驱动文本算漏网）：%v", others)
	}
	// 这条是本用例的命门：若八条被预检彻底串行化（全都走更新分支），
	// 唯一索引一次都没被撞到，用例就成了空跑——判失败，逼着把重叠做实。
	if conflict == 0 {
		t.Fatalf("没有任何一条撞上唯一索引（成功 %d 条）：预检把请求串行化了，本用例没测到索引兜底", ok)
	}
	// 幸存的那行必须是可读回的那套策略，不是清洗留下的空壳。
	got, err := svc.GetSLAPolicy(ctx, "race-desk")
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "race-desk" || got.Label != "撞码策略" || len(got.Holidays) != 1 {
		t.Fatalf("并发后读回的策略不对：%+v", got)
	}
	// 删侧口径：唯一索引之后一个 code 至多一行，DELETE WHERE code = ? 不带 limit 就是删那一行。
	if err := svc.DeleteSLAPolicy(ctx, "race-desk"); err != nil {
		t.Fatal(err)
	}
	if n := itSLARowCount(t, gdb, "race-desk"); n != 0 {
		t.Fatalf("删除后还剩 %d 行", n)
	}
}

func TestITMySQLSLAPolicyConcurrentSave(t *testing.T) {
	runSLAPolicyConcurrentSave(t, itSLAMySQLLane(t, "mysql", false), "mysql")
}

func TestITMariaDBSLAPolicyConcurrentSave(t *testing.T) {
	runSLAPolicyConcurrentSave(t, itSLAMySQLLane(t, "mariadb", false), "mariadb")
}

func TestITPostgresSLAPolicyConcurrentSave(t *testing.T) {
	runSLAPolicyConcurrentSave(t, itSLAPGLane(t, false), "postgres")
}
