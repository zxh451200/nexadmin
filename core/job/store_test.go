package job

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

var candCols = []string{"id", "tenant_id", "owner_id", "type", "status"}

// 乐观抢占：UPDATE ... WHERE id = ? AND status = 'queued' 影响 0 行说明被别的实例抢走，换下一个候选；影响 1 行才算抢到。
func TestGormClaimOptimistic(t *testing.T) {
	db, mock := mockDB(t)
	s := NewGormStore(db)
	ctx := context.Background()
	now := time.Now()

	mock.ExpectQuery("FROM `sys_job` WHERE").WillReturnRows(sqlmock.NewRows(candCols).AddRow(1, 1, 5, "t", "queued"))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_job` SET .* WHERE id = \\? AND status = \\?").WillReturnResult(sqlmock.NewResult(0, 0)) // 输了
	mock.ExpectCommit()
	mock.ExpectQuery("FROM `sys_job` WHERE").WillReturnRows(sqlmock.NewRows(candCols).AddRow(2, 1, 5, "t", "queued"))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_job` SET .* WHERE id = \\? AND status = \\?").WillReturnResult(sqlmock.NewResult(0, 1)) // 赢了
	mock.ExpectCommit()

	j, err := s.Claim(ctx, []string{"t"}, "n1", now)
	if err != nil {
		t.Fatal(err)
	}
	if j == nil || int64(j.ID) != 2 || j.Status != StatusRunning || j.Node != "n1" {
		t.Fatalf("claimed = %+v", j)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 队列为空是正常情况：Claim 返回 nil, nil，且不应产生 ErrRecordNotFound 错误日志。
func TestGormClaimEmptyQueue(t *testing.T) {
	db, mock := mockDB(t)
	s := NewGormStore(db)

	mock.ExpectQuery("FROM `sys_job` WHERE").WillReturnRows(sqlmock.NewRows(candCols))
	j, err := s.Claim(context.Background(), []string{"t"}, "n1", time.Now())
	if err != nil || j != nil {
		t.Fatalf("job=%v err=%v", j, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 结束落库同样带旧状态条件：被取消的任务 Finish 返回 false。
func TestGormFinishGuardedByRunning(t *testing.T) {
	db, mock := mockDB(t)
	s := NewGormStore(db)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_job` SET .* WHERE id = \\? AND status = \\?").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	ok, err := s.Finish(context.Background(), 1, StatusSuccess, ResultFile{Key: "k"}, "", time.Now())
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

// 队列指标：一条 GROUP BY，缺失的状态补 0。
func TestGormCountByStatus(t *testing.T) {
	db, mock := mockDB(t)
	s := NewGormStore(db)
	mock.ExpectQuery("FROM `sys_job` WHERE status IN .* GROUP BY").
		WillReturnRows(sqlmock.NewRows([]string{"status", "n"}).AddRow("queued", 3))
	got, err := s.CountByStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got[StatusQueued] != 3 || got[StatusRunning] != 0 || len(got) != 2 {
		t.Fatalf("counts = %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

type countStore struct {
	Store
	m   map[string]int64
	err error
}

func (c countStore) CountByStatus(context.Context) (map[string]int64, error) { return c.m, c.err }

func TestManagerQueueLengths(t *testing.T) {
	m := New(Options{Store: countStore{m: map[string]int64{"queued": 4, "running": 1}}})
	if got := m.QueueLengths(); got["queued"] != 4 || got["running"] != 1 {
		t.Fatalf("got %v", got)
	}
	m = New(Options{Store: countStore{err: context.DeadlineExceeded}})
	if got := m.QueueLengths(); len(got) != 0 {
		t.Fatalf("error should yield empty map, got %v", got)
	}
	m = New(Options{Store: struct{ Store }{}}) // 不支持统计的 Store
	if got := m.QueueLengths(); len(got) != 0 {
		t.Fatalf("unsupported store should yield empty map, got %v", got)
	}
}
