package cron

// 真引擎集成测试：cron 插件里三段手写/动态拼的 SQL 在 sqlmock 上从不执行——
//   - builtin.go:187  `Exec("DELETE FROM "+table+" WHERE tenant_id = ? AND created_at < ?")`
//   - service.go:459  `SUBSTR(output, 1, 500)` + `ORDER BY started_at DESC, id DESC`
//   - service.go:334  `LOWER(name) LIKE LOWER(?)`
// 本文件把它们放到真 MySQL / PostgreSQL 上跑真数据。
//
// 守卫：默认跳过，设置对应 DSN 才执行（MySQL 侧库名必须留空，见 AGENTS.md）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass sslmode=disable' \
//	  GOMAXPROCS=2 go test ./plugins/cron -p 1 -run TestITCron -v
//
// 隔离：一次性库 nexadmin_rcov_cron_<纳秒>，只建本用例需要的表，收尾 DROP 自己的库。
// 句柄必须走 database.Open（生产构造器）：ListJobs / ListLogs / ExportLogs 的租户隔离靠的是
// 它注册的 tenantPlugin，裸 gorm.Open 的句柄上这条轴根本不存在（delete_many_test.go 的 mockDB
// 就是裸句柄 + sqlmock，那里只钉语句文本，注释里也写明了隔离由 core/database 自己保证）。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	gomysql "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/migrate"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	rcovTenants int64 = 7
	rcovOther   int64 = 8
)

// rcovIDBase 是夹具的固定号段：显式 id 让「同 started_at 比 id」这类断言可复现。
const rcovIDBase int64 = 900000

var rcovOnce sync.Once

// rcovLiveEngines 是本机可跑的两条引擎泳道（MariaDB 与 MySQL 共用同一套驱动/方言，
// 这台机器上只有 mysqld 一个实例，见报告）。
var rcovLiveEngines = []string{"mysql", "postgres"}

func rcovCtx(tid int64) context.Context { return tenant.With(context.Background(), idgen.ID(tid)) }

// openRcov 建一次性库、跑建表迁移、返回生产构造器的句柄。
func openRcov(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	rcovOnce.Do(func() {
		if err := idgen.Init(43); err != nil {
			t.Fatalf("idgen.Init(43): %v", err)
		}
	})
	switch engine {
	case "mysql":
		return openRcovMySQL(t)
	case "postgres":
		return openRcovPG(t)
	}
	t.Fatalf("unknown engine %q", engine)
	return nil
}

// rcovFiles 是本用例要建的表：sys_oper_log / sys_login_log（clean_logs 的目标）+ 插件自己的两张。
func rcovFiles(engine string) []string {
	pl := "migrations/" + engine
	return []string{
		"../../migrations/framework/" + engine + "/log_001_init.sql",
		"../cron/" + pl + "/001_init.sql",
		"../cron/" + pl + "/002_job_timeout.sql",
	}
}

func runRcovFiles(t *testing.T, db *gorm.DB, files []string) {
	t.Helper()
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读迁移 %s：%v", f, err)
		}
		for _, stmt := range migrate.Split(string(b)) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("%s: %v\n语句：%s", f, err, stmt)
			}
		}
	}
}

func openRcovMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	raw := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL cron test")
	}
	d, err := gomysql.ParseDSN(raw)
	if err != nil {
		t.Fatalf("parse NEXADMIN_IT_MYSQL_DSN: %v", err)
	}
	if d.DBName != "" {
		t.Fatalf("NEXADMIN_IT_MYSQL_DSN 的库名要留空（夹具自己换一次性库），当前是 %q", d.DBName)
	}
	admin, err := sql.Open("mysql", raw)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("mysql not reachable: %v", err)
	}
	dbName := fmt.Sprintf("nexadmin_rcov_cron_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		_ = admin.Close()
		t.Fatalf("create throwaway db: %v", err)
	}
	cfg := config.Database{Type: "mysql", User: d.User, Password: d.Passwd, Host: "127.0.0.1", Port: 3306, Name: dbName, NodeID: 43}
	if host, portStr, e := net.SplitHostPort(d.Addr); e == nil {
		cfg.Host = host
		if p, e2 := strconv.Atoi(portStr); e2 == nil {
			cfg.Port = p
		}
	} else if d.Addr != "" {
		cfg.Host = d.Addr
	}
	return finishRcov(t, admin, dbName, cfg)
}

