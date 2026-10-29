package market

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gormmysql "gorm.io/driver/mysql"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// purgePluginTables：从 information_schema 找出 plg_<id>_ 前缀的表并逐个 DROP。
// 白名单外的名字（不应该出现，防御性测试）必须被跳过。
func TestPurgePluginTables(t *testing.T) {
	sqlDB, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	rows := sqlmock.NewRows([]string{"table_name"}).
		AddRow("plg_demo_order").
		AddRow("plg_demo_items").
		AddRow("plg_other_order") // 前缀不在 plg_demo_ 精确段内（演示防御过滤）
	m.ExpectQuery("SELECT table_name FROM information_schema\\.tables").
		WithArgs("plg\\_demo\\_%").
		WillReturnRows(rows)
	m.ExpectExec("DROP TABLE IF EXISTS `plg_demo_order`").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec("DROP TABLE IF EXISTS `plg_demo_items`").WillReturnResult(sqlmock.NewResult(0, 0))

	var buf strings.Builder
	n, err := purgePluginTables(db, "mysql", "demo", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 dropped, got %d (%s)", n, buf.String())
	}
	// 输出列出已删除的表（白名单内的两张）。
	for _, want := range []string{"已删除表 plg_demo_order", "已删除表 plg_demo_items"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in output", want)
		}
	}
	if strings.Contains(buf.String(), "plg_other_order") {
		t.Error("out-of-scope table must not be dropped or listed")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 非法 id / 非法数据库类型直接报错，不发 SQL。
func TestPurgePluginTablesGuards(t *testing.T) {
	if _, err := purgePluginTables(nil, "sqlite", "demo", nil); err == nil {
		t.Error("unknown engine should error")
	}
}

func TestPurgePluginDatabaseClearsOnlyOwnMigrations(t *testing.T) {
	for _, engine := range []string{"mysql", "mariadb", "postgres"} {
		for _, failDrop := range []bool{false, true} {
			name := engine
			if failDrop {
				name += "/failed-drop"
			}
			t.Run(name, func(t *testing.T) {
				sqlDB, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = sqlDB.Close() })
				var dial gorm.Dialector = gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true})
				if engine == "postgres" {
					dial = gormpg.New(gormpg.Config{Conn: sqlDB})
				}
				db, err := gorm.Open(dial, &gorm.Config{DisableAutomaticPing: true})
				if err != nil {
					t.Fatal(err)
				}
				if engine == "postgres" {
					mock.ExpectQuery("SELECT pg_try_advisory_lock").WithArgs(int64(0x6e65786d6967)).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
					mock.ExpectBegin()
					mock.ExpectQuery("SELECT tablename FROM pg_tables").WithArgs("plg\\_demo\\_%").WillReturnRows(sqlmock.NewRows([]string{"tablename"}).AddRow("plg_demo_order"))
				} else {
					mock.ExpectQuery("SELECT VERSION").WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("10.11-" + engine))
					mock.ExpectQuery("SELECT CONCAT").WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("nexadmin_migrate_test"))
					mock.ExpectQuery("SELECT GET_LOCK").WithArgs("nexadmin_migrate_test").WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
					mock.ExpectQuery("SELECT table_name FROM information_schema").WithArgs("plg\\_demo\\_%").WillReturnRows(sqlmock.NewRows([]string{"table_name"}).AddRow("plg_demo_order"))
				}
				drop := mock.ExpectExec("DROP TABLE IF EXISTS .plg_demo_order.")
				if failDrop {
					drop.WillReturnError(errors.New("drop refused"))
				} else {
					drop.WillReturnResult(sqlmock.NewResult(0, 0))
					mock.ExpectQuery("SELECT COUNT.*information_schema.tables").WithArgs("sys_migration").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
					mock.ExpectExec("DELETE FROM sys_migration WHERE scope = ").WithArgs("plugin:demo").WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectQuery("SELECT COUNT.*information_schema.tables").WithArgs("sys_plugin_migration").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
					mock.ExpectExec("DELETE FROM sys_plugin_migration WHERE plugin_id = ").WithArgs("demo").WillReturnResult(sqlmock.NewResult(0, 1))
				}
				if engine == "postgres" {
					if failDrop {
						mock.ExpectRollback()
					} else {
						mock.ExpectCommit()
					}
					mock.ExpectExec("SELECT pg_advisory_unlock").WithArgs(int64(0x6e65786d6967)).WillReturnResult(sqlmock.NewResult(0, 1))
				} else {
					mock.ExpectExec("SELECT RELEASE_LOCK").WithArgs("nexadmin_migrate_test").WillReturnResult(sqlmock.NewResult(0, 1))
				}
				var out strings.Builder
				n, err := purgePluginDatabase(db, "demo", &out)
				if (err != nil) != failDrop || (!failDrop && n != 1) {
					t.Fatalf("dropped=%d err=%v", n, err)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestPurgePluginDatabaseRejectsInvalidID(t *testing.T) {
	if _, err := purgePluginDatabase(nil, "../demo", nil); err == nil {
		t.Fatal("invalid plugin ID must be rejected before accessing the database")
	}
}

func TestRenderDocsJSON(t *testing.T) {
	entries := []Entry{
		{ID: "cron", Name: "定时任务", Version: "1.0.0", Edition: "pro", Description: "调度", Price: "待定", Author: "NexAdmin"},
		{ID: "hello", Name: "示例", Edition: "free"},
	}
	raw, err := RenderDocsJSON(entries)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		`"id": "cron"`, `"edition": "pro"`, `"price": "待定"`,
		`"id": "hello"`, `"edition": "free"`, `"version": ""`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("plugins.json missing %q:\n%s", want, s)
		}
	}
	if !strings.HasSuffix(s, "]\n") {
		t.Error("output must end with newline for clean git diffs")
	}
}

