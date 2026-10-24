package user

import (
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"golang.org/x/crypto/bcrypt"

	"nexadmin/core/auth"
)

// ---- SCIM 供号通道：guardTarget 不给超管豁免 ----

func TestGuardTargetProvisionActorHasNoSuperExemption(t *testing.T) {
	s, mock, ctx := newScopeSvc(t)
	super := &User{IsSuper: true}
	super.ID = 2
	if _, ok := isForbidden(s.guardTarget(ctx, provisionActor, super)); !ok {
		t.Error("provision actor must not touch a super admin")
	}
	holder := &User{}
	holder.ID = 3
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	if _, ok := isForbidden(s.guardTarget(ctx, provisionActor, holder)); !ok {
		t.Error("provision actor must not touch a builtin-role holder")
	}
	plain := &User{}
	plain.ID = 4
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	if err := s.guardTarget(ctx, provisionActor, plain); err != nil {
		t.Errorf("plain account must be allowed: %v", err)
	}
	// 与 provision actor 同 ID 的目标也不享受“自己”豁免
	self := &User{IsSuper: true}
	self.ID = 0
	if _, ok := isForbidden(s.guardTarget(ctx, provisionActor, self)); !ok {
		t.Error("self exemption must not apply to the provision actor")
	}
	// 普通超管操作者行为不变
	if err := s.guardTarget(ctx, actor{ID: 9, Super: true}, super); err != nil {
		t.Errorf("real super admin keeps the exemption: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ---- matchExisting 不得绑定超管与内置角色持有者 ----

func TestResolveExternalMatchExistingRefusesBuiltinRoleHolder(t *testing.T) {
	e := newEnv(t)
	m := &module{}
	m.deps.Store(e.d)
	ext := auth.ExternalIdentity{ProviderKey: "ldapu-5", Subject: "admin2", Username: "admin2", MatchExisting: true}
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"id"})) // 新键未绑定
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"id"})) // 旧键未绑定
	e.mock.ExpectQuery("FROM `sys_user` WHERE username").WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "username", "status", "is_super"}).AddRow(7, 1, "admin2", "1", false))
	e.mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	if _, err := m.ResolveExternalUser(e.ctx, "ldap", ext); err != errExternalDenied {
		t.Fatalf("a builtin-role holder must never be matched by name, err=%v", err)
	}
	e.done(t) // 严格顺序：拒绝后没有任何 INSERT / UPDATE
}

func TestResolveExternalMatchExistingStillBindsPlainAccount(t *testing.T) {
	e := newEnv(t)
	m := &module{}
	m.deps.Store(e.d)
	ext := auth.ExternalIdentity{ProviderKey: "ldapu-5", Subject: "grace", Username: "grace", MatchExisting: true}
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	e.mock.ExpectQuery("FROM `sys_user` WHERE username").WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "username", "status", "is_super"}).AddRow(8, 1, "grace", "1", false))
	e.mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0)) // hasIdentity(新键)
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0)) // hasIdentity(旧键)
	e.mock.ExpectBegin()
	e.mock.ExpectExec("INSERT INTO `sys_user_identity`").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		sqlmock.AnyArg(), "ldapu-5", "grace", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	e.mock.ExpectCommit()
	e.mock.ExpectBegin()
	e.mock.ExpectExec("UPDATE `sys_user`").WillReturnResult(sqlmock.NewResult(1, 1))
	e.mock.ExpectCommit()
	e.mock.ExpectQuery("sys_user_role").WillReturnRows(sqlmock.NewRows([]string{"code", "builtin"}).AddRow("guest", false))
	e.mock.ExpectQuery("DISTINCT m.perm").WillReturnRows(sqlmock.NewRows([]string{"perm"}))
	u, err := m.ResolveExternalUser(e.ctx, "ldap", ext)
	if err != nil || u.Username != "grace" || u.LoginMethod != "ldap" {
		t.Fatalf("u=%+v err=%v", u, err)
	}
	e.done(t)
}

