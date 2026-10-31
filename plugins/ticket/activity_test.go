package ticket

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/pkg/idgen"
)

func activityRows(ids []int64, actions []string) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "ticket_id", "actor_type", "actor_name", "action", "from_value", "to_value"})
	for i := range ids {
		rows.AddRow(ids[i], 1001, ActorAgent, "alice", actions[i], "open", "pending")
	}
	return rows
}

func TestTicketActivitiesReturnsAscending(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	// 按 id DESC 取最近若干条（LIMIT 是参数），返回前端的顺序要倒回时间升序。
	m.ExpectQuery("SELECT .* FROM `plg_ticket_activity` WHERE ticket_id = \\? ORDER BY id DESC").
		WithArgs(int64(1001), int64(activityLimit)).
		WillReturnRows(activityRows([]int64{3, 2, 1}, []string{ActRatingAdded, ActStatusChanged, ActCreated}))

	acts, err := s.TicketActivities(context.Background(), idgen.ID(1001), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 3 || acts[0].Action != ActCreated || acts[2].Action != ActRatingAdded {
		t.Fatalf("acts = %+v", acts)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestTicketActivitiesPublicFilter(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery("SELECT .* FROM `plg_ticket_activity` WHERE ticket_id = \\? AND action IN").
		WithArgs([]driver.Value{int64(1001), ActCreated, ActStatusChanged, int64(activityLimit)}...).
		WillReturnRows(activityRows([]int64{2, 1}, []string{ActStatusChanged, ActCreated}))

	acts, err := s.TicketActivities(context.Background(), idgen.ID(1001), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range acts {
		if a.Action != ActCreated && a.Action != ActStatusChanged {
			t.Errorf("公开动态里出现 %q", a.Action)
		}
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 改派给同一个人不该再记一行动态：期望里只有 SELECT + UPDATE，多写的 INSERT 会让这个用例红。
func TestAssignSamePersonRecordsNothing(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no = \\?").
		WillReturnRows(ticketRowAssignee("TK1001", StatusProcessing, 2, 7, "alice"))
	m.ExpectExec("UPDATE `plg_ticket`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	if _, err := s.assign(context.Background(), "TK1001", idgen.ID(7), "alice"); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 没有工单号时不该发出任何 SQL：拿不到 id 就静默跳过，而不是写一行孤儿动态。
func TestRecordActivitySkipsZeroTicket(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	if err := s.recordActivity(context.Background(), nil, nil, actorFrom(context.Background()), ActCreated, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestActorFromContext(t *testing.T) {
	if got := actorFrom(context.Background()); got.Type != ActorSystem {
		t.Errorf("无登录态应记为系统，得到 %+v", got)
	}
	ctx := WithActor(context.Background(), ActivityActor{Type: ActorAgent, ID: "7", Name: "alice"})
	if got := actorFrom(ctx); got.Type != ActorAgent || got.Name != "alice" {
		t.Errorf("context 发起人丢失：%+v", got)
	}
	// 接口参数里的操作人名（SetStatus/Bulk 都从登录态取）在没有 context 发起人的场景下记为坐席。
	if got := agentActor(context.Background(), "alice"); got.Type != ActorAgent || got.Name != "alice" {
		t.Errorf("agentActor fallback = %+v", got)
	}
	if got := agentActor(context.Background(), ""); got.Type != ActorSystem {
		t.Errorf("无发起人且无名字应记系统，得到 %+v", got)
	}
	if got := agentActor(ctx, ""); got.Name != "alice" {
		t.Errorf("context 应优先，得到 %+v", got)
	}
}
