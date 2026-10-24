package report

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"nexadmin/core/plugin"
)

func TestManifestIsValid(t *testing.T) {
	m, err := plugin.LoadManifest(".")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if m.ID != "report" || m.Edition != "pro" || m.Version != "1.0.0" {
		t.Fatalf("manifest = %s %s %s", m.ID, m.Edition, m.Version)
	}
	perms := map[string]bool{}
	for _, mu := range m.Menus {
		if mu.Perm != "" {
			perms[mu.Perm] = true
		}
	}
	for _, p := range []string{"report:view", "report:dataset", "report:chart", "report:report", "report:screen"} {
		if !perms[p] {
			t.Errorf("permission %s is not declared in plugin.yaml", p)
		}
	}
}

var createTableRe = regexp.MustCompile(`(?s)CREATE TABLE (\w+) \((.*?)\n\);`)

// 三种引擎各一份迁移：表名带 plg_report_ 前缀、每张表都有 tenant_id 与 BIGINT 主键、配置用 TEXT，且没有引擎私有语法。
func TestMigrationsAllEngines(t *testing.T) {
	want := map[string]bool{"plg_report_dataset": true, "plg_report_chart": true, "plg_report_report": true, "plg_report_screen": true, "plg_report_screen_role": true}
	for _, engine := range []string{"mysql", "mariadb", "postgres"} {
		raw, err := os.ReadFile("migrations/" + engine + "/001_init.sql")
		if err != nil {
			t.Fatalf("%s: %v", engine, err)
		}
		var lines []string
		for _, l := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(l), "--") { // 注释里可以提到 JSON，只检查 SQL 语句
				lines = append(lines, l)
			}
		}
		sql := strings.Join(lines, "\n")
		got := map[string]bool{}
		for _, m := range createTableRe.FindAllStringSubmatch(sql, -1) {
			got[m[1]] = true
			if !strings.HasPrefix(m[1], "plg_report_") || !strings.Contains(m[2], "tenant_id BIGINT NOT NULL") || !strings.Contains(m[2], "id BIGINT NOT NULL") {
				t.Errorf("%s: table %s is missing tenant_id/id", engine, m[1])
			}
		}
		for tb := range want {
			if !got[tb] {
				t.Errorf("%s: table %s missing", engine, tb)
			}
		}
		for _, bad := range []string{"AUTO_INCREMENT", "SERIAL", "ENGINE=", "JSON", "JSONB", "ON UPDATE", "UNSIGNED", "ENUM(", "BOOLEAN", "TINYINT"} {
			if strings.Contains(strings.ToUpper(sql), bad) {
				t.Errorf("%s: engine-specific token %q in migration", engine, bad)
			}
		}
		if (engine == "postgres") != strings.Contains(sql, "TIMESTAMP(3)") || (engine != "postgres") != strings.Contains(sql, "DATETIME(3)") {
			t.Errorf("%s: wrong datetime type", engine)
		}
	}
}
