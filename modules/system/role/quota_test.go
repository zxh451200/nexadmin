package role

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type roleSource map[idgen.ID]*tenant.Info

func (r roleSource) TenantInfo(_ context.Context, id idgen.ID) (*tenant.Info, error) {
	return r[id], nil
}
func (roleSource) TenantByDomain(context.Context, string) (idgen.ID, bool, error) {
	return 0, false, nil
}

// 用 sqlmock 验证，没有连接真实数据库。
func TestEnsureRoleQuota(t *testing.T) {
	tenant.SetSource(roleSource{5: {ID: 5, Status: "active", MaxRoles: 2}, 6: {ID: 6, Status: "active"}})
	t.Cleanup(func() { tenant.SetSource(nil) })
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.With(context.Background(), 5)
	count := func(n int) {
		// 先锁租户行（SELECT ... FOR UPDATE），再统计；sqlmock 默认按声明顺序匹配，错序即失败
		mock.ExpectQuery("SELECT id FROM `sys_tenant` WHERE id = \\? LIMIT \\? FOR UPDATE").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(5))
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_role`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(n))
	}

	count(1)
	if err := ensureRoleQuota(ctx, db); err != nil {
		t.Fatalf("2nd role must pass: %v", err)
	}
	count(2)
	err = ensureRoleQuota(ctx, db)
	var e *response.Error
	if !errors.As(err, &e) || e.Status != 403 || e.Key != "tenant.quota_roles" || e.Reason != tenant.ReasonQuotaRoles {
		t.Fatalf("3rd role must be rejected: %v", err)
	}
	// 不限制的租户与平台级操作不产生任何 SQL
	if err := ensureRoleQuota(tenant.With(context.Background(), 6), db); err != nil {
		t.Fatal(err)
	}
	if err := ensureRoleQuota(tenant.Skip(ctx), db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
