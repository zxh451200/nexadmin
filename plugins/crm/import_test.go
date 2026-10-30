package crm

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/tenant"
	"nexadmin/pkg/importspec"
)

// 编译期保证：导入中心按 importspec.Provider 发现来源，crm 必须实现它。
var _ importspec.Provider = (*module)(nil)

// 出厂来源：crm_customers，必有必填的客户名称列，且列 key 与 Handler 读的字段一致。
func TestImportSourcesExposesCustomers(t *testing.T) {
	db, _ := mockDB(t)
	srcs := (&module{}).ImportSources(db)
	if len(srcs) != 1 {
		t.Fatalf("want 1 source, got %d", len(srcs))
	}
	src := srcs[0]
	if src.Name != SourceCustomers || src.Handler == nil || len(src.Columns) == 0 {
		t.Fatalf("bad source: %+v", src)
	}
	required := ""
	for _, c := range src.Columns {
		if c.Key == "" || c.Label == "" {
			t.Fatalf("column missing key/label: %+v", c)
		}
		if c.Required {
			required = c.Key
		}
	}
	if required != "name" {
		t.Fatalf("only 客户名称 should be required, got %q", required)
	}
}

// 手机号未命中 → 新建客户。
func TestImportCustomerCreates(t *testing.T) {
	db, m := mockDB(t)
	src := (&module{}).ImportSources(db)[0]

	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE phone = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}))
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(3001, 1))
	m.ExpectCommit()

	ctx := tenant.With(context.Background(), 7)
	err := src.Handler(ctx, 1, map[string]string{
		"name": "张三", "phone": "13800138000", "email": "z@example.com", "level": "VIP",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 手机号命中 → 更新，不新建（同一份文件重复导入不会翻倍）。
func TestImportCustomerUpdatesByPhone(t *testing.T) {
	db, m := mockDB(t)
	src := (&module{}).ImportSources(db)[0]

	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE phone = \\?").
		WillReturnRows(customerRow(2002, "旧名", "", ""))
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE `plg_crm_customer`.`id` = \\?").
		WillReturnRows(customerRow(2002, "旧名", "", ""))
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_crm_customer` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	ctx := tenant.With(context.Background(), 7)
	if err := src.Handler(ctx, 1, map[string]string{"name": "新名", "phone": "13800138000"}); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 名称为空 → 该行失败且不碰库（必填校验兜底之外再守一层）。
func TestImportCustomerRequiresName(t *testing.T) {
	db, _ := mockDB(t)
	src := (&module{}).ImportSources(db)[0]
	err := src.Handler(tenant.With(context.Background(), 7), 1, map[string]string{"phone": "1"})
	if err == nil || !strings.Contains(err.Error(), "imports.required_empty") {
		t.Fatalf("want required_empty, got %v", err)
	}
}
