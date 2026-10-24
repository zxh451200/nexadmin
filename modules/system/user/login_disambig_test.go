package user

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/auth"
)

// username 的唯一约束是 (tenant_id, username)：跨租户同名合法，
// 登录必须按密码在候选中消歧，而不是 First() 命中任意一条。
func TestPasswordLoginDisambiguatesSameNameAcrossTenants(t *testing.T) {
	e := newEnv(t)
	newAuthSvc(t, e)
	m := &module{}
	m.deps.Store(e.d)

	pwA, _ := auth.HashPassword("password-a")
	pwB, _ := auth.HashPassword("password-b")

	e.mock.ExpectQuery("FROM .sys_user.").WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "username", "password", "status", "is_super"}).
			AddRow(11, 1, "tadmin", pwA, statusOn, true).
			AddRow(22, 2, "tadmin", pwB, statusOn, true))
	e.mock.ExpectBegin()
	e.mock.ExpectExec("UPDATE .sys_user.").WillReturnResult(sqlmock.NewResult(0, 1))
	e.mock.ExpectCommit()
	e.mock.ExpectQuery("sys_user_role").WillReturnRows(sqlmock.NewRows([]string{"code", "builtin"}))

	au, err := m.passwordLogin(e.ctx, auth.LoginRequest{Type: "password", Username: "tadmin", Password: "password-b"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if au.ID != 22 || au.TenantID != 2 {
		t.Fatalf("want user 22 of tenant 2, got user %v of tenant %v", au.ID, au.TenantID)
	}
	e.done(t)
}

// 候选存在但密码全不匹配：仍走统一失败路径（与单用户行为一致），不 panic、不泄露存在性。
func TestPasswordLoginSameNameAllMismatchFails(t *testing.T) {
	e := newEnv(t)
	newAuthSvc(t, e)
	m := &module{}
	m.deps.Store(e.d)

	pwA, _ := auth.HashPassword("password-a")

	e.mock.ExpectQuery("FROM .sys_user.").WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "username", "password", "status", "is_super"}).
			AddRow(11, 1, "tadmin", pwA, statusOn, true).
			AddRow(22, 2, "tadmin", pwA, statusOn, true))

	_, err := m.passwordLogin(e.ctx, auth.LoginRequest{Type: "password", Username: "tadmin", Password: "wrong"})
	if err == nil {
		t.Fatal("want invalid credentials error")
	}
	e.done(t)
}
