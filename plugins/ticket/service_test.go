package ticket

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func TestMain(m *testing.M) {
	if err := idgen.Init(1); err != nil {
		panic(err)
	}
	gin.SetMode(gin.TestMode)
	m.Run()
}

var testNow = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		NowFunc:              func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, m
}

type nopLogger struct{}

func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

func ticketRow(no, status string, replySeq int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "ticket_no", "biz_type", "biz_no", "title", "status", "priority",
		"reporter_type", "reporter_id", "reporter_name", "assignee_id", "assignee_name",
		"reply_seq", "first_reply_at", "last_reply_at", "last_replier_type", "last_preview",
		"created_at", "updated_at",
	}).AddRow(
		1001, no, "trade_group", "G-1", "order broken", status, PriorityNormal,
		ReplierReporter, "tok-1", "visitor", 0, "",
		replySeq, nil, testNow, ReplierReporter, "hi",
		testNow, testNow,
	)
}

// ValidPriority / Settleable 的小函数面。
func TestConstants(t *testing.T) {
	for _, p := range []string{PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent} {
		if !ValidPriority(p) {
			t.Fatalf("ValidPriority(%q) = false", p)
		}
	}
	if ValidPriority("urgent!") {
		t.Fatal("invalid priority accepted")
	}
	if !Settleable(StatusOpen) || !Settleable(StatusProcessing) {
		t.Fatal("open/processing should be settleable")
	}
	if Settleable(StatusResolved) || Settleable(StatusClosed) {
		t.Fatal("resolved/closed should not be settleable")
	}
}

// SLA 时限：按优先级取值，未知回落 normal。
func TestSLAFor(t *testing.T) {
	if SLAFor(PriorityUrgent) != 2*time.Hour {
		t.Fatal("urgent should be 2h")
	}
	if SLAFor(PriorityNormal) != 24*time.Hour {
		t.Fatal("normal should be 24h")
	}
	if SLAFor("whatever") != 24*time.Hour {
		t.Fatal("unknown priority should fall back to normal")
	}
}

// MarkOverdue 只更新未完结且到期的行（gorm 默认事务包裹 → Begin/Commit）。
func TestMarkOverdue(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_ticket`").WillReturnResult(sqlmock.NewResult(0, 3))
	m.ExpectCommit()
	n, err := s.MarkOverdue(context.Background(), testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("marked %d, want 3", n)
	}
}

// Create 写工单 + seq=1 首条回复（同一事务），并发 ticket.created 事件。
func TestCreatePublishesAndInsertsReply(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	events := make(chan event.Event, 4)
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name == EventCreated {
			events <- e
		}
	}))

	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_ticket`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("INSERT INTO `plg_ticket_reply`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	tk, err := s.Create(context.Background(), CreateInput{
		BizType: "trade_group", BizNo: "G-1", Title: "order broken",
		Content: "item arrived broken", Priority: PriorityHigh,
		ReporterType: ReplierReporter, ReporterID: "tok-1", ReporterName: "visitor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != StatusOpen || tk.Priority != PriorityHigh || tk.ReplySeq != 1 {
		t.Fatalf("unexpected ticket %+v", tk)
	}
	select {
	case ev := <-events:
		data, ok := ev.Data.(EventData)
		if !ok || data.Seq != 1 || data.SenderType != ReplierReporter {
			t.Fatalf("unexpected event payload %+v", ev.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ticket.created not published")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Create 缺标题/内容直接 400，不碰数据库。
func TestCreateValidation(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})
	_, err := s.Create(context.Background(), CreateInput{Title: "x", ReporterType: ReplierReporter})
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
}

// 坐席回复：seq 递增、open→processing、补首次响应时间与默认认领。
func TestAgentReplyTransitions(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectExec("INSERT INTO `plg_ticket_reply`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("UPDATE `plg_ticket`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE id = \\?").
		WillReturnRows(ticketRow("TK1001", StatusProcessing, 2))
	m.ExpectCommit()

	reply, tk, err := s.Reply(context.Background(), "TK1001", ReplyInput{
		ReplierType: ReplierAgent, ReplierID: "7", ReplierName: "alice", Content: "working on it",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Seq != 2 {
		t.Fatalf("seq = %d, want 2", reply.Seq)
	}
	if tk.Status != StatusProcessing {
		t.Fatalf("status = %s, want processing", tk.Status)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 报告人在 closed 工单上回复自动重开为 processing。
func TestVisitorReplyReopens(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusClosed, 3))
	m.ExpectExec("INSERT INTO `plg_ticket_reply`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("UPDATE `plg_ticket`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE id = \\?").
		WillReturnRows(ticketRow("TK1001", StatusProcessing, 4))
	m.ExpectCommit()

	_, tk, err := s.Reply(context.Background(), "TK1001", ReplyInput{
		ReplierType: ReplierReporter, ReplierID: "tok-1", ReplierName: "visitor", Content: "still broken",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != StatusProcessing {
		t.Fatalf("status = %s, want processing (reopened)", tk.Status)
	}
}

// 状态机：resolved 只能从 open/processing 进；关闭后的工单再 resolved 是冲突。
func TestSetStatusTransitions(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// open → resolved 合法
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 2))
	m.ExpectExec("UPDATE `plg_ticket`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	if _, err := s.SetStatus(context.Background(), "TK1001", StatusResolved, "alice"); err != nil {
		t.Fatal(err)
	}

	// closed → resolved 非法
	db2, m2 := mockDB(t)
	s2 := NewService(db2, nopLogger{})
	m2.ExpectBegin()
	m2.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusClosed, 2))
	m2.ExpectRollback()
	_, err := s2.SetStatus(context.Background(), "TK1001", StatusResolved, "alice")
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusConflict {
		t.Fatalf("want 409, got %v", err)
	}

	// 非法目标值直接 400
	if _, err := s.SetStatus(context.Background(), "TK1001", "nonsense", "alice"); err == nil {
		t.Fatal("invalid target accepted")
	}
}

// Replies 按 since 游标增量拉取。
func TestRepliesSinceCursor(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	rows := sqlmock.NewRows([]string{
		"id", "ticket_id", "seq", "replier_type", "replier_id", "replier_name", "content", "created_at", "updated_at",
	}).AddRow(2001, 1001, 2, ReplierAgent, "7", "alice", "hi", testNow, testNow)

	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusProcessing, 2))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_reply` WHERE").
		WithArgs(int64(1001), 1, 50).
		WillReturnRows(rows)

	_, list, err := s.Replies(context.Background(), "TK1001", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Seq != 2 {
		t.Fatalf("unexpected replies %+v", list)
	}
}