func openRcovPG(t *testing.T) *gorm.DB {
	t.Helper()
	raw := os.Getenv("NEXADMIN_IT_PG_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL cron test")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("postgres not reachable: %v", err)
	}
	cfg := config.Database{Type: "postgres", Host: "localhost", Port: 5432, NodeID: 43}
	for _, p := range strings.Fields(raw) {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		switch k {
		case "host":
			cfg.Host = v
		case "port":
			cfg.Port, _ = strconv.Atoi(v)
		case "user":
			cfg.User = v
		case "password":
			cfg.Password = v
		}
	}
	if cfg.User == "" {
		_ = admin.Close()
		t.Fatalf("NEXADMIN_IT_PG_DSN 需要 user=（当前 %q）", raw)
	}
	dbName := fmt.Sprintf("nexadmin_rcov_cron_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE "` + dbName + `" ENCODING 'UTF8'`); err != nil {
		_ = admin.Close()
		t.Fatalf("create throwaway db: %v", err)
	}
	cfg.Name = dbName
	return finishRcov(t, admin, dbName, cfg)
}

// finishRcov 的收尾顺序必须是：关业务连接池 → 删库 → 关管理连接。
func finishRcov(t *testing.T, admin *sql.DB, dbName string, cfg config.Database) *gorm.DB {
	t.Helper()
	var gdb *gorm.DB
	t.Cleanup(func() {
		if gdb != nil {
			if pool, err := gdb.DB(); err == nil {
				_ = pool.Close()
			}
		}
		stmt := "DROP DATABASE IF EXISTS `" + dbName + "`"
		if cfg.Type == "postgres" {
			stmt = `DROP DATABASE IF EXISTS "` + dbName + `" WITH (FORCE)`
		}
		if _, err := admin.Exec(stmt); err != nil {
			t.Errorf("drop throwaway db %s: %v", dbName, err)
		}
		_ = admin.Close()
	})
	gdb, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("database.Open(%s): %v", cfg.Type, err)
	}
	runRcovFiles(t, gdb, rcovFiles(cfg.Type))
	return gdb
}

func rcovCount(t *testing.T, db *gorm.DB, table string, where string, args ...any) int64 {
	t.Helper()
	var n int64
	tx := db.Table(table)
	if where != "" {
		tx = tx.Where(where, args...)
	}
	if err := tx.Count(&n).Error; err != nil {
		t.Fatalf("count %s：%v", table, err)
	}
	return n
}

// seedCleanTargets 给两张日志表铺四种行：本租户的旧行（该删）、本租户的新行（该留）、
// 别的租户的旧行（必须留——原生 SQL 不经过租户 scope，靠的就是 builtin.go:187 那句显式 tenant_id）。
func seedCleanTargets(t *testing.T, db *gorm.DB) {
	t.Helper()
	fresh := time.Now().UTC().Add(-time.Hour)
	stampOld := time.Now().UTC().Add(-2400 * time.Hour)
	rows := []struct {
		table string
		id    int64
		tid   int64
		when  time.Time
		op    string
	}{
		{"sys_oper_log", rcovIDBase + 11, rcovTenants, stampOld, "A"},
		{"sys_oper_log", rcovIDBase + 12, rcovTenants, stampOld, "B"},
		{"sys_oper_log", rcovIDBase + 13, rcovTenants, fresh, "C"},
		{"sys_oper_log", rcovIDBase + 14, rcovOther, stampOld, "D"},
		{"sys_login_log", rcovIDBase + 21, rcovTenants, stampOld, "A"},
		{"sys_login_log", rcovIDBase + 22, rcovTenants, fresh, "B"},
		{"sys_login_log", rcovIDBase + 23, rcovOther, stampOld, "C"},
	}
	for _, r := range rows {
		row := map[string]any{
			"id": r.id, "tenant_id": r.tid, "status": "success",
			"created_at": r.when, "updated_at": r.when,
		}
		if r.table == "sys_oper_log" {
			row["module"], row["type"], row["operator"] = "定时任务", "clean", "rcov"
			row["method"], row["url"], row["cost_ms"] = "POST", "/api/x", 5
		} else {
			row["username"], row["login_type"] = "rcov", "password"
		}
		if err := db.Table(r.table).Create(row).Error; err != nil {
			t.Fatalf("播种 %s：%v", r.table, err)
		}
	}
}

