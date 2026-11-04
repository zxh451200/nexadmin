package report

// 真引擎集成测试：报表的动态 SQL（反引号/双引号、日期分桶、白名单列、LOWER LIKE、数据范围子查询）
// 在 sqlmock 里只做正则匹配、从不执行，本文件把每条语句放到真 MySQL / PostgreSQL 上过一遍。
//
// 守卫：默认跳过，设置对应 DSN 才执行（MySQL 侧库名必须留空，见 AGENTS.md）：
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass sslmode=disable' \
//	  GOMAXPROCS=2 go test ./plugins/report -p 1 -run TestITReport -v
//
// 隔离：每个用例自建一次性库 nexadmin_rcov_rpt_<纳秒>，只建白名单需要的表，收尾 DROP 自己的库。
// 夹具必须走 database.Open（生产构造器）：租户 scope 与 UTC 时区处理都在它内部装配，
// 裸 gorm.Open 的句柄少了那道闸，租户与换算类断言会退化成空断言（三格全绿也是零覆盖）。
// 本包已有的 sqlmock 用例（query_test.go 的 newDB）用的就是裸句柄 + mock 连接，钉的是语句文本，
// 不执行，因此不能替代本文件。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/datascope"
	"nexadmin/core/migrate"
	"nexadmin/core/response"
)

const (
	rcovTenantA int64 = 7
	rcovTenantB int64 = 8
)

// 两个租户的固定时间戳：分桶断言要的是「真引擎把这一列换算成哪一天/哪一月/哪一年」。
var (
	rcovStampA = time.Date(2026, 11, 3, 10, 30, 0, 0, time.UTC)
	rcovStampB = time.Date(2026, 10, 2, 23, 5, 0, 0, time.UTC)
)

// 固定号段：夹具库是一次性库，显式 id 让断言可复现，也不依赖进程里的 idgen 节点号。
const rcovIDBase int64 = 900000

func rcovEnv(tid int64) Env {
	return Env{TenantID: tid, Scope: &datascope.Scope{All: true}, Timeout: 5 * time.Second}
}

// rcovFiles 是 7 张白名单表的建表迁移（框架 4 张 + cron 2 张 + pay 1 张）。
// 只跑这些：全套框架迁移与用例要证的语句无关，多一张表就多一处无关失败面。
func rcovFiles(engine string) []string {
	fw := "../../migrations/framework/" + engine
	pl := "migrations/" + engine
	return []string{
		fw + "/log_001_init.sql",          // sys_oper_log / sys_login_log
		fw + "/sendlog_001_init.sql",      // sys_send_log
		fw + "/clienterror_001_init.sql",  // sys_client_error
		fw + "/system_user.sql",           // sys_user：数据范围子查询的归属表
		"../cron/" + pl + "/001_init.sql", // plg_cron_job / plg_cron_log
		"../cron/" + pl + "/002_job_timeout.sql",
		"../pay/" + pl + "/001_init.sql", // plg_pay_order
		"../pay/" + pl + "/002_channel_capabilities.sql",
	}
}

// runRcovFiles 逐条执行迁移语句（不依赖 multiStatements，两种引擎同一套代码）。
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

// openRcov 在真引擎上起一个一次性库，返回生产构造器的句柄。
func openRcov(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	switch engine {
	case "mysql":
		return openRcovMySQL(t)
	case "postgres":
		return openRcovPG(t)
	}
	t.Fatalf("unknown engine %q", engine)
	return nil
}

func openRcovMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	raw := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL report test")
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
	dbName := fmt.Sprintf("nexadmin_rcov_rpt_%d", time.Now().UnixNano())
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
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL report test")
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
	dbName := fmt.Sprintf("nexadmin_rcov_rpt_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE "` + dbName + `" ENCODING 'UTF8'`); err != nil {
		_ = admin.Close()
		t.Fatalf("create throwaway db: %v", err)
	}
	cfg.Name = dbName
	return finishRcov(t, admin, dbName, cfg)
}

