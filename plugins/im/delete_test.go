package im

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
	"nexadmin/core/storage"
)

// stubStore 只记录被删除的对象键，其余方法返回 ErrNotSupported。
type stubStore struct {
	deleted []string
}

func (s *stubStore) Put(context.Context, string, io.Reader, int64, storage.PutOptions) error {
	return storage.ErrNotSupported
}

func (s *stubStore) PutStream(context.Context, string, io.Reader, storage.PutOptions) error {
	return storage.ErrNotSupported
}

func (s *stubStore) Get(context.Context, string, *storage.Range) (*storage.Object, error) {
	return nil, storage.ErrNotSupported
}

func (s *stubStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

func (s *stubStore) Exists(context.Context, string) (bool, error) { return false, nil }

func (s *stubStore) Stat(context.Context, string) (*storage.ObjectInfo, error) {
	return nil, storage.ErrNotSupported
}

func (s *stubStore) PresignGet(context.Context, string, time.Duration, storage.PresignOptions) (string, error) {
	return "", storage.ErrNotSupported
}

func statusOf(err error) int {
	if e, ok := err.(*response.Error); ok {
		return e.Status
	}
	if err != nil {
		return -1
	}
	return 0
}

// 进行中的会话不能删：访客可能正在聊，直接抹掉会让对面突然断流。
func TestDeleteConversationRejectsOpen(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(convRow("IM1001", StatusOpen, 2, 2))
	m.ExpectRollback()

	_, err := s.DeleteConversation(context.Background(), "IM1001")
	if got := statusOf(err); got != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%v)", got, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 删除已结束会话：消息与附件行在事务里级联删除，存储对象在事务外删。
func TestDeleteConversationCascades(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	store := &stubStore{}
	s.SetStorage(store)

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(convRow("IM1001", StatusClosed, 4, 4))
	m.ExpectQuery("SELECT `storage_key` FROM `plg_im_attachment` WHERE conv_id = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"storage_key"}).AddRow("1/202610/a.png").AddRow("1/202610/b.pdf"))
	m.ExpectExec("DELETE FROM `plg_im_message` WHERE conv_id = \\?").WillReturnResult(sqlmock.NewResult(0, 3))
	m.ExpectExec("DELETE FROM `plg_im_attachment` WHERE conv_id = \\?").WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectExec("DELETE FROM `plg_im_conversation` WHERE id = \\?").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	res, err := s.DeleteConversation(context.Background(), "IM1001")
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 || res.Messages != 3 {
		t.Fatalf("result = %+v", res)
	}
	if len(store.deleted) != 2 || store.deleted[0] != "1/202610/a.png" {
		t.Fatalf("store deleted = %v", store.deleted)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 保留期清理只挑「已结束且结束时间早于 cutoff」的会话。
func TestPurgeConversationsByCutoff(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	cutoff := testNow.AddDate(0, 0, -30)

	m.ExpectQuery("SELECT `conv_no` FROM `plg_im_conversation` WHERE status = \\? AND closed_at IS NOT NULL AND closed_at < \\?").
		WithArgs(StatusClosed, cutoff, 10).
		WillReturnRows(sqlmock.NewRows([]string{"conv_no"}).AddRow("IM9001"))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(convRow("IM9001", StatusClosed, 1, 1))
	m.ExpectQuery("SELECT `storage_key` FROM `plg_im_attachment` WHERE conv_id = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"storage_key"}))
	m.ExpectExec("DELETE FROM `plg_im_message` WHERE conv_id = \\?").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("DELETE FROM `plg_im_attachment` WHERE conv_id = \\?").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec("DELETE FROM `plg_im_conversation` WHERE id = \\?").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	scanned, deleted, err := s.PurgeConversations(context.Background(), cutoff, 10)
	if err != nil {
		t.Fatal(err)
	}
	if scanned != 1 || deleted != 1 {
		t.Fatalf("scanned=%d deleted=%d", scanned, deleted)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 批量删除：已结束的删掉，进行中的进 skipped，不因一条失败中止整批。
func TestDeleteConversationsSkipsOpen(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(convRow("IM1001", StatusOpen, 1, 1))
	m.ExpectRollback()
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(convRow("IM1002", StatusClosed, 1, 1))
	m.ExpectQuery("SELECT `storage_key` FROM `plg_im_attachment` WHERE conv_id = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"storage_key"}))
	m.ExpectExec("DELETE FROM `plg_im_message` WHERE conv_id = \\?").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("DELETE FROM `plg_im_attachment` WHERE conv_id = \\?").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec("DELETE FROM `plg_im_conversation` WHERE id = \\?").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	res, err := s.DeleteConversations(context.Background(), []string{"IM1001", "IM1002"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 || len(res.Skipped) != 1 || res.Skipped[0] != "IM1001" {
		t.Fatalf("result = %+v", res)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 内部备注走 UPDATE + 回读；空串表示清空。
func TestSetNote(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_im_conversation` SET .*note.*WHERE conv_no = \\?").
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectQuery("SELECT \\* FROM `plg_im_conversation` WHERE conv_no = \\? LIMIT \\?").
		WillReturnRows(convRow("IM1001", StatusOpen, 1, 1))

	c, err := s.SetNote(context.Background(), "IM1001", "  已电话回访  ")
	if err != nil {
		t.Fatal(err)
	}
	if c.ConvNo != "IM1001" {
		t.Fatalf("conv = %+v", c)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 欢迎语落成一条客服消息，并把会话摘要更新为欢迎语。
func TestSendWelcome(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_im_setting` WHERE k = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "k", "v", "created_at", "updated_at"}).
			AddRow(1, settingKeyGeneral, `{"welcomeText":"你好，请描述你的问题"}`, testNow, testNow))
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_im_message`").WillReturnResult(sqlmock.NewResult(2, 1))
	m.ExpectExec("UPDATE `plg_im_conversation` SET .*last_preview.*WHERE id = \\?").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	c := &Conversation{ConvNo: "IM1001"}
	c.ID = 1001
	s.sendWelcome(context.Background(), c)

	if c.MsgSeq != 1 || c.LastPreview != "你好，请描述你的问题" {
		t.Fatalf("welcome not applied: %+v", c)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 保留天数超出范围直接拒绝，不落库。
func TestSaveSettingsRejectsRetentionOutOfRange(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})
	for _, days := range []int{-1, 3651} {
		_, err := s.SaveSettings(context.Background(), Settings{RetentionDays: days})
		if got := statusOf(err); got != http.StatusBadRequest {
			t.Fatalf("retentionDays=%d status=%d, want 400 (%v)", days, got, err)
		}
	}
}
