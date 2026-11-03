package notice

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// DismissMany 的两条正确性：
//  1. 先过 visible()：不可见（越权）的 id 不会被插 dismiss 记录；
//  2. 已经移除过的不再插第二遍，批量 INSERT 只带真正新增的那些。
//
// 说明：这里的夹具是裸 gorm 句柄，没有 database.Open 里装的租户回调，所以本用例钉的是
// 「visible 过滤 + 幂等」这两条我自己写的逻辑，不声明租户隔离已被覆盖。
func TestDismissManyOnlyInsertsVisibleAndNotAlreadyGone(t *testing.T) {
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	// TenantModel 的 BeforeCreate 要求 ctx 带租户（与生产同一条路径），没有就直接报错
	ctx := tenant.With(context.Background(), idgen.ID(5))
	const uid = idgen.ID(7)

	// visible() 的前置：部门、角色、清空时间点
	mock.ExpectQuery(`FROM .sys_user. WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"dept_id"}).AddRow(0))
	mock.ExpectQuery(`FROM .sys_user_role. WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"role_id"}))
	mock.ExpectQuery(`FROM .sys_notice_clear. WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	// 请求 11,12,13 —— 可见的只有 11、12
	mock.ExpectQuery(`FROM .sys_notice. WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11).AddRow(12))
	// 其中 11 已经移除过
	mock.ExpectQuery(`FROM .sys_notice_dismiss. WHERE user_id = \? AND notice_id IN \(\?,\?\)`).
		WithArgs(int64(uid), int64(11), int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"notice_id"}).AddRow(11))

	// 只应插 12：VALUES 里出现两组括号就说明幂等判断失效（结尾必须是单组括号 + 可选 RETURNING）
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO .sys_notice_dismiss. .* VALUES \(\?(,\?)*\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := svc.DismissMany(ctx, uid, []idgen.ID{11, 12, 13})
	if err != nil {
		t.Fatalf("DismissMany: %v", err)
	}
	if n != 1 {
		t.Errorf("dismissed = %d, want 1（13 不可见、11 已移除）", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
