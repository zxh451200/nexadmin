package identity

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type ldapEnv struct {
	srv *testLDAP
	svc *Service
	st  *memStore
	a   *ldapAuthenticator
}

func newLDAPEnv(t *testing.T, users []testEntry) *ldapEnv {
	t.Helper()
	initTest(t)
	srv := newTestLDAP(t, users, nil, false)
	st := newMemStore()
	svc := NewService(st, nil)
	cfg := baseCfg(srv)
	cfg.MatchExisting, cfg.AutoCreate, cfg.DefaultRole = true, true, "staff"
	addLDAP(t, svc, 1, cfg, true)
	return &ldapEnv{srv: srv, svc: svc, st: st, a: &ldapAuthenticator{svc: svc, dial: svc.dial}}
}

func TestLDAPLoginSearchThenBind(t *testing.T) {
	e := newLDAPEnv(t, testUsers)
	id, err := e.a.Authenticate(tctx(1), "alice", "alice-pw")
	if err != nil || id == nil {
		t.Fatalf("id=%v err=%v", id, err)
	}
	if id.Username != "alice" || id.Subject != "alice" || id.Nickname != "Alice A" || id.Email != "alice@example.com" || id.Phone != "13800000001" {
		t.Fatalf("identity = %+v", id)
	}
	if !id.MatchExisting || !id.AutoCreate || id.DefaultRole != "staff" {
		t.Fatalf("provisioning flags not carried: %+v", id)
	}
	binds := e.srv.bindLog()
	if len(binds) != 2 || binds[0].dn != "cn=svc,dc=example,dc=com" || binds[1].dn != "uid=alice,ou=people,dc=example,dc=com" || !binds[1].succeeded {
		t.Fatalf("binds = %+v (want service bind, then user bind with the DN found by search)", binds)
	}
	for _, c := range []struct{ u, p string }{{"alice", "wrong"}, {"nobody", "alice-pw"}, {"bob", "alice-pw"}} {
		if id, err := e.a.Authenticate(tctx(1), c.u, c.p); id != nil || !errors.Is(err, auth.ErrExternalRejected) {
			t.Fatalf("%v: id=%v err=%v", c, id, err)
		}
	}
}

func TestLDAPEmptyPasswordNeverReachesDirectory(t *testing.T) {
	e := newLDAPEnv(t, testUsers)
	e.srv.unauthSuccess = true // 目录会把“DN + 空口令”当成功；这是经典的空密码绕过
	for _, pw := range []string{""} {
		id, err := e.a.Authenticate(tctx(1), "alice", pw)
		if id != nil || !errors.Is(err, auth.ErrExternalRejected) {
			t.Fatalf("empty password must be rejected: id=%v err=%v", id, err)
		}
	}
	if len(e.srv.bindLog()) != 0 || len(e.srv.filterLog()) != 0 {
		t.Fatal("an empty password must be rejected before any directory traffic")
	}
	// 直接调用 verify 同样拒绝，且服务端没有收到任何空口令 bind
	if _, err := e.a.verify(tctx(1), baseCfgOf(t, e), "alice", ""); !errors.Is(err, errBadCredentials) {
		t.Fatalf("verify err = %v", err)
	}
	if _, err := e.a.Authenticate(tctx(1), "", "alice-pw"); !errors.Is(err, auth.ErrExternalRejected) {
		t.Fatalf("empty username err = %v", err)
	}
}

