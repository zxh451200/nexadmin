package tenant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func aiTenantRow(aiEnabled bool, aiQuota any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "domain", "package_id", "expire_at", "status", "admin_username", "ai_enabled", "ai_daily_quota"}).
		AddRow(9, "T", "t.nexadmin.dev", 5, time.Now().Add(time.Hour), "active", "a", aiEnabled, aiQuota)
}

func aiPkgRows(quota int) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "menu_ids", "max_users", "max_roles", "max_storage_mb", "ai_daily_quota"}).AddRow(5, "[]", 0, 0, 0, quota)
}

// TenantInfo 把租户的 AI 开关与每日配额带给 core/tenant：租户覆盖优先（含 0 = 不限），其次套餐。
func TestTenantInfoAIPolicy(t *testing.T) {
	cases := []struct {
		name      string
		enabled   bool
		tenantQ   any
		pkgQ      int
		wantOff   bool
		wantQuota int
	}{
		{"inherit package quota", true, nil, 50, false, 50},
		{"tenant override", true, 5, 50, false, 5},
		{"tenant override unlimited", true, 0, 50, false, 0},
		{"switched off", false, nil, 50, true, 50},
	}
	for _, c := range cases {
		s, mock := newSvc(t)
		unordered(mock)
		mock.ExpectQuery("FROM `sys_tenant`").WillReturnRows(aiTenantRow(c.enabled, c.tenantQ))
		expectConfig(mock, "", "")
		mock.ExpectQuery("FROM `sys_tenant_pkg`").WillReturnRows(aiPkgRows(c.pkgQ))
		info, err := s.TenantInfo(tctx, 9)
		if err != nil || info == nil {
			t.Fatalf("%s: %v %v", c.name, info, err)
		}
		if info.AIDisabled != c.wantOff || info.AIDailyQuota != c.wantQuota {
			t.Errorf("%s: AIDisabled=%v AIDailyQuota=%d, want %v %d", c.name, info.AIDisabled, info.AIDailyQuota, c.wantOff, c.wantQuota)
		}
	}
}

// 迁移是新增的编号文件（不改旧文件），三个引擎各一份，只含 sys_tenant* 的 ALTER TABLE。
func TestAIMigrationsExistAndAreDDLOnly(t *testing.T) {
	for _, engine := range []string{"mysql", "mariadb", "postgres"} {
		p := filepath.Join("..", "..", "..", "migrations", "framework", engine, "system_tenant_003_ai.sql")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", engine, err)
		}
		var stmts int
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "--") {
				continue
			}
			stmts++
			if !strings.HasPrefix(line, "ALTER TABLE sys_tenant") || !strings.HasSuffix(line, ";") {
				t.Errorf("%s: only single-line ALTER TABLE on sys_tenant* allowed, got %q", engine, line)
			}
		}
		if stmts != 3 {
			t.Errorf("%s: want 3 statements, got %d", engine, stmts)
		}
	}
}
