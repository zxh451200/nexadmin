package core

// 隔离配置与 sys_tenant 的一致性校验：配置里的池没有对应租户时必须拒绝启动，而不是留个连不通的死池。

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/config"
)

func platformMock(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func TestCheckTenantPools(t *testing.T) {
	db, mock := platformMock(t)
	// 7 有配置但没有租户行；8 正常。
	mock.ExpectQuery("SELECT `id` FROM `sys_tenant` WHERE id IN").
		WithArgs(int64(7), int64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(8))
	err := checkTenantPools(db, map[string]*gorm.DB{"7": nil, "8": nil})
	if err == nil || !strings.Contains(err.Error(), "7") || strings.Contains(err.Error(), "8") {
		t.Fatalf("err = %v, want only tenant 7 reported", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// 全部存在时放行。
	mock.ExpectQuery("SELECT `id` FROM `sys_tenant`").WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	if err := checkTenantPools(db, map[string]*gorm.DB{"9": nil}); err != nil {
		t.Fatalf("all present: %v", err)
	}

	// 校验查询本身失败也要拒绝启动（fail-closed），不能当成「没有缺失」。
	mock.ExpectQuery("SELECT `id` FROM `sys_tenant`").WillReturnError(errors.New("platform db down"))
	if err := checkTenantPools(db, map[string]*gorm.DB{"9": nil}); err == nil {
		t.Fatal("query error must be reported")
	}

	// 没有配置池时不查库。
	if err := checkTenantPools(db, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckIsolationConsistencySkipsWhenDisabled(t *testing.T) {
	a := &App{cfg: &config.Config{}}
	if err := a.checkIsolationConsistency(nil); err != nil {
		t.Fatalf("disabled isolation must not touch the database: %v", err)
	}
}
