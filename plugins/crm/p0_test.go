package crm

// P0 三件事的用例：联系人（主联系人事务降级）、跟进记录（记录人落当前账号、入参校验）、
// 数据范围（受限操作者按 owner 裁剪，范围外 403 防枚举）。

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
)

// 指定主联系人时，事务里先把原主联系人降级再插入。
func TestCreateContactPrimaryDemotedInTransaction(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// visibleCustomer：客户预查
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(customerRow(2001, "张三", "", ""))
	m.ExpectBegin()
	// 单测库没挂租户插件，WHERE 只有 customer_id；gorm 自动填 updated_at。
	m.ExpectExec("UPDATE `plg_crm_contact` SET").
		WithArgs(false, sqlmock.AnyArg(), 2001).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO `plg_crm_contact`").WillReturnResult(sqlmock.NewResult(9001, 1))
	m.ExpectCommit()

	ct, err := s.CreateContact(tenantCtx, &datascope.Scope{All: true}, 2001, ContactInput{
		Name: "王联系人", Phone: "13800000000", IsPrimary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ct.IsPrimary || ct.CustomerID != 2001 {
		t.Fatalf("primary contact not persisted: %+v", ct)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 跟进记录的 author 取当前账号；类型与内容校验在前。
func TestCreateActivityFillsAuthorAndValidates(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	ctx := auth.ContextWithUser(tenantCtx, 42, "sales01")
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(customerRow(2001, "张三", "", ""))
	// gorm 默认给单条 Create 包事务。
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_crm_activity`").WillReturnResult(sqlmock.NewResult(9101, 1))
	m.ExpectCommit()

	a, err := s.CreateActivity(ctx, &datascope.Scope{All: true}, ActivityInput{
		CustomerID: 2001, Kind: KindCall, Content: "电话沟通年会方案", NextAt: "2026-11-05",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Author != "sales01" || a.Kind != KindCall || a.NextAt != "2026-11-05" {
		t.Fatalf("activity fields wrong: %+v", a)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// 非法类型 / 空内容 / 坏日期都在校验层拒绝，不动库。
	for _, tc := range []struct {
		name string
		in   ActivityInput
		key  string
	}{
		{"bad kind", ActivityInput{CustomerID: 2001, Kind: "fax", Content: "x"}, "crm.activity_kind_invalid"},
		{"empty content", ActivityInput{CustomerID: 2001, Kind: KindNote, Content: " "}, "crm.activity_content_required"},
		{"bad date", ActivityInput{CustomerID: 2001, Kind: KindTodo, Content: "x", NextAt: "2026/11/05"}, "crm.date_invalid"},
	} {
		if _, err := s.CreateActivity(ctx, &datascope.Scope{All: true}, tc.in); err == nil {
			t.Fatalf("%s: want error", tc.name)
		} else if !containsKey(err, tc.key) {
			t.Fatalf("%s: want key %s, got %v", tc.name, tc.key, err)
		}
	}
}

// 受限操作者对范围外客户 403（与不存在区分开，防枚举由统一 403 语义保证）。
func TestVisibleCustomerForbiddenOutsideScope(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(customerRow(2001, "张三", "", ""))
	// UsernameCond 的 Count 子查询：owner 只归当前用户时命不中别人的客户。
	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_customer`").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	sc := &datascope.Scope{UserID: 42, Preset: "self"}
	if _, err := s.visibleCustomer(tenantCtx, sc, 2001); err == nil {
		t.Fatal("out-of-scope customer must be forbidden")
	} else if !containsKey(err, "datascope.forbidden") {
		t.Fatalf("want datascope.forbidden, got %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 列表按 owner 裁剪：受限操作者的查询带上 UsernameCond 生成的子查询条件。
func TestListCustomersAppliesScopeCond(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_customer` WHERE owner IN").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE owner IN").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}))

	_, _, err := s.ListCustomers(tenantCtx, CustomerQuery{
		Scope: &datascope.Scope{UserID: 42, Preset: "self"}, Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 客户删除时联系人与跟进记录随事务清理，不留孤儿。
func TestDeleteCustomerCascadesContactsAndActivities(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(customerRow(2001, "张三", "", ""))
	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_opportunity`").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_crm_contact`").WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectExec("DELETE FROM `plg_crm_activity`").WillReturnResult(sqlmock.NewResult(0, 5))
	m.ExpectExec("DELETE FROM `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	if err := s.DeleteCustomer(tenantCtx, &datascope.Scope{All: true}, 2001); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// containsKey 判断错误是否携带指定 i18n key（response.Error）。
func containsKey(err error, key string) bool {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Key == key
	}
	return false
}
