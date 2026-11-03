package role

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 批量删除里被删角色的 id 与单条删除一致，护栏语句也必须一字不差：
// count 查询少了 `u.deleted_at IS NULL` 就是把「已注销用户仍占用」误判成可用，
// 反过来少了 ur.role_id 条件则会把别人的角色一起放行。
func expectDeleteOne(mock sqlmock.Sqlmock, id int64, builtin bool, holders int) {
	mock.ExpectQuery("SELECT \\* FROM `sys_role` WHERE id = \\?").
		WithArgs(id, 1). // 第二个参数是 GORM First 的 LIMIT 绑定值
		WillReturnRows(sqlmock.NewRows([]string{"id", "builtin"}).AddRow(id, builtin))
	mock.ExpectQuery(`SELECT count\(\*\) FROM sys_user_role AS ur ` +
		`JOIN sys_user u ON u\.id = ur\.user_id AND u\.deleted_at IS NULL ` +
		`WHERE ur\.role_id = \?`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(holders))
}

func expectDeleteWrites(mock sqlmock.Sqlmock, id int64) {
	mock.ExpectExec("DELETE FROM `sys_role` WHERE id = \\?").
		WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
	for _, table := range []string{"sys_role_menu", "sys_role_dept", "sys_user_role"} {
		mock.ExpectExec("DELETE FROM " + table + " WHERE role_id = \\?").
			WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 0))
	}
}

// 全部可删时：一个事务内逐条走单条删除的判断，本体与三张关联表都清干净，返回实际条数。
func TestDeleteManyAppliesSingleDeleteGuards(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectBegin()
	for _, id := range []int64{9, 10} {
		expectDeleteOne(mock, id, false, 0)
		expectDeleteWrites(mock, id)
	}
	mock.ExpectCommit()

	n, err := s.deleteMany(tenant.With(context.Background(), 3), []idgen.ID{9, 10})
	if err != nil {
		t.Fatalf("deleteMany: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 第二条是内置角色：整批回滚（Rollback，第一条的 DELETE 也一起作废），不返回「删了 1 条」的部分成功。
func TestDeleteManyRollsBackWholeBatchOnBuiltinRole(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectBegin()
	expectDeleteOne(mock, 9, false, 0)
	expectDeleteWrites(mock, 9)
	mock.ExpectQuery("SELECT \\* FROM `sys_role` WHERE id = \\?").
		WithArgs(int64(10), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "builtin"}).AddRow(10, true))
	mock.ExpectRollback()

	n, err := s.deleteMany(tenant.With(context.Background(), 3), []idgen.ID{9, 10})
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusBadRequest || re.Key != "role.builtin_delete_forbidden" {
		t.Fatalf("期望 400 role.builtin_delete_forbidden，得到 %#v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0（整批回滚）", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 第二条仍被用户占用：同样是整批回滚 + 409，错误里带上占用人数。
func TestDeleteManyRollsBackWholeBatchWhenRoleInUse(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectBegin()
	expectDeleteOne(mock, 9, false, 0)
	expectDeleteWrites(mock, 9)
	expectDeleteOne(mock, 10, false, 4) // 护栏判定命中后不再预期任何写语句
	mock.ExpectRollback()

	n, err := s.deleteMany(tenant.With(context.Background(), 3), []idgen.ID{9, 10})
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusConflict || re.Key != "role.in_use" ||
		len(re.Args) != 1 || re.Args[0] != int64(4) {
		t.Fatalf("期望 409 role.in_use(4)，得到 %#v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0（整批回滚）", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
