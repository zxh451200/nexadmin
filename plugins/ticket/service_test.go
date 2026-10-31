package ticket

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
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
	return ticketRowWithRating(no, status, replySeq, nil)
}

func ticketRowWithRating(no, status string, replySeq int64, rating any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "ticket_no", "biz_type", "biz_no", "title", "status", "priority",
		"reporter_type", "reporter_id", "reporter_name", "assignee_id", "assignee_name",
		"reply_seq", "first_reply_at", "last_reply_at", "last_replier_type", "last_preview",
		"created_at", "updated_at", "rating", "rating_comment", "rated_at",
		"resolution_due_at", "resolution_overdue",
	}).AddRow(
		1001, no, "trade_group", "G-1", "order broken", status, PriorityNormal,
		ReplierReporter, "tok-1", "visitor", 0, "",
		replySeq, nil, testNow, ReplierReporter, "hi",
		testNow, testNow, rating, "", nil, nil, false,
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
	if !Settleable(StatusOpen) || !Settleable(StatusProcessing) || !Settleable(StatusPending) {
		t.Fatal("open/processing/pending should be settleable")
	}
	if Settleable(StatusResolved) || Settleable(StatusClosed) {
		t.Fatal("resolved/closed should not be settleable")
	}
}

// 状态机矩阵：pending 只能由处理中进入，可由报告人回复/坐席操作恢复为 processing。
func TestValidTransitions(t *testing.T) {
	cases := []struct {
		from, to string
		ok       bool
	}{
		{StatusOpen, StatusProcessing, true},
		{StatusOpen, StatusPending, true},
		{StatusOpen, StatusResolved, true},
		{StatusOpen, StatusClosed, true},
		{StatusProcessing, StatusPending, true},
		{StatusProcessing, StatusResolved, true},
		{StatusProcessing, StatusClosed, true},
		{StatusPending, StatusProcessing, true},
		{StatusPending, StatusResolved, true},
		{StatusPending, StatusClosed, true},
		{StatusResolved, StatusProcessing, true},
		{StatusResolved, StatusClosed, true},
		{StatusResolved, StatusPending, false},
		{StatusClosed, StatusProcessing, false},
		{StatusClosed, StatusPending, false},
		{StatusClosed, StatusResolved, false},
		{StatusClosed, StatusClosed, false},
	}
	for _, c := range cases {
		if got := validTransition(c.from, c.to); got != c.ok {
			t.Errorf("validTransition(%s → %s) = %v, want %v", c.from, c.to, got, c.ok)
		}
	}
}