func rcovEnv(db *gorm.DB, tid int64) *Env {
	return &Env{DB: db.WithContext(rcovCtx(tid)), Log: zap.NewNop(), TenantID: tid, JobID: rcovIDBase}
}

// TestITCronCleanLogsLive 在真引擎上执行 builtin.go:187 那句字符串拼接的 DELETE：
// 真删除、真行数、真租户边界，并且证明表名不可能来自请求输入
// （targets 是 {"oper","login"} 的闭集，其余取值在任何语句之前就报错，一行都不动）。
func TestITCronCleanLogsLive(t *testing.T) {
	for _, engine := range rcovLiveEngines {
		t.Run(engine, func(t *testing.T) {
			db := openRcov(t, engine)
			ctx := rcovCtx(rcovTenants)
			env := rcovEnv(db, rcovTenants)

			// 反证（表名来自输入）：把别的表名塞进 targets，必须被拒且不动任何表。
			for _, bad := range []string{
				`{"days":1,"targets":["plg_cron_job"]}`,
				`{"days":1,"targets":["sys_user"]}`,
				`{"days":1,"targets":["oper\"] OR 1=1 --"]}`,
				`{"days":1,"targets":["oper;DROP TABLE sys_oper_log"]}`,
				`{"days":0,"targets":["oper","nope"]}`,
				`{"days":9999}`,
				`{"days":-1}`,
			} {
				out, err := runCleanLogs(ctx, env, bad)
				if err == nil {
					t.Fatalf("params %s 被接受（output=%q）：表名/目标就不是闭集", bad, out)
				}
			}
			if n := rcovCount(t, db, "sys_oper_log", ""); n != 0 {
				t.Fatalf("非法参数竟然删了 %d 行日志：拒绝发生在 DELETE 之后", n)
			}
			if n := rcovCount(t, db, "plg_cron_job", ""); n != 0 {
				t.Errorf("非法 targets 动了 plg_cron_job（%d 行）：表名可以被输入控制", n)
			}

			seedCleanTargets(t, db)
			out, err := runCleanLogs(ctx, env, `{"days":30}`)
			if err != nil {
				t.Fatalf("clean_logs 失败：%v\noutput=%s", err, out)
			}
			// 真行数：本租户两张表的旧行各 1/2 条被删，报数必须与库里实际剩下的一致。
			if !strings.Contains(out, "sys_oper_log: deleted 2 rows older than 30 days") ||
				!strings.Contains(out, "sys_login_log: deleted 1 rows older than 30 days") {
				t.Errorf("clean_logs 的报数不对：%s", out)
			}
			if n := rcovCount(t, db, "sys_oper_log", "tenant_id = ? AND created_at < ?", rcovTenants, time.Now().UTC().AddDate(0, 0, -30)); n != 0 {
				t.Errorf("本租户的旧操作日志还剩 %d 行，DELETE 没落到引擎上", n)
			}
			if n := rcovCount(t, db, "sys_oper_log", "tenant_id = ?", rcovTenants); n != 1 {
				t.Errorf("本租户剩余操作日志 = %d，想要只剩那条新行（删多了）", n)
			}
			// 别的租户的旧行必须一行不少：原生 SQL 不经过租户 scope，全靠语句里显式的 tenant_id。
			if n := rcovCount(t, db, "sys_oper_log", "tenant_id = ?", rcovOther); n != 1 {
				t.Errorf("跨租户误删：sys_oper_log 租户 %d 剩 %d 行，想要 1 行", rcovOther, n)
			}
			if n := rcovCount(t, db, "sys_login_log", "tenant_id = ?", rcovOther); n != 1 {
				t.Errorf("跨租户误删：sys_login_log 租户 %d 剩 %d 行，想要 1 行", rcovOther, n)
			}

			// 表不存在（内置模块被停用/尚未迁移）：优雅跳过并把这一张表报成失败，另一张照常清。
			if err := db.Exec("DROP TABLE sys_login_log").Error; err != nil {
				t.Fatalf("drop sys_login_log：%v", err)
			}
			seedCleanTargets2(t, db) // 再铺一批本租户旧行，验证 oper 仍被清、login 只报缺失
			out, err = runCleanLogs(ctx, env, `{"days":30}`)
			if err == nil || !strings.Contains(err.Error(), "clean_logs incomplete: sys_login_log") {
				t.Fatalf("缺表时应当报错点名 sys_login_log，实际 err=%v out=%s", err, out)
			}
			if !strings.Contains(out, "sys_login_log: table does not exist, skipped") {
				t.Errorf("缺表的输出没有写明跳过：%s", out)
			}
			if !strings.Contains(out, "sys_oper_log: deleted 2 rows") {
				t.Errorf("另一张表应当照常清理，output=%s", out)
			}
			if n := rcovCount(t, db, "sys_oper_log", "tenant_id = ? AND created_at < ?", rcovTenants, time.Now().UTC().AddDate(0, 0, -30)); n != 0 {
				t.Errorf("缺表分支里本租户的旧操作日志没删净，剩 %d 行", n)
			}
		})
	}
}

