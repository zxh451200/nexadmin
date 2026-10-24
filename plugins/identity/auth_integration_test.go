package identity

import (
	"errors"
	"testing"

	"nexadmin/core/auth"
	"nexadmin/core/response"
)

// LDAP 与真实 auth.Service 的集成：注册校验器 -> TryExternal -> ResolveExternal（用户模块规则的替身）。
func TestLDAPWithRealAuthService(t *testing.T) {
	e := newLDAPEnv(t, append(append([]testEntry{}, testUsers...), testEntry{dn: "uid=root,ou=people,dc=example,dc=com", password: "root-pw",
		attrs: map[string][]string{"objectclass": {"person"}, "uid": {"root"}}}))
	svc, prov, _ := newAuthService(t)
	if err := svc.RegisterExternalAuthenticator(e.a); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := svc.RegisterExternalAuthenticator(e.a); err == nil {
		t.Fatal("registering the same method twice must fail")
	}
	prov.add(1, "bob", false)
	prov.add(1, "root", true) // 超管：目录里有同名账号也永不匹配
	ctx := tctx(1)
	try := func(u, p string) (*auth.User, error) {
		m, id := svc.TryExternal(ctx, u, p, nil)
		if id == nil {
			return nil, errors.New("not authenticated")
		}
		if m != "ldap" {
			t.Fatalf("method = %q", m)
		}
		return svc.ResolveExternal(ctx, m, *id)
	}
	// 1) 匹配已有同名账号
	if u, err := try("bob", "bob-pw"); err != nil || u.Username != "bob" || u.LoginMethod != "ldap" {
		t.Fatalf("match existing: %+v %v", u, err)
	}
	// 2) 超管永不匹配（即使目录口令正确）
	if u, err := try("root", "root-pw"); err == nil {
		t.Fatalf("super admin must never be matched: %+v", u)
	}
	// 3) 自动建号：默认角色来自配置；再次登录走已绑定路径
	if u, err := try("alice", "alice-pw"); err != nil || u.Username != "alice" || len(u.Roles) != 1 || u.Roles[0] != "staff" {
		t.Fatalf("auto-create: %+v %v", u, err)
	}
	if u, err := try("alice", "alice-pw"); err != nil || u.Username != "alice" {
		t.Fatalf("second login: %+v %v", u, err)
	}
	// 4) 额度：用户数已满则不再建号
	prov.quota[1] = 3 // bob, root, alice 已占满
	prov.add(1, "pad", false)
	_, err := try("dup2x", "x") // 不存在的目录账号 -> 根本不会通过目录校验
	if err == nil {
		t.Fatal("unknown directory user must not log in")
	}
	e.srv.mu.Lock()
	e.srv.entries = append(e.srv.entries, testEntry{dn: "uid=carol,ou=people,dc=example,dc=com", password: "c-pw",
		attrs: map[string][]string{"objectclass": {"person"}, "uid": {"carol"}}})
	e.srv.mu.Unlock()
	var re *response.Error
	if _, err := try("carol", "c-pw"); !errors.As(err, &re) || re.Status != 403 {
		t.Fatalf("quota must block auto-create: %v", err)
	}
	// 5) 租户隔离：同一目录用户在租户 2 的登录不会使用租户 1 的源
	if m, id := svc.TryExternal(tctx(2), "bob", "bob-pw", nil); id != nil || m != "" {
		t.Fatalf("tenant 2 must not authenticate against tenant 1's source: %q %v", m, id)
	}
	// 6) 不开启建号 / 匹配的源：校验器通过，但 core 拒绝落地
	e2 := newLDAPEnv(t, testUsers)
	cfg := baseCfgOf(t, e2)
	cfg.MatchExisting, cfg.AutoCreate = false, false
	cfg.BindPassword = ""
	if _, err := e2.svc.Save(tctx(1), mustFirstID(t, e2), SourceForm{Name: "n", Enabled: true, Config: mustJSON(t, cfg)}); err != nil {
		t.Fatal(err)
	}
	ident, err := e2.a.Authenticate(tctx(1), "bob", "bob-pw")
	if err != nil || ident.MatchExisting || ident.AutoCreate {
		t.Fatalf("flags must follow config (default off): %+v %v", ident, err)
	}
	svc2, prov2, _ := newAuthService(t)
	prov2.add(1, "bob", false)
	if _, err := svc2.ResolveExternal(ctx, "ldap", *ident); err == nil {
		t.Fatal("with matching and auto-create off, an unbound directory user must be refused")
	}
}

func TestZeroValueIdentityNeverCreatesAccounts(t *testing.T) {
	initTest(t)
	// 配置里不开 autoCreate / matchExisting（默认）时，身份只带 Subject 与 Username
	r := &casResult{User: "newbie"}
	id, err := r.identity(CASConfig{})
	if err != nil || id.AutoCreate || id.MatchExisting || id.DefaultRole != "" {
		t.Fatalf("%+v %v", id, err)
	}
}
