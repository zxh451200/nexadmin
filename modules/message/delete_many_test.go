package message

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/pkg/idgen"
)

// 带了 ids 却解析不出合法 ID 必须报错：DELETE /messages 不带 ids 的语义是「清空全部」，
// 静默退回去就是误删整箱。任一项非法也整体拒绝，不做「删掉能删的」。
func TestParseIDsRejectsAnythingThatCouldFallBackToClearAll(t *testing.T) {
	for _, raw := range []string{"", " ", "abc", "1,abc", "0", "-3"} {
		ids, err := parseIDs(raw)
		if err == nil {
			t.Errorf("parseIDs(%q) = %v, want error", raw, ids)
		}
	}
	ids, err := parseIDs("11, 12")
	if err != nil || len(ids) != 2 {
		t.Errorf(`parseIDs("11, 12") = %v, %v; want 2 ids`, ids, err)
	}
}

// 少了 user_id 就是「拿别人的消息 ID 也能删」——越权删除在列表接口上看不出来，只能在这里钉住。
func TestDeleteManyScopesToOwner(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, nil, "UTC")

	const uid = idgen.ID(91)
	// GORM 的 Delete 自带事务包裹，期望里必须一起排上
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM ` + "`sys_message`" + ` WHERE user_id = \? AND id IN \(\?,\?\)`).
		WithArgs(int64(uid), int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := svc.DeleteMany(context.Background(), uid, []idgen.ID{11, 12})
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
