package crm

// 真实库用例：sqlmock 只做正则匹配、从不执行 SQL，所以手写列名（UpdateCustomer 的
// updates map）、跨引擎列类型与「按类型清空另一组字段」只能在这里验。
//
// 跑法（两个一次性测试库，与业务库分开）：
//
//	NEXADMIN_LIVE_DB=mysql NEXADMIN_LIVE_HOST=127.0.0.1 NEXADMIN_LIVE_PORT=3306 \
//	NEXADMIN_LIVE_USER=root NEXADMIN_LIVE_PASSWORD=... \
//	NEXADMIN_LIVE_CLEANUP=1 go test ./plugins/crm/ -run TestLive -v
//
// 把 NEXADMIN_LIVE_DB 换成 postgres 即在另一套引擎上跑同一批断言。

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/migrate"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// liveOpen 未设置 NEXADMIN_LIVE_DB 时跳过整条用例。库名必须以 _live 结尾，
// 否则不参与 NEXADMIN_LIVE_CLEANUP 的删库清理（宁可留残库，也不误删）。
func liveOpen(t *testing.T, dbName string) *gorm.DB {
	t.Helper()
	kind := os.Getenv("NEXADMIN_LIVE_DB")
	if kind == "" {
		t.Skip("未设置 NEXADMIN_LIVE_DB，跳过真实库验证")
	}
	port, _ := strconv.Atoi(os.Getenv("NEXADMIN_LIVE_PORT"))
	host := os.Getenv("NEXADMIN_LIVE_HOST")
	if host == "" {
		host = "localhost"
	}
	cfg := config.Database{Type: kind, Host: host, Port: port, User: os.Getenv("NEXADMIN_LIVE_USER"),
		Password: os.Getenv("NEXADMIN_LIVE_PASSWORD"), Name: dbName, NodeID: 13}
	ctx := context.Background()
	if err := database.EnsureDatabase(ctx, cfg); err != nil {
		t.Fatalf("ensure database: %v", err)
	}
	// 走生产构造器：database.Open 里装了租户回调与时区，裸 gorm.Open 会让跨租户断言退化成空断言。
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := idgen.Init(13); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("NEXADMIN_LIVE_CLEANUP") == "1" && strings.HasSuffix(dbName, "_live") {
		t.Cleanup(func() {
			_ = database.Close(db)
			adminCfg := cfg
			if cfg.Type == "postgres" {
				adminCfg.Name = "postgres" // PG 不能在连着目标库的会话里删它
			}
			admin, err := database.Open(adminCfg)
			if err != nil {
				t.Logf("cleanup open: %v", err)
				return
			}
			stmt := "DROP DATABASE `" + dbName + "`"
			if cfg.Type == "postgres" {
				stmt = `DROP DATABASE "` + dbName + `"`
			}
			if err := admin.Exec(stmt).Error; err != nil {
				t.Logf("cleanup drop: %v", err)
			}
			_ = database.Close(admin)
		})
	}
	return db
}

// liveCustomer 按 ID 直读一遍库，拿列的真实值而不是服务层回显的结构体。
func liveCustomer(t *testing.T, db *gorm.DB, id idgen.ID) Customer {
	t.Helper()
	var got Customer
	if err := db.WithContext(tenant.Skip(context.Background())).Take(&got, int64(id)).Error; err != nil {
		t.Fatalf("read back customer %d: %v", id, err)
	}
	return got
}