// seedCleanTargets2 只补本租户的旧行（第二轮用，别的不受影响）。
func seedCleanTargets2(t *testing.T, db *gorm.DB) {
	t.Helper()
	old := time.Now().UTC().Add(-200 * 24 * time.Hour)
	for i, id := range []int64{rcovIDBase + 31, rcovIDBase + 32} {
		if err := db.Table("sys_oper_log").Create(map[string]any{
			"id": id, "tenant_id": rcovTenants, "module": "定时任务", "type": "clean",
			"operator": "rcov", "status": "success", "method": "POST", "url": "/api/x",
			"cost_ms": 1, "created_at": old, "updated_at": old,
		}).Error; err != nil {
			t.Fatalf("补种 sys_oper_log %d：%v", i, err)
		}
	}
}

// rcovLogCols 是翻倍插入用的列清单（与 001_init.sql 的建表列一致）。
const rcovLogCols = "id, tenant_id, job_id, job_name, handler, trigger_type, status, attempts, " +
	"started_at, finished_at, cost_ms, output, error, created_at, updated_at"

// rcovDoubleLogs 把某个租户在 plg_cron_log 里的行数翻倍到至少 want 行（一条 INSERT..SELECT 一轮）。
// 判据必须是「该租户的行数」：ExportLogs 的上限是按租户 scope 后的 count 比的，
// 拿全表行数当闸会铺出一个够不着上限的数据集（第一次跑就是这样：全表 20480、本租户 16384）。
func rcovDoubleLogs(t *testing.T, db *gorm.DB, tid int64, want int64) {
	t.Helper()
	for {
		n := rcovCount(t, db, "plg_cron_log", "tenant_id = ?", tid)
		if n >= want {
			return
		}
		var maxID int64
		if err := db.Table("plg_cron_log").Select("MAX(id)").Scan(&maxID).Error; err != nil {
			t.Fatalf("max(id)：%v", err)
		}
		base := maxID + 1
		sql := "INSERT INTO plg_cron_log (" + rcovLogCols + ") SELECT id + ?, tenant_id, job_id, job_name, handler, " +
			"trigger_type, status, attempts, started_at, finished_at, cost_ms, output, error, created_at, updated_at " +
			"FROM plg_cron_log"
		if err := db.Exec(sql, base).Error; err != nil {
			t.Fatalf("翻倍插入（当前 %d 行，base %d）：%v", n, base, err)
		}
		if rcovCount(t, db, "plg_cron_log", "tenant_id = ?", tid) <= n {
			t.Fatalf("翻倍插入没有增行：%d -> 不变", n)
		}
	}
}