// finishRcov 建句柄、跑建表迁移，并登记「关连接池 → 删库 → 关管理连接」的收尾顺序
// （反过来 DROP 会撞 database is closed，把一次性库留在实例上）。
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
	// 生产构造器：database.Open 里装租户 scope 插件、UTC NowFunc 与连接池。
	gdb, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("database.Open(%s): %v", cfg.Type, err)
	}
	runRcovFiles(t, gdb, rcovFiles(cfg.Type))
	seedRcov(t, gdb)
	return gdb
}

func rcovIns(t *testing.T, db *gorm.DB, table string, row map[string]any) {
	t.Helper()
	if err := db.Table(table).Create(row).Error; err != nil {
		t.Fatalf("插入 %s：%v", table, err)
	}
}

// seedRcov 给 7 张白名单表各插入租户 7（rcovStampA）与租户 8（rcovStampB）的真实行。
// 值要能同时喂饱「按声明类型的探测」和「LIKE 折叠大小写/转义通配符」两用例。
func seedRcov(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tid := range []int64{rcovTenantA, rcovTenantB} {
		stamp := rcovStampA
		if tid == rcovTenantB {
			stamp = rcovStampB
		}
		n := int64(tid)
		rcovIns(t, db, "plg_cron_job", map[string]any{
			"id": rcovIDBase + n, "tenant_id": n, "name": fmt.Sprintf("rcov-清理-%d", n),
			"cron_expr": "0 3 * * *", "handler": "clean_logs", "params": "https://intranet.example/x",
			"enabled": tid == rcovTenantA, "retry_count": 2, "timeout_seconds": 30,
			"last_run_at": stamp, "last_status": "success", "last_slot": 0,
			"created_at": stamp, "updated_at": stamp,
		})
		// job_name 里的三条是给 LIKE 用例的：混合大小写、字面 % 与字面 _。
		for i, name := range []string{"rcov-sync-region", "RCov Sync REGION", "rcov-50%-done", "rcov-50x-done", "rcov-a_b-x", "rcov-axbx-x"} {
			rcovIns(t, db, "plg_cron_log", map[string]any{
				"id": rcovIDBase + 1000*n + int64(i), "tenant_id": n, "job_id": rcovIDBase + n,
				"job_name": name, "handler": "clean_logs", "trigger_type": "schedule",
				"status": "success", "attempts": 1, "started_at": stamp,
				// 同一天内结束：分桶断言按 stamp 那一天，别让 finished_at 落到另一天去。
				"finished_at": stamp.Add(3 * time.Minute),
				"cost_ms":     420 + i, "output": "deleted 1 rows", "error": "",
				"created_at": stamp, "updated_at": stamp,
			})
		}
		// fail_reason 是数据范围用例的可见标记：username 本身不可选（只作归属列），
		// 所以「谁看得到谁的行」要靠一个可选列的取值来读。
		for i, u := range []struct{ name, mark string }{
			{"rcov_alice", "mark-alice"},
			{"rcov_bob", "mark-bob"},
		} {
			rcovIns(t, db, "sys_login_log", map[string]any{
				"id": rcovIDBase + 2000*n + int64(i), "tenant_id": n, "username": u.name,
				"login_type": "password", "ip": "10.0.0.1", "location": "上海",
				"browser": "Chrome", "os": "Windows", "status": "success", "fail_reason": u.mark,
				"created_at": stamp, "updated_at": stamp,
			})
		}
		rcovIns(t, db, "sys_oper_log", map[string]any{
			"id": rcovIDBase + 3000 + n, "tenant_id": n, "module": "报表", "type": "query",
			"operator": "rcov_alice", "ip": "10.0.0.1", "location": "上海", "status": "success",
			"cost_ms": 15, "method": "GET", "url": "/api/plugins/report/query",
			"params": "{}", "response": "{}", "error_stack": "",
			"created_at": stamp, "updated_at": stamp,
		})
		rcovIns(t, db, "sys_send_log", map[string]any{
			"id": rcovIDBase + 4000 + n, "tenant_id": n, "channel": "email", "scene": "report_sub",
			"recipient": "a@example.com", "subject": "日报", "status": "success", "error": "",
			"duration_ms": 9, "created_at": stamp, "updated_at": stamp,
		})
		rcovIns(t, db, "sys_client_error", map[string]any{
			"id": rcovIDBase + 5000 + n, "tenant_id": n, "user_id": 4000 + n, "username": "rcov_alice",
			"route": "/admin/report", "message": "boom", "stack": "at x", "ua": "UA",
			"version": "1.2.3", "fingerprint": "fp-rcov", "hit_count": 3,
			"last_seen_at": stamp, "created_at": stamp, "updated_at": stamp,
		})
		rcovIns(t, db, "plg_pay_order", map[string]any{
			"id": rcovIDBase + 6000 + n, "tenant_id": n, "payment_no": fmt.Sprintf("RCOV-%d", n),
			"biz_type": "sub", "biz_no": fmt.Sprintf("B%d", n), "subject": "订阅", "subject_id": "S1",
			"amount_minor": 1234, "refunded_minor": 100, "fee_minor": 20, "currency": "CNY",
			"channel_code": "wx", "method": "native", "scene": "jsapi", "status": "paid",
			"paid_at": stamp, "created_at": stamp, "updated_at": stamp,
		})
	}
	// sys_user：数据范围（UsernameCond）的子查询目标。3003 已软删，历史记录不该归属于它。
	for _, u := range []struct {
		id   int64
		name string
		dept int64
		del  any
	}{
		{4007, "rcov_alice", 11, nil},
		{4008, "rcov_bob", 22, nil},
		{4009, "rcov_ghost", 11, rcovStampA},
	} {
		rcovIns(t, db, "sys_user", map[string]any{
			"id": u.id, "tenant_id": rcovTenantA, "username": u.name, "password": "x",
			"nickname": u.name, "avatar": "", "phone": "", "email": "", "gender": "1",
			"dept_id": u.dept, "status": "1", "province": "", "city": "", "district": "",
			"remark": "", "is_super": false, "created_at": rcovStampA, "updated_at": rcovStampA,
			"deleted_at": u.del,
		})
	}
}

