package report

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
)

// newRegexDB 的匹配器是正则，适合校验 gorm 为自有表生成的语句（列清单较长，不逐字比较）。
func newRegexDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func newTestService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	db, mock := newRegexDB(t)
	svc := NewService(db, nil, newRegistry(defaultRaw(), nil), Settings{MaxRows: 3, Timeout: 2 * time.Second})
	return svc, mock
}

func tctx(id int64) context.Context { return tenant.With(context.Background(), idOf(id)) }

func allViewer() Viewer { return Viewer{Scope: &datascope.Scope{All: true}} }

func TestOwnTablesAlwaysTenantScoped(t *testing.T) {
	svc, mock := newTestService(t)
	for _, tid := range []int64{7, 8} {
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_report_dataset` WHERE tenant_id = \\? AND name LIKE \\?").
			WithArgs(tid, "%a\\%b%").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
		mock.ExpectQuery("SELECT \\* FROM `plg_report_dataset` WHERE tenant_id = \\? AND name LIKE \\? ORDER BY id DESC LIMIT \\?").
			WithArgs(tid, "%a\\%b%", 20).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))
		if _, _, err := svc.ListDatasets(tctx(tid), "a%b", 1, 20); err != nil {
			t.Fatal(err)
		}
	}
	// 其他租户的记录：带租户条件的查询查不到 -> 404，而不是返回别人的数据。
	mock.ExpectQuery("SELECT \\* FROM `plg_report_dataset` WHERE tenant_id = \\? AND id = \\?").
		WithArgs(int64(7), int64(99), 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	_, err := svc.GetDataset(tctx(7), 99)
	var re *response.Error
	if !errors.As(err, &re) || re.Status != 404 {
		t.Fatalf("err = %v, want 404", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNoTenantNoAccess(t *testing.T) {
	svc, mock := newTestService(t) // 没有任何 Expect：只要发出一条 SQL 就会失败
	bg := context.Background()
	if _, _, err := svc.ListDatasets(bg, "", 1, 20); err == nil {
		t.Error("ListDatasets without tenant succeeded")
	}
	if _, err := svc.GetChart(bg, 1); err == nil {
		t.Error("GetChart without tenant succeeded")
	}
	if _, err := svc.QueryChart(bg, 1, allViewer()); err == nil {
		t.Error("QueryChart without tenant succeeded")
	}
	if _, err := svc.ListScreensView(bg, allViewer()); err == nil {
		t.Error("ListScreensView without tenant succeeded")
	}
	if _, err := svc.Roles(bg); err == nil {
		t.Error("Roles without tenant succeeded")
	}
	if err := svc.DeleteReport(bg, 1); err == nil {
		t.Error("DeleteReport without tenant succeeded")
	}
	if _, err := svc.SaveDataset(bg, 0, DatasetForm{Name: "x", Source: "plg_cron_job", Columns: []string{"name"}}); err == nil {
		t.Error("SaveDataset without tenant succeeded")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDatasetRejectsForbiddenSources(t *testing.T) {
	svc, mock := newTestService(t)
	bad := []DatasetForm{
		{Name: "u", Source: "sys_user", Columns: []string{"username"}},
		{Name: "u", Source: "SYS_USER", Columns: []string{"username"}},
		{Name: "u", Source: "`sys_user`", Columns: []string{"password"}},
		{Name: "u", Source: "public.sys_user", Columns: []string{"password"}},
		{Name: "c", Source: "sys_config", Columns: []string{"value"}},
		{Name: "j", Source: "plg_cron_job", Columns: []string{"params"}},    // 白名单之外的列
		{Name: "j", Source: "plg_cron_job", Columns: []string{"tenant_id"}}, // 租户列不可选
		{Name: "j", Source: "plg_cron_job", Columns: []string{"name; drop table x"}},
		{Name: "j", Source: "plg_cron_job", Columns: nil},
		{Name: "", Source: "plg_cron_job", Columns: []string{"name"}},
		{Name: "j", Source: "plg_cron_job", Columns: []string{"name"}, Filters: []Filter{{Column: "params", Op: "=", Value: "x"}}},
		{Name: "j", Source: "plg_cron_job", Columns: []string{"name"}, Filters: []Filter{{Column: "name", Op: "OR 1=1", Value: "x"}}},
	}
	for i, f := range bad {
		if _, err := svc.SaveDataset(tctx(7), 0, f); err == nil {
			t.Errorf("case %d accepted: %+v", i, f)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil { // 没有任何 SQL 被执行
		t.Fatal(err)
	}
}

func TestSaveDatasetInsertsWithTenant(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectExec("INSERT INTO `plg_report_dataset`").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(7), "任务", "plg_cron_job", `{"columns":["name","enabled"],"filters":[]}`, "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	v, err := svc.SaveDataset(tctx(7), 0, DatasetForm{Name: "任务", Source: "plg_cron_job", Columns: []string{"name", "enabled", "name"}})
	if err != nil {
		t.Fatal(err)
	}
	if v.ID == 0 || v.Source != "plg_cron_job" || len(v.Columns) != 2 {
		t.Fatalf("view = %+v", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeFilters(t *testing.T) {
	cfg := ReportConfig{Filters: []ReportFilter{
		{Key: "range", Type: "dateRange", DatasetID: "5", Column: "started_at"},
		{Key: "st", Type: "select", DatasetID: "5", Column: "status", Multiple: true},
		{Key: "one", Type: "select", DatasetID: "5", Column: "handler"},
		{Key: "other", Type: "select", DatasetID: "6", Column: "status"},
	}}
	got, err := runtimeFilters(cfg, 5, map[string]any{
		"range": []any{"2026-01-01", "2026-01-31"}, "st": []any{"ok", "failed"}, "one": "http", "other": "zzz", "tenant_id": 1.0, "x": "y",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Filter{
		{Column: "started_at", Op: ">=", Value: "2026-01-01"}, {Column: "started_at", Op: "<", Value: "2026-02-01"},
		{Column: "status", Op: "in", Value: []any{"ok", "failed"}}, {Column: "handler", Op: "=", Value: "http"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i].Column != want[i].Column || got[i].Op != want[i].Op {
			t.Errorf("filter[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got[1].Value != "2026-02-01" {
		t.Errorf("end date must be exclusive next day, got %v", got[1].Value)
	}
	for _, bad := range []map[string]any{{"range": "2026-01-01"}, {"range": []any{"a"}}, {"one": []any{"a", "b"}}} {
		if _, err := runtimeFilters(cfg, 5, bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	// 请求里的键只能命中报表配置的筛选器；"tenant_id" 之类的键被忽略，不会变成过滤条件。
	got, _ = runtimeFilters(cfg, 5, map[string]any{"tenant_id": 2.0, "status": "x"})
	if len(got) != 0 {
		t.Errorf("unknown keys produced filters: %+v", got)
	}
}

func TestWriteXLSX(t *testing.T) {
	res := &Result{
		Columns: []ResultCol{{Key: "d0", Label: "名称", Kind: "dim", Type: TypeString}, {Key: "m0", Label: "数量", Kind: "measure", Type: TypeNumber}},
		Rows:    []map[string]any{{"d0": "=HYPERLINK(\"http://x\")", "m0": int64(3)}, {"d0": nil, "m0": 2.5}},
	}
	var buf bytes.Buffer
	if err := writeXLSX(&buf, "图表/1", res); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("PK")) || buf.Len() < 200 {
		t.Fatalf("not a zip/xlsx: %d bytes", buf.Len())
	}
	if strings.ContainsAny(safeFileName("a/b\\c\"d;e"), `/\";`) {
		t.Error("unsafe characters in download name")
	}
}
