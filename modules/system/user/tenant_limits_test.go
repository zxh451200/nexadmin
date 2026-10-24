package user

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 这些测试用 sqlmock 校验 SQL 与事务边界，没有连接真实数据库。

type limitSource map[idgen.ID]*tenant.Info

func (l limitSource) TenantInfo(_ context.Context, id idgen.ID) (*tenant.Info, error) {
	return l[id], nil
}
func (limitSource) TenantByDomain(context.Context, string) (idgen.ID, bool, error) {
	return 0, false, nil
}

func useSource(t *testing.T, s limitSource) {
	t.Helper()
	tenant.SetSource(s)
	t.Cleanup(func() { tenant.SetSource(nil) })
}

func quotaReason(err error) string {
	var e *response.Error
	if errors.As(err, &e) && e.Status == 403 && e.Key == "tenant.quota_users" {
		return e.Reason
	}
	return ""
}

func countRows(n int) *sqlmock.Rows { return sqlmock.NewRows([]string{"count"}).AddRow(n) }

// expectTenantLock 期望额度检查先对租户行加锁（SELECT ... FOR UPDATE），之后才统计。
func expectTenantLock(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT id FROM `sys_tenant` WHERE id = \\? LIMIT \\? FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
}

func TestEnsureUserQuota(t *testing.T) {
	useSource(t, limitSource{1: {ID: 1, Status: "active", MaxUsers: 3}})
	s, mock, ctx := newScopeSvc(t)

	expectTenantLock(mock)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(2))
	if err := ensureUserQuota(ctx, s.db, 1); err != nil {
		t.Fatalf("3rd user must pass: %v", err)
	}
	expectTenantLock(mock)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(3))
	if got := quotaReason(ensureUserQuota(ctx, s.db, 1)); got != tenant.ReasonQuotaUsers {
		t.Fatalf("4th user must be rejected, reason=%q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureUserQuotaNoQueryWhenUnlimited(t *testing.T) {
	useSource(t, limitSource{1: {ID: 1, Status: "active"}, 2: {ID: 2, Status: "active", MaxUsers: 1}})
	s, mock, ctx := newScopeSvc(t)
	if err := ensureUserQuota(ctx, s.db, 1); err != nil { // 0 = 不限，不应该有任何 SQL
		t.Fatal(err)
	}
	if err := ensureUserQuota(tenant.Skip(ctx), s.db, 1); err != nil { // 平台级操作不受限
		t.Fatal(err)
	}
	if err := ensureUserQuota(tenant.With(ctx, tenant.DefaultID+100), s.db, 1); err != nil { // 不受管租户
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateUserRejectedOverQuotaAndRollsBack(t *testing.T) {
	useSource(t, limitSource{1: {ID: 1, Status: "active", MaxUsers: 3}})
	s, mock, ctx := newScopeSvc(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(0)) // usernameTaken
	mock.ExpectQuery("sys_config").WillReturnRows(sqlmock.NewRows([]string{"config_value"}))
	mock.ExpectBegin()
	expectTenantLock(mock)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(3)) // 额度统计
	mock.ExpectRollback()

	_, err := s.create(ctx, userInput{Username: "newbie", Nickname: "n"}, actor{ID: 1, Super: true})
	if quotaReason(err) != tenant.ReasonQuotaUsers {
		t.Fatalf("want quota rejection, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestImportReportsQuotaReasonPerRow(t *testing.T) {
	useSource(t, limitSource{1: {ID: 1, Status: "active", MaxUsers: 1}})
	s, mock, ctx := newScopeSvc(t)
	for _, tbl := range []string{"sys_role", "sys_dept", "sys_post"} { // loadLookups
		mock.ExpectQuery(tbl).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "code"}))
	}
	for i := 0; i < 2; i++ {
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(0))
		mock.ExpectQuery("sys_config").WillReturnRows(sqlmock.NewRows([]string{"config_value"}))
		mock.ExpectBegin()
		expectTenantLock(mock)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(1))
		mock.ExpectRollback()
	}
	res, err := s.importUsers(ctx, []importRow{{Row: 2, Username: "alice"}, {Row: 3, Username: "bob"}}, nil, actor{ID: 1, Super: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success != 0 || len(res.Failed) != 2 {
		t.Fatalf("result = %+v", res)
	}
	for _, f := range res.Failed {
		if f.Reason == "" || f.Reason == "写入失败" {
			t.Fatalf("quota failure must carry the i18n reason, got %q", f.Reason)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func menuRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "parent_id", "type", "perm"}).
		AddRow(1, 0, "menu", "user:list").AddRow(2, 1, "button", "user:create").
		AddRow(3, 0, "menu", "role:list").AddRow(4, 3, "button", "role:create").
		AddRow(5, 0, "menu", "dashboard:view").AddRow(6, 0, "dir", "")
}

func TestRestrictByPackageIntersection(t *testing.T) {
	const tid = idgen.ID(9)
	useSource(t, limitSource{tid: {ID: tid, Status: "active", MenuIDs: []idgen.ID{1}}})
	s, mock, ctx := newScopeSvc(t)
	db := s.db.WithContext(ctx)
	u := &User{IsSuper: false}
	u.TenantID = tid

	// 角色权限 ∩ 套餐菜单
	mock.ExpectQuery("FROM `sys_menu`").WillReturnRows(menuRows())
	out, err := restrictByPackage(db, u, &auth.User{Perms: []string{"user:list", "role:list", "role:create", "dashboard:view"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Perms) != 2 || out.Perms[0] != "dashboard:view" || out.Perms[1] != "user:list" {
		t.Fatalf("perms = %v", out.Perms)
	}

	// 内置管理员的 "*" 展开为套餐允许的权限（含已勾选菜单下的按钮与平台级权限）
	mock.ExpectQuery("FROM `sys_menu`").WillReturnRows(menuRows())
	out, _ = restrictByPackage(db, u, &auth.User{Perms: []string{"*"}})
	if len(out.Perms) != 3 {
		t.Fatalf("star perms = %v", out.Perms)
	}
	for _, p := range out.Perms {
		if p == "role:list" || p == "role:create" {
			t.Fatalf("perm outside package leaked: %v", out.Perms)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestrictByPackageExemptions(t *testing.T) {
	useSource(t, limitSource{
		9:  {ID: 9, Status: "active", MenuIDs: []idgen.ID{1}},
		10: {ID: 10, Status: "active"}, // 套餐没勾菜单：不限制
	})
	s, mock, ctx := newScopeSvc(t)
	db := s.db.WithContext(ctx)
	mk := func(tid idgen.ID, super bool) *User {
		u := &User{IsSuper: super}
		u.TenantID = tid
		return u
	}
	in := func() *auth.User { return &auth.User{Perms: []string{"*"}} }

	for name, u := range map[string]*User{
		"super admin":      mk(9, true),
		"default tenant":   mk(tenant.DefaultID, false),
		"unrestricted pkg": mk(10, false),
		"unmanaged tenant": mk(77, false),
	} {
		out, err := restrictByPackage(db, u, in())
		if err != nil || len(out.Perms) != 1 || out.Perms[0] != "*" {
			t.Errorf("%s: perms=%v err=%v", name, out.Perms, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil { // 以上情况都不应查询菜单表
		t.Fatal(err)
	}
}
