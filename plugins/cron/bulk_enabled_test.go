package cron

// 批量启停与手动执行参数覆盖的行为闸。

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

var idgenOnce sync.Once

// initIDgen 供会真实建行（INSERT）的用例初始化 ID 发号器。
func initIDgen(t *testing.T) {
	t.Helper()
	idgenOnce.Do(func() {
		if err := idgen.Init(43); err != nil {
			t.Fatalf("idgen.Init: %v", err)
		}
	})
}

// 批量启停只动点名的 id，UPDATE 走 map 形式（false 不能被 default 吞掉）。
func TestSetJobsEnabledOnlyRequestedIDs(t *testing.T) {
	s, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `plg_cron_job` SET `enabled`=").WithArgs(false, sqlmock.AnyArg(), int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := s.SetJobsEnabled(tenant.With(context.Background(), 1), []idgen.ID{11, 12}, false)
	if err != nil {
		t.Fatalf("SetJobsEnabled: %v", err)
	}
	if n != 2 {
		t.Errorf("updated = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 手动执行带参数覆盖：空参数在触发前就拒绝（不建日志行、不起 goroutine）。
func TestRunNowRejectsEmptyParamsOverride(t *testing.T) {
	s, mock := mockDB(t)
	blank := "   "
	mock.ExpectQuery("SELECT \\* FROM `plg_cron_job`").WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "handler", "params"}).AddRow(11, "清理日志", "clean_logs", "{}"))

	if _, err := s.RunNow(tenant.With(context.Background(), 1), 11, &blank); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("err = %v, want 400", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 带合法参数覆盖时不报错（dispatch 建日志行走 INSERT，但 goroutine 里的执行不校验）。
func TestRunNowAcceptsParamsOverride(t *testing.T) {
	initIDgen(t)
	s, mock := mockDB(t)
	p := `{"days":7}`
	mock.ExpectQuery("SELECT \\* FROM `plg_cron_job`").WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "handler", "params", "tenant_id"}).AddRow(11, "清理日志", "clean_logs", "{}", 1))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `plg_cron_log`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if _, err := s.RunNow(tenant.With(context.Background(), 1), 11, &p); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
