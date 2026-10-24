package ctl

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nexadmin/core/config"
)

type fakeRestorer struct {
	tool  string
	calls int
	err   error
}

func (f *fakeRestorer) Tool() string { return f.tool }
func (f *fakeRestorer) Restore(context.Context, string) error {
	f.calls++
	return f.err
}

type restoreEnv struct {
	r    *Restore
	svc  *fakeSvc
	pre  *fakeBackup
	tool *fakeRestorer
	out  *bytes.Buffer
	file string
}

func newRestoreEnv(t *testing.T, input string, production bool) *restoreEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir}
	cfg.Backup.Dir = filepath.Join(dir, "backup")
	cfg.App.Env = map[bool]string{true: "production", false: "dev"}[production]
	file := filepath.Join(dir, "nexadmin-mysql-v1-20261017-030405.sql.gz")
	if err := os.WriteFile(file, []byte("backup-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum, _ := FileSHA256(file)
	if err := WriteSidecar(file, sum); err != nil {
		t.Fatal(err)
	}
	e := &restoreEnv{
		svc:  &fakeSvc{},
		pre:  &fakeBackup{tool: "mysqldump"},
		tool: &fakeRestorer{tool: "mysql"},
		out:  &bytes.Buffer{},
		file: file,
	}
	db := &fakeDB{info: DBInfo{Engine: "mysql", Host: "127.0.0.1", Port: 3306, Name: "nexadmin", Tables: []string{"sys_user"}, Rows: 10, Bytes: 1 << 20}}
	e.r = &Restore{Cfg: cfg, Svc: e.svc, DB: db, Pre: e.pre, Tool: e.tool, In: strings.NewReader(input), Out: e.out,
		Now: func() time.Time { return time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC) }}
	return e
}

func TestRestoreRefusesWhenSHA256Mismatch(t *testing.T) {
	e := newRestoreEnv(t, "y\n", false)
	if err := os.WriteFile(e.file, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := e.r.Run(context.Background(), RestoreOptions{File: e.file, Yes: true})
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("want sha256 error, got %v", err)
	}
	if e.tool.calls != 0 || e.pre.calls != 0 {
		t.Fatal("nothing may run when sha256 does not match")
	}
	// dry-run 也要校验
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file, DryRun: true}); err == nil {
		t.Fatal("dry-run must verify sha256 too")
	}
}

func TestRestoreRefusesWithoutChecksum(t *testing.T) {
	e := newRestoreEnv(t, "y\n", false)
	_ = os.Remove(SidecarPath(e.file))
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file, Yes: true}); err == nil {
		t.Fatal("restore without any checksum must be refused")
	}
	// 手工给 --sha256 则可以
	sum, _ := FileSHA256(e.file)
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file, Yes: true, SHA256: sum}); err != nil {
		t.Fatalf("explicit sha256 should work: %v", err)
	}
}

func TestRestoreRefusesWhileServiceRunning(t *testing.T) {
	e := newRestoreEnv(t, "y\n", false)
	e.svc.st = State{Running: true, Pid: &PidFile{PID: 42}}
	err := e.r.Run(context.Background(), RestoreOptions{File: e.file, Yes: true})
	if err == nil || !strings.Contains(err.Error(), "正在运行") {
		t.Fatalf("want running refusal, got %v", err)
	}
	if e.tool.calls != 0 || e.pre.calls != 0 {
		t.Fatal("nothing may run while the service is running")
	}
	// dry-run 只列清单，并提示服务在运行
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file, DryRun: true}); err != nil || !strings.Contains(e.out.String(), "正在运行") {
		t.Fatalf("dry-run should list and warn: %v\n%s", err, e.out.String())
	}
	if e.tool.calls != 0 {
		t.Fatal("dry-run must not restore")
	}
}

func TestRestoreDefaultsToNoAndOnlyYRestores(t *testing.T) {
	for _, in := range []string{"", "\n", "n\n", "Y\n", "yes\n"} {
		e := newRestoreEnv(t, in, false)
		if err := e.r.Run(context.Background(), RestoreOptions{File: e.file}); err != nil {
			t.Fatal(err)
		}
		if e.tool.calls != 0 || e.pre.calls != 0 {
			t.Fatalf("input %q must cancel", in)
		}
	}
	e := newRestoreEnv(t, "y\n", false)
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file}); err != nil {
		t.Fatal(err)
	}
	if e.pre.calls != 1 || e.tool.calls != 1 {
		t.Fatalf("pre-backup=%d restore=%d, want 1/1", e.pre.calls, e.tool.calls)
	}
	if !strings.Contains(e.out.String(), "mysql://127.0.0.1:3306/nexadmin") {
		t.Fatalf("plan must list the database to be overwritten:\n%s", e.out.String())
	}
}

func TestRestoreProductionNeedsDatabaseName(t *testing.T) {
	e := newRestoreEnv(t, "y\nwrong\n", true)
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file}); err != nil || e.tool.calls != 0 {
		t.Fatalf("wrong db name must cancel: %v calls=%d", err, e.tool.calls)
	}
	e = newRestoreEnv(t, "y\nnexadmin\n", true)
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file}); err != nil || e.tool.calls != 1 {
		t.Fatalf("right db name must restore: %v calls=%d", err, e.tool.calls)
	}
}

func TestRestoreAbortsWhenPreBackupFailsOrToolMissing(t *testing.T) {
	e := newRestoreEnv(t, "y\n", false)
	e.pre.err = errors.New("disk full")
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file}); err == nil || e.tool.calls != 0 {
		t.Fatalf("pre-backup failure must abort before restoring: %v calls=%d", err, e.tool.calls)
	}
	e = newRestoreEnv(t, "y\n", false)
	e.pre.tool = ""
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file, Yes: true}); err == nil || e.tool.calls != 0 {
		t.Fatalf("missing dump tool must abort: %v", err)
	}
	e = newRestoreEnv(t, "y\n", false)
	e.tool.tool = ""
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file, Yes: true}); err == nil {
		t.Fatal("missing restore tool must abort")
	}
}

func TestRestoreDryRunOnlyLists(t *testing.T) {
	e := newRestoreEnv(t, "y\n", false)
	if err := e.r.Run(context.Background(), RestoreOptions{File: e.file, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if e.tool.calls != 0 || e.pre.calls != 0 || !strings.Contains(e.out.String(), "--dry-run") {
		t.Fatalf("dry-run must only list:\n%s", e.out.String())
	}
}
