package user

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

var (
	errDBDown     = errors.New("db down")
	securityQuery = regexp.QuoteMeta("SELECT content FROM `sys_setting` WHERE section = ? LIMIT ?")
)

// 设置页填了就用设置页的值；idle=30 分钟、最长 8 小时。
func TestSessionPolicyReadsSecuritySettings(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery(securityQuery).
		WithArgs("security", 1).
		WillReturnRows(sqlmock.NewRows([]string{"content"}).
			AddRow(`{"maxAttempts":"5","lockMinutes":"15","idleMinutes":"30","maxHours":"8"}`))

	pol := sessionPolicy(e.d.DB)
	if pol.IdleTTL != 30*time.Minute {
		t.Fatalf("idle = %v, want 30m", pol.IdleTTL)
	}
	if pol.MaxLifetime != 8*time.Hour {
		t.Fatalf("max = %v, want 8h", pol.MaxLifetime)
	}
}

// 设置页没有这两项（升级前的存量库）时用默认值：闲置 2 小时、最长 12 小时。
func TestSessionPolicyDefaultsWhenMissing(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery(securityQuery).
		WithArgs("security", 1).
		WillReturnRows(sqlmock.NewRows([]string{"content"}).AddRow(`{"maxAttempts":"5","lockMinutes":"15"}`))

	pol := sessionPolicy(e.d.DB)
	if pol.IdleTTL != defaultIdleMinutes*time.Minute || pol.MaxLifetime != defaultMaxHours*time.Hour {
		t.Fatalf("defaults = %v / %v", pol.IdleTTL, pol.MaxLifetime)
	}
}

// 0 = 不限制：闲置回落到配置的 refresh_ttl（IdleTTL 0），最长会话不设上限。
func TestSessionPolicyZeroMeansUnlimited(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery(securityQuery).
		WithArgs("security", 1).
		WillReturnRows(sqlmock.NewRows([]string{"content"}).
			AddRow(`{"idleMinutes":"0","maxHours":"0"}`))

	pol := sessionPolicy(e.d.DB)
	if pol.IdleTTL != 0 || pol.MaxLifetime != 0 {
		t.Fatalf("unlimited = %v / %v", pol.IdleTTL, pol.MaxLifetime)
	}
}

// 表不存在 / 查询失败时不能把时限变成 0（那样等于取消保护）：退回默认值。
func TestSessionPolicyFallsBackOnQueryError(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery(securityQuery).WithArgs("security", 1).WillReturnError(errDBDown)

	pol := sessionPolicy(e.d.DB)
	if pol.IdleTTL != defaultIdleMinutes*time.Minute || pol.MaxLifetime != defaultMaxHours*time.Hour {
		t.Fatalf("fallback = %v / %v", pol.IdleTTL, pol.MaxLifetime)
	}
}
