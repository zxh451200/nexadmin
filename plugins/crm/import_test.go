package crm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/event"
	"nexadmin/core/tenant"
	"nexadmin/pkg/importspec"
)

// 编译期保证：导入中心按 importspec.Provider 发现来源，crm 必须实现它。
var _ importspec.Provider = (*module)(nil)

// drainEvent 订阅指定事件并非阻塞投递。落库会往进程级异步总线发事件，不等它派发完就退出，
// 事件会被后面才注册订阅者的用例读到（它们的过滤条件只有事件名）。
func drainEvent(t *testing.T, name string) chan event.Event {
	t.Helper()
	ch := make(chan event.Event, 16)
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name != name {
			return
		}
		select {
		case ch <- e:
		default:
		}
	}))
	return ch
}

// waitEvent 等到带有关键字的事件真正派发出去，确保总线里没有留给后面用例的事件。
func waitEvent(t *testing.T, ch chan event.Event, name string, want string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ch:
			raw, _ := json.Marshal(e.Data)
			if strings.Contains(string(raw), want) {
				return
			}
		case <-deadline:
			t.Fatalf("event %s carrying %q was not dispatched", name, want)
		}
	}
}

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
	created := drainEvent(t, EventCustomerCreated)

	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE phone = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}))
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(3001, 1))
	m.ExpectCommit()

	ctx := tenant.With(context.Background(), 7)
	err := src.Handler(ctx, 1, map[string]string{
		"name": "导入用例甲", "phone": "13800138000", "email": "z@example.com", "level": "VIP",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, created, EventCustomerCreated, "导入用例甲")
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 手机号命中 → 更新，不新建（同一份文件重复导入不会翻倍）。
func TestImportCustomerUpdatesByPhone(t *testing.T) {
	db, m := mockDB(t)
	src := (&module{}).ImportSources(db)[0]
	updated := drainEvent(t, EventCustomerUpdated)

	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE phone = \\?").
		WillReturnRows(customerRow(2002, "旧名", "", ""))
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE `plg_crm_customer`.`id` = \\?").
		WillReturnRows(customerRow(2002, "旧名", "", ""))
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_crm_customer` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	ctx := tenant.With(context.Background(), 7)
	if err := src.Handler(ctx, 1, map[string]string{"name": "导入用例乙", "phone": "13800138000"}); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, updated, EventCustomerUpdated, "导入用例乙")
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
