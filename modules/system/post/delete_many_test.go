package post

import (
	"errors"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 批量版必须逐条复用单条删除的护栏：先按租户把岗位读出来（get），再数「未删除的用户」有没有在用它。
// count 查询少了 `u.deleted_at IS NULL`，岗位就会在被真实用户占着的时候被批量删走。
func expectDeleteOne(mock sqlmock.Sqlmock, id int64, assigned int) {
	expectGet(mock, id, true)
	mock.ExpectQuery(`SELECT count\(\*\) FROM sys_user_post AS up ` +
		`JOIN sys_user u ON u\.id = up\.user_id AND u\.deleted_at IS NULL ` +
		`WHERE up\.post_id = \?`).
		WithArgs(id).WillReturnRows(count(assigned))
}

func TestDeleteManyAppliesSingleDeleteGuards(t *testing.T) {
	s, mock := setup(t)
	mock.ExpectBegin()
	for _, id := range []int64{8, 9} {
		expectDeleteOne(mock, id, 0)
		mock.ExpectExec("DELETE FROM `sys_post` WHERE id = \\?").
			WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()

	n, err := s.DeleteMany(ctx, []idgen.ID{8, 9})
	if err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 第二条仍在被使用：整批回滚（Rollback，第一条的 DELETE 一起作废），不返回「删了 1 条」的部分成功。
func TestDeleteManyRollsBackWholeBatchWhenPostInUse(t *testing.T) {
	s, mock := setup(t)
	mock.ExpectBegin()
	expectDeleteOne(mock, 8, 0)
	mock.ExpectExec("DELETE FROM `sys_post` WHERE id = \\?").
		WithArgs(int64(8)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectDeleteOne(mock, 9, 3) // 判定命中后不再预期任何写语句
	mock.ExpectRollback()

	n, err := s.DeleteMany(ctx, []idgen.ID{8, 9})
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusConflict || re.Key != "post.in_use" ||
		len(re.Args) != 1 || re.Args[0] != int64(3) {
		t.Fatalf("期望 409 post.in_use(3)，得到 %#v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0（整批回滚）", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 批次里有不存在的岗位（含跨租户的 id）：404 且整批不写，不能静默跳过。
func TestDeleteManyRejectsUnknownPost(t *testing.T) {
	s, mock := setup(t)
	mock.ExpectBegin()
	expectGet(mock, 8, true)
	mock.ExpectQuery(`SELECT count\(\*\) FROM sys_user_post`).WithArgs(int64(8)).WillReturnRows(count(0))
	mock.ExpectExec("DELETE FROM `sys_post` WHERE id = \\?").
		WithArgs(int64(8)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectGet(mock, 9, false)
	mock.ExpectRollback()

	n, err := s.DeleteMany(ctx, []idgen.ID{8, 9})
	if statusOf(err) != http.StatusNotFound {
		t.Fatalf("期望 404，得到 %v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0（整批回滚）", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
