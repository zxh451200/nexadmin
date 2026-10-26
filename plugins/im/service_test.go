package im

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

func convRow(no, status string, msgSeq, agentRead int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "conv_no", "biz_type", "biz_no", "visitor_token", "visitor_name", "subject",
		"status", "msg_seq", "agent_read_seq", "last_msg_at", "last_preview", "created_at", "updated_at",
	}).AddRow(
		1001, no, "trade_group", "G-1", "tok-1", "visitor", "order G-1",
		status, msgSeq, agentRead, testNow, "hi", testNow, testNow,
	)
}

func msgRows(seq ...int64) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{
		"id", "conv_id", "seq", "sender_type", "sender_id", "sender_name", "content", "created_at", "updated_at",
	})
	for i, s := range seq {
		rows.AddRow(2000+int64(i), 1001, s, "visitor", "tok-1", "visitor", "msg", testNow, testNow)
	}
	return rows
}

// Open is idempotent on (biz_type, biz_no, visitor_token): the second call
// returns the existing conversation without an INSERT.
func TestOpenIdempotent(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE").
		WillReturnRows(convRow("IM1001", StatusOpen, 0, 0))

	c, err := s.Open(context.Background(), OpenInput{BizType: "trade_group", BizNo: "G-1", VisitorToken: "tok-1"})
	if err != nil {
		t.Fatal(err)
	}
	if c.ConvNo != "IM1001" || c.Status != StatusOpen {
		t.Fatalf("unexpected conversation %+v", c)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Open requires a visitor token.
func TestOpenRequiresVisitor(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})
	_, err := s.Open(context.Background(), OpenInput{})
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
}

// Send allocates the next per-conversation seq, updates the summary columns,
// and publishes im.message.created after commit.
func TestSendAllocatesSeqAndPublishes(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	events := make(chan event.Event, 4)
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name == EventMessageCreated {
			events <- e
		}
	}))

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\?").
		WillReturnRows(convRow("IM1001", StatusOpen, 3, 3))
	m.ExpectExec("INSERT INTO `plg_im_message`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("UPDATE `plg_im_conversation`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\?").
		WillReturnRows(convRow("IM1001", StatusOpen, 4, 3))

	msg, err := s.Send(context.Background(), "IM1001", SendInput{
		SenderType: SenderAgent, SenderID: "7", SenderName: "agent", Content: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Seq != 4 {
		t.Fatalf("seq = %d, want 4", msg.Seq)
	}
	select {
	case ev := <-events:
		data, ok := ev.Data.(EventData)
		if !ok || data.Seq != 4 || data.SenderType != SenderAgent {
			t.Fatalf("unexpected event payload %+v", ev.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("im.message.created not published")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Send on a closed conversation is rejected and writes nothing.
func TestSendClosedConflict(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\?").
		WillReturnRows(convRow("IM1001", StatusClosed, 3, 3))
	m.ExpectRollback()

	_, err := s.Send(context.Background(), "IM1001", SendInput{SenderType: SenderVisitor, Content: "hi"})
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusConflict {
		t.Fatalf("want 409, got %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Messages returns only messages after the since cursor, ascending.
func TestMessagesSinceCursor(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\?").
		WillReturnRows(convRow("IM1001", StatusOpen, 5, 5))
	m.ExpectQuery("SELECT \\* FROM `plg_im_message` WHERE").
		WithArgs(int64(1001), int64(3), 50).
		WillReturnRows(msgRows(4, 5))

	_, rows, err := s.Messages(context.Background(), "IM1001", 3, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Seq != 4 || rows[1].Seq != 5 {
		t.Fatalf("unexpected rows %+v", rows)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Close only transitions an open conversation.
func TestCloseTransition(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\?").
		WillReturnRows(convRow("IM1001", StatusOpen, 3, 3))
	m.ExpectExec("UPDATE `plg_im_conversation`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	c, err := s.Close(context.Background(), "IM1001")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusClosed {
		t.Fatalf("status = %s, want closed", c.Status)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// Closing again is a conflict.
	m2db, m2 := mockDB(t)
	s2 := NewService(m2db, nopLogger{})
	m2.ExpectBegin()
	m2.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\?").
		WillReturnRows(convRow("IM1001", StatusClosed, 3, 3))
	m2.ExpectRollback()
	if _, err := s2.Close(context.Background(), "IM1001"); err == nil {
		t.Fatal("second close should conflict")
	}
}

// preview truncates to 120 bytes for the event payload and conversation summary.
func TestPreview(t *testing.T) {
	if got := preview("  hello  "); got != "hello" {
		t.Fatalf("preview = %q", got)
	}
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	if got := preview(string(long)); len(got) != 120 {
		t.Fatalf("len = %d, want 120", len(got))
	}
}
