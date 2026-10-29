package menu

import (
	"strings"
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

// rowsExcept 模拟库里已有的出厂菜单：除 skip 之外都在。
func rowsExcept(skip func(code string) bool) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "code"})
	for _, it := range buildSeed() {
		if !skip(it.m.Code) {
			rows.AddRow(int64(it.m.ID), it.m.Code)
		}
	}
	return rows
}

// expectIDScan 补上 seedMenus 写库前的号段对账查询（reserveFreeIDs 会先扫一遍出厂号段）。
func expectIDScan(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("FROM `sys_menu` WHERE id >= ").WillReturnRows(
		sqlmock.NewRows([]string{"id", "code"}).AddRow(int64(factoryIDBase)+1, "dashboard:view"))
}

// expectReconcileNoop 补上 SeedOnStartup 末尾的结构对账事务（菜单历史形状收敛走 Seeder，不走迁移）。
// 两个目录都查不到 → 只读不写，正好表达「这台库里没有要收敛的形状」。
func expectReconcileNoop(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectQuery("FROM sys_menu WHERE code = .*type = 'dir'").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("FROM sys_menu WHERE code = .*type = 'dir'").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()
}

// 出厂菜单都在：每次启动只有一次查询，零写入。
func TestSeedOnStartupAllPresentIsZeroWrites(t *testing.T) {
	db, mock := mockDB(t)
	expectIDScan(mock)
	mock.ExpectQuery("FROM `sys_menu` WHERE code IN").WillReturnRows(rowsExcept(func(string) bool { return false }))
	expectReconcileNoop(mock)
	if err := (module{}).SeedOnStartup(db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 升级前的存量库没有单点登录菜单：重启补上，其余（包括被客户改过的）一律不动。
func TestSeedOnStartupBackfillsSSOMenusOnly(t *testing.T) {
	isSSO := func(code string) bool { return code == "menu:portal" || strings.HasPrefix(code, "sso:") }
	missing := 0
	for _, it := range buildSeed() {
		if isSSO(it.m.Code) {
			missing++
		}
	}
	if missing != 3 { // sso:list、sso:edit、门户（menu:portal —— 无权限码的顶级菜单必须带显式业务键）
		t.Fatalf("出厂种子里的 SSO 菜单数变了: %d", missing)
	}
	db, mock := mockDB(t)
	expectIDScan(mock)
	mock.ExpectQuery("FROM `sys_menu` WHERE code IN").WillReturnRows(rowsExcept(isSSO))
	mock.ExpectBegin()
	for i := 0; i < missing; i++ {
		mock.ExpectExec("INSERT INTO `sys_menu`").WillReturnResult(sqlmock.NewResult(1, 1))
	}
	mock.ExpectCommit()
	expectReconcileNoop(mock)
	if err := (module{}).SeedOnStartup(db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
