package ctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"nexadmin/core/config"
)

func TestPidFileRoundTrip(t *testing.T) {
	path := PidPath(t.TempDir())
	want := PidFile{PID: 1234, Port: 8080, Token: NewToken(), Started: time.Now().UTC().Truncate(time.Second)}
	if err := WritePid(path, want); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("pid file mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	got, err := ReadPid(path)
	if err != nil || got.PID != want.PID || got.Port != want.Port || got.Token != want.Token {
		t.Fatalf("round trip: got %+v err %v", got, err)
	}
	// 覆盖写不能放宽权限
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0o644)
		_ = WritePid(path, want)
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode after rewrite = %v, want 0600", fi.Mode().Perm())
		}
	}
	if _, err := ReadPid(filepath.Join(t.TempDir(), "none")); !os.IsNotExist(err) {
		t.Fatalf("missing file should be IsNotExist, got %v", err)
	}
}

func TestTokenOK(t *testing.T) {
	tok := NewToken()
	if len(tok) != 32 || tok == NewToken() {
		t.Fatalf("token should be 32 hex chars and random, got %q", tok)
	}
	cases := []struct {
		want, got string
		ok        bool
	}{{tok, tok, true}, {tok, tok + "x", false}, {tok, "", false}, {"", "", false}, {"", tok, false}}
	for _, c := range cases {
		if TokenOK(c.want, c.got) != c.ok {
			t.Errorf("TokenOK(%q,%q) != %v", c.want, c.got, c.ok)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:5000": true, "[::1]:5000": true, "10.1.2.3:5000": false, "192.168.0.9:1": false, "garbage": false, "": false,
	} {
		if IsLoopback(addr) != want {
			t.Errorf("IsLoopback(%q) != %v", addr, want)
		}
	}
}

// Stop 要带上 pid 文件里的令牌调用关停接口，成功后服务退出并清理 pid 文件。
func TestStopCallsShutdownWithToken(t *testing.T) {
	const token = "secret-token"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/system/status":
			fmt.Fprint(w, `{"code":0,"data":{"initialized":true}}`)
		case r.URL.Path == ShutdownPath && r.Method == http.MethodPost && TokenOK(token, r.Header.Get(ShutdownHeader)):
			fmt.Fprint(w, `{"code":0}`)
			go srv.Close()
		default:
			http.Error(w, "forbidden", http.StatusForbidden)
		}
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])

	cfg := &config.Config{DataDir: t.TempDir()}
	cfg.Server.Port = port
	var out bytes.Buffer
	c := New(cfg, "unused", &out)

	// 令牌不对：被拒绝，pid 文件保留
	_ = WritePid(c.pidPath(), PidFile{PID: 42, Port: port, Token: "wrong"})
	if err := c.Stop(); err == nil {
		t.Fatal("Stop with wrong token should fail")
	}
	if _, err := os.Stat(c.pidPath()); err != nil {
		t.Fatal("pid file must be kept when stop is refused")
	}

	_ = WritePid(c.pidPath(), PidFile{PID: 42, Port: port, Token: token})
	if err := c.Stop(); err != nil {
		t.Fatalf("Stop: %v (%s)", err, out.String())
	}
	if _, err := os.Stat(c.pidPath()); !os.IsNotExist(err) {
		t.Fatal("pid file should be removed after stop")
	}
}

func TestStopWhenNotRunningCleansStalePid(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0") // 取一个肯定没人监听的端口
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	cfg := &config.Config{DataDir: t.TempDir()}
	cfg.Server.Port = port
	c := New(cfg, "unused", &bytes.Buffer{})
	_ = WritePid(c.pidPath(), PidFile{PID: 1, Port: port, Token: "x"})
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.pidPath()); !os.IsNotExist(err) {
		t.Fatal("stale pid file should be removed")
	}
}

// ---- reinit：接口抽象 + 假实现 ----

type fakeDB struct {
	info    DBInfo
	err     error
	dropped []string
}