// SLA 停表：恢复时把暂停时长补回到期时间；没有暂停记录时不动。
func TestResumeDueAt(t *testing.T) {
	due := testNow.Add(2 * time.Hour)
	paused := testNow
	now := testNow.Add(30 * time.Minute)
	got := resumeDueAt(&due, &paused, now)
	if got == nil || !got.Equal(due.Add(30*time.Minute)) {
		t.Fatalf("暂停 30 分钟后到期时间应后移 30 分钟，得到 %v", got)
	}
	if resumeDueAt(&due, nil, now) == nil || !resumeDueAt(&due, nil, now).Equal(due) {
		t.Fatal("无暂停记录时不应改动到期时间")
	}
	if resumeDueAt(nil, &paused, now) != nil {
		t.Fatal("没有到期时间时不应凭空造一个")
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

// MarkOverdue 先取候选再按 id 更新（gorm 默认事务包裹 → Begin/Commit）。
func TestMarkOverdue(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	rows := sqlmock.NewRows([]string{"id", "ticket_no", "title", "assignee_id", "priority"}).
		AddRow(1001, "TK1001", "order broken", 0, PriorityNormal)
	m.ExpectQuery("SELECT .* FROM `plg_ticket` WHERE overdue = \\? AND status IN").
		WillReturnRows(rows)
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_ticket` SET `overdue`=.*WHERE id IN").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectQuery("SELECT .* FROM `plg_ticket` WHERE resolution_overdue = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "ticket_no", "title", "assignee_id", "priority"}))
	n, err := s.MarkOverdue(context.Background(), testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("marked %d, want 1", n)
	}
}

// 超时提醒按负责人合并：未认领的不提醒。
func TestOverdueByAssignee(t *testing.T) {
	got := overdueByAssignee([]Ticket{
		{AssigneeID: 7}, {AssigneeID: 7}, {AssigneeID: 8}, {AssigneeID: 0},
	})
	if got[7] != 2 || got[8] != 1 || len(got) != 2 {
		t.Fatalf("unexpected grouping: %+v", got)
	}
}

// 新单提醒：命中租户内持 ticket:list 的坐席，创建人自己排除。
func TestNewTicketNotifiesAgents(t *testing.T) {
	db, m := mockDB(t)
	sender := &fakeSender{}
	s := NewService(db, nopLogger{}).WithNotify(sender, "zh-CN")
	ctx := tenant.With(context.Background(), 1)

	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_ticket`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("INSERT INTO `plg_ticket_reply`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectQuery("DISTINCT u.id AS user_id").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "tenant_id"}).AddRow(7, 1).AddRow(8, 1))

	_, err := s.Create(ctx, CreateInput{
		Title: "new ticket", Content: "help", ReporterType: ReplierReporter, ReporterName: "visitor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sender.count() != 1 || len(sender.msgs[0].To) != 2 {
		t.Fatalf("agents should be notified once, got %+v", sender.msgs)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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

// 坐席公开回复：seq 递增、进入 pending（等客户）、补首次响应时间与默认认领。
func TestAgentReplyTransitions(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusOpen, 1))
	m.ExpectExec("INSERT INTO `plg_ticket_reply`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("UPDATE `plg_ticket`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE id = \\?").
		WillReturnRows(ticketRow("TK1001", StatusPending, 2))
	m.ExpectCommit()

	reply, tk, err := s.Reply(context.Background(), "TK1001", ReplyInput{
		ReplierType: ReplierAgent, ReplierID: "7", ReplierName: "alice", Content: "working on it",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Seq != 2 || reply.Internal {
		t.Fatalf("unexpected reply %+v", reply)
	}
	if tk.Status != StatusPending {
		t.Fatalf("status = %s, want pending", tk.Status)
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
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE id = \\?").
		WillReturnRows(ticketRow("TK1001", StatusResolved, 2))
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

	_, list, err := s.Replies(context.Background(), "TK1001", 1, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Seq != 2 {
		t.Fatalf("unexpected replies %+v", list)
	}
}

// 访客视角必须过滤内部备注：查询要带 is_internal = false。
func TestVisitorRepliesExcludeInternal(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusProcessing, 2))
	rows := sqlmock.NewRows([]string{
		"id", "ticket_id", "seq", "replier_type", "replier_id", "replier_name", "content", "is_internal", "created_at", "updated_at",
	}).AddRow(2001, 1001, 2, ReplierAgent, "7", "alice", "public", false, testNow, testNow)
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_reply` WHERE ticket_id = \\? AND is_internal = \\?").
		WithArgs(int64(1001), false, 50).
		WillReturnRows(rows)

	_, list, err := s.Replies(context.Background(), "TK1001", 0, 50, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Internal {
		t.Fatalf("unexpected replies %+v", list)
	}
}

// 内部备注只有坐席能写，且不改状态机（本用例在参数校验阶段就返回 400）。
func TestInternalNoteRequiresAgent(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})
	_, _, err := s.Reply(context.Background(), "TK1001", ReplyInput{
		ReplierType: ReplierReporter, Content: "note", Internal: true,
	})
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
}

// 满意度：只在已解决/已关闭后、且只能评一次。
func TestRateTicket(t *testing.T) {
	// 成功
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRowWithRating("TK1001", StatusResolved, 2, nil))
	m.ExpectExec("UPDATE `plg_ticket`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE id = \\?").
		WillReturnRows(ticketRowWithRating("TK1001", StatusResolved, 2, 5))
	m.ExpectCommit()
	tk, err := s.Rate(context.Background(), "TK1001", RateInput{Score: 5, Comment: " great "})
	if err != nil {
		t.Fatal(err)
	}
	if tk.Rating == nil || *tk.Rating != 5 {
		t.Fatalf("unexpected rating: %+v", tk)
	}

	// 分数越界：不碰数据库
	if _, err := s.Rate(context.Background(), "TK1001", RateInput{Score: 0}); err == nil {
		t.Fatal("score 0 accepted")
	}

	// 未解决：409
	db2, m2 := mockDB(t)
	s2 := NewService(db2, nopLogger{})
	m2.ExpectBegin()
	m2.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRow("TK1001", StatusProcessing, 2))
	m2.ExpectRollback()
	if _, err := s2.Rate(context.Background(), "TK1001", RateInput{Score: 5}); err == nil {
		t.Fatal("unresolved ticket accepted a rating")
	}

	// 已评价：409
	db3, m3 := mockDB(t)
	s3 := NewService(db3, nopLogger{})
	m3.ExpectBegin()
	m3.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRowWithRating("TK1001", StatusClosed, 2, 4))
	m3.ExpectRollback()
	if _, err := s3.Rate(context.Background(), "TK1001", RateInput{Score: 5}); err == nil {
		t.Fatal("ticket accepted a second rating")
	}
}

// 批量操作的入参边界：空列表与超过 100 张都在碰数据库前拒绝。
func TestBulkRejectsBadInput(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})
	if _, err := s.Bulk(context.Background(), BulkInput{Action: "status", Status: StatusClosed}, 0, "alice"); err == nil {
		t.Fatal("empty ticket list accepted")
	}
	nos := make([]string, bulkLimit+1)
	for i := range nos {
		nos[i] = "TK" + strconv.Itoa(i)
	}
	if _, err := s.Bulk(context.Background(), BulkInput{TicketNos: nos, Action: "status", Status: StatusClosed}, 0, "alice"); err == nil {
		t.Fatal("oversized ticket list accepted")
	}
}

// 快捷回复校验：标题/内容必填，超长拒绝。
func TestCannedInputNormalize(t *testing.T) {
	var in CannedInput
	if err := in.normalize(); err == nil {
		t.Fatal("empty canned reply accepted")
	}
	in = CannedInput{Title: "退款话术", Content: "已为您提交退款"}
	if err := in.normalize(); err != nil {
		t.Fatalf("valid canned reply rejected: %v", err)
	}
	long := make([]byte, 2001)
	for i := range long {
		long[i] = 'a'
	}
	in = CannedInput{Title: "x", Content: string(long)}
	if err := in.normalize(); err == nil {
		t.Fatal("oversized canned reply accepted")
	}
}
