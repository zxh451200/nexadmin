package report

import (
	"bytes"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func idOf(v int64) idgen.ID { return idgen.ID(v) }

func isStatus(err error, status int) bool {
	var re *response.Error
	return errors.As(err, &re) && re.Status == status
}

const reportRowSQL = "FROM `plg_report_report` WHERE tenant_id = \\? AND id = \\?"

func reportRows(cfg string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "tenant_id", "name", "group_name", "remark", "config"}).AddRow(3, 7, "R", "", "", cfg)
}

func expectChartAndDataset(mock sqlmock.Sqlmock, typ, chartCfg, dsCfg string) {
	mock.ExpectQuery("FROM `plg_report_chart` WHERE tenant_id = \\? AND id = \\?").WithArgs(int64(7), int64(11), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "dataset_id", "type", "config"}).AddRow(11, 7, "状态明细", 5, typ, chartCfg))
	mock.ExpectQuery("FROM `plg_report_dataset` WHERE tenant_id = \\? AND id = \\?").WithArgs(int64(7), int64(5), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "source", "config"}).AddRow(5, 7, "日志", "plg_cron_log", dsCfg))
}

const tableChartCfg = `{"dimensions":[{"column":"status"}],"measures":[],"filters":[],"orderBy":[],"limit":100,"colors":[]}`

// 报表只能跑自己引用的图表；导出受行数上限约束（测试里 MaxRows=3）。
func TestExportRowCapAndChartMembership(t *testing.T) {
	svc, mock := newTestService(t)
	cfg := `{"items":[{"chartId":"11","group":"","span":12}],"filters":[]}`

	mock.ExpectQuery(reportRowSQL).WithArgs(int64(7), int64(3), 1).WillReturnRows(reportRows(cfg))
	expectChartAndDataset(mock, "table", tableChartCfg, `{"columns":["status","attempts"],"filters":[]}`)
	mock.ExpectQuery("SELECT `status` AS `d0` FROM `plg_cron_log` WHERE `tenant_id` = \\? ORDER BY 1 LIMIT \\?").WithArgs(int64(7), 4).
		WillReturnRows(sqlmock.NewRows([]string{"d0"}).AddRow("a").AddRow("b").AddRow("c").AddRow("d"))
	var buf bytes.Buffer
	var got ExportFile
	err := svc.ExportReportChart(tctx(7), func(f ExportFile) ioWriter { got = f; return &buf }, 3, 11, nil, allViewer())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || got.Name != "状态明细" || !bytes.HasPrefix(buf.Bytes(), []byte("PK")) {
		t.Fatalf("export = %+v, %d bytes", got, buf.Len())
	}

	// 图表 12 不在报表 3 里：不能借报表入口跑任意图表（只读了报表，没有去查图表/数据）。
	mock.ExpectQuery(reportRowSQL).WithArgs(int64(7), int64(3), 1).WillReturnRows(reportRows(cfg))
	if _, err = svc.QueryReportChart(tctx(7), 3, 12, nil, allViewer()); !isStatus(err, 404) {
		t.Fatalf("err = %v, want 404", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExportOnlyTableCharts(t *testing.T) {
	svc, mock := newTestService(t)
	cfg := `{"items":[{"chartId":"11","group":"","span":12}],"filters":[]}`
	mock.ExpectQuery(reportRowSQL).WithArgs(int64(7), int64(3), 1).WillReturnRows(reportRows(cfg))
	expectChartAndDataset(mock, "pie",
		`{"dimensions":[{"column":"status"}],"measures":[{"agg":"count"}],"filters":[],"orderBy":[],"limit":0,"colors":[]}`,
		`{"columns":["status"],"filters":[]}`)
	mock.ExpectQuery("SELECT `status` AS `d0`,COUNT\\(\\*\\) AS `m0` FROM `plg_cron_log`").WillReturnRows(sqlmock.NewRows([]string{"d0", "m0"}))
	var buf bytes.Buffer
	err := svc.ExportReportChart(tctx(7), func(ExportFile) ioWriter { return &buf }, 3, 11, nil, allViewer())
	if !isStatus(err, 400) || buf.Len() != 0 {
		t.Fatalf("err = %v, wrote %d bytes", err, buf.Len())
	}
}

// 数据集被撤出白名单（或列被撤）后，已有图表立刻不可用。
func TestRevokedSourceStopsExistingCharts(t *testing.T) {
	svc, mock := newTestService(t)
	cfg := `{"items":[{"chartId":"11","group":"","span":12}],"filters":[]}`
	mock.ExpectQuery(reportRowSQL).WithArgs(int64(7), int64(3), 1).WillReturnRows(reportRows(cfg))
	expectChartAndDataset(mock, "table", tableChartCfg, `{"columns":["status"],"filters":[]}`)
	svc.reg = newRegistry(nil, nil) // 白名单清空
	if _, err := svc.QueryReportChart(tctx(7), 3, 11, nil, allViewer()); !isStatus(err, 400) {
		t.Fatalf("err = %v, want 400", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
