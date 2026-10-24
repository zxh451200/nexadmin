package security

import (
	"context"
	"net/http"
	"testing"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func rv(action, scope, cidr string, enabled bool) ruleView {
	p, err := normalizeCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return ruleView{Action: action, Scope: scope, Prefix: p, Enabled: enabled}
}

func TestNormalizeCIDR(t *testing.T) {
	for in, want := range map[string]string{
		"1.2.3.4":             "1.2.3.4/32",
		"10.1.2.3/8":          "10.0.0.0/8",
		"2001:db8::1":         "2001:db8::1/128",
		"2001:db8::dead/32":   "2001:db8::/32",
		"::ffff:1.2.3.4":      "1.2.3.4/32",
		"::ffff:10.0.0.0/104": "10.0.0.0/8",
		" 192.168.0.0/16 ":    "192.168.0.0/16",
	} {
		p, err := normalizeCIDR(in)
		if err != nil || p.String() != want {
			t.Errorf("%q -> %v %v, want %s", in, p, err, want)
		}
	}
	for _, bad := range []string{"", "x", "1.2.3", "1.2.3.4/33", "fe80::1%eth0", "::ffff:0:0/80", "1.2.3.4/-1"} {
		if _, err := normalizeCIDR(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestAllowLogin(t *testing.T) {
	cases := []struct {
		name  string
		rules []ruleView
		ip    string
		super bool
		want  bool
	}{
		{"no rules", nil, "1.2.3.4", false, true},
		{"deny hit", []ruleView{rv("deny", "all", "1.2.3.0/24", true)}, "1.2.3.4", false, false},
		{"deny miss", []ruleView{rv("deny", "all", "1.2.3.0/24", true)}, "1.2.4.4", false, true},
		{"allow list restricts", []ruleView{rv("allow", "all", "10.0.0.0/8", true)}, "11.0.0.1", false, false},
		{"allow list hit", []ruleView{rv("allow", "all", "10.0.0.0/8", true)}, "10.9.9.9", false, true},
		{"deny beats allow", []ruleView{rv("allow", "all", "10.0.0.0/8", true), rv("deny", "all", "10.0.0.5", true)}, "10.0.0.5", false, false},
		{"disabled allow is ignored", []ruleView{rv("allow", "all", "10.0.0.0/8", false)}, "11.0.0.1", false, true},
		{"disabled deny is ignored", []ruleView{rv("deny", "all", "0.0.0.0/0", false)}, "11.0.0.1", false, true},
		{"ipv6 allow hit", []ruleView{rv("allow", "all", "2001:db8::/32", true)}, "2001:db8:1::5", false, true},
		{"ipv6 allow miss", []ruleView{rv("allow", "all", "2001:db8::/32", true)}, "2001:db9::1", false, false},
		{"ipv6 deny", []ruleView{rv("deny", "all", "2001:db8::/32", true)}, "2001:db8::1", false, false},
		{"v4 rule vs v6 ip", []ruleView{rv("deny", "all", "0.0.0.0/0", true)}, "2001:db8::1", false, true},
		{"v4-mapped v6 ip matches v4 rule", []ruleView{rv("deny", "all", "1.2.3.4", true)}, "::ffff:1.2.3.4", false, false},
		{"super-scope deny ignores normal user", []ruleView{rv("deny", "super", "1.2.3.4", true)}, "1.2.3.4", false, true},
		{"super-scope deny hits super", []ruleView{rv("deny", "super", "1.2.3.4", true)}, "1.2.3.4", true, false},
		{"super-scope allow restricts only super", []ruleView{rv("allow", "super", "10.0.0.0/8", true)}, "8.8.8.8", false, true},
		{"super-scope allow restricts super", []ruleView{rv("allow", "super", "10.0.0.0/8", true)}, "8.8.8.8", true, false},
		{"all+super combine", []ruleView{rv("allow", "all", "10.0.0.0/8", true), rv("allow", "super", "10.1.0.0/16", true)}, "10.2.0.1", true, false},
		{"unparsable ip with allow list", []ruleView{rv("allow", "all", "10.0.0.0/8", true)}, "garbage", false, false},
		{"unparsable ip with deny only", []ruleView{rv("deny", "all", "10.0.0.0/8", true)}, "", false, true},
	}
	for _, c := range cases {
		if got := allowLogin(c.rules, c.ip, c.super); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestPreLoginUsesRulesPerTenant(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	op := User{TID: 1, ID: 1, Name: "admin", IP: "10.0.0.1"}
	if _, err := e.s.SaveRule(ctx, op, 0, RuleForm{Action: "deny", CIDR: "198.51.100.0/24", Scope: "all", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	t1 := tenant.With(ctx, 1)
	if err := e.s.preLogin(t1, "198.51.100.7", "x"); err == nil {
		t.Fatal("denied ip must be rejected")
	}
	if err := e.s.preLogin(t1, "198.51.101.7", "x"); err != nil {
		t.Fatalf("other ip: %v", err)
	}
	if err := e.s.preLogin(tenant.With(ctx, 2), "198.51.100.7", "x"); err != nil {
		t.Fatalf("rules of tenant 1 must not apply to tenant 2: %v", err)
	}
}

func TestSelfLockoutGuard(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	op := User{TID: 1, ID: 1, Name: "admin", IP: "10.0.0.7"}
	lockout := func(err error) bool { return statusOf(err) == http.StatusConflict }

	// 白名单不含自己 -> 拒绝
	if _, err := e.s.SaveRule(ctx, op, 0, RuleForm{Action: "allow", CIDR: "192.0.2.0/24", Scope: "all", Enabled: true}); !lockout(err) {
		t.Fatalf("allow list excluding self must be refused, got %v", err)
	}
	// deny 自己 -> 拒绝（单 IP、包含自己的 CIDR）
	for _, c := range []string{"10.0.0.7", "10.0.0.0/8"} {
		if _, err := e.s.SaveRule(ctx, op, 0, RuleForm{Action: "deny", CIDR: c, Scope: "all", Enabled: true}); !lockout(err) {
			t.Fatalf("deny %s must be refused, got %v", c, err)
		}
	}
	// 停用的规则不会锁人，可以保存
	if _, err := e.s.SaveRule(ctx, op, 0, RuleForm{Action: "deny", CIDR: "10.0.0.0/8", Scope: "all", Enabled: false}); err != nil {
		t.Fatalf("disabled rule: %v", err)
	}
	// 包含自己的白名单 -> 允许
	allowSelf, err := e.s.SaveRule(ctx, op, 0, RuleForm{Action: "allow", CIDR: "10.0.0.0/24", Scope: "all", Enabled: true})
	if err != nil {
		t.Fatalf("allow incl. self: %v", err)
	}
	// 之后启用那条 deny 10/8 -> 拒绝（启停也要防自锁）
	rules, _ := e.st.ListRules(ctx, 1)
	var denyID = rules[0].ID
	if _, err := e.s.SetRuleEnabled(ctx, op, denyID, true); !lockout(err) {
		t.Fatalf("enabling a deny that covers self must be refused, got %v", err)
	}
	// 删除唯一包含自己的 allow：删除后没有 allow 规则了 -> 放行，应允许；再加一条别的 allow 后，删除 self 的 allow 要被拒
	if _, err := e.s.SaveRule(ctx, op, 0, RuleForm{Action: "allow", CIDR: "10.0.0.0/16", Scope: "all", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.SaveRule(ctx, op, 0, RuleForm{Action: "allow", CIDR: "172.16.0.0/12", Scope: "all", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.s.DeleteRule(ctx, op, allowSelf.ID); err != nil { // 10.0.0.0/16 仍包含自己
		t.Fatalf("delete redundant allow: %v", err)
	}
	rules, _ = e.st.ListRules(ctx, 1)
	var wide = ""
	for _, r := range rules {
		if r.CIDR == "10.0.0.0/16" {
			wide = r.ID.String()
		}
	}
	id, _ := parseID(wide)
	if err := e.s.DeleteRule(ctx, op, id); !lockout(err) {
		t.Fatalf("deleting the last allow rule covering self must be refused, got %v", err)
	}
	// 修改 allow 规则的网段把自己排除 -> 拒绝
	if _, err := e.s.SaveRule(ctx, op, id, RuleForm{Action: "allow", CIDR: "10.9.0.0/16", Scope: "all", Enabled: true}); !lockout(err) {
		t.Fatalf("editing the allow rule to exclude self must be refused, got %v", err)
	}
	// 超管：scope=super 的白名单不含自己也要拒绝；普通管理员不受影响
	sup := op
	sup.Super = true
	if _, err := e.s.SaveRule(ctx, sup, 0, RuleForm{Action: "allow", CIDR: "192.0.2.0/24", Scope: "super", Enabled: true}); !lockout(err) {
		t.Fatalf("super-scope allow excluding a super operator must be refused, got %v", err)
	}
	if _, err := e.s.SaveRule(ctx, op, 0, RuleForm{Action: "allow", CIDR: "192.0.2.0/24", Scope: "super", Enabled: true}); err != nil {
		t.Fatalf("super-scope allow does not affect a non-super operator: %v", err)
	}
	// IPv6 操作者
	op6 := User{TID: 5, ID: 1, Name: "admin6", IP: "2001:db8::42"}
	if _, err := e.s.SaveRule(ctx, op6, 0, RuleForm{Action: "allow", CIDR: "2001:db8::/32", Scope: "all", Enabled: true}); err != nil {
		t.Fatalf("v6 allow incl. self: %v", err)
	}
	if _, err := e.s.SaveRule(ctx, op6, 0, RuleForm{Action: "deny", CIDR: "2001:db8::/64", Scope: "all", Enabled: true}); !lockout(err) {
		t.Fatalf("v6 deny covering self must be refused, got %v", err)
	}
}

func TestRuleValidation(t *testing.T) {
	e := newEnv(t)
	op := User{TID: 1, ID: 1, Name: "admin", IP: "10.0.0.7"}
	for _, f := range []RuleForm{
		{Action: "x", CIDR: "1.1.1.1", Scope: "all"}, {Action: "deny", CIDR: "1.1.1.1", Scope: "y"},
		{Action: "deny", CIDR: "nope", Scope: "all"},
	} {
		if _, err := e.s.SaveRule(context.Background(), op, 0, f); statusOf(err) != http.StatusBadRequest {
			t.Errorf("%+v: %v", f, err)
		}
	}
	f := RuleForm{Action: "deny", CIDR: "1.1.1.1", Scope: "all", Enabled: true}
	if _, err := e.s.SaveRule(context.Background(), op, 0, f); err != nil {
		t.Fatal(err)
	}
	f.CIDR = "1.1.1.1/32"
	if _, err := e.s.SaveRule(context.Background(), op, 0, f); statusOf(err) != http.StatusConflict {
		t.Fatalf("duplicate rule must be 409, got %v", err)
	}
}

func parseID(s string) (id idgen.ID, err error) { return idgen.Parse(s) }
