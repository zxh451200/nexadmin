package seed

import (
	"context"
	"testing"

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

var items = []MenuItem{
	{ID: 11, Parent: "/system", Code: "dir:/demo", Type: "dir", Title: "demo.dir", Path: "/demo", Sort: 1},
	{ID: 12, Parent: "dir:/demo", Code: "demo:list", Type: "menu", Title: "demo.menu", Path: "/demo/list", Perm: "demo:list"},
	{ID: 13, Parent: "demo:list", Code: "demo:edit", Type: "button", Title: "common.edit", Perm: "demo:edit"},
}

// 已齐全：只有一次查询，没有事务、没有写入（sqlmock 对任何未预期的语句都会报错）。
func TestMenusAllPresentIsOneQueryZeroWrites(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery("SELECT `id`,`code` FROM `sys_menu` WHERE code IN").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code"}).
			AddRow(1, "dir:/system").AddRow(11, "dir:/demo").AddRow(12, "demo:list").AddRow(13, "demo:edit"))

	rep := &Report{}
	n, err := Menus(db.WithContext(WithReport(context.Background(), rep)), items)
	if err != nil || n != 0 || rep.Menus() != 0 {
		t.Fatalf("已齐全应新增 0 个: n=%d err=%v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 缺失的补上：父节点按业务键解析（目录写路径或 dir:路径、按钮写菜单权限码），已存在的不动。
func TestMenusInsertsOnlyMissingAndResolvesParents(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery("SELECT `id`,`code` FROM `sys_menu` WHERE code IN").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code"}).AddRow(1, "dir:/system").AddRow(11, "dir:/demo"))
	mock.ExpectBegin()
	// 两条 INSERT，没有对 dir:/demo 的 INSERT / UPDATE
	mock.ExpectExec("INSERT INTO `sys_menu`").WillReturnResult(sqlmock.NewResult(12, 1))
	mock.ExpectExec("INSERT INTO `sys_menu`").WillReturnResult(sqlmock.NewResult(13, 1))
	mock.ExpectCommit()

	rep := &Report{}
	n, err := Menus(db.WithContext(WithReport(context.Background(), rep)), items)
	if err != nil || n != 2 || rep.Menus() != 2 {
		t.Fatalf("应补上 2 个: n=%d err=%v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 父节点被客户删了：连同子孙跳过并记录，不报错也不乱挂到顶级。
func TestMenusSkipsWhenParentMissing(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery("SELECT `id`,`code` FROM `sys_menu` WHERE code IN").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code"})) // 连 /system 都没有
	mock.ExpectBegin()
	mock.ExpectCommit()

	rep := &Report{}
	n, err := Menus(db.WithContext(WithReport(context.Background(), rep)), items[1:])
	if err != nil || n != 0 {
		t.Fatalf("父节点缺失应跳过而不是失败: n=%d err=%v", n, err)
	}
	if got := rep.Skipped(); len(got) != 2 || got[0] != "demo:list" || got[1] != "demo:edit" {
		t.Errorf("应记录被跳过的菜单: %v", got)
	}
}
