package profile

import (
	"strings"
	"testing"
)

// 不设密码强度规则：新密码只要求非空且不超过 bcrypt 的 72 字节。
func TestValidNewPasswordHasNoStrengthRule(t *testing.T) {
	for _, pw := range []string{"a", "1234", "password", "中文", strings.Repeat("x", 72)} {
		if !validNewPassword(pw) {
			t.Errorf("%q should be accepted", pw)
		}
	}
	for _, pw := range []string{"", strings.Repeat("x", 73)} {
		if validNewPassword(pw) {
			t.Errorf("%d-byte password should be rejected", len(pw))
		}
	}
}
