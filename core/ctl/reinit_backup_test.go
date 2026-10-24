package ctl

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"nexadmin/core/config"
)

// reinit 的备份必须与备份页同格式（gzip + sha256 旁路文件、同目录、nexadmin-*.sql.gz 命名），
// 并且能被 restore 直接识别和校验。用 PATH 里的假 mysqldump 跑真实的 gzipBackup。
func TestReinitBackupIsRestorableFormat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\necho '-- fake dump'\necho 'CREATE TABLE t (id int);'\n"
	if err := os.WriteFile(filepath.Join(bin, "mysqldump"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := &config.Config{DataDir: t.TempDir()}
	cfg.Backup.Dir = filepath.Join(t.TempDir(), "data", "backup")
	cfg.Database = config.Database{Type: "mysql", Host: "127.0.0.1", Port: 3306, User: "root", Name: "nexadmin"}

	bk := &gzipBackup{cfg: cfg.Database, version: "1.2.3", kind: "reinit"}
	path, err := bk.Backup(context.Background(), BackupDir(cfg), "20261016-141825")
	if err != nil {
		t.Fatal(err)
	}
	// 命名与目录
	if want := filepath.Join(cfg.Backup.Dir, "nexadmin-reinit-mysql-v1.2.3-20261016-141825.sql.gz"); path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	if !SafeBackupName(filepath.Base(path)) {
		t.Fatalf("%s must be a safe backup name", filepath.Base(path))
	}
	// gzip 内容 + 0600 + sha256 旁路文件
	f, _ := os.Open(path)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	if b, _ := io.ReadAll(gz); !strings.Contains(string(b), "CREATE TABLE t") {
		t.Fatalf("unexpected content %q", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	sum, err := ReadSidecar(path)
	if err != nil {
		t.Fatalf("missing sha256 sidecar: %v", err)
	}
	if err := VerifySHA256(path, sum); err != nil {
		t.Fatal(err)
	}

	// restore 直接接受它：格式识别 + sha256 校验通过，进入清单（dry-run 不执行任何导入）。
	var out bytes.Buffer
	r := &Restore{
		Cfg: cfg, Svc: &fakeSvc{}, Pre: &fakeBackup{tool: "mysqldump"}, Tool: &fakeRestorer{tool: "mysql"},
		DB:  &fakeDB{info: DBInfo{Engine: "mysql", Host: "127.0.0.1", Port: 3306, Name: "nexadmin"}},
		Out: &out, In: strings.NewReader(""), Now: time.Now,
	}
	if err := r.Run(context.Background(), RestoreOptions{File: path, DryRun: true}); err != nil {
		t.Fatalf("restore must accept the reinit backup: %v", err)
	}
	if !strings.Contains(out.String(), "校验 sha256 通过") {
		t.Errorf("restore plan missing sha256 check:\n%s", out.String())
	}
}

// reinit 的备份目录取 backup.dir（与备份页、restore 同一个），不再写死 ./backup。
func TestReinitUsesConfiguredBackupDir(t *testing.T) {
	e := newEnv(t, "n\n", false)
	e.r.Cfg.Backup.Dir = "/app/data/backup"
	if err := e.r.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if e.bk.dir != "/app/data/backup" {
		t.Fatalf("backup dir = %q, want /app/data/backup", e.bk.dir)
	}
	// dry-run 清单里（还没备份时）显示的也是配置的目录，绝对路径不加 "./"。
	p, err := e.r.BuildPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	p.Print(&buf, ".initialized")
	if !strings.Contains(buf.String(), "备份到 /app/data/backup/（") {
		t.Errorf("plan should show the configured dir:\n%s", buf.String())
	}
}
