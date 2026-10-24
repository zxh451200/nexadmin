package sendlog

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMenuSeedShape(t *testing.T) {
	items := menuSeed()
	codes := map[string]bool{}
	for _, it := range items {
		if it.Code == "" || codes[it.Code] {
			t.Fatalf("menu codes must be unique and non-empty: %+v", it)
		}
		codes[it.Code] = true
	}
	menu, btn := items[0], items[1]
	if menu.Parent != "/monitor" || menu.Type != "menu" || menu.Perm != PermList || menu.Code != PermList || menu.Path != "/monitor/send-log" {
		t.Fatalf("menu = %+v", menu)
	}
	if btn.Parent != PermList || btn.Type != "button" || btn.Perm != PermDelete {
		t.Fatalf("button = %+v", btn)
	}
}

// 出厂菜单已齐全：每次启动只有一次查询，零写入。
func TestSeedOnStartupAllPresentIsZeroWrites(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("FROM `sys_menu` WHERE code IN").WillReturnRows(
		sqlmock.NewRows([]string{"id", "code"}).AddRow(1, "dir:/monitor").AddRow(2, PermList).AddRow(3, PermDelete))
	if err := (module{}).SeedOnStartup(db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
