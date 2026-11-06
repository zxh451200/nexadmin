package crm

// 公海池的行为闸：领取靠条件 UPDATE 决出先后，不存在与被抢走分开报；
// 释放继承客户的可见性校验；公海列表只认 owner 为空。

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
)

// 公海列表只数 owner 为空的客户，不叠加数据范围。
func TestListPoolFiltersUnowned(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_customer` WHERE owner = ''").
		WillReturnRows(sqlmockCount(3))
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE owner = ''").
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner"}))

	rows, total, err := s.ListPool(tenantCtx, CustomerQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(rows) != 0 {
		t.Fatalf("want total=3 rows=0, got total=%d rows=%d", total, len(rows))
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 领取成功：条件 UPDATE 命中，owner 写成当前账号（断言落在 UPDATE 的绑定参数上）。
func TestClaimCustomerTakesOwnership(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_crm_customer`").
		WithArgs("sales01", sqlmock.AnyArg(), int64(2001)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(customerRow(2001, "张三", "", ""))

	ctx := auth.ContextWithUser(tenantCtx, 42, "sales01")
	if _, err := s.ClaimCustomer(ctx, 2001); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 两人同时抢：条件 UPDATE 只有一个命中，后到者 409 而不是覆盖。
func TestClaimCustomerTaken(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectCommit()
	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_customer`").
		WillReturnRows(sqlmockCount(1))

	ctx := auth.ContextWithUser(tenantCtx, 42, "sales01")
	_, err := s.ClaimCustomer(ctx, 2001)
	if !containsKey(err, "crm.pool_taken") {
		t.Fatalf("want crm.pool_taken, got %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 不存在与被抢走分开报：count=0 时是 404。
func TestClaimCustomerMissing(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectCommit()
	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_customer`").
		WillReturnRows(sqlmockCount(0))

	ctx := auth.ContextWithUser(tenantCtx, 42, "sales01")
	_, err := s.ClaimCustomer(ctx, 2001)
	if !containsKey(err, "crm.not_found") {
		t.Fatalf("want crm.not_found, got %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 无登录身份领取 401。
func TestClaimCustomerRequiresIdentity(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})

	if _, err := s.ClaimCustomer(tenantCtx, 2001); err == nil {
		t.Fatal("无身份领取应 401")
	} else if !containsKey(err, "error.unauthorized") {
		t.Fatalf("want error.unauthorized, got %v", err)
	}
}

// 释放把 owner 清空；受限账号对范围外客户仍 403（复用 visibleCustomer 校验）。
func TestReleaseCustomerClearsOwner(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// 归属 sales01 的在册客户（customerRow 夹具 owner 恒空，这里单造）。
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "owner"}).AddRow(2001, "张三", "sales01"))
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_crm_customer`").
		WithArgs("", sqlmock.AnyArg(), int64(2001)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	c, err := s.ReleaseCustomer(tenantCtx, &datascope.Scope{All: true}, 2001)
	if err != nil {
		t.Fatal(err)
	}
	if c.Owner != "" {
		t.Fatalf("release 应清空 owner, got %q", c.Owner)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 已在公海的客户释放是幂等成功，不再发 UPDATE。
func TestReleaseCustomerIdempotentInPool(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// owner 为空：SELECT 回来就返回，不触发 UPDATE。
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "owner"}).AddRow(2001, "张三", ""))

	c, err := s.ReleaseCustomer(tenantCtx, &datascope.Scope{All: true}, 2001)
	if err != nil {
		t.Fatal(err)
	}
	if c.Owner != "" {
		t.Fatalf("公海客户释放应幂等, got %q", c.Owner)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// sqlmockCount 造一列名为 count 的单行结果。
func sqlmockCount(n int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"count"}).AddRow(n)
}
