package tenant

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/response"
	ctenant "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 这些测试用 sqlmock 校验 SQL 形状与事务边界，没有连接真实数据库。

func newSvc(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db), mock
}

var tctx = ctenant.With(context.Background(), 1)

func TestNormalizeDomain(t *testing.T) {
	ok := map[string]string{
		"Acme.Example-Corp.com":    "acme.example-corp.com",
		" acme.nexadmin.dev. ":     "acme.nexadmin.dev",
		"a.b.c.d.dev":              "a.b.c.d.dev",
		"xn--fiq228c.nexadmin.dev": "xn--fiq228c.nexadmin.dev",
	}
	for in, want := range ok {
		got, err := NormalizeDomain(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	reserved := []string{"localhost", "foo.localhost", "127.0.0.1", "10.0.0.1", "::1", "192.168.1.10", "1.2.3.4",
		"corp.local", "svc.internal", "a.test", "example.com", "www.example.com", "x.example.org", "router.lan", "a.home.arpa"}
	for _, in := range reserved {
		if _, err := NormalizeDomain(in); err == nil || !strings.HasPrefix(err.Error(), "tenant.domain_") {
			t.Errorf("%q should be rejected with a tenant.domain_* key, got %v", in, err)
		}
	}
	invalid := []string{"", "  ", "nodots", "-a.dev", "a-.dev", "a..dev", "http://a.dev", "a.dev/x", "a.dev:8080", "a_b.dev", "*.a.dev",
		"a b.dev", strings.Repeat("a", 64) + ".dev", "user@a.dev"}
	for _, in := range invalid {
		if _, err := NormalizeDomain(in); err == nil {
			t.Errorf("%q should be rejected", in)
		}
	}
	if _, err := NormalizeDomain("localhost"); err == nil || err.Error() != "tenant.domain_reserved" {
		t.Errorf("localhost should be reserved: %v", err)
	}
}

func pkgRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "menu_ids", "max_users", "max_roles", "max_storage_mb", "status"}).
		AddRow(5, "pkg", "[]", 10, 3, 100, "1")
}

func TestCheckDomainUniqueCaseInsensitive(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectQuery("FROM `sys_tenant_pkg`").WillReturnRows(pkgRows())
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_tenant` WHERE LOWER\\(domain\\) = \\? AND id <> \\?").
		WithArgs("acme.nexadmin.dev", int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	in := TenantInput{Name: "x", Domain: "ACME.nexadmin.DEV", PackageID: 5}
	err := s.check(tctx, &in, 0)
	var e *response.Error
	if !errors.As(err, &e) || e.Status != 409 || e.Key != "tenant.domain_taken" {
		t.Fatalf("want 409 tenant.domain_taken, got %v", err)
	}
	if in.Domain != "acme.nexadmin.dev" {
		t.Fatalf("domain should be normalized in place, got %q", in.Domain)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRejectsReservedDomainBeforeQuery(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectQuery("FROM `sys_tenant_pkg`").WillReturnRows(pkgRows())
	in := TenantInput{Name: "x", Domain: "localhost", PackageID: 5}
	if err := s.check(tctx, &in, 0); err == nil || err.Error() != "tenant.domain_reserved" {
		t.Fatalf("got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

const secretPwd = "S3cret-Passw0rd!"

func adminInput() TenantInput {
	return TenantInput{Name: "Acme", Domain: "acme.nexadmin.dev", PackageID: 5, ExpireAt: "2027-01-01", Status: "active",
		CreateAdmin: true, AdminUsername: "acme_admin", AdminPassword: secretPwd, AdminEmail: "root@acme.dev"}
}

func expectCreatePrelude(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("FROM `sys_tenant_pkg`").WillReturnRows(pkgRows())
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_tenant`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
}

