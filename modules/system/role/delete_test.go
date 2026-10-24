package role

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
)

func expectRole(mock sqlmock.Sqlmock, builtin bool) {
	mock.ExpectQuery("SELECT \\* FROM `sys_role` WHERE id = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "builtin"}).AddRow(9, builtin))
}

func expectRoleUsers(mock sqlmock.Sqlmock, n int) {
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur JOIN sys_user u ON u.id = ur.user_id AND u.deleted_at IS NULL WHERE ur.role_id = \\?").
		WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(n))
}

func TestRemoveRejectedWhenUsersHoldRole(t *testing.T) {
	s, mock := newSvc(t)
	expectRole(mock, false)
	mock.ExpectBegin()
	expectRoleUsers(mock, 4) // 之后不预期任何 DELETE
	mock.ExpectRollback()
	err := s.remove(tenant.With(context.Background(), 3), 9)
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusConflict || re.Key != "role.in_use" || len(re.Args) != 1 || re.Args[0] != int64(4) {
		t.Fatalf("期望 409 role.in_use(4)，得到 %#v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestRemoveUnusedRoleSucceeds(t *testing.T) {
	s, mock := newSvc(t)
	expectRole(mock, false)
	mock.ExpectBegin()
	expectRoleUsers(mock, 0)
	mock.ExpectExec("DELETE FROM `sys_role` WHERE id = \\?").WillReturnResult(sqlmock.NewResult(0, 1))
	for _, table := range []string{"sys_role_menu", "sys_role_dept", "sys_user_role"} {
		mock.ExpectExec("DELETE FROM " + table + " WHERE role_id = \\?").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectCommit()
	if err := s.remove(tenant.With(context.Background(), 3), 9); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