// TestLiveCustomerType 在真实引擎上验证客户「个人 / 企业」两条路径：
// 生效字段落库、另一组被清空、切换类型后不残留、列表按类型与证件号检索。
func TestLiveCustomerType(t *testing.T) {
	db := liveOpen(t, "nexadmin_crm_live")
	if err := db.AutoMigrate(&Customer{}, &Contact{}, &Activity{}, &Opportunity{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	ctx := tenant.With(context.Background(), 7)
	svc := NewService(db, nopLogger{})
	uniq := strconv.FormatInt(time.Now().UnixNano(), 10)
	idNo := "E" + uniq[:8]
	taxNo := "91330100" + uniq[:10]

	t.Cleanup(func() {
		db.WithContext(tenant.Skip(ctx)).Exec("DELETE FROM plg_crm_customer WHERE name LIKE ?", "真库用例%")
	})

	person, err := svc.CreateCustomer(ctx, CustomerInput{
		Name: "真库用例-个人-" + uniq, Nationality: "cn", IdType: IdTypePassport, IdNo: idNo, Country: "cn",
	})
	if err != nil {
		t.Fatal(err)
	}
	company, err := svc.CreateCustomer(ctx, CustomerInput{
		Name: "真库用例-企业-" + uniq, Type: TypeCompany, Nationality: "CN",
		IdType: IdTypeIDCard, IdNo: "33010619900101001X", TaxNo: strings.ToLower(taxNo),
		Industry: "住宿业", Address: "浙江省杭州市西湖区",
	})
	if err != nil {
		t.Fatal(err)
	}

	gotPerson, gotCompany := liveCustomer(t, db, person.ID), liveCustomer(t, db, company.ID)
	if gotPerson.Type != TypePerson || gotPerson.Nationality != "CN" || gotPerson.Country != "CN" ||
		gotPerson.IdType != IdTypePassport || gotPerson.IdNo != idNo {
		t.Fatalf("person identity not persisted: %+v", gotPerson)
	}
	if gotCompany.Type != TypeCompany || gotCompany.TaxNo != taxNo || gotCompany.Industry != "住宿业" {
		t.Fatalf("company identity not persisted: %+v", gotCompany)
	}
	// 服务层清空要真的落到列上，不是只在返回体里装作干净。
	if gotCompany.Nationality != "" || gotCompany.IdType != "" || gotCompany.IdNo != "" {
		t.Fatalf("company kept person identity in DB: %+v", gotCompany)
	}

	// 个人改成企业：证件与国籍必须被冲掉，编码沿用（PUT 是全量更新）。
	updated, err := svc.UpdateCustomer(ctx, nil, person.ID, CustomerInput{
		Name: "真库用例-个人-" + uniq, Type: TypeCompany, Industry: "软件业", Phone: gotPerson.Phone,
	})
	if err != nil {
		t.Fatal(err)
	}
	after := liveCustomer(t, db, updated.ID)
	if after.Type != TypeCompany || after.Nationality != "" || after.IdType != "" || after.IdNo != "" {
		t.Fatalf("switching type left stale identity: %+v", after)
	}
	if after.Code != gotPerson.Code {
		t.Fatalf("code wiped on update: %q -> %q", gotPerson.Code, after.Code)
	}

	// 详情回显的是脱敏串，用户直接保存时也不能把证件写回企业客户名下：类型清空优先于「没改过就沿用」。
	if _, err := svc.UpdateCustomer(ctx, nil, person.ID, CustomerInput{
		Name: "真库用例-个人-" + uniq, Type: TypeCompany, Industry: "软件业",
		IdType: IdTypeIDCard, IdNo: "110101********001X",
	}); err != nil {
		t.Fatal(err)
	}
	if back := liveCustomer(t, db, person.ID); back.IdNo != "" || back.IdType != "" {
		t.Fatalf("masked id restored onto company row in DB: %+v", back)
	}

	// 列表：类型过滤 + 关键词按证件号 / 信用代码命中（这两列是这次新加进检索的）。
	rows, total, err := svc.ListCustomers(ctx, CustomerQuery{Type: TypeCompany, Keyword: taxNo, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != company.ID {
		t.Fatalf("credit-code search missed: total=%d rows=%+v", total, rows)
	}
	if _, _, err := svc.ListCustomers(ctx, CustomerQuery{Type: TypePerson, Keyword: idNo, Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("person turned company should no longer match by id no: %v", err)
	}
	rows, total, err = svc.ListCustomers(ctx, CustomerQuery{Type: TypeCompany, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Type != TypeCompany {
			t.Fatalf("type filter leaked %q", r.Type)
		}
	}
	if total < 2 {
		t.Fatalf("type filter should return both company rows, got %d", total)
	}

	// 超长中文按字符裁到列宽：不能报 1366（把汉字切成半个），也不能留非法 UTF-8。
	long, err := svc.CreateCustomer(ctx, CustomerInput{
		Name: "真库用例-超长-" + strings.Repeat("杭", 300), Type: TypeCompany,
		Address: strings.Repeat("杭", 300), Remark: strings.Repeat("记", 600),
	})
	if err != nil {
		t.Fatal(err)
	}
	gotLong := liveCustomer(t, db, long.ID)
	if n := utf8.RuneCountInString(gotLong.Address); n != 200 {
		t.Fatalf("address should be cut to 200 chars, got %d", n)
	}
	if n := utf8.RuneCountInString(gotLong.Remark); n != 500 {
		t.Fatalf("remark should be cut to 500 chars, got %d", n)
	}
	if n := utf8.RuneCountInString(gotLong.Name); n != 200 {
		t.Fatalf("name should be cut to 200 chars, got %d", n)
	}
	if !utf8.ValidString(gotLong.Address) || !utf8.ValidString(gotLong.Name) {
		t.Fatal("truncation produced invalid UTF-8")
	}
}

// TestLiveCrmMigrationFiles 按方言目录里的 SQL 文件从零建表再写一遍数据。
// AutoMigrate 是按模型建表，照不到迁移文件本身；线上升级跑的是这些文件，
// 列名/列宽只要与模型不一致，第一次写入就会炸在真机上。
func TestLiveCrmMigrationFiles(t *testing.T) {
	kind := os.Getenv("NEXADMIN_LIVE_DB")
	if kind == "" {
		t.Skip("未设置 NEXADMIN_LIVE_DB，跳过真实库验证")
	}
	db := liveOpen(t, "nexadmin_crm_schema_live")
	for _, m := range []any{&Activity{}, &Contact{}, &Opportunity{}, &Customer{}} {
		if err := db.Migrator().DropTable(m); err != nil {
			t.Logf("drop before apply: %v", err) // 首次建库没有这些表，不算失败
		}
	}
	// 本包的迁移目录（测试的工作目录就是 plugins/crm）。
	files, err := migrate.Discover(kind, []migrate.Source{{Scope: migrate.ScopeBusiness, Root: "migrations"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 5 {
		t.Fatalf("至少要跑到 005_customer_type，实际发现 %d 个迁移文件", len(files))
	}
	for _, f := range files {
		for _, stmt := range migrate.Split(f.SQL) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("apply %s: %v", f.Name, err)
			}
		}
	}
	for _, col := range []string{"type", "id_type", "id_no", "tax_no", "industry", "address", "country", "nationality"} {
		if !db.Migrator().HasColumn(&Customer{}, col) {
			t.Fatalf("迁移建出的 plg_crm_customer 缺列 %s", col)
		}
	}

	ctx := tenant.With(context.Background(), 7)
	svc := NewService(db, nopLogger{})
	uniq := strconv.FormatInt(time.Now().UnixNano(), 10)
	cu, err := svc.CreateCustomer(ctx, CustomerInput{
		Name: "迁移用例-企业-" + uniq, Type: TypeCompany,
		TaxNo: "91110" + uniq[:9], Industry: "建筑业", Address: "北京市", Country: "cn",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := liveCustomer(t, db, cu.ID)
	if got.Type != TypeCompany || got.TaxNo == "" || got.Country != "CN" {
		t.Fatalf("SQL 建出的表接不住服务层写入: %+v", got)
	}
	if _, _, err := svc.ListCustomers(ctx, CustomerQuery{Type: TypeCompany, Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("type filter on migrated schema: %v", err)
	}
}
