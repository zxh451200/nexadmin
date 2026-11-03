package report

// 批量删除的回归网：三张表（数据集/图表/报表）的批量端点。
// 每条断言都要能「删掉守卫就变红」——所以 SELECT/DELETE 都用 WithArgs 逐字钉住租户条件与 id 清单，
// sqlmock 只做正则匹配、从不执行 SQL，参数对不上或语句顺序不对都会红。

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func ids(v ...int64) []idgen.ID {
	out := make([]idgen.ID, 0, len(v))
	for _, n := range v {
		out = append(out, idgen.ID(n))
	}
	return out
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	var re *response.Error
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want *response.Error", err)
	}
	return re.Status
}

// 批量路由不带 ids（或 ids 非法）必须 400。
// newTestEngine 里的 svc 用的是「一条期望都没注册」的 sqlmock：任何 SQL 都会被拒，
// 业务层出错只会得到 500。所以「恰好 400」本身就证明了守卫在发任何语句之前拦下了请求。
func TestBatchDeleteRoutesRejectBadIDs(t *testing.T) {
	e := newTestEngine(t, &auth.Claims{UserID: 5, TenantID: 7, Super: true})
	for _, path := range []string{
		"/api/v1/plugins/report/datasets",
		"/api/v1/plugins/report/charts",
		"/api/v1/plugins/report/reports",
	} {
		for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=0", "?ids=1,abc", "?ids=,"} {
			if code := hit(e, http.MethodDelete, path+q); code != http.StatusBadRequest {
				t.Errorf("DELETE %s%s -> %d, want 400", path, q, code)
			}
		}
	}
}

