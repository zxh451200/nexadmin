package im

import (
	"context"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/config"
)

// sqlmockMessageRows 造一条消息查询结果（列顺序与 SELECT * 的模型字段一致）。
func sqlmockMessageRows(id, convID, seq int64, senderType, senderID string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "conv_id", "seq", "sender_type", "sender_id", "sender_name", "content", "kind", "meta", "recalled_at", "created_at", "updated_at",
	}).AddRow(id, convID, seq, senderType, senderID, "n", "hi", KindText, "", nil, testNow, testNow)
}

// 心跳写入后算在线，没心跳的坐席是离线；缓存缺失时全部视为在线（单实例无需心跳）。
func TestPresenceHeartbeat(t *testing.T) {
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})
	s.SetCache(c)
	ctx := context.Background()
	s.Heartbeat(ctx, "1")
	got := s.onlineMap(ctx, []string{"1", "2"})
	if !got["1"] || got["2"] {
		t.Fatalf("online = %v", got)
	}
	s.SetCache(nil)
	got = s.onlineMap(ctx, []string{"2"})
	if !got["2"] {
		t.Fatal("without cache every agent is treated as online")
	}
}

// 没开自动分配时心跳不领会话：只读一次设置，不应产生会话查询。
func TestAssignNextDisabled(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery("SELECT \\* FROM `plg_im_setting` WHERE k = \\? LIMIT \\?").
		WillReturnError(gorm.ErrRecordNotFound)

	no, err := s.AssignNext(context.Background(), "7", "agent")
	if err != nil || no != "" {
		t.Fatalf("assignNext = %q, %v", no, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 只能撤回坐席自己的消息：访客消息直接拒绝。
func TestRecallRejectsVisitorMessage(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\? LIMIT \\?").
		WillReturnRows(convRow("IM1001", StatusOpen, 5, 5))
	m.ExpectQuery("SELECT \\* FROM `plg_im_message` WHERE conv_id = \\? AND seq = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(sqlmockMessageRows(2001, 1001, 3, SenderVisitor, "tok"))
	m.ExpectRollback()

	_, err := s.Recall(context.Background(), "IM1001", 3, "7")
	if got := statusOf(err); got != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%v)", got, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 超出 2 分钟窗口的本人消息也不能撤回。
func TestRecallRejectsExpired(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\? LIMIT \\?").
		WillReturnRows(convRow("IM1001", StatusOpen, 5, 5))
	m.ExpectQuery("SELECT \\* FROM `plg_im_message` WHERE conv_id = \\? AND seq = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(sqlmockMessageRows(2001, 1001, 3, SenderAgent, "7"))
	m.ExpectRollback()

	_, err := s.Recall(context.Background(), "IM1001", 3, "7")
	if got := statusOf(err); got != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%v)", got, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 目标会话已结束时不允许合并。
func TestMergeRejectsClosedTarget(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(convRow("IM1001", StatusOpen, 2, 2))
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(convRow("IM1002", StatusClosed, 1, 1))
	m.ExpectRollback()

	_, err := s.Merge(context.Background(), "IM1001", "IM1002", "7")
	if got := statusOf(err); got != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%v)", got, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 合并到自己直接拒绝，不碰库。
func TestMergeRejectsSelf(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})
	if _, err := s.Merge(context.Background(), "IM1001", "IM1001", "7"); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
}

// 已结束的会话再次被打开时复用同一行重新开启：清掉评分/归属并追加系统提示。
func TestOpenReopensClosedConversation(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE biz_type = \\? AND biz_no = \\? AND visitor_token = \\? LIMIT \\?").
		WillReturnRows(convRow("IM1001", StatusClosed, 3, 3))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE id = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(convRow("IM1001", StatusClosed, 3, 3))
	m.ExpectExec("INSERT INTO `plg_im_message`").WillReturnResult(sqlmock.NewResult(2, 1))
	m.ExpectExec("UPDATE `plg_im_conversation` SET .*status.*WHERE id = \\?").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectQuery("SELECT \\* FROM `plg_im_setting` WHERE k = \\? LIMIT \\?").WillReturnError(gorm.ErrRecordNotFound)

	c, err := s.Open(context.Background(), OpenInput{BizType: "trade_group", BizNo: "G-1", VisitorToken: "tok-1"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusOpen || c.MsgSeq != 4 {
		t.Fatalf("reopened = %+v", c)
	}
	if c.Rating != 0 || c.AssigneeID != "" {
		t.Fatalf("reopen must clear rating/assignee: %+v", c)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