// expectSeedBase 覆盖开通初始化：根部门、默认岗位、普通员工角色与基础菜单。
func expectSeedBase(mock sqlmock.Sqlmock) {
	empty := func() *sqlmock.Rows { return sqlmock.NewRows([]string{"count"}).AddRow(0) }
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_dept`").WillReturnRows(empty())
	mock.ExpectExec("INSERT INTO `sys_dept`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_post`").WillReturnRows(empty())
	mock.ExpectExec("INSERT INTO `sys_post`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("FROM `sys_role` WHERE tenant_id = \\? AND code = \\?").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("INSERT INTO `sys_role`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("FROM `sys_menu`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	mock.ExpectExec("INSERT INTO `sys_role_menu`").WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestCreateTenantWithAdminCommits(t *testing.T) {
	s, mock := newSvc(t)
	expectCreatePrelude(mock)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_tenant`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("FROM `sys_role` WHERE tenant_id = \\? AND code = \\?").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("INSERT INTO `sys_role`").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		adminRoleName, adminRoleCode, 10, "1", "all", true, false, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `sys_user`").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		"acme_admin", pwdIsHashed{}, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "root@acme.dev", sqlmock.AnyArg(), sqlmock.AnyArg(),
		"1", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `sys_user_role`").WillReturnResult(sqlmock.NewResult(1, 1))
	expectSeedBase(mock)
	mock.ExpectCommit()

	got, err := s.CreateTenant(tctx, adminInput())
	if err != nil {
		t.Fatal(err)
	}
	if got.AdminUsername != "acme_admin" || got.Domain != "acme.nexadmin.dev" {
		t.Fatalf("tenant = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// pwdIsHashed 断言写库的是 bcrypt 哈希而不是明文。
type pwdIsHashed struct{}

func (pwdIsHashed) Match(v driver.Value) bool {
	s, ok := v.(string)
	return ok && s != secretPwd && strings.HasPrefix(s, "$2")
}

func TestCreateTenantAdminFailureRollsBackEverything(t *testing.T) {
	s, mock := newSvc(t)
	expectCreatePrelude(mock)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_tenant`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("FROM `sys_role`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("INSERT INTO `sys_role`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `sys_user`").WillReturnError(errors.New("duplicate entry"))
	mock.ExpectRollback() // 租户行与角色行随之回滚，没有 Commit

	got, err := s.CreateTenant(tctx, adminInput())
	if err == nil || got != nil {
		t.Fatalf("want error, got %v %v", got, err)
	}
	if strings.Contains(err.Error(), secretPwd) {
		t.Fatal("error must not contain the initial password")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTenantUserRoleFailureRollsBack(t *testing.T) {
	s, mock := newSvc(t)
	expectCreatePrelude(mock)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_tenant`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("FROM `sys_role`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("INSERT INTO `sys_role`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `sys_user`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `sys_user_role`").WillReturnError(errors.New("boom"))
	mock.ExpectRollback()
	if _, err := s.CreateTenant(tctx, adminInput()); err == nil {
		t.Fatal("want error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTenantReusesExistingAdminRole(t *testing.T) {
	s, mock := newSvc(t)
	expectCreatePrelude(mock)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_tenant`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("FROM `sys_role`").WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "code"}).AddRow(77, 1, "admin"))
	mock.ExpectExec("INSERT INTO `sys_user`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `sys_user_role`").WithArgs(sqlmock.AnyArg(), int64(77)).WillReturnResult(sqlmock.NewResult(1, 1))
	expectSeedBase(mock)
	mock.ExpectCommit()
	if _, err := s.CreateTenant(tctx, adminInput()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTenantWithoutAdminOnlyInsertsTenant(t *testing.T) {
	s, mock := newSvc(t)
	expectCreatePrelude(mock)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_tenant`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	in := adminInput()
	in.CreateAdmin, in.AdminUsername, in.AdminPassword = false, "", ""
	if _, err := s.CreateTenant(tctx, in); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminPlanValidation(t *testing.T) {
	base := adminInput()
	bad := map[string]func(*TenantInput){
		"empty password": func(i *TenantInput) { i.AdminPassword = "" },
		"long password":  func(i *TenantInput) { i.AdminPassword = strings.Repeat("a", 73) },
		"bad username":   func(i *TenantInput) { i.AdminUsername = "a b" },
		"empty username": func(i *TenantInput) { i.AdminUsername = "" },
		"bad email":      func(i *TenantInput) { i.AdminEmail = "not-an-email" },
	}
	for name, mut := range bad {
		in := base
		mut(&in)
		_, err := newAdminPlan(in)
		var e *response.Error
		if !errors.As(err, &e) || e.Key != "tenant.admin_invalid" {
			t.Errorf("%s: got %v", name, err)
		}
		if err != nil && in.AdminPassword != "" && strings.Contains(err.Error(), in.AdminPassword) {
			t.Errorf("%s: error leaks password", name)
		}
	}
	p, err := newAdminPlan(base)
	if err != nil || p.hash == "" || p.hash == secretPwd {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	in := base
	in.AdminPassword = "a" // 不设强度规则：1 个字符也可以
	if _, err := newAdminPlan(in); err != nil {
		t.Fatalf("weak password must be accepted: %v", err)
	}
	in = base
	in.AdminEmail = ""
	if _, err := newAdminPlan(in); err != nil {
		t.Fatalf("email is optional: %v", err)
	}
}

func tenantRow(pkg any, maxUsers, maxRoles, maxStorage any, status string, expire time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "domain", "package_id", "expire_at", "status", "admin_username", "max_users", "max_roles", "max_storage_mb"}).
		AddRow(9, "T", "t.nexadmin.dev", pkg, expire, status, "a", maxUsers, maxRoles, maxStorage)
}

// expectConfig 预期依次读取 grace_days、expire_warn_days 两个参数；空串表示参数不存在。
func expectConfig(mock sqlmock.Sqlmock, grace, warn string) {
	for _, v := range []string{grace, warn} {
		rows := sqlmock.NewRows([]string{"config_value"})
		if v != "" {
			rows.AddRow(v)
		}
		mock.ExpectQuery("FROM `sys_config`").WillReturnRows(rows)
	}
}

func unordered(mock sqlmock.Sqlmock) { mock.MatchExpectationsInOrder(false) }

func TestTenantInfoEffectiveQuotas(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	// 沿用套餐
	s, mock := newSvc(t)
	unordered(mock)
	mock.ExpectQuery("FROM `sys_tenant`").WillReturnRows(tenantRow(5, nil, nil, nil, "active", exp))
	expectConfig(mock, "", "")
	mock.ExpectQuery("FROM `sys_tenant_pkg`").WillReturnRows(sqlmock.NewRows([]string{"id", "menu_ids", "max_users", "max_roles", "max_storage_mb"}).AddRow(5, "[101,102]", 10, 3, 100))
	info, err := s.TenantInfo(tctx, 9)
	if err != nil || info == nil {
		t.Fatal(info, err)
	}
	if info.MaxUsers != 10 || info.MaxRoles != 3 || info.MaxStorageMB != 100 || len(info.MenuIDs) != 2 || info.GraceDays != 0 || info.WarnDays != 15 {
		t.Fatalf("inherit: %+v", info)
	}

	// 租户覆盖（0 = 不限制，也是覆盖）
	s, mock = newSvc(t)
	unordered(mock)
	mock.ExpectQuery("FROM `sys_tenant`").WillReturnRows(tenantRow(5, 0, 7, 2048, "active", exp))
	expectConfig(mock, "3", "3")
	mock.ExpectQuery("FROM `sys_tenant_pkg`").WillReturnRows(sqlmock.NewRows([]string{"id", "menu_ids", "max_users", "max_roles", "max_storage_mb"}).AddRow(5, "[]", 10, 3, 100))
	info, err = s.TenantInfo(tctx, 9)
	if err != nil {
		t.Fatal(err)
	}
	if info.MaxUsers != 0 || info.MaxRoles != 7 || info.MaxStorageMB != 2048 || info.MenuIDs != nil {
		t.Fatalf("override: %+v", info)
	}
	if info.GraceDays != 3 || info.WarnDays != 3 { // 两个参数键都读到了 "3"
		t.Fatalf("config days: grace=%d warn=%d", info.GraceDays, info.WarnDays)
	}

	// 无租户记录（默认租户）
	s, mock = newSvc(t)
	mock.ExpectQuery("FROM `sys_tenant`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if info, err := s.TenantInfo(tctx, 1); info != nil || err != nil {
		t.Fatalf("unmanaged tenant: %v %v", info, err)
	}
}

func TestTenantByDomainLowercases(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectQuery("WHERE LOWER\\(domain\\) = \\?").WithArgs("acme.nexadmin.dev", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
	id, ok, err := s.TenantByDomain(context.Background(), "ACME.nexadmin.dev")
	if err != nil || !ok || id != 42 {
		t.Fatalf("got %v %v %v", id, ok, err)
	}
	mock.ExpectQuery("WHERE LOWER\\(domain\\)").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, ok, _ := s.TenantByDomain(context.Background(), "none.dev"); ok {
		t.Fatal("unbound domain must miss")
	}
}

func TestUsageAggregation(t *testing.T) {
	s, mock := newSvc(t)
	unordered(mock)
	ctenant.StorageMeter.Invalidate(9)
	exp := time.Now().Add(10*24*time.Hour - time.Hour)
	mock.ExpectQuery("FROM `sys_tenant`").WillReturnRows(tenantRow(5, nil, nil, nil, "active", exp))
	expectConfig(mock, "", "")
	mock.ExpectQuery("FROM `sys_tenant_pkg`").WillReturnRows(sqlmock.NewRows([]string{"id", "menu_ids", "max_users", "max_roles", "max_storage_mb"}).AddRow(5, "[]", 10, 3, 100))
	mock.ExpectQuery("FROM `sys_user` WHERE tenant_id = \\? AND deleted_at IS NULL").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))
	mock.ExpectQuery("FROM `sys_role` WHERE tenant_id = \\?").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery("COALESCE\\(SUM\\(size\\), 0\\).*FROM `sys_file` WHERE tenant_id = \\? AND storage_path <> ''").
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(int64(3*1024*1024 + 1)))

	u, err := s.Usage(tctx, 9)
	if err != nil {
		t.Fatal(err)
	}
	if u.Users != 4 || u.MaxUsers != 10 || u.Roles != 2 || u.MaxRoles != 3 || u.StorageMB != 4 || u.MaxStorageMB != 100 || u.DaysLeft != 10 {
		t.Fatalf("usage = %+v", u)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	// 第二次：存储汇总命中缓存，不再查 sys_file
	mock.ExpectQuery("FROM `sys_tenant`").WillReturnRows(tenantRow(5, nil, nil, nil, "active", exp))
	expectConfig(mock, "", "")
	mock.ExpectQuery("FROM `sys_tenant_pkg`").WillReturnRows(sqlmock.NewRows([]string{"id", "menu_ids", "max_users", "max_roles", "max_storage_mb"}).AddRow(5, "[]", 10, 3, 100))
	mock.ExpectQuery("FROM `sys_user`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery("FROM `sys_role`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	if u, err = s.Usage(tctx, 9); err != nil || u.Users != 5 || u.StorageMB != 4 {
		t.Fatalf("cached storage: %+v %v", u, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUsageMissingFileTableIsZero(t *testing.T) {
	s, mock := newSvc(t)
	ctenant.StorageMeter.Invalidate(9)
	mock.ExpectQuery("FROM `sys_file`").WillReturnError(errors.New("Error 1146: Table 'x.sys_file' doesn't exist"))
	n, err := s.storageBytes(tctx, 9)
	if err != nil || n != 0 {
		t.Fatalf("got %d %v", n, err)
	}
	mock.ExpectQuery("FROM `sys_file`").WillReturnError(errors.New("connection refused"))
	if _, err := s.storageBytes(tctx, 9); err == nil {
		t.Fatal("other errors must propagate")
	}
}

func TestUsageUnknownTenant(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectQuery("FROM `sys_tenant`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := s.Usage(tctx, 404); err == nil {
		t.Fatal("want 404")
	}
}