// 数据集混合场景：点名 3 个，1 个被图表引用 -> 删 2 拦 1。
// SELECT 与 DELETE 都带 tenant_id = ?，去掉租户条件参数就对不上。
func TestBatchDeleteDatasetsSkipsInUse(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectQuery("SELECT `id` FROM `plg_report_dataset` WHERE tenant_id = \\? AND id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(7), int64(10), int64(11), int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(10).AddRow(11).AddRow(12))
	mock.ExpectQuery("SELECT DISTINCT `dataset_id` FROM `plg_report_chart` WHERE tenant_id = \\? AND dataset_id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(7), int64(10), int64(11), int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"dataset_id"}).AddRow(11))
	mock.ExpectExec("DELETE FROM `plg_report_dataset` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(10), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))

	deleted, blocked, err := svc.DeleteDatasets(tctx(7), ids(10, 11, 12))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if deleted != 2 || blocked != 1 {
		t.Fatalf("deleted=%d blocked=%d, want 2/1", deleted, blocked)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 全部被引用：409，且一条写语句都不发（只注册了两个查询期望）。
func TestBatchDeleteDatasetsAllInUseIs409(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectQuery("SELECT `id` FROM `plg_report_dataset` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(10), int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(10).AddRow(11))
	mock.ExpectQuery("SELECT DISTINCT `dataset_id` FROM `plg_report_chart` WHERE tenant_id = \\? AND dataset_id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(10), int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"dataset_id"}).AddRow(10).AddRow(11))

	deleted, blocked, err := svc.DeleteDatasets(tctx(7), ids(10, 11))
	if s := statusOf(t, err); s != http.StatusConflict {
		t.Fatalf("err = %v, want 409", err)
	}
	if deleted != 0 || blocked != 2 {
		t.Fatalf("deleted=%d blocked=%d, want 0/2", deleted, blocked)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 图表批量删：只删真实存在的。DELETE 的 IN (...) 只能含 existing 的那几个 id，
// 将来有人把 existing 换成原始 ids，参数数量就对不上。
func TestBatchDeleteChartsOnlyExisting(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectQuery("SELECT `id` FROM `plg_report_chart` WHERE tenant_id = \\? AND id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(7), int64(11), int64(12), int64(13)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11).AddRow(13))
	mock.ExpectExec("DELETE FROM `plg_report_chart` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(11), int64(13)).
		WillReturnResult(sqlmock.NewResult(0, 2))

	deleted, blocked, err := svc.DeleteCharts(tctx(7), ids(11, 12, 13))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if deleted != 2 || blocked != 1 {
		t.Fatalf("deleted=%d blocked=%d, want 2/1", deleted, blocked)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBatchDeleteReportsOnlyExisting(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectQuery("SELECT `id` FROM `plg_report_report` WHERE tenant_id = \\? AND id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(8), int64(21), int64(22), int64(23)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(22))
	mock.ExpectExec("DELETE FROM `plg_report_subscription` WHERE tenant_id = \\? AND report_id IN \\(\\?\\)").
		WithArgs(int64(8), int64(22)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM `plg_report_report` WHERE tenant_id = \\? AND id IN \\(\\?\\)").
		WithArgs(int64(8), int64(22)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	deleted, blocked, err := svc.DeleteReports(tctx(8), ids(21, 22, 23))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if deleted != 1 || blocked != 2 {
		t.Fatalf("deleted=%d blocked=%d, want 1/2", deleted, blocked)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 单条与批量同口径：删报表必须一起清掉它的订阅，否则 RunDue 每次扫到这条孤儿都只 warn
// 并跳过（跳过时不记 last_sent_date），日志刷不完。这里只注册「清订阅 + 删报表」两条语句，
// 少写任何一条都会因为「期望未满足」而红。
func TestDeleteReportClearsItsSubscription(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectQuery("SELECT \\* FROM `plg_report_report` WHERE tenant_id = \\? AND id = \\? ORDER BY").
		WithArgs(int64(8), int64(31), int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name"}).AddRow(31, 8, "月度销售"))
	mock.ExpectExec("DELETE FROM `plg_report_subscription` WHERE tenant_id = \\? AND report_id = \\?").
		WithArgs(int64(8), int64(31)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM `plg_report_report` WHERE tenant_id = \\? AND id = \\?").
		WithArgs(int64(8), int64(31)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := svc.DeleteReport(tctx(8), 31); err != nil {
		t.Fatalf("err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 订阅提醒邮件的文案：语言分支要真的换语言；内置报表存的是前端 i18n 键，
// 后端词典里没有它，绝不能把键名当报表名发出去。
func TestComposeSubscriptionMailLanguageAndKeyGuard(t *testing.T) {
	zhTitle, zhBody := composeSubscriptionMail("zh-CN", "月度销售")
	if !strings.Contains(zhTitle, "月度销售") || !strings.Contains(zhBody, "月度销售") {
		t.Fatalf("中文文案没带上报表名：%q / %q", zhTitle, zhBody)
	}
	enTitle, enBody := composeSubscriptionMail("en-US", "Monthly sales")
	if !strings.HasPrefix(enTitle, "Your subscribed report: ") || !strings.Contains(enBody, "Sign in") {
		t.Fatalf("英文分支没换语言：%q / %q", enTitle, enBody)
	}
	for _, lang := range []string{"zh-CN", "en-US", ""} {
		title, body := composeSubscriptionMail(lang, keyPrefix+"sales_daily")
		if strings.Contains(title, keyPrefix) || strings.Contains(body, keyPrefix) {
			t.Fatalf("lang=%s 把 i18n 键名发给了用户：%q / %q", lang, title, body)
		}
	}
}

// 一条都不存在（含别人的记录）：404，且零写操作——只注册了 existing 查询。
func TestBatchDeleteAllMissingIs404WithNoWrites(t *testing.T) {
	svc, mock := newTestService(t)
	cases := []struct {
		name  string
		table string
		del   func() (int64, int64, error)
	}{
		{"dataset", "plg_report_dataset", func() (int64, int64, error) { return svc.DeleteDatasets(tctx(7), ids(91, 92)) }},
		{"chart", "plg_report_chart", func() (int64, int64, error) { return svc.DeleteCharts(tctx(7), ids(91, 92)) }},
		{"report", "plg_report_report", func() (int64, int64, error) { return svc.DeleteReports(tctx(7), ids(91, 92)) }},
	}
	for _, tc := range cases {
		mock.ExpectQuery("SELECT `id` FROM `" + tc.table + "` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
			WithArgs(int64(7), int64(91), int64(92)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		deleted, blocked, err := tc.del()
		if s := statusOf(t, err); s != http.StatusNotFound {
			t.Errorf("%s: err = %v, want 404", tc.name, err)
		}
		if deleted != 0 || blocked != 2 {
			t.Errorf("%s: deleted=%d blocked=%d, want 0/2", tc.name, deleted, blocked)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 没有租户上下文的批量删：一条 SQL 都不该发（这里没有任何期望）。
func TestBatchDeleteWithoutTenantSendsNoSQL(t *testing.T) {
	svc, mock := newTestService(t)
	bg := context.Background()
	if _, _, err := svc.DeleteDatasets(bg, ids(1)); err == nil {
		t.Error("DeleteDatasets without tenant succeeded")
	}
	if _, _, err := svc.DeleteCharts(bg, ids(1)); err == nil {
		t.Error("DeleteCharts without tenant succeeded")
	}
	if _, _, err := svc.DeleteReports(bg, ids(1)); err == nil {
		t.Error("DeleteReports without tenant succeeded")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
