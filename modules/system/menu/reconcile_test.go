package menu

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// 老库里 LDAP 配置、单点登录应用、地图服务还是侧栏可见的独立菜单：启动时把它们隐藏。
// 只改 hidden — 菜单行保留（权限点与角色授权仍引用），也不改父节点（角色分配会自动带上祖先节点）。
func TestReconcileHidesSettingsSectionMenus(t *testing.T) {
	db, mock := mockDB(t)
	db = db.WithContext(context.Background())
	mock.ExpectBegin()
	mock.ExpectQuery("FROM sys_menu WHERE code = .*type = 'dir'").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("FROM sys_menu WHERE code = .*type = 'dir'").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM sys_menu WHERE code IN`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectExec(`UPDATE sys_menu SET hidden = 1 WHERE code IN`).
		WithArgs("ldap:list", "sso:list", "maps:config").
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	checked, fixed, err := reconcileMenuShape(db)
	if err != nil {
		t.Fatal(err)
	}
	if checked != 5 || fixed != 3 {
		t.Fatalf("对账结果应为 检查 5 项 / 修正 3 项，实际 %d / %d", checked, fixed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 已经隐藏过时只读不写：每次启动只多一次 COUNT 查询。
func TestReconcileSettingsSectionMenusConvergedIsNoop(t *testing.T) {
	db, mock := mockDB(t)
	db = db.WithContext(context.Background())
	mock.ExpectBegin()
	mock.ExpectQuery("FROM sys_menu WHERE code = .*type = 'dir'").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("FROM sys_menu WHERE code = .*type = 'dir'").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM sys_menu WHERE code IN`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectCommit()

	checked, fixed, err := reconcileMenuShape(db)
	if err != nil {
		t.Fatal(err)
	}
	if checked != 5 || fixed != 0 {
		t.Fatalf("对账结果应为 检查 5 项 / 修正 0 项，实际 %d / %d", checked, fixed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
