package crm

// 合并客户的行为闸：关联记录整体迁移、主联系人只留一条、主客户补空不覆盖、从客户删除，
// 全程一个事务。

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/datascope"
)

// 不能把客户合并进它自己：请求在进数据库之前就被拒。
func TestMergeSelfRejected(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	if _, err := s.MergeCustomers(tenantCtx, &datascope.Scope{All: true}, MergeInput{Target: 2001, Source: 2001}); err == nil {
		t.Fatal("merge into self must fail")
	} else if !containsKey(err, "crm.merge_self") {
		t.Fatalf("want crm.merge_self, got %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 合并主流程：三个关联表整体迁到主客户、主客户用从客户补空字段、删除从客户，一个事务。
func TestMergeMovesRelationsAndFillsBlanks(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// 可见性检查：两个客户各一次 Take（Scope All 不再追加范围子查询）。
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(customerRow(2001, "张三", "", ""))
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(customerRow(2002, "张三丰", "", ""))

	m.ExpectBegin()
	// 事务内再取一次主/从（以事务快照为准）。
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(customerRow(2001, "张三", "", ""))
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(customerRow(2002, "张三丰", "", ""))
	// 关联记录迁移：联系人、跟进、商机。
	m.ExpectExec("UPDATE `plg_crm_contact`").WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectExec("UPDATE `plg_crm_activity`").WillReturnResult(sqlmock.NewResult(0, 5))
	m.ExpectExec("UPDATE `plg_crm_opportunity`").WillReturnResult(sqlmock.NewResult(0, 1))
	// 主联系人冲突检查：只有一条，不降级。
	m.ExpectQuery("SELECT \\* FROM `plg_crm_contact` WHERE customer_id =").
		WillReturnRows(sqlmock.NewRows([]string{"id", "customer_id", "is_primary"}).AddRow(9, 2001, true))
	// 夹具两行字段全空，fills 为空：跳过补空 UPDATE，直接删除从客户。
	m.ExpectExec("DELETE FROM `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(0, 1))
	// 回读合并结果。
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(customerRow(2001, "张三", "", ""))
	m.ExpectCommit()

	merged, err := s.MergeCustomers(tenantCtx, &datascope.Scope{All: true}, MergeInput{Target: 2001, Source: 2002})
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID == 0 {
		t.Fatal("merge 应返回主客户")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 补空语义：主客户已有的字段不被从客户覆盖，只有空位才补。
func TestMergeFillsBlanksOnly(t *testing.T) {
	target := &Customer{Name: "张三", Owner: "sales01", Phone: ""}
	source := &Customer{Name: "张三丰", Owner: "sales02", Phone: "13800138000"}

	fills := mergeFills(target, source)
	if fills["owner"] == "sales02" {
		t.Fatal("主客户已有 owner 不应被覆盖")
	}
	if fills["phone"] != "13800138000" {
		t.Fatalf("主客户空着的电话应补上, got %v", fills["phone"])
	}
}

// 类型不同不互填身份字段：个人客户不会继承企业客户的信用代码。
func TestMergeFillsRespectType(t *testing.T) {
	target := &Customer{Name: "张三", Type: TypePerson}
	source := &Customer{Name: "某公司", Type: TypeCompany, TaxNo: "91330100MA2AB1CD3X"}

	fills := mergeFills(target, source)
	if _, ok := fills["tax_no"]; ok {
		t.Fatal("类型不同不应互填身份字段")
	}
}
