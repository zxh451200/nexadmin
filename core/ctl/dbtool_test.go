package ctl

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"nexadmin/core/config"
)

const testPwd = "S3cr3t!pw$`'\"x"

func dbCfg(typ string) config.Database {
	return config.Database{Type: typ, Host: "db.internal", Port: 3306, User: "nex", Password: testPwd, Name: "nexadmin"}
}

func found(string) (string, error) { return "/usr/bin/x", nil }

func TestCommandsNeverContainPassword(t *testing.T) {
	for _, typ := range []string{"mysql", "mariadb", "postgres"} {
		cfg := dbCfg(typ)
		for name, build := range map[string]func(config.Database, LookPath) (ToolSpec, error){"dump": DumpTool, "restore": RestoreTool} {
			spec, err := build(cfg, found)
			if err != nil {
				t.Fatalf("%s %s: %v", typ, name, err)
			}
			if strings.Contains(spec.String(), testPwd) || strings.Contains(strings.Join(spec.Args, "\x00"), testPwd) {
				t.Errorf("%s %s: password leaked into command line: %s", typ, name, spec)
			}
			wantEnv := "MYSQL_PWD="
			if typ == "postgres" {
				wantEnv = "PGPASSWORD="
			}
			if len(spec.Env) != 1 || spec.Env[0] != wantEnv+testPwd {
				t.Errorf("%s %s: password must be passed via %s, got %v", typ, name, wantEnv, spec.Env)
			}
		}
	}
}

func TestToolErrorsAreRedacted(t *testing.T) {
	spec, _ := DumpTool(dbCfg("mysql"), found)
	if got := spec.redact("access denied, using password " + testPwd); strings.Contains(got, testPwd) {
		t.Fatalf("password not redacted: %s", got)
	}
}

func TestConnValuesCannotInjectOptions(t *testing.T) {
	for _, bad := range []string{"--defaults-file=/etc/shadow", "-e", "", "a b", "x\ny", "host;rm -rf /", "$(id)"} {
		for _, mut := range []func(*config.Database){
			func(c *config.Database) { c.Host = bad }, func(c *config.Database) { c.User = bad }, func(c *config.Database) { c.Name = bad },
		} {
			c := dbCfg("mysql")
			mut(&c)
			if _, err := DumpTool(c, found); err == nil {
				t.Errorf("value %q must be rejected", bad)
			}
			if _, err := RestoreTool(c, found); err == nil {
				t.Errorf("value %q must be rejected for restore", bad)
			}
		}
	}
	c := dbCfg("postgres")
	c.Host = "::1"
	if _, err := DumpTool(c, found); err != nil {
		t.Errorf("IPv6 host should be accepted: %v", err)
	}
	c.Port = 70000
	if _, err := DumpTool(c, found); err == nil {
		t.Error("bad port must be rejected")
	}
}

func TestMissingToolIsReported(t *testing.T) {
	none := func(string) (string, error) { return "", errors.New("not found") }
	_, err := DumpTool(dbCfg("mysql"), none)
	var miss *ToolMissingError
	if !errors.As(err, &miss) || !strings.Contains(err.Error(), "mysqldump") {
		t.Fatalf("want ToolMissingError naming mysqldump, got %v", err)
	}
	if _, err := DumpTool(dbCfg("postgres"), none); !errors.As(err, &miss) || !strings.Contains(err.Error(), "pg_dump") {
		t.Fatalf("want ToolMissingError naming pg_dump, got %v", err)
	}
	// mariadb-dump 作为后备
	onlyMaria := func(n string) (string, error) {
		if n == "mariadb-dump" {
			return "/x", nil
		}
		return "", errors.New("no")
	}
	if s, err := DumpTool(dbCfg("mariadb"), onlyMaria); err != nil || s.Name != "mariadb-dump" {
		t.Fatalf("want mariadb-dump fallback, got %v %v", s.Name, err)
	}
}

func TestBackupNamesCannotTraverse(t *testing.T) {
	bad := []string{"../x.sql.gz", "..", "a/b.sql.gz", `a\b.sql.gz`, "/etc/passwd", "x..y.sql.gz", "x.sql", "", ".sql.gz", "a\x00b.sql.gz", "-rf.sql.gz", "..%2f.sql.gz"}
	dir := t.TempDir()
	for _, n := range bad {
		if SafeBackupName(n) {
			t.Errorf("%q must not be a safe name", n)
		}
		if _, err := BackupPath(dir, n); err == nil {
			t.Errorf("BackupPath must reject %q", n)
		}
	}
	good := BackupFileName("mysql", "1.2.3", time.Date(2026, 10, 17, 3, 4, 5, 0, time.UTC))
	if good != "nexadmin-mysql-v1.2.3-20261017-030405.sql.gz" {
		t.Fatalf("unexpected name %q", good)
	}
	p, err := BackupPath(dir, good)
	if err != nil || filepath.Dir(p) != dir {
		t.Fatalf("good name rejected: %v %s", err, p)
	}
	// 版本号里带路径字符也不能产生非法文件名
	for _, v := range []string{"../../etc", "1.0/../x", "v 1;2", "", "..", "1..2"} {
		if n := BackupFileName("postgres", v, time.Now()); !SafeBackupName(n) {
			t.Errorf("version %q produced unsafe name %q", v, n)
		}
	}
}

func TestVerifySHA256(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.sql.gz")
	if err := os.WriteFile(p, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum, _ := FileSHA256(p)
	if err := VerifySHA256(p, strings.ToUpper(sum)); err != nil {
		t.Fatalf("matching sha256 rejected: %v", err)
	}
	if err := VerifySHA256(p, strings.Repeat("0", 64)); err == nil {
		t.Fatal("mismatching sha256 accepted")
	}
	if err := WriteSidecar(p, sum); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadSidecar(p); err != nil || got != sum {
		t.Fatalf("sidecar round trip: %v %q", err, got)
	}
	// Windows 不实现 POSIX 权限位，写 0600 也只会报成 0666；只在该断言有意义平台上检查。
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(SidecarPath(p)); fi.Mode().Perm() != 0o600 {
			t.Fatalf("sidecar mode %v", fi.Mode().Perm())
		}
	}
}
