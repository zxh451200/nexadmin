package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEscapeFilterValue(t *testing.T) {
	cases := map[string]string{
		"alice":              "alice",
		"a*b":                `a\2ab`,
		"(x)":                `\28x\29`,
		`back\slash`:         `back\5cslash`,
		"nul\x00byte":        `nul\00byte`,
		"*)(uid=*":           `\2a\29\28uid=\2a`,
		"*)(|(objectClass=*": `\2a\29\28|\28objectClass=\2a`,
		"tab\there":          `tab\09here`,
		"中文":                 "中文",
	}
	for in, want := range cases {
		if got := escapeFilterValue(in); got != want {
			t.Errorf("escape(%q) = %q, want %q", in, got, want)
		}
	}
}

// 注入输入经过转义后，编译出的 BER 过滤器结构必须不变：AND 下恰好两个子过滤器，第二个是等值匹配且值等于原始输入。
func TestInjectionCannotChangeFilterStructure(t *testing.T) {
	tpl := "(&(objectClass=person)(uid={username}))"
	for _, evil := range []string{"*)(uid=*", "*", "admin)(|(uid=*", `x\29`, "a)(!(uid=zzz", "*)(objectClass=*))(|(uid=*"} {
		f, err := compileFilter(buildUserFilter(tpl, evil))
		if err != nil {
			t.Fatalf("%q: %v", evil, err)
		}
		tag, val, rest, err := parseTLV(f)
		if err != nil || tag != fAnd || len(rest) != 0 {
			t.Fatalf("%q: top-level is not a single AND", evil)
		}
		kids := children(val)
		if len(kids) != 2 {
			t.Fatalf("%q: AND has %d children, want 2", evil, len(kids))
		}
		if kids[1][0].(byte) != fEq {
			t.Fatalf("%q: second child tag = %#x, want equality", evil, kids[1][0])
		}
		_, _, r, _ := parseTLV(kids[1][1].([]byte))
		_, v, _, _ := parseTLV(r)
		if string(v) != evil {
			t.Fatalf("%q: asserted value = %q", evil, v)
		}
	}
}

func TestCompileFilterErrors(t *testing.T) {
	for _, bad := range []string{"", "uid=a", "(uid=a", "(uid=a))", "(&)", "(uid=a(b)", `(uid=\zz)`, "(=a)", "(uid:=a)"} {
		if _, err := compileFilter(bad); err == nil {
			t.Errorf("filter %q must fail", bad)
		}
	}
	for _, ok := range []string{"(uid=a)", "(uid=*)", "(&(a=1)(|(b=2)(!(c=3))))", "(cn=a*b*c)", "(age>=18)", `(cn=a\2ab)`} {
		if _, err := compileFilter(ok); err != nil {
			t.Errorf("filter %q: %v", ok, err)
		}
	}
}

