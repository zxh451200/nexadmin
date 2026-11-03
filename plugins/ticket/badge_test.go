package ticket

// 徽标计数与报告人历史的单元测试；守卫存在性检查（ticketExists）也在这批。

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBadgeCount(t *testing.T) {
	db, m := mockDB(t)
	svc := NewService(db, nopLogger{})

	// 未认领 open 2 张 + 认领给我 1 张（含 pending）= 3。
	m.ExpectQuery("SELECT count\\(\\*\\) FROM .plg_ticket.").WithArgs(
		StatusOpen, StatusProcessing, StatusPending, StatusOpen, int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"count(*)"}).AddRow(3))
	n, err := svc.BadgeCount(testCtx(), 11)
	if err != nil {
		t.Fatalf("badge count: %v", err)
	}
	if n != 3 {
		t.Fatalf("badge = %d, want 3", n)
	}
}

func TestReporterHistory(t *testing.T) {
	db, m := mockDB(t)
	svc := NewService(db, nopLogger{})

	// 报告人留了联系方式：查同 reporterId 的最近 10 张。
	m.ExpectQuery("FROM .plg_ticket. WHERE ticket_no").
		WillReturnRows(sqlmock.NewRows([]string{"id", "reporter_id"}).AddRow(1001, "cust@example.org"))
	m.ExpectQuery("FROM .plg_ticket. WHERE reporter_id").
		WillReturnRows(sqlmock.NewRows([]string{"ticket_no", "title", "status", "priority", "reporter_name", "created_at"}).
			AddRow("TK1002", "上次的问题", StatusClosed, PriorityNormal, "customer", testNow))
	rows, err := svc.ReporterHistory(testCtx(), "TK1001")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(rows) != 1 || rows[0].TicketNo != "TK1002" {
		t.Fatalf("rows = %+v", rows)
	}

	// 没留联系方式：没有匹配口径，直接空列表（第二条查询不该发生）。
	m.ExpectQuery("FROM .plg_ticket. WHERE ticket_no").
		WillReturnRows(sqlmock.NewRows([]string{"id", "reporter_id"}).AddRow(1002, ""))
	rows, err = svc.ReporterHistory(testCtx(), "TK1002")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty reporter should give empty history, got %+v", rows)
	}
}

func TestTicketExists(t *testing.T) {
	db, m := mockDB(t)
	svc := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT count\\(\\*\\) FROM .plg_ticket.").WithArgs("TK1001").
		WillReturnRows(sqlmock.NewRows([]string{"count(*)"}).AddRow(1))
	if !svc.ticketExists(testCtx(), "TK1001") {
		t.Fatal("existing ticket should be found")
	}

	m.ExpectQuery("SELECT count\\(\\*\\) FROM .plg_ticket.").WithArgs("TK404").
		WillReturnRows(sqlmock.NewRows([]string{"count(*)"}).AddRow(0))
	if svc.ticketExists(testCtx(), "TK404") {
		t.Fatal("missing ticket should not be found")
	}
	if svc.ticketExists(testCtx(), "") {
		t.Fatal("empty no should short-circuit false")
	}
}

func TestWSFrameShape(t *testing.T) {
	got := marshalFrame(wsFrame{Type: wsKindReplied, TicketNo: "TK1", Status: StatusPending, Preview: "你好"})
	want := `{"type":"replied","ticketNo":"TK1","status":"pending","preview":"你好"}`
	if got != want {
		t.Fatalf("frame = %s, want %s", got, want)
	}
	// rated 不进 feed（ws.go 的 publishWS 分支），这里锁帧类型常量防手滑改名。
	for _, k := range []string{"created", "replied", "status", "assigned"} {
		if k != wsKindCreated && k != wsKindReplied && k != wsKindStatus && k != wsKindAssigned {
			t.Fatalf("unknown kind %s", k)
		}
	}
}
