package workflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// mockDefsDB 用 sqlmock 造一个只认语句形状的定义库（不执行 SQL）。
// 租户隔离由 core/database 的 tenantPlugin 在 DELETE 前追加 tenant_id 条件：裸 gorm 的
// sqlmock 里注册不上该插件，这里钉住 id 条件与「只碰 plg_workflow_def」，插件本身由 core/database 的用例保证。
func mockDefsDB(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true, NowFunc: func() time.Time { return time.Unix(0, 0).UTC() }})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db, nopLogger{}), mock
}

// defDeleteSQL 是单条与批量共用的那条 DELETE（store_gorm.go deleteDef）。
func defDeleteSQL() string {
	return "DELETE FROM `plg_workflow_def` WHERE id = \\?"
}

// 批量删必须只动点名的那几条：逐条 DELETE ... WHERE id = ?，一条语句一个 id。
// 去掉 id 条件（改成无条件删除）语句就匹配不上，当场红；
// 顺手给 plg_workflow_inst / plg_workflow_def_version / plg_workflow_task 加级联删除，
// 会在有序模式里冒出没有预期的语句（ sqlmock 报错 → 整批失败）——与单条删除一致是硬要求。
func TestDefDeleteManyOnlyRequestedIDs(t *testing.T) {
	s, mock := mockDefsDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(defDeleteSQL()).WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(defDeleteSQL()).WithArgs(int64(12)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := s.DefDeleteMany(tctx(1), []idgen.ID{11, 12})
	if err != nil {
		t.Fatalf("DefDeleteMany: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 越权 / 已不存在的 id 删不到，也就不计入实删数：三条里只命中两条（一条 0 行），返回 2。
// 按「点名的条数」而不是 RowsAffected 累加（deleted++ / deleted = len(ids)）这里就红。
func TestDefDeleteManyCountsOnlyRealDeletes(t *testing.T) {
	s, mock := mockDefsDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(defDeleteSQL()).WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(defDeleteSQL()).WithArgs(int64(12)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(defDeleteSQL()).WithArgs(int64(13)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := s.DefDeleteMany(tctx(1), []idgen.ID{11, 12, 13})
	if err != nil {
		t.Fatalf("DefDeleteMany: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 任一条失败 = 整批回滚：期望里只有 Rollback 没有 Commit，
// 改成「删掉能删的、继续往下走」这里就红，不会留下删了一半的定义。
func TestDefDeleteManyRollsBackWholeBatchOnFailure(t *testing.T) {
	s, mock := mockDefsDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(defDeleteSQL()).WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(defDeleteSQL()).WithArgs(int64(12)).WillReturnError(errors.New("boom"))
	mock.ExpectRollback()

	n, err := s.DefDeleteMany(tctx(1), []idgen.ID{11, 12})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0（整批回滚后不能有残留计数）", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 0 命中报 not found，不能静默返回 deleted=0 让界面以为删成功了。
// 语句本身都执行成功（各 0 行），所以事务照常提交：not found 是批量结束后判的。
func TestDefDeleteManyNotFoundWhenNothingMatches(t *testing.T) {
	s, mock := mockDefsDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(defDeleteSQL()).WithArgs(int64(99)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(defDeleteSQL()).WithArgs(int64(100)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if _, err := s.DefDeleteMany(tctx(1), []idgen.ID{99, 100}); errStatus(err) != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ?ids= 缺失、整体为空或任一项非法一律 400，而且在任何数据库访问之前拒绝：
// 这里一条 sqlmock 预期都没挂，实现只要碰库（Begin/Exec）就会报错，
// 于是「不带 ids 就删光所有定义」这种退化当场红。svc 用真实 mock 服务而不是 nil，
// 正是为了让「有没有下过 SQL」可证。
func TestDefDeleteManyHandlerRejectsMissingOrBadIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0", "?ids=,"} {
		s, mock := mockDefsDB(t)
		e := gin.New()
		e.DELETE("/plugins/workflow/defs", (&handler{svc: s}).defDeleteMany)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/workflow/defs"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code=%d want 400 body=%s", q, w.Code, w.Body.String())
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("query %q: 校验前就下了 SQL: %v", q, err)
		}
	}
}

// 成功回执是 {"deleted":n}，与 modules/message、plugins/cron 的批量删同形状。
func TestDefDeleteManyHandlerReturnsDeletedCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, mock := mockDefsDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(defDeleteSQL()).WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	e := gin.New()
	e.DELETE("/plugins/workflow/defs", (&handler{svc: s}).defDeleteMany)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/workflow/defs?ids=11", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d want 200 body=%s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, `"deleted":1`) {
		t.Errorf("body = %s, want deleted:1", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 没装配库时报内部错误，不是「删了 0 条」也不是放行。
func TestDefDeleteManyWithoutDB(t *testing.T) {
	s := NewService(nil, nopLogger{})
	if _, err := s.DefDeleteMany(context.Background(), []idgen.ID{11}); errStatus(err) != http.StatusInternalServerError {
		t.Fatalf("err = %v, want 500", err)
	}
}

func errStatus(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}