func rcovColumnNames(s *Source) []string {
	out := make([]string, 0, len(s.Cols))
	for _, c := range s.Cols {
		out = append(out, c.Name)
	}
	return out
}

func rcovFirst(res *Result, key string) (any, bool) {
	if len(res.Rows) == 0 {
		return nil, false
	}
	return res.Rows[0][key], true
}

func rcovErrKey(t *testing.T, err error) (string, int) {
	t.Helper()
	var re *response.Error
	if !errors.As(err, &re) {
		t.Fatalf("err = %v，想要 *response.Error", err)
	}
	return re.Key, re.Status
}

// TestITReportWhitelistColumnsExecuteLive 把「7 张白名单表的每一列、按声明类型的那种用法」
// 在真引擎上各执行一次：
//   - 每一列都做一次投影（钉住 quoteFor 的引号形状与列名真的存在）；
//   - time 列跑三种分桶（钉住 query.go:193-197 的 DATE_FORMAT / to_char 配对）与 min/max；
//   - number 列跑 SUM/AVG（钉住列真是数值列）；bool 列跑 = true（钉住 PG 侧是 BOOLEAN）；
//   - string 列跑 LOWER LIKE（钉住 query.go:423）；
//   - 同一条语句换租户必须取到另一个租户的数据（钉住租户谓词在真引擎上生效）。
//
// sqlmock 对这些一条都照不出来：它只匹配文本，列名写错、类型对不上、方言不认，全都照样绿。
func TestITReportWhitelistColumnsExecuteLive(t *testing.T) {
	for _, engine := range []string{"mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := openRcov(t, engine)
			e := newEngine(db, nil)
			reg := newRegistry(defaultRaw(), nil)
			srcs := reg.List()
			// 审计口径：白名单是 7 张表。少一张就是有人在没补真引擎用例的情况下删了源。
			if len(srcs) != 7 {
				t.Fatalf("白名单表数 = %d，想要 7：%+v", len(srcs), srcs)
			}
			for _, src := range srcs {
				t.Run(src.Table, func(t *testing.T) {
					tg, err := reg.Target(src.Table, rcovColumnNames(src))
					if err != nil {
						t.Fatalf("白名单自己的列被 Target 拒绝（不该发生）：%v", err)
					}
					for _, c := range src.Cols {
						t.Run(c.Name+"/"+c.Type, func(t *testing.T) {
							res := mustRun(t, e, tg, Spec{Dimensions: []Dim{{Column: c.Name}}, Limit: 50}, rcovEnv(rcovTenantA))
							if len(res.Rows) == 0 {
								t.Fatalf("投影 %s.%s 返回 0 行，夹具没落库或谓词把它筛空了", src.Table, c.Name)
							}
							switch c.Type {
							case TypeTime:
								for _, tc := range []struct{ bucket, want string }{
									{"day", "2026-11-03"}, {"month", "2026-11"}, {"year", "2026"},
								} {
									r := mustRun(t, e, tg, Spec{
										Dimensions: []Dim{{Column: c.Name, Bucket: tc.bucket}},
										Measures:   []Measure{{Agg: "count"}},
									}, rcovEnv(rcovTenantA))
									v, ok := rcovFirst(r, "d0")
									if !ok {
										t.Fatalf("%s 分桶没有返回行", tc.bucket)
									}
									if got := fmt.Sprint(v); got != tc.want {
										t.Errorf("%s(%s).%s 分桶 = %q，想要 %q（方言格式串或列的真实类型不对）",
											tc.bucket, src.Table, c.Name, got, tc.want)
									}
								}
								mustRun(t, e, tg, Spec{Measures: []Measure{{Agg: "min", Column: c.Name}, {Agg: "max", Column: c.Name}}}, rcovEnv(rcovTenantA))
							case TypeNumber:
								r := mustRun(t, e, tg, Spec{Measures: []Measure{{Agg: "sum", Column: c.Name}, {Agg: "avg", Column: c.Name}}}, rcovEnv(rcovTenantA))
								if v, _ := rcovFirst(r, "m0"); v == nil {
									t.Errorf("SUM(%s.%s) 返回 NULL：列不是可求和的数值列", src.Table, c.Name)
								}
							case TypeBool:
								r := mustRun(t, e, tg, Spec{
									Dimensions: []Dim{{Column: c.Name}},
									Filters:    []Filter{{Column: c.Name, Op: "=", Value: true}},
								}, rcovEnv(rcovTenantA))
								if len(r.Rows) == 0 {
									t.Errorf("%s.%s = true 没有命中（真引擎上这一列不是布尔列或值不对）", src.Table, c.Name)
								}
							case TypeString:
								mustRun(t, e, tg, Spec{
									Dimensions: []Dim{{Column: c.Name}},
									Filters:    []Filter{{Column: c.Name, Op: "like", Value: "rcov"}},
								}, rcovEnv(rcovTenantA))
							}
							// 配对：同一条语句换租户，必须真的取了另一批行（time 列再钉住另一天的分桶值）。
							other := mustRun(t, e, tg, Spec{Dimensions: []Dim{{Column: c.Name}}, Limit: 50}, rcovEnv(rcovTenantB))
							if len(other.Rows) == 0 {
								t.Errorf("租户 %d 投影 %s.%s 返回 0 行：租户谓词把别人的数据吃掉的同时也吃掉了夹具", rcovTenantB, src.Table, c.Name)
							}
							if c.Type == TypeTime {
								r := mustRun(t, e, tg, Spec{
									Dimensions: []Dim{{Column: c.Name, Bucket: "day"}},
									Measures:   []Measure{{Agg: "count"}},
								}, rcovEnv(rcovTenantB))
								v, _ := rcovFirst(r, "d0")
								if got := fmt.Sprint(v); got != "2026-10-02" {
									t.Errorf("租户 %d 查 %s.%s 的日分桶 = %q，想要 2026-10-02（谓词串了租户）",
										rcovTenantB, src.Table, c.Name, got)
								}
							}
						})
					}
				})
			}
		})
	}
}

