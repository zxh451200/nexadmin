package user

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"nexadmin/core/tenant"
)

func TestRegisterNameRe(t *testing.T) {
	for _, s := range []string{"abc", "a1_", "Admin99", "z" + string(make([]byte, 0)) + "zz"} {
		if !registerNameRe.MatchString(s) {
			t.Errorf("registerNameRe(%q) should match", s)
		}
	}
	for _, s := range []string{"", "ab", "1abc", "_abc", "a b", "中文账号", "aBc-"} {
		if registerNameRe.MatchString(s) {
			t.Errorf("registerNameRe(%q) should NOT match", s)
		}
	}
}

// registerRoleIDs：未配置返回 nil（不挂角色）；配置的角色码必须全部存在，缺一个整体拒绝（不静默降权）。
func TestRegisterRoleIDs(t *testing.T) {
	ctx := tenant.With(context.Background(), tenant.DefaultID)
	sqlDB, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 未配置 → 不发 SQL，返回 nil。
	ids, err := registerRoleIDs(ctx, db, nil)
	if err != nil || ids != nil {
		t.Fatalf("nil codes: got %v, %v", ids, err)
	}
	ids, err = registerRoleIDs(ctx, db, []string{"", "  "})
	if err != nil || ids != nil {
		t.Fatalf("blank codes: got %v, %v", ids, err)
	}

	// 配置两个角色、库里有两个 → 返回。
	m.ExpectQuery("SELECT id FROM `sys_role`").
		WithArgs(int64(tenant.DefaultID), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2100000000000000004).AddRow(2100000000000000002))
	ids, err = registerRoleIDs(ctx, db, []string{"guest", "operator"})
	if err != nil || len(ids) != 2 {
		t.Fatalf("two codes: got %v, %v", ids, err)
	}

	// 配置两个角色、库里只有一个 → 整体拒绝。
	m.ExpectQuery("SELECT id FROM `sys_role`").
		WithArgs(int64(tenant.DefaultID), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2100000000000000004))
	if _, err := registerRoleIDs(ctx, db, []string{"guest", "operator"}); err == nil {
		t.Fatal("missing role must reject registration")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