// ---- 绑定键带 source id；存量旧键在本次登录迁移 ----

func TestResolveExternalAdoptsLegacyBinding(t *testing.T) {
	e := newEnv(t)
	m := &module{}
	m.deps.Store(e.d)
	ext := auth.ExternalIdentity{ProviderKey: "ldapu-5", Subject: "alice", Username: "alice"}
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"id"})) // 新键：没有
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "user_id", "provider", "external_id"}).
		AddRow(9, 1, 5, "ldap", "alice")) // 旧键：存量绑定
	e.mock.ExpectQuery("FROM `sys_user` WHERE id").WillReturnRows(userRows("1", 5))
	e.mock.ExpectBegin()
	e.mock.ExpectExec("UPDATE `sys_user_identity` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	e.mock.ExpectCommit()
	e.mock.ExpectBegin()
	e.mock.ExpectExec("UPDATE `sys_user`").WillReturnResult(sqlmock.NewResult(1, 1))
	e.mock.ExpectCommit()
	e.mock.ExpectQuery("sys_user_role").WillReturnRows(sqlmock.NewRows([]string{"code", "builtin"}).AddRow("guest", false))
	e.mock.ExpectQuery("DISTINCT m.perm").WillReturnRows(sqlmock.NewRows([]string{"perm"}))
	u, err := m.ResolveExternalUser(e.ctx, "ldap", ext)
	if err != nil || u.ID != 5 || u.LoginMethod != "ldap" {
		t.Fatalf("an already-bound user must still log in after the upgrade: u=%+v err=%v", u, err)
	}
	e.done(t)
}

