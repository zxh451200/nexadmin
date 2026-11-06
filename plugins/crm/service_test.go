package crm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
		"id", "tenant_id", "name", "code", "level", "status", "type",
		"external_source", "external_no", "owner", "phone", "email", "country", "nationality",
		"id_type", "id_no", "tax_no", "industry", "address", "remark",
		"created_at", "updated_at",
	}).AddRow(id, 7, name, "C-1", "", "active", TypePerson, source, no, "", "", "", "", "",
		"", "", "", "", "", "", testNow, testNow)
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

	// visibleOpportunity 先做一次非事务预查（nil 范围不裁剪，但 Take 仍在）。
	m.ExpectQuery("SELECT \\* FROM `plg_crm_opportunity`").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "customer_id", "title", "stage", "amount_minor", "currency",
			"expected_at", "owner", "remark", "created_at", "updated_at",
		}).AddRow(3001, 7, 2001, "年会酒席", "won", 100000, "CNY", "", "", "", testNow, testNow))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_crm_opportunity`").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "customer_id", "title", "stage", "amount_minor", "currency",
			"expected_at", "owner", "remark", "created_at", "updated_at",
		}).AddRow(3001, 7, 2001, "年会酒席", "won", 100000, "CNY", "", "", "", testNow, testNow))
	m.ExpectRollback()

	_, err := s.UpdateOpportunity(tenantCtx, nil, 3001, OpportunityInput{
		Title: "年会酒席", Stage: StageLead, AmountMinor: 100000, Currency: "CNY",
	})
	if err == nil {
		t.Fatal("final stage should be locked")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 雪花 ID 超出 JS 安全整数：商机关联客户也要按字符串出 JSON。
func TestOpportunityCustomerIDSerializesAsString(t *testing.T) {
	o := Opportunity{CustomerID: 7430000000000000001}
	o.ID = 3001
	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"customerId":"7430000000000000001"`) {
		t.Fatalf("customerId must be a JSON string: %s", raw)
	}
}