func TestParseLDAPURL(t *testing.T) {
	good := map[string][3]string{
		"ldap://dc.example.com":       {"ldap", "dc.example.com", "389"},
		"LDAPS://dc.example.com:3636": {"ldaps", "dc.example.com", "3636"},
		"ldaps://[::1]":               {"ldaps", "::1", "636"},
	}
	for in, want := range good {
		s, h, p, err := parseLDAPURL(in)
		if err != nil || [3]string{s, h, p} != want {
			t.Errorf("%q => %v %v %v %v", in, s, h, p, err)
		}
	}
	for _, bad := range []string{"", "http://x", "ldap://", "ldap://u:p@x", "ldap://x/dc=a", "ldap://x?a=b", "ldap://x:0", "ldap://x:99999", "ldap://x:abc", "file:///etc/passwd", "ldap://x#f"} {
		if _, _, _, err := parseLDAPURL(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestReadTLVEnforcesSizeLimit(t *testing.T) {
	// 声明 2GB 长度的报文：必须在分配前拒绝
	if _, err := readTLV(bytes.NewReader([]byte{0x30, 0x84, 0x7f, 0xff, 0xff, 0xff}), 1<<20); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
	for _, bad := range [][]byte{{0x30, 0x80}, {0x30, 0x85, 1, 2, 3, 4, 5}, {0x1f, 0x01, 0x00}, {0x30}} {
		if _, err := readTLV(bytes.NewReader(bad), 1<<20); err == nil {
			t.Errorf("% x must fail", bad)
		}
	}
	if _, _, _, err := parseTLV([]byte{0x30, 0x05, 0x01}); err == nil {
		t.Error("truncated TLV must fail")
	}
	b := enc(tagSeq, encInt(tagInt, 300), encInt(tagInt, -2))
	_, v, _, _ := parseTLV(b)
	_, a, rest, _ := parseTLV(v)
	_, c, _, _ := parseTLV(rest)
	if x, _ := decodeInt(a); x != 300 {
		t.Errorf("300 round trip = %d", x)
	}
	if x, _ := decodeInt(c); x != -2 {
		t.Errorf("-2 round trip = %d", x)
	}
}

var testUsers = []testEntry{
	{dn: "cn=svc,dc=example,dc=com", password: "svc-secret", attrs: map[string][]string{"objectclass": {"person"}, "uid": {"svc"}}},
	{dn: "uid=alice,ou=people,dc=example,dc=com", password: "alice-pw", attrs: map[string][]string{
		"objectclass": {"person"}, "uid": {"alice"}, "cn": {"Alice A"}, "mail": {"alice@example.com"}, "mobile": {"13800000001"}}},
	{dn: "uid=bob,ou=people,dc=example,dc=com", password: "bob-pw", attrs: map[string][]string{"objectclass": {"person"}, "uid": {"bob"}, "cn": {"Bob"}}},
	{dn: "uid=dup1,ou=people,dc=example,dc=com", password: "x", attrs: map[string][]string{"objectclass": {"person"}, "uid": {"dup"}}},
	{dn: "uid=dup2,ou=people,dc=example,dc=com", password: "x", attrs: map[string][]string{"objectclass": {"person"}, "uid": {"dup"}}},
}

func baseCfg(srv *testLDAP) LDAPConfig {
	return LDAPConfig{URL: srv.url(), BindDN: "cn=svc,dc=example,dc=com", BindPassword: "svc-secret", BaseDN: "dc=example,dc=com",
		UserFilter: "(&(objectClass=person)(uid={username}))", AllowPlaintext: true, AttrUsername: "uid", AttrNickname: "cn", AttrEmail: "mail", AttrPhone: "mobile", TimeoutSec: 3}
}

func TestClientBindAndSearch(t *testing.T) {
	srv := newTestLDAP(t, testUsers, nil, false)
	c, err := dialLDAP(context.Background(), baseCfg(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Bind("cn=svc,dc=example,dc=com", "svc-secret"); err != nil {
		t.Fatal(err)
	}
	var le *ldapError
	if err := c.Bind("cn=svc,dc=example,dc=com", "wrong"); !errors.As(err, &le) || le.Code != ldapInvalidCredentials {
		t.Fatalf("wrong password err = %v", err)
	}
	es, err := c.Search("dc=example,dc=com", scopeSub, "(&(objectClass=person)(uid=alice))", []string{"uid", "mail", "cn"}, 2)
	if err != nil || len(es) != 1 || es[0].DN != "uid=alice,ou=people,dc=example,dc=com" || es[0].first("MAIL") != "alice@example.com" {
		t.Fatalf("es=%+v err=%v", es, err)
	}
	// 子串 / 存在 / 非 过滤器
	es, err = c.Search("dc=example,dc=com", scopeSub, "(&(uid=*)(!(uid=svc))(cn=*lic*))", []string{"uid"}, 10)
	if err != nil || len(es) != 1 {
		t.Fatalf("substr: es=%+v err=%v", es, err)
	}
	// 服务端 sizeLimit：命中上限时回 4(sizeLimitExceeded)。截断一律按错误处理，绝不把部分结果当成完整枚举
	es, err = c.Search("dc=example,dc=com", scopeSub, "(objectClass=person)", []string{"uid"}, 2)
	var se *ldapError
	if !errors.As(err, &se) || se.Code != ldapSizeLimitExceeded || len(es) != 0 {
		t.Fatalf("size limit: es=%d err=%v", len(es), err)
	}
}

func TestBindRejectsEmptyPasswordAndDN(t *testing.T) {
	srv := newTestLDAP(t, testUsers, nil, false)
	srv.unauthSuccess = true // 这个服务端会把 “DN + 空口令” 当成功
	c, err := dialLDAP(context.Background(), baseCfg(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Bind("uid=alice,ou=people,dc=example,dc=com", ""); !errors.Is(err, errEmptyPassword) {
		t.Fatalf("empty password err = %v", err)
	}
	if err := c.Bind("", "x"); !errors.Is(err, errEmptyDN) {
		t.Fatalf("empty DN err = %v", err)
	}
	if n := len(srv.bindLog()); n != 0 {
		t.Fatalf("empty-credential bind must never reach the server, got %d binds", n)
	}
}

func TestDialTimeout(t *testing.T) {
	srv := newTestLDAP(t, testUsers, nil, false)
	srv.bindDelay = 3 * time.Second
	cfg := baseCfg(srv)
	cfg.TimeoutSec = 1
	c, err := dialLDAP(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	if err := c.Bind(cfg.BindDN, "svc-secret"); err == nil {
		t.Fatal("bind must time out")
	}
	if d := time.Since(start); d > 2500*time.Millisecond {
		t.Fatalf("timeout not enforced: %v", d)
	}
}