// TestITCronExportLogsLive 是导出路径的真引擎版本（sqlmock 那条只钉了 count 的正则）：
//   - SUBSTR(output, 1, 500) 在两种引擎上都要按「字符」截到 500（MySQL 若按字节会把中文截成半个字）；
//   - ORDER BY started_at DESC, id DESC：同一分钟的行必须按 id 倒序，不能靠行的物理顺序；
//   - 行数超限必须在取数据之前拒绝（真数据，不是 mock 的一行）；
//   - 别人的租户不能出现在导出里。
func TestITCronExportLogsLive(t *testing.T) {
	for _, engine := range rcovLiveEngines {
		t.Run(engine, func(t *testing.T) {
			db := openRcov(t, engine)
			svc := NewService(db, nil)
			ctx := rcovCtx(rcovTenants)

			long := strings.Repeat("输出x", 300) // 1200 个字符，远超 500
			short := "GET https://example.com -> 200"
			// 三条同一 started_at、不同 id：排序轴只在这一分钟里才有区分度。
			same := time.Now().UTC().Truncate(time.Minute)
			seed := []Log{
				{JobID: idgen.ID(rcovIDBase + 1), JobName: "rcov-a", Handler: "http", TriggerType: TriggerSchedule,
					Status: StatusSuccess, Attempts: 1, CostMs: 10, StartedAt: same.Add(-time.Hour), Output: short},
				{JobID: idgen.ID(rcovIDBase + 1), JobName: "rcov-b", Handler: "clean_logs", TriggerType: TriggerManual,
					Status: StatusSuccess, Attempts: 2, CostMs: 20, StartedAt: same, Output: long},
				{JobID: idgen.ID(rcovIDBase + 1), JobName: "rcov-c", Handler: "log", TriggerType: TriggerManual,
					Status: StatusFailed, Attempts: 3, CostMs: 30, StartedAt: same, Error: long},
				{JobID: idgen.ID(rcovIDBase + 1), JobName: "rcov-d", Handler: "log", TriggerType: TriggerManual,
					Status: StatusSuccess, Attempts: 1, CostMs: 40, StartedAt: same, Error: "boom"},
			}
			for i := range seed {
				row := seed[i]
				row.ID = idgen.ID(rcovIDBase + int64(100+i))
				row.TenantID = idgen.ID(rcovTenants)
				if err := db.WithContext(ctx).Create(&row).Error; err != nil {
					t.Fatalf("种日志 %d：%v", i, err)
				}
			}
			// 别的租户一行更新的，绝不能出现在本租户的列表/导出里。
			foreign := Log{JobID: idgen.ID(rcovIDBase + 9), JobName: "别人家的任务", Handler: "log",
				TriggerType: TriggerManual, Status: StatusSuccess, StartedAt: same.Add(time.Hour), Output: "mine"}
			foreign.ID = idgen.ID(rcovIDBase + 500)
			foreign.TenantID = idgen.ID(rcovOther)
			if err := db.WithContext(rcovCtx(rcovOther)).Create(&foreign).Error; err != nil {
				t.Fatalf("种跨租户日志：%v", err)
			}

			rows, total, err := svc.ListLogs(ctx, LogQuery{Page: 1, PageSize: 20})
			if err != nil {
				t.Fatalf("ListLogs：%v", err)
			}
			if total != int64(len(seed)) || len(rows) != len(seed) {
				t.Fatalf("ListLogs total=%d rows=%d，想要 %d（租户条件在真引擎上没生效）", total, len(rows), len(seed))
			}
			for _, r := range rows {
				if r.JobName == foreign.JobName {
					t.Fatalf("导出/列表里出现了别的租户的行：%+v", r)
				}
			}
			// SUBSTR：output / error 都截到 500 个字符（不是字节）。
			for _, r := range rows {
				for _, cell := range []struct {
					name string
					val  string
				}{{"output", r.Output}, {"error", r.Error}} {
					if n := utf8.RuneCountInString(cell.val); n > 500 {
						t.Errorf("%s.%s 有 %d 个字符，SUBSTR(...,1,500) 没生效", r.JobName, cell.name, n)
					}
					if !utf8.ValidString(cell.val) {
						t.Errorf("%s.%s 截断后不是合法 UTF-8（按字节截的）：%q", r.JobName, cell.name, cell.val)
					}
				}
			}
			var longRow, shortRow *Log
			for i := range rows {
				switch rows[i].JobName {
				case "rcov-b":
					longRow = &rows[i]
				case "rcov-a":
					shortRow = &rows[i]
				}
			}
			if longRow == nil || shortRow == nil {
				t.Fatal("按 job_name 没找回两条目标行")
			}
			if utf8.RuneCountInString(longRow.Output) != 500 {
				t.Errorf("SUBSTR 截出来的长度 = %d 字符，想要正好 500：%q", utf8.RuneCountInString(longRow.Output), longRow.Output)
			}
			if longRow.Output != string([]rune(long)[:500]) {
				t.Errorf("SUBSTR 截的位置不对（前 500 字符不匹配）：%q", longRow.Output[:min(30, len(longRow.Output))])
			}
			if shortRow.Output != short {
				t.Errorf("短输出被改坏了：%q，想要 %q", shortRow.Output, short)
			}
			// 全文走 GetLog，不受 500 限制。
			full, err := svc.GetLog(ctx, idgen.ID(rcovIDBase+101))
			if err != nil {
				t.Fatalf("GetLog：%v", err)
			}
			if full.Output != long {
				t.Errorf("GetLog 拿到的是截断过的输出（%d 字符），想要全文 %d 字符", utf8.RuneCountInString(full.Output), utf8.RuneCountInString(long))
			}
			// ORDER BY started_at DESC, id DESC：最新一分钟在前，同分钟内 id 大的在前。
			want := []string{"rcov-d", "rcov-c", "rcov-b", "rcov-a"}
			got := make([]string, 0, len(rows))
			for _, r := range rows {
				got = append(got, r.JobName)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("ListLogs 顺序 = %v，想要 %v（started_at DESC, id DESC 在真引擎上不成立）", got, want)
			}

			exported, err := svc.ExportLogs(ctx, LogQuery{})
			if err != nil {
				t.Fatalf("ExportLogs（未超限）：%v", err)
			}
			if len(exported) != len(seed) {
				t.Errorf("ExportLogs 行数 = %d，想要 %d", len(exported), len(seed))
			}
			// 同一套 ORDER BY / SUBSTR，导出口径与列表一致。
			if strings.Join(namesOf(exported), ",") != strings.Join(want, ",") {
				t.Errorf("ExportLogs 顺序 = %v，想要 %v", namesOf(exported), want)
			}
			csv := BuildLogsCSV(exported)
			if !strings.Contains(string(csv), longRow.Output) || strings.Contains(string(csv), long) {
				t.Errorf("CSV 里的输出没带 SUBSTR 的截断（前 500 字符在、全文不该在）")
			}

			// 超上限：本租户真数据堆到 logExportMax+1 行以上，必须在取数据之前拒绝。
			rcovDoubleLogs(t, db, rcovTenants, logExportMax+1)
			tenantRows := rcovCount(t, db, "plg_cron_log", "tenant_id = ?", rcovTenants)
			if tenantRows <= logExportMax {
				t.Fatalf("夹具只给租户 %d 铺了 %d 行，够不到上限 %d", rcovTenants, tenantRows, logExportMax)
			}
			rows2, err := svc.ExportLogs(ctx, LogQuery{})
			if err == nil {
				t.Fatalf("命中 %d 行（> %d）的导出被放行，返回 %d 行", tenantRows, logExportMax, len(rows2))
			}
			var re *response.Error
			if !errors.As(err, &re) {
				t.Fatalf("超限错误 = %v，想要 *response.Error", err)
			}
			if re.Key != "cron.export_too_many_rows" || len(re.Args) != 2 || re.Args[1] != int(logExportMax) {
				t.Errorf("超限错误 key=%q args=%v，想要 cron.export_too_many_rows 与 [total %d]", re.Key, re.Args, logExportMax)
			}
			// 报的行数必须是本租户的真实命中数（不是全表、也不是上限常量）。
			if re.Args[0] != tenantRows {
				t.Errorf("超限错误报的行数 = %v，想要本租户命中的 %d 行", re.Args[0], tenantRows)
			}
			if len(rows2) != 0 {
				t.Errorf("超限拒绝时还返回了 %d 行数据（说明先取了数据）", len(rows2))
			}
			// 配对：翻倍之后再补两行独立的（没被复制），按 job_id 收窄到上限之内，必须照常导出。
			for i := range 2 {
				small := Log{JobID: idgen.ID(rcovIDBase + 3), JobName: fmt.Sprintf("rcov-small-%d", i),
					Handler: "log", TriggerType: TriggerManual, Status: StatusSuccess,
					StartedAt: same.Add(time.Duration(i) * time.Second), Output: "small"}
				small.ID = idgen.ID(rcovIDBase + 800 + int64(i))
				small.TenantID = idgen.ID(rcovTenants)
				if err := db.WithContext(ctx).Create(&small).Error; err != nil {
					t.Fatalf("补小批行 %d：%v", i, err)
				}
			}
			under, err := svc.ExportLogs(ctx, LogQuery{JobID: strconv.FormatInt(rcovIDBase+3, 10)})
			if err != nil {
				t.Fatalf("按 job_id 收窄后仍被拒（上限内的导出）：%v", err)
			}
			if len(under) != 2 {
				t.Errorf("收窄后的导出 = %d 行，想要 2 行（筛选条件在真引擎上没生效）", len(under))
			}
			// 非法 job_id 在任何查询之前就被拒。
			if _, err := svc.ExportLogs(ctx, LogQuery{JobID: "not-a-number"}); err == nil {
				t.Error("非法 job_id 被放行")
			}
		})
	}
}

