package crm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func TestMain(m *testing.M) {
	if err := idgen.Init(1); err != nil {
		panic(err)
	}
	gin.SetMode(gin.TestMode)
	m.Run()
}

var testNow = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		NowFunc:              func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, m
}

type nopLogger struct{}

func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

var tenantCtx = tenant.With(context.Background(), 7)

func customerRow(id int64, name, source, no string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "name", "code", "level", "status",
		"external_source", "external_no", "owner", "phone", "email", "remark",
		"created_at", "updated_at",
	}).AddRow(id, 7, name, "C-1", "", "active", source, no, "", "", "", "", testNow, testNow)
}

// 锚点锚定：同锚点重复建档返回已有客户（member 对齐客户 ID 的幂等基础）。
func TestResolveByExternalIdempotent(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE external_source = \\? AND external_no = \\?").
		WillReturnRows(customerRow(2001, "张三", "hotel", "M0001"))

	c, created, err := s.ResolveByExternal(tenantCtx, "hotel", "M0001", "张三")
	if err != nil {
		t.Fatal(err)
	}
	if created || c.ID != 2001 {
		t.Fatalf("want existing customer 2001, got %+v created=%v", c, created)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 未命中且给了 fallbackName：自动建档并发布 crm.customer.created。
func TestResolveByExternalCreates(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	events := make(chan event.Event, 4)
	// 总线是进程级异步的，别的用例也会发同名事件：只收自己那条，且永不阻塞派发协程。
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name != EventCustomerCreated {
			return
		}
		raw, _ := json.Marshal(e.Data)
		if !strings.Contains(string(raw), `"externalNo":"10086"`) {
			return
		}
		select {
		case events <- e:
		default:
		}
	}))

	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE external_source = \\? AND external_no = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}))
	// CreateCustomer 内部还有一次同锚点幂等预查。
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE external_source = \\? AND external_no = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}))
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	c, created, err := s.ResolveByExternal(tenantCtx, "mall", "10086", "李四")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("want created=true")
	}
	if c.ExternalSource == nil || *c.ExternalSource != "mall" {
		t.Fatalf("external anchor not persisted: %+v", c)
	}
	// 编码留空时按 ID 派生，自动建档的会员也有稳定编码。
	if !strings.HasPrefix(c.Code, "C-") || c.Code == "C-" {
		t.Fatalf("auto code not generated: %q", c.Code)
	}
	select {
	case ev := <-events:
		data, _ := json.Marshal(ev.Data)
		if !strings.Contains(string(data), `"externalNo":"10086"`) {
			t.Fatalf("event payload missing anchor: %s", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("crm.customer.created not published")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 非法来源拒绝且不动库。
func TestResolveByExternalInvalidSource(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})
	if _, _, err := s.ResolveByExternal(tenantCtx, "weird", "1", "x"); err == nil {
		t.Fatal("invalid source should fail")
	}
}

// 已到终态的商机不允许再改阶段。
func TestOpportunityFinalStageLocked(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_crm_opportunity`").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "customer_id", "title", "stage", "amount_minor", "currency",
			"expected_at", "owner", "remark", "created_at", "updated_at",
		}).AddRow(3001, 7, 2001, "年会酒席", "won", 100000, "CNY", "", "", "", testNow, testNow))
	m.ExpectRollback()

	_, err := s.UpdateOpportunity(tenantCtx, 3001, OpportunityInput{
		CustomerID: 2001, Title: "年会酒席", Stage: StageLead, AmountMinor: 100000, Currency: "CNY",
	})
	if err == nil {
		t.Fatal("final stage should be locked")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
