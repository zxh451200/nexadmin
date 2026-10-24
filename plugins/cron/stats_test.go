package cron

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/tenant"
)

func countRows(n int64) *sqlmock.Rows { return sqlmock.NewRows([]string{"count"}).AddRow(n) }

// 概览三个数字各一条 COUNT，且都带当前租户条件（租户 scope 在 database.Open 里注册；
// 这里用裸 gorm，所以只校验语句形状与结果映射，租户隔离由 core/database 的 scope 测试保证）。
func TestStats(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_cron_job`").WillReturnRows(countRows(5))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_cron_job` WHERE enabled = \\?").WithArgs(true).WillReturnRows(countRows(3))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_cron_log` WHERE status = \\? AND started_at >= \\?").
		WithArgs(StatusFailed, now.Add(-24*time.Hour)).WillReturnRows(countRows(2))

	got, err := NewService(db, nil).Stats(tenant.With(context.Background(), 1), now)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Stats{Total: 5, Enabled: 3, Failed24h: 2}) {
		t.Fatalf("got %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