func (f *fakeDB) Inspect(context.Context) (DBInfo, error) { return f.info, f.err }
func (f *fakeDB) DropTables(_ context.Context, t []string) error {
	f.dropped = append(f.dropped, t...)
	return nil
}
func (f *fakeDB) Close() error { return nil }

type fakeSvc struct {
	st            State
	stops, starts int
}

func (f *fakeSvc) State() State { return f.st }
func (f *fakeSvc) Stop() error  { f.stops++; return nil }
func (f *fakeSvc) Start() error { f.starts++; return nil }

type fakeBackup struct {
	tool  string
	calls int
	err   error
	dir   string // 最近一次收到的备份目录
}

func (f *fakeBackup) Tool() string { return f.tool }
func (f *fakeBackup) Backup(_ context.Context, dir, stamp string) (string, error) {
	f.calls++
	f.dir = dir
	return "./backup/nexadmin-reinit-mysql-vdev-" + stamp + ".sql.gz", f.err
}

type fakePurger struct{ n, purged int }

func (f *fakePurger) CountKeys(context.Context) (int, error) { return f.n, nil }
func (f *fakePurger) Purge(context.Context) (int, error)     { f.purged++; return f.n, nil }
func (f *fakePurger) Close() error                           { return nil }

type env struct {
	r      *Reinit
	db     *fakeDB
	svc    *fakeSvc
	bk     *fakeBackup
	out    *bytes.Buffer
	upload string
	marker string
}

func newEnv(t *testing.T, input string, production bool) *env {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir}
	cfg.App.Env = map[bool]string{true: "production", false: "dev"}[production]
	up := filepath.Join(dir, "uploads")
	_ = os.MkdirAll(filepath.Join(up, "a"), 0o755)
	_ = os.WriteFile(filepath.Join(up, "a", "1.bin"), make([]byte, 2048), 0o644)
	_ = os.WriteFile(filepath.Join(up, "2.bin"), make([]byte, 1024), 0o644)
	marker := filepath.Join(dir, ".initialized")
	_ = os.WriteFile(marker, []byte("x"), 0o600)
	e := &env{
		db:     &fakeDB{info: DBInfo{Engine: "mysql", Host: "127.0.0.1", Port: 3306, Name: "nexadmin", Tables: []string{"sys_user", "sys_role"}, Rows: 128000, Bytes: 184 << 20}},
		svc:    &fakeSvc{st: State{Running: true, Pid: &PidFile{PID: 1234}}},
		bk:     &fakeBackup{tool: "mysqldump"},
		out:    &bytes.Buffer{},
		upload: up, marker: marker,
	}
	e.r = &Reinit{Cfg: cfg, Svc: e.svc, DB: e.db, Backup: e.bk, In: strings.NewReader(input), Out: e.out,
		Now: func() time.Time { return time.Date(2026, 10, 16, 14, 18, 25, 0, time.UTC) }}
	return e
}

func TestPlanUsesActuallyReadValues(t *testing.T) {
	e := newEnv(t, "", false)
	e.r.Cache = &fakePurger{n: 7}
	p, err := e.r.BuildPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.PID != 1234 || !p.Running || p.Uploads.Files != 2 || p.Uploads.Bytes != 3072 || p.CacheKeys != 7 || p.BackupTool != "mysqldump" {
		t.Fatalf("unexpected plan: %+v", p)
	}
	var buf bytes.Buffer
	p.Print(&buf, ".initialized")
	for _, want := range []string{"pid 1234", "mysql://127.0.0.1:3306/nexadmin", "共 2 张表", "约 12.8 万行", "184.0 MB", "nex:*", "2 个文件", "将用 mysqldump 备份到 ./backup/（nexadmin-reinit-*-20261016-141825.sql.gz"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("plan output missing %q:\n%s", want, buf.String())
		}
	}
}