func mustRun(t *testing.T, e *Engine, tg *Target, sp Spec, env Env) *Result {
	t.Helper()
	res, err := e.Run(context.Background(), tg, sp, env)
	if err != nil {
		t.Fatalf("真引擎执行失败（列不存在 / 方言不认 / 类型对不上）：%v", err)
	}
	return res
}

// rcovUnlistedColumns 是「表里真实存在、但白名单没放出来」的列（按 DDL 逐列核对过）。
// 拿真实存在的列做负例才有意义：引擎本来接受它，被拒只能是我们的白名单在起作用。
var rcovUnlistedColumns = map[string][]string{
	"plg_cron_job":     {"params", "cron_expr", "last_slot", "timeout_seconds", "updated_at"},
	"plg_cron_log":     {"output", "error", "job_id", "updated_at"},
	"sys_login_log":    {"username", "ip", "location", "updated_at"},
	"sys_oper_log":     {"operator", "ip", "params", "response", "error_stack"},
	"sys_send_log":     {"recipient", "subject", "error"},
	"sys_client_error": {"message", "stack", "ua", "username", "user_id", "fingerprint"},
	"plg_pay_order":    {"payment_no", "payer_id", "metadata_json", "biz_no", "subject"},
}

// TestITReportNonWhitelistedRejectedBeforeSQL 钉住两件事，并把「有没有走到数据库」分开证：
//  1. 非白名单的表/列在 Registry.Target 与 Engine.build 就被拒（400，build 返回 nil 语句），
//     而同一个列用原生句柄查得到 —— 说明拦下它的是白名单，不是引擎；
//  2. 白名单里的表/列若在真库里不存在，错误必须来自引擎（500 report.query_failed）——
//     说明真引擎确实在执行并会拒绝坏语句，所以第 1 条的 400 不是「引擎反正也会拒」。
//
// 这就是 devicehub 那类 bug 的探测器：对没有 device_id 的表发 device_id 谓词，
// sqlmock 全绿，真库当场红。
func TestITReportNonWhitelistedRejectedBeforeSQL(t *testing.T) {
	for _, engine := range []string{"mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := openRcov(t, engine)
			e := newEngine(db, nil)
			reg := newRegistry(defaultRaw(), nil)

			t.Run("白名单外但库里存在的列：本地拒绝，不产出语句", func(t *testing.T) {
				for _, src := range reg.List() {
					for _, col := range rcovUnlistedColumns[src.Table] {
						cols := append(rcovColumnNames(src), col)
						if _, err := reg.Target(src.Table, cols); err == nil {
							t.Errorf("%s.%s 进了白名单（defaultRaw 之外没有放行通道）", src.Table, col)
							continue
						} else if key, status := rcovErrKey(t, err); key != "report.source_or_column_unavailable" || status != 400 {
							t.Errorf("%s.%s 的错误 = %s/%d，想要 report.source_or_column_unavailable/400", src.Table, col, key, status)
						}
						// 连「请求里只把它当过滤列」的入口也要拒，且 build 不返回语句。
						tg, err := reg.Target(src.Table, rcovColumnNames(src))
						if err != nil {
							t.Fatalf("%s: %v", src.Table, err)
						}
						tx, _, err := e.build(context.Background(), tg, Spec{
							Dimensions: []Dim{{Column: src.Cols[0].Name}},
							Filters:    []Filter{{Column: col, Op: "=", Value: "x"}},
						}, rcovEnv(rcovTenantA))
						if err == nil {
							t.Errorf("%s.%s 作为过滤列被放行", src.Table, col)
						}
						if tx != nil {
							t.Errorf("%s.%s 被拒后仍然产出了语句（说明拦截发生在构造之后）", src.Table, col)
						}
						// 反证：同一个列真在库里，引擎接受它 —— 拦下它的是白名单而不是数据库。
						var n int64
						if err := db.Table(src.Table).Where("tenant_id = ?", rcovTenantA).Count(&n).Error; err != nil {
							t.Fatalf("%s 计数失败：%v", src.Table, err)
						}
						if n == 0 {
							t.Errorf("%s 没有夹具行，上面的拒绝可能只是表空", src.Table)
						}
						if err := db.Table(src.Table).Select(col).Limit(1).Find(&[]map[string]any{}).Error; err != nil {
							t.Errorf("%s.%s 用原生句柄也查不动（列其实不存在？）：%v", src.Table, col, err)
						}
					}
				}
			})

			t.Run("非白名单的表：本地拒绝", func(t *testing.T) {
				for _, tbl := range []string{"sys_user", "sys_config", "plg_report_dataset", "information_schema.columns", "mysql.user", "sys_oper_log_shadow"} {
					if _, err := reg.Target(tbl, []string{"status"}); err == nil {
						t.Errorf("表 %s 被放行", tbl)
					} else if key, status := rcovErrKey(t, err); key != "report.source_or_column_unavailable" || status != 400 {
						t.Errorf("表 %s 的错误 = %s/%d", tbl, key, status)
					}
				}
			})

			t.Run("白名单放过但库里没有：引擎拒绝", func(t *testing.T) {
				// 这两条一定会走到数据库，返回的必须是 500（引擎的错误），不是 400。
				ghostTable := newRegistry(append(defaultRaw(), rawSource{
					Table: "rcov_ghost_table", TenantColumn: "tenant_id",
					Columns: []rawColumn{{Name: "status", Type: TypeString}},
				}), nil)
				tg, err := ghostTable.Target("rcov_ghost_table", []string{"status"})
				if err != nil {
					t.Fatalf("ghost target: %v", err)
				}
				_, err = e.Run(context.Background(), tg, Spec{Dimensions: []Dim{{Column: "status"}}}, rcovEnv(rcovTenantA))
				key, status := rcovErrKey(t, err)
				if key != "report.query_failed" || status != 500 {
					t.Errorf("缺表的可疑查询 = %s/%d，想要 report.query_failed/500（引擎没执行它）", key, status)
				}

				ghostCol := newRegistry(append(defaultRaw(), rawSource{
					Table: "plg_cron_log", TenantColumn: "tenant_id",
					Columns: []rawColumn{{Name: "device_id", Type: TypeString}},
				}), nil)
				tg2, err := ghostCol.Target("plg_cron_log", []string{"device_id"})
				if err != nil {
					t.Fatalf("ghost column target: %v", err)
				}
				_, err = e.Run(context.Background(), tg2, Spec{Dimensions: []Dim{{Column: "device_id"}}}, rcovEnv(rcovTenantA))
				key, status = rcovErrKey(t, err)
				if key != "report.query_failed" || status != 500 {
					t.Errorf("缺列的可疑查询（device_id 那种）= %s/%d，想要 report.query_failed/500", key, status)
				}
				// 对外文案不得带库细节（错误收敛也在真引擎上生效）。
				var re *response.Error
				if errors.As(err, &re) && (strings.Contains(re.Msg, "device_id") || strings.Contains(re.Msg, "SELECT")) {
					t.Errorf("500 文案泄漏 SQL/列名：%q", re.Msg)
				}
			})
		})
	}
}