func TestResolveExternalScopedBindingDoesNotCrossSources(t *testing.T) {
	e := newEnv(t)
	m := &module{}
	m.deps.Store(e.d)
	// 源 6 的同名用户：新键未绑定、旧键未绑定、未开启匹配 / 建号 -> 拒绝；不会借用源 5 的绑定。
	ext := auth.ExternalIdentity{ProviderKey: "ldapu-6", Subject: "alice", Username: "alice"}
	e.mock.ExpectQuery("sys_user_identity").WithArgs("ldapu-6", "alice", 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	e.mock.ExpectQuery("sys_user_identity").WithArgs("ldap", "alice", 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := m.ResolveExternalUser(e.ctx, "ldap", ext); err != errExternalDenied {
		t.Fatalf("err=%v", err)
	}
	e.done(t)
}

func TestResolveExternalRejectsMalformedProviderKeys(t *testing.T) {
	e := newEnv(t)
	m := &module{}
	m.deps.Store(e.d)
	for _, tc := range []struct {
		method string
		ext    auth.ExternalIdentity
	}{
		{"ldap", auth.ExternalIdentity{ProviderKey: "ldapu-", Subject: "a1", Username: "a1"}},
		{"ldap", auth.ExternalIdentity{ProviderKey: "ldapu-5x", Subject: "a1", Username: "a1"}},
		{"ldap", auth.ExternalIdentity{ProviderKey: "cas-5", Subject: "a1", Username: "a1"}}, // 与方式不一致
		{"cas", auth.ExternalIdentity{ProviderKey: "ldapu-5", Subject: "a1", Username: "a1"}},
		{"ldap", auth.ExternalIdentity{ProviderKey: "github", Subject: "a1", Username: "a1"}},
		{"cas", auth.ExternalIdentity{ProviderKey: "cas-" + strings.Repeat("9", 40), Subject: "a1", Username: "a1"}},
	} {
		if _, err := m.ResolveExternalUser(e.ctx, tc.method, tc.ext); err != errExternalDenied {
			t.Errorf("%s/%q must be denied, err=%v", tc.method, tc.ext.ProviderKey, err)
		}
	}
	e.done(t) // 一条 SQL 都不应发出
}

// ---- 目录已消失用户的停用 ----

func identRows(external ...string) *sqlmock.Rows {
	r := sqlmock.NewRows([]string{"id", "tenant_id", "user_id", "provider", "external_id"})
	for i, x := range external {
		r.AddRow(100+i, 1, 10+i, "ldap-5", x)
	}
	return r
}

func TestDeactivateMissingExternalGuardsAgainstShrunkDirectory(t *testing.T) {
	e := newEnv(t)
	m := &module{}
	m.deps.Store(e.d)
	e.mock.ExpectQuery("FROM `sys_user_identity`").WillReturnRows(identRows("uuid:a", "uuid:b", "uuid:c", "uuid:d"))
	n, err := m.DeactivateMissingExternal(e.ctx, "ldap-5", map[string]bool{"uuid:a": true}) // 4 个已绑定，只看到 1 个
	if err == nil || n != 0 {
		t.Fatalf("a directory that shrank below half must abort, n=%d err=%v", n, err)
	}
	e.done(t) // 没有任何 UPDATE
}

func TestDeactivateMissingExternalOnlyTouchesBoundUnprotectedAccounts(t *testing.T) {
	e := newEnv(t)
	m := &module{}
	m.deps.Store(e.d)
	e.mock.ExpectQuery("FROM `sys_user_identity`").WillReturnRows(identRows("uuid:a", "uuid:b", "uuid:c"))
	userRow := func(id int64) *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "tenant_id", "username", "status"}).AddRow(id, 1, "u", "1")
	}
	// uuid:b 已消失，普通账号 -> 停用；uuid:c 已消失但持有内置角色 -> 跳过
	e.mock.ExpectQuery("FROM `sys_user` WHERE").WillReturnRows(userRow(11))
	e.mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	e.mock.ExpectBegin()
	e.mock.ExpectExec("UPDATE `sys_user` SET `status`").WillReturnResult(sqlmock.NewResult(0, 1))
	e.mock.ExpectCommit()
	e.mock.ExpectQuery("FROM `sys_user` WHERE").WillReturnRows(userRow(12))
	e.mock.ExpectQuery("SELECT count\\(\\*\\) FROM sys_user_role AS ur").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	n, err := m.DeactivateMissingExternal(e.ctx, "ldap-5", map[string]bool{"uuid:a": true, "uuid:x": true})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	e.done(t)
	if _, err := m.DeactivateMissingExternal(e.ctx, "github", nil); err != errExternalDenied {
		t.Fatalf("only directory-scoped providers may be deactivated, err=%v", err)
	}
}

// ---- SCIM 供号不使用共享默认口令 ----

func TestUnusablePasswordHashIsRandomAndStrong(t *testing.T) {
	a, err := unusablePasswordHash()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := unusablePasswordHash()
	if a == b || !strings.HasPrefix(a, "$2") {
		t.Fatalf("hashes must be unique bcrypt values: %q %q", a, b)
	}
	for _, guess := range []string{"", "123456", "Admin@123"} {
		if bcrypt.CompareHashAndPassword([]byte(a), []byte(guess)) == nil {
			t.Fatalf("guessable password %q matches", guess)
		}
	}
}

func TestProvisionedCreateIgnoresDefaultPassword(t *testing.T) {
	s, mock, ctx := newScopeSvc(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(0)) // usernameTaken
	// 严格顺序：若实现仍读取 user.default_password，这条会先被消费，事务 Begin 就不会是“下一个意外调用”。
	mock.ExpectQuery("sys_config").WillReturnRows(sqlmock.NewRows([]string{"config_value"}).AddRow("Shared#Default1"))
	_, err := s.create(ctx, userInput{Username: "scim.user", Status: statusOn, NoPassword: true}, provisionActor)
	if err == nil || !strings.Contains(err.Error(), "Begin") {
		t.Fatalf("create must proceed straight to the transaction without reading sys_config, err=%v", err)
	}
	if mock.ExpectationsWereMet() == nil {
		t.Fatal("sys_config must not be consulted for provisioned users")
	}
}
