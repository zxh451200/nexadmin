package report

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
)

// newDB 用 sqlmock 构造 gorm 连接，精确匹配 SQL 文本（QueryMatcherEqual），这样能逐字校验生成的语句与参数。
func newDB(t *testing.T, dialect string) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var d gorm.Dialector
	if dialect == "postgres" {
		d = postgres.New(postgres.Config{Conn: sqlDB})
	} else {
		d = mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true})
	}
	db, err := gorm.Open(d, &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func nil2DB(t *testing.T) *gorm.DB { db, _ := newDB(t, "mysql"); return db }

func testEnv() Env {
	return Env{TenantID: 7, Scope: &datascope.Scope{All: true}, Timeout: 2 * time.Second}
}

func testTarget(t *testing.T, table string, cols ...string) *Target {
	t.Helper()
	tg, err := newRegistry(defaultRaw(), nil).Target(table, cols)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func logTarget(t *testing.T) *Target {
	return testTarget(t, "plg_cron_log", "job_name", "status", "attempts", "cost_ms", "started_at", "handler")
}

// dry 返回 build 出来的语句的 SQL 文本与参数（不执行）。
func dry(t *testing.T, e *Engine, tg *Target, sp Spec, env Env) (string, []any) {
	t.Helper()
	tx, _, err := e.build(context.Background(), tg, sp, env)
	if err != nil {
		t.Fatal(err)
	}
	st := tx.Session(&gorm.Session{DryRun: true}).Find(&[]map[string]any{}).Statement
	return st.SQL.String(), st.Vars
}

func TestBuildGroupedSQLMySQL(t *testing.T) {
	db, mock := newDB(t, "mysql")
	e := newEngine(db, nil)
	mock.ExpectQuery("SELECT `status` AS `d0`,COUNT(*) AS `m0`,SUM(`cost_ms`) AS `m1` FROM `plg_cron_log` WHERE `tenant_id` = ? AND (`status` = ?) GROUP BY 1 ORDER BY 3 DESC LIMIT ?").
		WithArgs(int64(7), "failed", 101).
		WillReturnRows(sqlmock.NewRows([]string{"d0", "m0", "m1"}).AddRow("failed", int64(3), []byte("120.50")))
	res, err := e.Run(context.Background(), logTarget(t), Spec{
		Dimensions: []Dim{{Column: "status"}},
		Measures:   []Measure{{Agg: "count"}, {Agg: "sum", Column: "cost_ms"}},
		Filters:    []Filter{{Column: "status", Op: "=", Value: "failed"}},
		OrderBy:    []Order{{Ref: "m1", Desc: true}},
		Limit:      100,
	}, testEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Rows[0]["d0"] != "failed" || res.Rows[0]["m0"] != int64(3) || res.Rows[0]["m1"] != 120.5 {
		t.Fatalf("rows = %#v", res.Rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildSQLPostgresAndBuckets(t *testing.T) {
	db, mock := newDB(t, "postgres")
	mock.ExpectQuery(`SELECT to_char("started_at", 'YYYY-MM-DD') AS "d0",COUNT(*) AS "m0" FROM "plg_cron_log" WHERE "tenant_id" = $1 AND (("started_at" BETWEEN $2 AND $3)) GROUP BY 1 ORDER BY 1 LIMIT $4`).
		WithArgs(int64(7), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), 1001).
		WillReturnRows(sqlmock.NewRows([]string{"d0", "m0"}).AddRow("2026-01-02", int64(5)))
	_, err := newEngine(db, nil).Run(context.Background(), logTarget(t), Spec{
		Dimensions: []Dim{{Column: "started_at", Bucket: "day"}},
		Measures:   []Measure{{Agg: "count"}},
		Filters:    []Filter{{Column: "started_at", Op: "between", Value: []any{"2026-01-01", "2026-01-31"}}},
	}, testEnv())
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	db2, mock2 := newDB(t, "mysql")
	mock2.ExpectQuery("SELECT DATE_FORMAT(`started_at`, '%Y-%m') AS `d0`,COUNT(*) AS `m0` FROM `plg_cron_log` WHERE `tenant_id` = ? GROUP BY 1 ORDER BY 1 LIMIT ?").
		WithArgs(int64(7), 1001).WillReturnRows(sqlmock.NewRows([]string{"d0", "m0"}))
	if _, err := newEngine(db2, nil).Run(context.Background(), logTarget(t),
		Spec{Dimensions: []Dim{{Column: "started_at", Bucket: "month"}}, Measures: []Measure{{Agg: "count"}}}, testEnv()); err != nil {
		t.Fatal(err)
	}
}

func TestRawRowsAndDistinct(t *testing.T) {
	db, mock := newDB(t, "mysql")
	e := newEngine(db, nil)
	mock.ExpectQuery("SELECT DISTINCT `status` AS `d0` FROM `plg_cron_log` WHERE `tenant_id` = ? ORDER BY 1 LIMIT ?").
		WithArgs(int64(7), 201).WillReturnRows(sqlmock.NewRows([]string{"d0"}).AddRow("ok"))
	if _, err := e.Run(context.Background(), logTarget(t), Spec{Dimensions: []Dim{{Column: "status"}}, Distinct: true, Limit: 200}, testEnv()); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT `job_name` AS `d0`,`attempts` AS `d1` FROM `plg_cron_log` WHERE `tenant_id` = ? ORDER BY 1 LIMIT ?").
		WithArgs(int64(7), 1001).WillReturnRows(sqlmock.NewRows([]string{"d0", "d1"}).AddRow("j", int64(1)))
	if _, err := e.Run(context.Background(), logTarget(t), Spec{Dimensions: []Dim{{Column: "job_name"}, {Column: "attempts"}}}, testEnv()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLimitCapAndTruncation(t *testing.T) {
	e := newEngine(nil2DB(t), nil)
	for _, tc := range []struct{ in, maxRows, want int }{
		{0, 0, DefaultLimit}, {100, 0, 100}, {5000, 0, 5000}, {5001, 0, MaxLimit}, {1 << 30, 0, MaxLimit}, {900, 300, 300}, {0, 50, 50},
	} {
		env := testEnv()
		env.MaxRows = tc.maxRows
		_, pl, err := e.build(context.Background(), logTarget(t), Spec{Dimensions: []Dim{{Column: "status"}}, Limit: tc.in}, env)
		if err != nil || pl.limit != tc.want {
			t.Errorf("limit %d/max %d -> err %v, want %d", tc.in, tc.maxRows, err, tc.want)
		}
	}
	db, mock := newDB(t, "mysql")
	rows := sqlmock.NewRows([]string{"d0"})
	for i := 0; i < 6; i++ {
		rows.AddRow("s")
	}
	mock.ExpectQuery("SELECT `status` AS `d0` FROM `plg_cron_log` WHERE `tenant_id` = ? ORDER BY 1 LIMIT ?").WithArgs(int64(7), 6).WillReturnRows(rows)
	res, err := newEngine(db, nil).Run(context.Background(), logTarget(t), Spec{Dimensions: []Dim{{Column: "status"}}, Limit: 5}, testEnv())
	if err != nil || len(res.Rows) != 5 || !res.Truncated {
		t.Fatalf("rows=%d truncated=%v err=%v", len(res.Rows), res.Truncated, err)
	}
}

func TestDBErrorsAreNotEchoed(t *testing.T) {
	db, mock := newDB(t, "mysql")
	secret := `pq: relation "plg_cron_log_secret" does not exist; SELECT * FROM sys_user WHERE password = 'x'`
	mock.ExpectQuery("SELECT `status` AS `d0` FROM `plg_cron_log` WHERE `tenant_id` = ? ORDER BY 1 LIMIT ?").
		WithArgs(int64(7), 1001).WillReturnError(errors.New(secret))
	_, err := newEngine(db, nil).Run(context.Background(), logTarget(t), Spec{Dimensions: []Dim{{Column: "status"}}}, testEnv())
	var re *response.Error
	if !errors.As(err, &re) {
		t.Fatalf("err = %v", err)
	}
	if re.Status != 500 || re.Msg != "查询失败，请稍后重试" || strings.Contains(re.Msg, "plg_cron") || strings.Contains(re.Msg, "SELECT") {
		t.Fatalf("response error leaks details: %+v", re)
	}
}

func TestQueryTimeout(t *testing.T) {
	db, mock := newDB(t, "mysql")
	mock.ExpectQuery("SELECT `status` AS `d0` FROM `plg_cron_log` WHERE `tenant_id` = ? ORDER BY 1 LIMIT ?").
		WithArgs(int64(7), 1001).WillDelayFor(500 * time.Millisecond).WillReturnRows(sqlmock.NewRows([]string{"d0"}))
	env := testEnv()
	env.Timeout = 30 * time.Millisecond
	start := time.Now()
	_, err := newEngine(db, nil).Run(context.Background(), logTarget(t), Spec{Dimensions: []Dim{{Column: "status"}}}, env)
	var re *response.Error
	if !errors.As(err, &re) || re.Status != 504 {
		t.Fatalf("err = %v, want 504", err)
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Fatalf("timeout not enforced: %v", time.Since(start))
	}
}