// TestITReportBucketFormatIsEnginePair 钉住 query.go:193-197 的两两配对：
// 同一个 bucket 在 MySQL 走 DATE_FORMAT('%Y-...')，在 PG 走 to_char('YYYY-...')，
// 两边都必须产出同一个业务日期。
//
// 反证（同一用例内，改完立刻还原）：把 PG 槽位的格式串换成 MySQL 的那一份，
// 真 PG 就不再产出正确日期 —— 证明这条断言吃的是「引擎与格式串的配对」，不是「有没有返回一个字符串」。
func TestITReportBucketFormatIsEnginePair(t *testing.T) {
	for _, engine := range []string{"mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := openRcov(t, engine)
			e := newEngine(db, nil)
			tg := testTarget(t, "plg_cron_log", "started_at", "status")
			for _, tc := range []struct{ bucket, want string }{{"day", "2026-11-03"}, {"month", "2026-11"}, {"year", "2026"}} {
				res := mustRun(t, e, tg, Spec{
					Dimensions: []Dim{{Column: "started_at", Bucket: tc.bucket}},
					Measures:   []Measure{{Agg: "count"}},
				}, rcovEnv(rcovTenantA))
				got, _ := rcovFirst(res, "d0")
				if fmt.Sprint(got) != tc.want {
					t.Errorf("%s 的 %s 分桶 = %q，想要 %q", engine, tc.bucket, got, tc.want)
				}
			}

			t.Run("换错方言的格式串必须当场红", func(t *testing.T) {
				orig := bucketFmt["day"]
				t.Cleanup(func() { bucketFmt["day"] = orig })
				if engine == "postgres" {
					bucketFmt["day"] = [2]string{orig[0], orig[0]} // PG 槽位塞进 MySQL 的 %Y-%m-%d
				} else {
					bucketFmt["day"] = [2]string{orig[1], orig[1]} // 反过来：MySQL 的 DATE_FORMAT 拿到 YYYY-MM-DD
				}
				res, err := e.Run(context.Background(), tg, Spec{
					Dimensions: []Dim{{Column: "started_at", Bucket: "day"}},
					Measures:   []Measure{{Agg: "count"}},
				}, rcovEnv(rcovTenantA))
				if err == nil {
					if got, _ := rcovFirst(res, "d0"); fmt.Sprint(got) == "2026-11-03" {
						t.Errorf("%s 用错方言的格式串仍然产出 2026-11-03：这条断言没有吃格式串（用例是空的）", engine)
					}
					return
				}
				t.Logf("%s 用错格式串后引擎报错（也算被这套断言抓到）：%v", engine, err)
			})
		})
	}
}

