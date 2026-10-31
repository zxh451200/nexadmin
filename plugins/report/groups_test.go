package report

import (
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// 报表分组的下拉选项：去重、去空，且必须按租户隔离。
func TestListReportGroupsScopedAndDeduped(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectQuery("DISTINCT `group_name` FROM `plg_report_report`").
		WithArgs(int64(7), "").
		WillReturnRows(sqlmock.NewRows([]string{"group_name"}).AddRow("运维").AddRow("plugin_report.builtin.group.overview"))
	got, err := svc.ListReportGroups(tctx(7))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "运维" {
		t.Fatalf("分组列表不对：%v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 一条报表都没有时必须是 []：Go 的 nil 切片序列化成 null，前端 map 直接崩页。
func TestListReportGroupsEmptyIsNotNull(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectQuery("DISTINCT `group_name` FROM `plg_report_report`").
		WithArgs(int64(8), "").
		WillReturnRows(sqlmock.NewRows([]string{"group_name"}))
	got, err := svc.ListReportGroups(tctx(8))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Fatalf("空结果要序列化成 []，实得 %s", raw)
	}
}