// 商机列表回填客户名与类型，前端列表不用再猜客户是个人还是单位。
func TestListOpportunitiesFillsCustomerBrief(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_opportunity`").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	m.ExpectQuery("SELECT \\* FROM `plg_crm_opportunity`").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "customer_id", "title", "stage", "amount_minor", "currency",
			"expected_at", "owner", "remark", "created_at", "updated_at",
		}).AddRow(3001, 7, 2001, "年会酒席", "lead", 100000, "CNY", "", "", "", testNow, testNow))
	m.ExpectQuery("SELECT `id`,`name`,`type` FROM `plg_crm_customer` WHERE id IN").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type"}).AddRow(2001, "杭州某酒店", TypeCompany))

	rows, total, err := s.ListOpportunities(tenantCtx, OpportunityQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("want 1 row, got %+v total=%d", rows, total)
	}
	if rows[0].CustomerName != "杭州某酒店" || rows[0].CustomerType != TypeCompany {
		t.Fatalf("customer brief not filled: %+v", rows[0])
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 国籍/国家按 ISO 3166-1 alpha-2 收口：统一大写、去空格后入库；非 2 位字母直接拒绝，
// 空串视为未登记（不填是合法的）。
func TestCustomerCountryAndNationality(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	// 王五：国籍/国家留空 = 未登记，同样要能建。
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(2, 1))
	m.ExpectCommit()

	c, err := s.CreateCustomer(tenantCtx, CustomerInput{Name: "张三", Country: " cn ", Nationality: "us"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Country != "CN" || c.Nationality != "US" {
		t.Fatalf("want CN/US, got %q/%q", c.Country, c.Nationality)
	}

	for _, bad := range []string{"China", "C", "12"} {
		if _, err := s.CreateCustomer(tenantCtx, CustomerInput{Name: "李四", Country: bad}); err == nil {
			t.Fatalf("country %q should be rejected", bad)
		}
		if _, err := s.CreateCustomer(tenantCtx, CustomerInput{Name: "李四", Nationality: bad}); err == nil {
			t.Fatalf("nationality %q should be rejected", bad)
		}
	}
	if _, err := s.CreateCustomer(tenantCtx, CustomerInput{Name: "王五"}); err != nil {
		t.Fatalf("empty country/nationality should pass: %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 客户类型决定哪组身份字段生效：个人用国籍+证件，企业用信用代码+行业+地址，
// 另一组由服务层清空——切了类型还留着上一条护照号，列表和导出就会说假话。
func TestCustomerTypeDrivesIdentityFields(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// 个人（不填类型的默认）与企业各建一条。个人无手机号不触发查重；企业有信用代码，查重先查后插。
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_customer` WHERE tax_no = \\?").
		WithArgs("91330100MA2AB1CD3X").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_crm_customer`").WillReturnResult(sqlmock.NewResult(2, 1))
	m.ExpectCommit()

	person, err := s.CreateCustomer(tenantCtx, CustomerInput{Name: "张三", Nationality: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if person.Type != TypePerson || person.Nationality != "CN" {
		t.Fatalf("person default or nationality normalization wrong: %+v", person)
	}

	cu, err := s.CreateCustomer(tenantCtx, CustomerInput{
		Name: "杭州某酒店", Type: TypeCompany, Nationality: "CN",
		IdType: IdTypeIDCard, IdNo: "33010619900101001X",
		TaxNo: "91330100ma2ab1cd3x", Industry: "住宿业",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cu.Type != TypeCompany {
		t.Fatalf("type not persisted: %+v", cu)
	}
	if cu.Nationality != "" || cu.IdType != "" || cu.IdNo != "" {
		t.Fatalf("company must not carry person identity: %+v", cu)
	}
	if cu.TaxNo != "91330100MA2AB1CD3X" || cu.Industry != "住宿业" {
		t.Fatalf("company identity not normalized: %+v", cu)
	}

	// 类型/证件类型非法、证件只填一半：全部拒绝且不碰库。
	for _, tc := range []struct {
		name string
		in   CustomerInput
		key  string
	}{
		{"bad type", CustomerInput{Name: "x", Type: "gov"}, "crm.type_invalid"},
		{"bad id type", CustomerInput{Name: "x", IdType: "driver_license"}, "crm.id_type_invalid"},
		{"id type without no", CustomerInput{Name: "x", IdType: IdTypePassport}, "crm.id_pair_invalid"},
		{"id no without type", CustomerInput{Name: "x", IdNo: "E12345678"}, "crm.id_pair_invalid"},
	} {
		if _, err := s.CreateCustomer(tenantCtx, tc.in); err == nil {
			t.Fatalf("%s: want error", tc.name)
		} else if !containsKey(err, tc.key) {
			t.Fatalf("%s: want key %s, got %v", tc.name, tc.key, err)
		}
	}

	// 查重：个人同手机号、企业同信用代码 → 409 拒绝。
	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_customer` WHERE phone = \\?").
		WithArgs("13800138000").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	if _, err := s.CreateCustomer(tenantCtx, CustomerInput{Name: "重复甲", Phone: "13800138000"}); err == nil {
		t.Fatal("同手机号创建应被拒绝")
	} else if !containsKey(err, "crm.duplicate_customer") {
		t.Fatalf("want crm.duplicate_customer, got %v", err)
	}
	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_customer` WHERE tax_no = \\?").
		WithArgs("91330100MA2AB1CD3X").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	if _, err := s.CreateCustomer(tenantCtx, CustomerInput{Name: "重复乙", Type: TypeCompany, TaxNo: "91330100MA2AB1CD3X"}); err == nil {
		t.Fatal("同信用代码创建应被拒绝")
	} else if !containsKey(err, "crm.duplicate_customer") {
		t.Fatalf("want crm.duplicate_customer, got %v", err)
	}

	// 过滤条件容忍大小写与空格；非法值要直接报错，不能悄悄返回空列表。
	m.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_crm_customer` WHERE type = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer` WHERE type = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}))
	rows, _, err := s.ListCustomers(tenantCtx, CustomerQuery{Type: " COMPANY ", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("want empty page, got %+v", rows)
	}
	if _, _, err := s.ListCustomers(tenantCtx, CustomerQuery{Type: "gov", Page: 1, PageSize: 10}); err == nil {
		t.Fatal("invalid filter type should be rejected")
	} else if !containsKey(err, "crm.type_invalid") {
		t.Fatalf("want crm.type_invalid, got %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 裁剪按字符不是按字节：按字节切会把汉字切成半个码点，MySQL 直接以 1366 拒掉整行
// （plugins/crm/live_test.go 在真库上抓到过一次）。
func TestTruncateCutsByRunesNotBytes(t *testing.T) {
	// 2 runes：rune 语义下恰好整串保留；若按字节切（6 字节 > 4）会出半个码点
	if got := truncate("张三", 2); got != "张三" {
		t.Fatalf("want 张三, got %q", got)
	}
	// 3 runes 裁到 2：整字切，不出半个码点
	if got := truncate("张三张", 2); got != "张三" {
		t.Fatalf("want 张三, got %q", got)
	}
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("短字符串应原样通过: %q", got)
	}
	in := CustomerInput{Name: "真库用例-" + strings.Repeat("杭", 300), Remark: strings.Repeat("记", 600)}
	if err := in.normalize(); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(in.Name)); n != 200 {
		t.Fatalf("name should be cut to 200 chars, got %d", n)
	}
	if n := len([]rune(in.Remark)); n != 500 {
		t.Fatalf("remark should be cut to 500 chars, got %d", n)
	}
	if !utf8.ValidString(in.Name) || !utf8.ValidString(in.Remark) {
		t.Fatal("裁剪产出非法 UTF-8")
	}
}

// 类型清空的优先级高于「脱敏串视为没改过」：详情回显的是 110101********001X，
// 用户把这条个人客户改成企业并直接保存时，不能因为号码带星号就把原证件回填进来
// ——否则企业客户名下又挂回一条个人证件，类型分流白做。
func TestUpdateToCompanyDropsMaskedIdNo(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// sqlmock 的 Rows 是一次性游标，两次 Take 要各自一份。
	masked := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "tenant_id", "name", "type", "id_type", "id_no", "status"}).
			AddRow(2001, 7, "张三", TypePerson, IdTypeIDCard, "110101********001X", StatusActive)
	}
	// visibleCustomer 预查 + 更新前取原行。
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(masked())
	m.ExpectQuery("SELECT \\* FROM `plg_crm_customer`").WillReturnRows(masked())
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_crm_customer` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	cu, err := s.UpdateCustomer(tenantCtx, nil, 2001, CustomerInput{
		Name: "张三", Type: TypeCompany, IdNo: "110101********001X", IdType: IdTypeIDCard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cu.Type != TypeCompany {
		t.Fatalf("type not switched: %+v", cu)
	}
	if cu.IdNo != "" || cu.IdType != "" {
		t.Fatalf("masked id number was restored onto a company customer: %+v", cu)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
