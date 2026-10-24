package dept

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
)

func countRows(n int) *sqlmock.Rows { return sqlmock.NewRows([]string{"c"}).AddRow(n) }

func expectDeleteChecks(mock sqlmock.Sqlmock, users int) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `sys_dept` WHERE id = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "parent_id", "path"}).AddRow(7, nil, "/7/"))
	// 整棵子树（path 前缀）里的用户数
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user` WHERE deleted_at IS NULL AND dept_id IN \\(SELECT").WillReturnRows(countRows(users))
}

func conflictKey(t *testing.T, err error) (string, any) {
	t.Helper()
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusConflict || len(re.Args) != 1 {
		t.Fatalf("期望 409，得到 %#v", err)
	}
	return re.Key, re.Args[0]
}

func TestDeleteRejectedWhenHasUsers(t *testing.T) {
	s, mock := newSvc(t)
	expectDeleteChecks(mock, 5) // 子树里（本部门或下级）仍有 5 个用户
	mock.ExpectRollback()
	key, n := conflictKey(t, s.Delete(tenant.With(context.Background(), 3), 7))
	if key != "dept.has_users" || n != int64(5) {
		t.Fatalf("key=%s n=%v", key, n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestDeleteEmptyDeptSucceeds(t *testing.T) {
	s, mock := newSvc(t)
	expectDeleteChecks(mock, 0)
	mock.ExpectQuery("SELECT \\* FROM `sys_dept` WHERE path LIKE \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "parent_id", "path"}).AddRow(7, nil, "/7/"))
	mock.ExpectExec("DELETE FROM `sys_dept` WHERE path LIKE \\?").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := s.Delete(tenant.With(context.Background(), 3), 7); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