func TestPlanAbortsWhenCannotRead(t *testing.T) {
	e := newEnv(t, "y\n", false)
	e.db.err = errors.New("access denied")
	if err := e.r.Run(context.Background(), false); err == nil || !strings.Contains(err.Error(), "中止") {
		t.Fatalf("want abort error, got %v", err)
	}
	e.db.err, e.db.info.Name = nil, "" // 读到空库名也要中止
	if _, err := e.r.BuildPlan(context.Background()); err == nil {
		t.Fatal("empty database name must abort")
	}
	if e.bk.calls != 0 || len(e.db.dropped) != 0 || e.svc.stops != 0 {
		t.Fatal("nothing may be executed when the plan cannot be built")
	}
}

func TestDryRunOnlyLists(t *testing.T) {
	e := newEnv(t, "y\n", false)
	if err := e.r.Run(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if e.bk.calls != 0 || len(e.db.dropped) != 0 || e.svc.stops != 0 || e.svc.starts != 0 {
		t.Fatal("dry-run must not back up or change anything")
	}
	if _, err := os.Stat(e.marker); err != nil {
		t.Fatal("dry-run must keep the marker")
	}
	if !strings.Contains(e.out.String(), "dry-run") {
		t.Fatal("dry-run output should say so")
	}
}

func TestOnlyLowercaseYExecutes(t *testing.T) {
	for _, in := range []string{"", "\n", "n\n", "Y\n", "yes\n", "N\n"} {
		e := newEnv(t, in, false)
		if err := e.r.Run(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if len(e.db.dropped) != 0 || e.svc.stops != 0 {
			t.Errorf("input %q must cancel", in)
		}
		if _, err := os.Stat(e.upload); err != nil {
			t.Errorf("input %q must keep uploads", in)
		}
	}
}

func TestConfirmedRunExecutesEverything(t *testing.T) {
	e := newEnv(t, "y\n", false)
	pg := &fakePurger{n: 3}
	e.r.Cache = pg
	if err := e.r.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if e.bk.calls != 1 || e.svc.stops != 1 || e.svc.starts != 1 || pg.purged != 1 || len(e.db.dropped) != 2 {
		t.Fatalf("bk=%d stops=%d starts=%d purged=%d dropped=%v", e.bk.calls, e.svc.stops, e.svc.starts, pg.purged, e.db.dropped)
	}
	for _, p := range []string{e.upload, e.marker} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be removed", p)
		}
	}
	if !strings.Contains(e.out.String(), "已用 mysqldump 备份到") {
		t.Errorf("plan should show finished backup:\n%s", e.out.String())
	}
}

func TestProductionNeedsDatabaseName(t *testing.T) {
	e := newEnv(t, "y\nwrong\n", true)
	if err := e.r.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(e.db.dropped) != 0 {
		t.Fatal("wrong database name must cancel")
	}
	e = newEnv(t, "y\nnexadmin\n", true)
	if err := e.r.Run(context.Background(), false); err != nil || len(e.db.dropped) != 2 {
		t.Fatalf("correct name should execute: err=%v dropped=%v", err, e.db.dropped)
	}
}

func TestNoBackupToolIsStatedAndBackupFailureAborts(t *testing.T) {
	e := newEnv(t, "n\n", false)
	e.bk.tool = ""
	if err := e.r.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.out.String(), "备份：未备份") {
		t.Fatalf("missing 未备份 line:\n%s", e.out.String())
	}
	e = newEnv(t, "y\n", false)
	e.bk.err = errors.New("boom")
	if err := e.r.Run(context.Background(), false); err == nil || len(e.db.dropped) != 0 || e.svc.stops != 0 {
		t.Fatalf("backup failure must abort before touching anything: err=%v", err)
	}
}

func TestRunningWithoutPidFileAborts(t *testing.T) {
	e := newEnv(t, "y\n", false)
	e.svc.st = State{Running: true}
	if _, err := e.r.BuildPlan(context.Background()); err == nil {
		t.Fatal("running service that cannot be stopped must abort")
	}
}
