package message

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/pkg/idgen"
)

// 日期区间按站点时区解释用户选的「某一天」，再换算成 UTC 比较（created_at 落库是 UTC）。
// 夹具用 Asia/Shanghai 而不是 UTC：UTC 是恒等元，偏移写错也照不出来。
func TestListDateRangeUsesSiteTimezone(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nil, "Asia/Shanghai")

	const uid = idgen.ID(91)
	from := time.Date(2026, 10, 30, 16, 0, 0, 0, time.UTC) // 2026-10-31 00:00 +08:00
	to := time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)   // 2026-11-01 00:00 +08:00（含 10-31 整天）

	mock.ExpectQuery(`SELECT count\(\*\)`).WithArgs(int64(uid), from, to).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM`).WithArgs(int64(uid), from, to, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, _, err := svc.List(context.Background(), uid, ListQuery{From: "2026-10-31", To: "2026-10-31", Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 空日期不参与筛选：只带 user_id，不能把 created_at 条件塞成零值。
func TestListIgnoresEmptyDateRange(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nil, "Asia/Shanghai")

	const uid = idgen.ID(92)
	mock.ExpectQuery(`SELECT count\(\*\)`).WithArgs(int64(uid)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM`).WithArgs(int64(uid), 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, _, err := svc.List(context.Background(), uid, ListQuery{From: "", To: "", Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
