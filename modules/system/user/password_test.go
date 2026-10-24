package user

import (
	"context"
	"strings"
	"testing"
)

// 重置密码不设强度规则；非空与 72 字节上限在访问数据库之前就校验（s 的 db 为 nil，走到库会 panic）。
func TestResetPasswordRejectsOnlyEmptyAndOverlong(t *testing.T) {
	s := &service{}
	for _, pw := range []string{"", strings.Repeat("a", 73)} {
		if err := s.resetPassword(context.Background(), 1, pw, actor{}); status(err) != 400 {
			t.Errorf("%d-byte password: want 400, got %v", len(pw), err)
		}
	}
}