// TestITReportLikeFilterLive 钉住 query.go:423 的 LOWER(col) LIKE LOWER(?)。
// PG 的 LIKE 区分大小写：只折叠一侧或都不折叠，管理员在 PG 上就搜不到 MySQL 上搜得到的东西。
// 通配符转义也在真引擎上验：`\%` / `\_` 必须是字面量，否则「50%」能捞出全表。
func TestITReportLikeFilterLive(t *testing.T) {
	for _, engine := range []string{"mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := openRcov(t, engine)
			e := newEngine(db, nil)
			tg := testTarget(t, "plg_cron_log", "job_name", "status")
			like := func(pattern string) map[string]bool {
				res := mustRun(t, e, tg, Spec{
					Dimensions: []Dim{{Column: "job_name"}},
					Filters:    []Filter{{Column: "job_name", Op: "like", Value: pattern}},
					Limit:      50,
				}, rcovEnv(rcovTenantA))
				out := map[string]bool{}
				for _, row := range res.Rows {
					out[fmt.Sprint(row["d0"])] = true
				}
				return out
			}
			// 大小写折叠：小写模式要命中大写的行（LOWER(col) 那一侧）。
			got := like("sync region")
			if !got["RCov Sync REGION"] {
				t.Errorf(`like "sync region" 命中 %v，想要命中大写行 "RCov Sync REGION"（PG 上不折叠列就搜不到）`, keysOf(got))
			}
			// 反方向：大写模式要命中小写的行（LOWER(?) 那一侧）。
			got = like("RCOV-SYNC-REGION")
			if !got["rcov-sync-region"] {
				t.Errorf(`like "RCOV-SYNC-REGION" 命中 %v，想要命中小写行 "rcov-sync-region"（模式没折叠）`, keysOf(got))
			}
			// 反向配对：模式对不上就是空，不能因为折叠而全命中。
			if n := like("definitely-no-such-name"); len(n) != 0 {
				t.Errorf("不存在的关键词命中了 %v", keysOf(n))
			}
			// 通配符必须按字面量处理。
			hit := like("50%-done")
			if !hit["rcov-50%-done"] || hit["rcov-50x-done"] {
				t.Errorf(`like "50%%-done" 命中 %v，想要只有字面 50%% 那行（转义符 %% 没生效）`, keysOf(hit))
			}
			hit = like("a_b-x")
			if !hit["rcov-a_b-x"] || hit["rcov-axbx-x"] {
				t.Errorf(`like "a_b-x" 命中 %v，想要只有字面下划线那行（转义符 %%_ 没生效）`, keysOf(hit))
			}
			// 反斜杠自身也是输入的一部分：种子行里没有以 \ 开头的名字，必须搜不到而不是报错。
			if n := like(`a\b`); len(n) != 0 {
				t.Errorf(`like "a\\b" 命中 %v`, keysOf(n))
			}
		})
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestITReportDatascopeLive 把 query.go:296-308 的数据范围条件放到真引擎上：
// sys_login_log 没有用户 id 列，只能靠 username 子查询 sys_user（username 本身不可选，
// 所以输出列用可选的 fail_reason 当「这一行属于谁」的标记）。
// 这条子查询的列名、括号与参数顺序在 sqlmock 里永远不会被真库校验。
func TestITReportDatascopeLive(t *testing.T) {
	for _, engine := range []string{"mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := openRcov(t, engine)
			e := newEngine(db, nil)
			tg := testTarget(t, "sys_login_log", "fail_reason", "status", "created_at")
			marks := func(scope *datascope.Scope, tid int64) map[string]bool {
				env := rcovEnv(tid)
				env.Scope = scope
				res, err := e.Run(context.Background(), tg, Spec{Dimensions: []Dim{{Column: "fail_reason"}}, Limit: 50}, env)
				if err != nil {
					t.Fatalf("数据范围查询失败：%v", err)
				}
				out := map[string]bool{}
				for _, row := range res.Rows {
					out[fmt.Sprint(row["d0"])] = true
				}
				return out
			}
			all := marks(&datascope.Scope{All: true}, rcovTenantA)
			if !all["mark-alice"] || !all["mark-bob"] {
				t.Fatalf("全量范围应当看到本租户两个用户的行，实际 %v", keysOf(all))
			}
			// 受限：只看到自己（dept 11）的账号；bob（dept 22）不可见。
			one := marks(&datascope.Scope{UserID: 4007, Depts: []int64{11}}, rcovTenantA)
			if !one["mark-alice"] || one["mark-bob"] {
				t.Errorf("受限范围 = %v，想要只含 mark-alice（UsernameCond 的子查询在真引擎上没生效）", keysOf(one))
			}
			// 配对：换一个 dept 受限，看到的应是另一个人而不是同一个人。
			two := marks(&datascope.Scope{UserID: 4008, Depts: []int64{22}}, rcovTenantA)
			if !two["mark-bob"] || two["mark-alice"] {
				t.Errorf("dept 22 的范围 = %v，想要只含 mark-bob", keysOf(two))
			}
			// 另一条分支（无 Depts 时用 id = ?）：已软删的用户没有可见账号，
			// 子查询里的 deleted_at IS NULL 少写一次，这个人的历史行就会漏给受限操作者。
			if err := db.Table("sys_login_log").Create(map[string]any{
				"id": rcovIDBase + 7009, "tenant_id": rcovTenantA, "username": "rcov_ghost",
				"login_type": "password", "status": "success", "fail_reason": "mark-ghost",
				"created_at": rcovStampA, "updated_at": rcovStampA,
			}).Error; err != nil {
				t.Fatalf("补历史登录行：%v", err)
			}
			if got := marks(&datascope.Scope{UserID: 4009}, rcovTenantA); len(got) != 0 {
				t.Errorf("已软删用户的范围 = %v，想要空（deleted_at IS NULL 没进子查询）", keysOf(got))
			}
			// 正控制：同一个分支对未删除的用户必须命中，且只命中他自己那一条。
			if got := marks(&datascope.Scope{UserID: 4007}, rcovTenantA); !got["mark-alice"] || len(got) != 1 {
				t.Errorf("无 Depts 的受限范围 = %v，想要只含 mark-alice", keysOf(got))
			}
			// ScopeRequired：受限操作者面对无归属列的表必须拒（错误在任何语句之前）。
			sr := newRegistry([]rawSource{{
				Table: "sys_send_log", TenantColumn: "tenant_id", ScopeRequired: true,
				Columns: []rawColumn{{Name: "channel", Type: TypeString}},
			}}, nil)
			stg, err := sr.Target("sys_send_log", []string{"channel"})
			if err != nil {
				t.Fatal(err)
			}
			limited := rcovEnv(rcovTenantA)
			limited.Scope = &datascope.Scope{UserID: 4007}
			if _, err := e.Run(context.Background(), stg, Spec{Dimensions: []Dim{{Column: "channel"}}}, limited); err == nil {
				t.Error("ScopeRequired 的表对受限操作者放行")
			} else if key, status := rcovErrKey(t, err); key != "report.datascope_denied" || status != 403 {
				t.Errorf("ScopeRequired 拒绝的错误 = %s/%d，想要 report.datascope_denied/403", key, status)
			}
		})
	}
}
