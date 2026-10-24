package user

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"nexadmin/pkg/oauth"
)

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"13800138000":       "13800138000",
		"+86 138-0013-8000": "13800138000",
		"8613800138000":     "13800138000",
		"+8613800138000":    "13800138000",
		"12345678901":       "", // 不是合法号段
		"1380013800":        "",
		"":                  "",
		"abc":               "",
	}
	for in, want := range cases {
		if got := normalizePhone(in); got != want {
			t.Errorf("normalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

// allowed_email_domains：未配置不限制；配置后必须是“已验证”邮箱且域名在名单内。
func TestDomainAllowed(t *testing.T) {
	corp := []string{"corp.com", "@sub.corp.com"}
	cases := []struct {
		name    string
		allowed []string
		id      oauth.Identity
		want    bool
	}{
		{"no restriction, no email", nil, oauth.Identity{}, true},
		{"no restriction, unverified", nil, oauth.Identity{Email: "a@x.com"}, true},
		{"verified corp", corp, oauth.Identity{Email: "a@corp.com", EmailVerified: true}, true},
		{"case insensitive", corp, oauth.Identity{Email: "A@CORP.com", EmailVerified: true}, true},
		{"@-prefixed entry", corp, oauth.Identity{Email: "a@sub.corp.com", EmailVerified: true}, true},
		{"unverified corp is rejected", corp, oauth.Identity{Email: "a@corp.com", EmailVerified: false}, false},
		{"other domain", corp, oauth.Identity{Email: "a@gmail.com", EmailVerified: true}, false},
		{"suffix trick", corp, oauth.Identity{Email: "a@evilcorp.com", EmailVerified: true}, false},
		{"subdomain not implied", corp, oauth.Identity{Email: "a@x.corp.com", EmailVerified: true}, false},
		{"no email", corp, oauth.Identity{EmailVerified: true}, false},
		{"malformed", corp, oauth.Identity{Email: "corp.com", EmailVerified: true}, false},
		{"trailing @", corp, oauth.Identity{Email: "a@", EmailVerified: true}, false},
		{"multiple @ rejected", corp, oauth.Identity{Email: "a@gmail.com@corp.com", EmailVerified: true}, false},
	}
	for _, c := range cases {
		if got := domainAllowed(c.allowed, &c.id); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// 邮箱自动匹配只认已验证；手机号必须合法；工号对应用户名。
func TestMatchTarget(t *testing.T) {
	col, val, ok := matchTarget("email", &oauth.Identity{Email: " Me@Corp.com ", EmailVerified: true})
	if !ok || col != "email" || val != "me@corp.com" {
		t.Fatalf("verified email: %q %q %v", col, val, ok)
	}
	if _, _, ok := matchTarget("email", &oauth.Identity{Email: "me@corp.com", EmailVerified: false}); ok {
		t.Fatal("unverified email must never be used for matching")
	}
	if _, _, ok := matchTarget("email", &oauth.Identity{EmailVerified: true}); ok {
		t.Fatal("empty email must not match")
	}
	col, val, ok = matchTarget("phone", &oauth.Identity{Phone: "+86 138 0013 8000"})
	if !ok || col != "phone" || val != "13800138000" {
		t.Fatalf("phone: %q %q %v", col, val, ok)
	}
	if _, _, ok := matchTarget("phone", &oauth.Identity{Phone: "999"}); ok {
		t.Fatal("invalid phone must not match")
	}
	col, val, ok = matchTarget("employee_no", &oauth.Identity{EmployeeNo: "E100"})
	if !ok || col != "username" || val != "E100" {
		t.Fatalf("employee_no: %q %q %v", col, val, ok)
	}
	for _, by := range []string{"", "username", "name"} {
		if _, _, ok := matchTarget(by, &oauth.Identity{Email: "a@b.com", EmailVerified: true, Phone: "13800138000", EmployeeNo: "x"}); ok {
			t.Errorf("match_by=%q must not match anything", by)
		}
	}
}

func TestIsDup(t *testing.T) {
	for _, e := range []error{errors.New("Error 1062: Duplicate entry 'x' for key"), errors.New(`pq: duplicate key value violates unique constraint "uk"`), gorm.ErrDuplicatedKey} {
		if !isDup(e) {
			t.Errorf("%v should be a duplicate error", e)
		}
	}
	if isDup(nil) || isDup(errors.New("connection refused")) {
		t.Error("non-duplicate errors misclassified")
	}
}

func TestClipKeepsRunes(t *testing.T) {
	if got := clip("你好世界", 2); got != "你好" {
		t.Fatalf("clip = %q", got)
	}
}