// Download 的 url 直链分支：httptest 服务器给 zip，命中即得临时文件。
func TestDownloadDirectURL(t *testing.T) {
	zipBytes := makeZipBytes(t, map[string]string{
		"plugin.yaml":       manifestYAML,
		"backend/plugin.go": "package demo\n",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mirror/p.zip" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(zipBytes)
	}))
	t.Cleanup(srv.Close)

	// 命中直链。
	p, err := Download(context.Background(), Entry{URL: srv.URL + "/mirror/p.zip"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p) //nolint:errcheck
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	// 404 报直链失效。
	if _, err := Download(context.Background(), Entry{URL: srv.URL + "/missing.zip"}); err == nil || !strings.Contains(err.Error(), "直链失效") {
		t.Errorf("missing url should fail with 直链失效, got %v", err)
	}
	// repo 与 url 二选一。
	if _, err := LoadCatalogWrite(t, "plugins:\n  - id: a\n    repo: a/b\n    url: https://x/y.zip\n    edition: free\n"); err == nil {
		t.Error("repo+url together must be rejected")
	}
}

// monorepo 安装：整仓 zip（含 GitHub zipball 的 <repo>-<ref>/ 前缀层）按 subdir 装指定插件。
func TestInstallSubMonorepo(t *testing.T) {
	z := makeZip(t, map[string]string{
		"nexadmin-plugins-1.0.0/cron/plugin.yaml":       manifestYAML,
		"nexadmin-plugins-1.0.0/cron/backend/plugin.go": "package cron\n",
		"nexadmin-plugins-1.0.0/cron/web/route.tsx":     "export {}\n",
		"nexadmin-plugins-1.0.0/audit/plugin.yaml":      "id: audit\nname: Audit\nversion: 1.0.0\nedition: pro\n",
		"nexadmin-plugins-1.0.0/audit/backend.atom":     "x",
	})
	root := newRoot(t)
	id, err := InstallSub(root, z, "cron", false)
	if err != nil || id != "demo-x" {
		t.Fatalf("monorepo install: %q, %v", id, err)
	}
	if !exists(filepath.Join(root, "plugins", "demo-x", "plugin.go")) || !exists(filepath.Join(root, "web", "src", "plugins", "demo-x", "route.tsx")) {
		t.Fatal("monorepo subdir not installed")
	}
	// 错误的 subdir 要报错且不留半成品。
	root2 := newRoot(t)
	if _, err := InstallSub(root2, z, "nope", false); err == nil {
		t.Fatal("missing subdir must fail")
	}
	if exists(filepath.Join(root2, "plugins", "demo-x")) {
		t.Fatal("failed install must leave nothing")
	}
}

func makeZipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// LoadCatalogWrite 写目录文件后加载（测试辅助）。
func LoadCatalogWrite(t *testing.T, yamlContent string) ([]Entry, error) {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "config", "plugin-market.yaml"), yamlContent)
	return LoadCatalog(root)
}

func TestLoadCatalog(t *testing.T) {
	root := t.TempDir()
	if entries, err := LoadCatalog(root); err != nil || entries != nil {
		t.Fatalf("missing file should be empty catalog, got %v, %v", entries, err)
	}
	write(t, root+"/config/plugin-market.yaml", "plugins:\n"+
		"  - id: im\n    name: 即时通讯\n    edition: pro\n    repo: acme/nexadmin-plugin-im\n    category: collaboration\n")
	entries, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "im" || entries[0].Edition != "pro" {
		t.Fatalf("catalog parse: %+v", entries)
	}
	if _, ok := FindEntry(entries, "nope"); ok {
		t.Fatal("FindEntry miss")
	}
	if e, ok := FindEntry(entries, "im"); !ok || e.Repo != "acme/nexadmin-plugin-im" {
		t.Fatal("FindEntry hit")
	}

	for _, bad := range []string{
		"plugins:\n  - id: ../evil\n    repo: a/b\n    edition: free\n",                                        // 非法 id
		"plugins:\n  - id: a\n    repo: a/b\n    edition: free\n  - id: a\n    repo: c/d\n    edition: free\n", // 重复 id
		"plugins:\n  - id: a\n    repo: https://evil/x\n    edition: free\n",                                   // repo 非 owner/name
		"plugins:\n  - id: a\n    repo: a/b\n    edition: gold\n",                                              // 非法 edition
		"plugins: [", // YAML 语法错误
	} {
		write(t, root+"/config/plugin-market.yaml", bad)
		if _, err := LoadCatalog(root); err == nil {
			t.Errorf("catalog should be rejected: %q", bad)
		}
	}
}

// 派生工程模块名与母版不同：安装时必须改写插件源码里的框架 import，否则装完编译不过。
func TestInstallRewritesFrameworkModuleImports(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "app")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.26\n")
	write(t, filepath.Join(root, "plugins", "plugins.go"), "package plugins\n\n// nexadmin:plugins:begin\n// nexadmin:plugins:end\n")
	write(t, filepath.Join(dir, "pkg", "plugin.yaml"), "id: demo\nname: Demo\nversion: 1.0.0\nedition: free\n")
	write(t, filepath.Join(dir, "pkg", "backend", "plugin.go"), `package demo

import (
	"nexadmin/core"
	"nexadmin/pkg/idgen"
)

const note = "nexadmin/core must stay in strings"
`)
	write(t, filepath.Join(dir, "pkg", "backend", "service.go"), "package demo\n\nimport \"nexadmin/core/database\"\n\nvar _ = database.Open\n")
	if _, err := Install(root, filepath.Join(dir, "pkg"), false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "plugins", "demo", "plugin.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{`"example.com/app/core"`, `"example.com/app/pkg/idgen"`, `note = "nexadmin/core must stay in strings"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in installed source:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"nexadmin/core"`) || strings.Contains(s, `"nexadmin/pkg/idgen"`) {
		t.Error("framework imports were not rewritten")
	}
}
