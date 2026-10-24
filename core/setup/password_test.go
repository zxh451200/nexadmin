package setup

import (
	"strings"
	"testing"
)

// 用户明确要求不设密码强度规则：只要求非空且不超过 bcrypt 的 72 字节。
func TestValidateAcceptsAnyPasswordWithinBcryptLimit(t *testing.T) {
	r := testRunner(t)
	for _, pw := range []string{"a", "123", "password", "          ", "中文密码", strings.Repeat("x", 72)} {
		req := baseReq()
		req.Admin.Password = pw
		if _, err := r.validate(req); err != nil {
			t.Errorf("password %q should be accepted: %v", pw, err)
		}
	}
	for name, pw := range map[string]string{"empty": "", "too long": strings.Repeat("x", 73)} {
		req := baseReq()
		req.Admin.Password = pw
		if _, err := r.validate(req); err == nil {
			t.Errorf("%s password should be rejected", name)
		} else if pw != "" && strings.Contains(err.Error(), pw) {
			t.Errorf("%s: error leaks password", name)
		}
	}
}
