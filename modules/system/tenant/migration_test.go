package tenant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 额度迁移是新增的编号文件（不改旧文件），三个引擎各一份，只含 DDL。
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