func baseCfgOf(t *testing.T, e *ldapEnv) LDAPConfig {
	t.Helper()
	srcs, _ := e.st.list(context.Background(), 1)
	cfg, err := srcs[0].ldapConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLDAPInjectionAndAmbiguity(t *testing.T) {
	users := append(append([]testEntry{}, testUsers...), testEntry{dn: "uid=weird,ou=people,dc=example,dc=com", password: "w",
		attrs: map[string][]string{"objectclass": {"person"}, "uid": {"we ird!"}}})
	e := newLDAPEnv(t, users)
	for _, evil := range []string{"*", "ali*", "*)(uid=*", "alice)(uid=bob", "x)(|(uid=alice", `alice\`, "a\x00b"} {
		// 密码取 alice 的真实密码：即使过滤器被注入、匹配到 alice，也必须因为转义而根本匹配不上
		if id, err := e.a.Authenticate(tctx(1), evil, "alice-pw"); id != nil || !errors.Is(err, auth.ErrExternalRejected) {
			t.Fatalf("%q: id=%v err=%v", evil, id, err)
		}
	}
	// 过滤器匹配多条：宁可拒绝
	if id, err := e.a.Authenticate(tctx(1), "dup", "x"); id != nil || !errors.Is(err, auth.ErrExternalRejected) {
		t.Fatalf("ambiguous: id=%v err=%v", id, err)
	}
	// 目录里的账号名不符合本地用户名规则：不做猜测式转换，作为错误（进日志）而不是登录成功
	if id, err := e.a.Authenticate(tctx(1), "we ird!", "w"); id != nil || err == nil {
		t.Fatalf("bad directory username: id=%v err=%v", id, err)
	}
}

func TestLDAPTenantIsolation(t *testing.T) {
	e := newLDAPEnv(t, testUsers) // 只有租户 1 配了源
	for name, ctx := range map[string]context.Context{
		"other tenant": tctx(2),
		"no tenant":    context.Background(),
		"skip tenant":  tenant.Skip(tctx(1)),
	} {
		if id, err := e.a.Authenticate(ctx, "alice", "alice-pw"); id != nil || !errors.Is(err, auth.ErrExternalRejected) {
			t.Fatalf("%s: id=%v err=%v", name, id, err)
		}
	}
	if n := len(e.srv.bindLog()); n != 0 {
		t.Fatalf("tenant 1's directory was contacted %d times for other tenants' logins", n)
	}
	// 租户 2 自己的源用自己的目录
	srv2 := newTestLDAP(t, []testEntry{{dn: "uid=zed,dc=t2", password: "zed-pw", attrs: map[string][]string{"objectclass": {"person"}, "uid": {"zed"}}},
		{dn: "cn=svc,dc=t2", password: "s2", attrs: map[string][]string{"objectclass": {"person"}}}}, nil, false)
	cfg2 := baseCfg(srv2)
	cfg2.BindDN, cfg2.BindPassword, cfg2.BaseDN = "cn=svc,dc=t2", "s2", "dc=t2"
	addLDAP(t, e.svc, 2, cfg2, true)
	if id, err := e.a.Authenticate(tctx(2), "zed", "zed-pw"); err != nil || id == nil || id.Username != "zed" {
		t.Fatalf("tenant 2 login: id=%v err=%v", id, err)
	}
	if id, _ := e.a.Authenticate(tctx(1), "zed", "zed-pw"); id != nil {
		t.Fatal("tenant 2's user must not log in under tenant 1")
	}
	if id, _ := e.a.Authenticate(tctx(2), "alice", "alice-pw"); id != nil {
		t.Fatal("tenant 1's user must not log in under tenant 2")
	}
}

func TestLDAPDisabledSourceAndInfraError(t *testing.T) {
	e := newLDAPEnv(t, testUsers)
	if _, err := e.svc.SetEnabled(tctx(1), mustFirstID(t, e), false); err != nil {
		t.Fatal(err)
	}
	if id, err := e.a.Authenticate(tctx(1), "alice", "alice-pw"); id != nil || !errors.Is(err, auth.ErrExternalRejected) {
		t.Fatalf("disabled source: id=%v err=%v", id, err)
	}
	// 目录不可达：返回非 Rejected 的错误（core 会写服务端日志），仍然没有身份
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	_ = ln.Close()
	cfg := baseCfg(e.srv)
	cfg.URL = "ldap://" + dead
	addLDAP(t, e.svc, 3, cfg, true)
	id, err := e.a.Authenticate(tctx(3), "alice", "alice-pw")
	if id != nil || err == nil || errors.Is(err, auth.ErrExternalRejected) || strings.Contains(err.Error(), "alice-pw") {
		t.Fatalf("infra: id=%v err=%v", id, err)
	}
}

func mustFirstID(t *testing.T, e *ldapEnv) idgen.ID {
	t.Helper()
	srcs, err := e.st.list(context.Background(), 1)
	if err != nil || len(srcs) == 0 {
		t.Fatal("no source for tenant 1")
	}
	return srcs[0].ID
}
