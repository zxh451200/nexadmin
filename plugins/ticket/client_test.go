package ticket

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/pkg/idgen"
)

// 宿主服务端入口（client.go）。这里只验两件事：默认服务能否解析到，以及反查条件是否真的进了 SQL；
// 业务逻辑本身已在 service 层各用例覆盖。

// useDefaultSvc 临时把测试服务设为进程级默认，并在用例结束后还原（别的用例也依赖它）。
func useDefaultSvc(t *testing.T, s *Service) {
	t.Helper()
	prev := current()
	SetDefault(s)
	t.Cleanup(func() {
		// 先清空再回填，避免 SetDefault 叠加成两个。
		drainDefaultSvc()
		if prev != nil {
			SetDefault(prev)
		}
	})
}

func drainDefaultSvc() {
	for {
		select {
		case <-defaultSvc:
		default:
			return
		}
	}
}

func TestBizTicketsCarriesBizFilters(t *testing.T) {
	db, m := mockDB(t)
	useDefaultSvc(t, NewService(db, nopLogger{}))

	m.ExpectQuery("SELECT count(.*) FROM `plg_ticket`").
		WithArgs("order", "O-77").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	m.ExpectQuery("SELECT .* FROM `plg_ticket`").
		WithArgs([]driver.Value{"order", "O-77", int64(hostQueryLimit)}...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ticket_no", "biz_no"}).AddRow(1001, "TK1001", "O-77"))

	rows, truncated, err := BizTickets(context.Background(), "order", "O-77")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TicketNo != "TK1001" || truncated {
		t.Fatalf("rows=%+v truncated=%v", rows, truncated)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 命中数超过上限必须报 truncated，而不是让调用方以为「这张业务单只有这几张工单」。
func TestHostTicketsReportTruncated(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery("SELECT count(.*) FROM `plg_ticket`").
		WithArgs("order").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	m.ExpectQuery("SELECT .* FROM `plg_ticket`").
		WithArgs("order", int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ticket_no"}).AddRow(1, "TK1").AddRow(2, "TK2"))

	rows, truncated, err := s.HostTickets(context.Background(), TicketQuery{BizType: "order"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !truncated {
		t.Fatalf("rows=%d truncated=%v, want 2 行且 truncated=true", len(rows), truncated)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 报告人筛选只在「类型 + ID」成对时才加：访客令牌与后台用户 ID 会撞号，宁可少筛也别串味。
// 两条用例的期望参数数量不同（2 个 vs 0 个），条件多加或少加都会红。
func TestReporterFilterNeedsBothTypeAndID(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT count(.*) FROM `plg_ticket` WHERE reporter_type").
		WithArgs(ReplierReporter, "tok-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectQuery("SELECT .* FROM `plg_ticket`").
		WithArgs(ReplierReporter, "tok-1", int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, _, err := s.HostTickets(context.Background(), TicketQuery{ReporterType: ReplierReporter, ReporterID: "tok-1"}, 10); err != nil {
		t.Fatal(err)
	}

	// 只给 ID 不给类型：条件不该出现，count 与取行都是零参数。
	m.ExpectQuery("SELECT count\\(.*\\) FROM `plg_ticket`$").
		WithArgs().
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectQuery("SELECT .* FROM `plg_ticket` ORDER").
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, _, err := s.HostTickets(context.Background(), TicketQuery{ReporterID: "tok-1"}, 10); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestClientUnavailableWithoutMountedPlugin(t *testing.T) {
	prev := current()
	drainDefaultSvc()
	t.Cleanup(func() {
		if prev != nil {
			SetDefault(prev)
		}
	})
	if current() != nil {
		t.Fatal("默认服务没被清空，后面的断言都是假的")
	}
	if _, err := Get(context.Background(), "TK1"); err == nil {
		t.Fatal("未挂载时 Ticket 应返回服务不可用")
	}
	if _, _, err := BizTickets(context.Background(), "order", "O-1"); err == nil {
		t.Fatal("未挂载时 BizTickets 应返回服务不可用")
	}
	if _, err := Activities(context.Background(), "TK1", true); err == nil {
		t.Fatal("未挂载时 Activities 应返回服务不可用")
	}
	if _, err := SetStatus(context.Background(), "TK1", StatusClosed, "host"); err == nil {
		t.Fatal("未挂载时 SetStatus 应返回服务不可用")
	}
	if _, err := Assign(context.Background(), "TK1", idgen.ID(7), "kf"); err == nil {
		t.Fatal("未挂载时 Assign 应返回服务不可用")
	}
}