func namesOf(logs []Log) []string {
	out := make([]string, 0, len(logs))
	for _, l := range logs {
		out = append(out, l.JobName)
	}
	return out
}

// TestITCronJobKeywordLive 钉住 service.go:334 的 LOWER(name) LIKE LOWER(?)
// 与 service.go:350 的 ORDER BY created_at DESC, id DESC：
// PG 的 LIKE 区分大小写，不折叠列就搜不到管理员在 MySQL 上搜得到的任务；
// 通配符不转义的话，关键词「%」会命中全部任务。
func TestITCronJobKeywordLive(t *testing.T) {
	for _, engine := range rcovLiveEngines {
		t.Run(engine, func(t *testing.T) {
			db := openRcov(t, engine)
			svc := NewService(db, nil)
			ctx := rcovCtx(rcovTenants)
			base := time.Now().UTC().Add(-time.Hour)
			names := []string{"RCov Sync Region", "rcov-sync-region", "rcov-50%-job", "rcov-50x-job", "另一个任务"}
			for i, name := range names {
				f := JobForm{Name: name, CronExpr: "0 3 * * *", Handler: "log", Enabled: true, RetryCount: 1}
				j := &Job{Name: f.Name, CronExpr: f.CronExpr, Handler: f.Handler, Enabled: f.Enabled, RetryCount: f.RetryCount}
				j.ID = idgen.ID(rcovIDBase + int64(600+i))
				j.TenantID = idgen.ID(rcovTenants)
				j.CreatedAt = base.Add(time.Duration(i) * time.Minute)
				j.UpdatedAt = j.CreatedAt
				if err := db.WithContext(ctx).Create(j).Error; err != nil {
					t.Fatalf("种任务 %q：%v", name, err)
				}
			}
			// 别的租户同名任务：不能出现在本租户的结果里（真引擎上的租户插件）。
			foreign := &Job{Name: "RCov Sync Region", CronExpr: "0 4 * * *", Handler: "log", Enabled: true}
			foreign.ID = idgen.ID(rcovIDBase + 699)
			foreign.TenantID = idgen.ID(rcovOther)
			foreign.CreatedAt, foreign.UpdatedAt = base, base
			if err := db.WithContext(rcovCtx(rcovOther)).Create(foreign).Error; err != nil {
				t.Fatalf("种跨租户任务：%v", err)
			}

			list := func(kw string) []string {
				t.Helper()
				rows, total, err := svc.ListJobs(ctx, JobQuery{Keyword: kw, Page: 1, PageSize: 20})
				if err != nil {
					t.Fatalf("ListJobs(%q)：%v", kw, err)
				}
				if int64(len(rows)) != total {
					t.Errorf("ListJobs(%q) total=%d 与行数 %d 不一致", kw, total, len(rows))
				}
				out := make([]string, 0, len(rows))
				for _, r := range rows {
					if r.TenantID != idgen.ID(rcovTenants) {
						t.Errorf("ListJobs(%q) 返回了别的租户的任务 %+v", kw, r.Job)
					}
					out = append(out, r.Name)
				}
				return out
			}
			// 折叠大小写：小写关键词命中大写任务名，大写关键词命中小写任务名。
			if got := list("sync region"); !containsName(got, "RCov Sync Region") {
				t.Errorf("keyword \"sync region\" = %v，想要命中大写任务名（列没折叠）", got)
			}
			if got := list("RCOV-SYNC-REGION"); !containsName(got, "rcov-sync-region") {
				t.Errorf("keyword \"RCOV-SYNC-REGION\" = %v，想要命中小写任务名（模式没折叠）", got)
			}
			// 通配符必须按字面量：50% 只该命中带字面 50% 的那一条。
			got := list("50%")
			if !containsName(got, "rcov-50%-job") {
				t.Errorf("keyword \"50%%\" 漏了字面命中的任务：%v", got)
			}
			for _, n := range got {
				if n == "rcov-50x-job" || n == "另一个任务" {
					t.Errorf("keyword \"50%%\" 命中了 %q：通配符没转义，一个百分号就能把全表捞出来", n)
				}
			}
			// 配对：不存在的关键词必须空。
			if got := list("definitely-no-such-job"); len(got) != 0 {
				t.Errorf("不存在的关键词命中 %v", got)
			}
			// 排序：created_at DESC, id DESC（新建的排在前面）。
			all := list("")
			if len(all) != 5 || all[0] != "另一个任务" || all[len(all)-1] != "RCov Sync Region" {
				t.Errorf("ListJobs 默认顺序 = %v，想要按 created_at 倒序（最新在前）", all)
			}
		})
	}
}

func containsName(list []string, want string) bool {
	for _, n := range list {
		if n == want {
			return true
		}
	}
	return false
}
