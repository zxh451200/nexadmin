package pay

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
)

// 订单导出与日期范围：财务对账的入口。日期错一格、导出被静默截断，
// 对出来的账就是错的——这两条必须显式报错，测试钉住。

func TestOrderDateRange(t *testing.T) {
	loc := time.UTC
	t.Run("两端含当天", func(t *testing.T) {
		from, to, err := orderDateRange("2026-05-01", "2026-05-03", loc)
		if err != nil {
			t.Fatal(err)
		}
		if !from.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, loc)) {
			t.Errorf("from = %v", from)
		}
		// to 是排他上界：5 月 3 日整天都要进结果
		if !to.Equal(time.Date(2026, 5, 4, 0, 0, 0, 0, loc)) {
			t.Errorf("to = %v", to)
		}
	})

	t.Run("空值不限", func(t *testing.T) {
		from, to, err := orderDateRange("", "", loc)
		if err != nil || !from.IsZero() || !to.IsZero() {
			t.Fatalf("empty range = %v %v %v", from, to, err)
		}
	})

	for _, tc := range []struct {
		name     string
		from, to string
	}{
		{"from 格式错", "2026/05/01", ""},
		{"to 格式错", "", "05-03"},
		{"起晚于止", "2026-05-03", "2026-05-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := orderDateRange(tc.from, tc.to, loc)
			var be *response.Error
			if !errors.As(err, &be) || be.Key != errDateInvalid || be.Status != http.StatusBadRequest {
				t.Fatalf("want %s 400, got %v", errDateInvalid, err)
			}
		})
	}
}

func TestListOrdersAppliesDateRange(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	m.ExpectQuery(`SELECT count\(\*\) FROM .plg_pay_order. WHERE created_at >= \? AND created_at < \?`).
		WithArgs(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectQuery(`SELECT \* FROM .plg_pay_order. WHERE created_at >= \? AND created_at < \?`).
		WithArgs(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC), 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, _, err := s.ListOrders(context.Background(), OrderQuery{
		CreatedFrom: "2026-05-01", CreatedTo: "2026-05-03", Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 超限必须报错而不是截断：少一行的对账单比没有更糟。
func TestExportOrdersRejectsOverflow(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	m.ExpectQuery(`SELECT count\(\*\) FROM .plg_pay_order.`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(exportMax + 1))

	_, total, err := s.ExportOrders(context.Background(), OrderQuery{})
	var be *response.Error
	if !errors.As(err, &be) || be.Key != errExportTooMany {
		t.Fatalf("want %s, got %v", errExportTooMany, err)
	}
	if total != exportMax+1 {
		t.Errorf("total = %d, want %d", total, exportMax+1)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExportOrdersWithinCap(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	m.ExpectQuery(`SELECT count\(\*\) FROM .plg_pay_order.`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	m.ExpectQuery(`SELECT \* FROM .plg_pay_order. ORDER BY id DESC LIMIT \?`).
		WithArgs(exportMax).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	rows, total, err := s.ExportOrders(context.Background(), OrderQuery{})
	if err != nil || total != 2 || len(rows) != 0 {
		t.Fatalf("rows=%d total=%d err=%v", len(rows), total, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
