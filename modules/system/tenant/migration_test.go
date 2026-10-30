package tenant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 额度迁移是新增的编号文件（不改旧文件），三个引擎各一份，只含 DDL。
// 资料/品牌与用量切片同样是新增编号文件，三引擎各一份。
func TestProfileAndUsageMigrationsExist(t *testing.T) {
	for _, engine := range []string{"mysql", "mariadb", "postgres"} {
		profile := filepath.Join("..", "..", "..", "migrations", "framework", engine, "system_tenant_004_profile.sql")
		b, err := os.ReadFile(profile)
		if err != nil {
			t.Fatalf("%s: %v", engine, err)
		}
		var alters int
		for _, line := range strings.Split(string(b), "\n") {
			if line = strings.TrimSpace(line); strings.HasPrefix(line, "ALTER TABLE sys_tenant ADD COLUMN") {
				alters++
			}
		}
		if alters != 5 {
			t.Errorf("%s: want 5 ALTER TABLE statements, got %d", engine, alters)
		}
		usage := filepath.Join("..", "..", "..", "migrations", "framework", engine, "system_tenant_005_usage_daily.sql")
		u, err := os.ReadFile(usage)
		if err != nil {
			t.Fatalf("%s: %v", engine, err)
		}
		if !strings.Contains(string(u), "CREATE TABLE sys_tenant_usage_daily") ||
			!strings.Contains(string(u), "uk_sys_tenant_usage_daily") {
			t.Errorf("%s: usage daily migration incomplete", engine)
		}
	}
}

func TestQuotaMigrationsExistAndAreDDLOnly(t *testing.T) {
	for _, engine := range []string{"mysql", "mariadb", "postgres"} {
		p := filepath.Join("..", "..", "..", "migrations", "framework", engine, "system_tenant_002_quota.sql")
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
			if !strings.HasPrefix(line, "ALTER TABLE sys_tenant") {
				t.Errorf("%s: only ALTER TABLE on sys_tenant* allowed, got %q", engine, line)
			}
		}
		if stmts != 4 {
			t.Errorf("%s: want 4 statements, got %d", engine, stmts)
		}
	}
}
