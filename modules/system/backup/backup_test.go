package backup

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/config"
	"nexadmin/core/ctl"
)

func rec(id int, ageDays int, status string, now time.Time) Record {
	r := Record{FileName: "f.sql.gz", Status: status}
	r.ID = 0
	r.CreatedAt = now.AddDate(0, 0, -ageDays).Add(time.Duration(id) * time.Second)
	r.Size = int64(id) // 用 Size 当作标识
	return r
}

func ids(rs []Record) map[int64]bool {
	m := map[int64]bool{}
	for _, r := range rs {
		m[r.Size] = true
	}
	return m
}

func TestRetentionByCount(t *testing.T) {
	now := time.Now()
	var all []Record
	for i := 1; i <= 5; i++ { // 1 最新 ... 5 最旧
		all = append(all, rec(i, i, StatusSuccess, now))
	}
	got := ids(selectExpired(all, now, 0, 3))
	if len(got) != 2 || !got[4] || !got[5] {
		t.Fatalf("keep_count=3 should expire the 2 oldest, got %v", got)
	}
}

func TestRetentionByDaysKeepsNewestAndSkipsRunning(t *testing.T) {
	now := time.Now()
	all := []Record{
		rec(1, 40, StatusSuccess, now), // 唯一的成功备份，哪怕过期也保留
		rec(2, 50, StatusFailed, now),  // 过期的失败记录要清
		rec(3, 1, StatusFailed, now),   // 新的失败记录保留
		rec(4, 90, StatusRunning, now), // 运行中的永远不动
	}
	got := ids(selectExpired(all, now, 30, 0))
	if len(got) != 1 || !got[2] {
		t.Fatalf("want only the old failed record expired, got %v", got)
	}
	all = append(all, rec(5, 0, StatusSuccess, now))
	got = ids(selectExpired(all, now, 30, 0))
	if !got[1] || !got[2] || got[5] || got[3] || got[4] {
		t.Fatalf("with a newer success, the 40-day-old one expires: %v", got)
	}
}

func TestRetentionDisabledAndCombined(t *testing.T) {
	now := time.Now()
	all := []Record{rec(1, 0, StatusSuccess, now), rec(2, 100, StatusSuccess, now)}
	if got := selectExpired(all, now, 0, 0); len(got) != 0 {
		t.Fatalf("0/0 must keep everything, got %v", got)
	}
	all = []Record{rec(1, 0, StatusSuccess, now), rec(2, 1, StatusSuccess, now), rec(3, 2, StatusSuccess, now), rec(4, 60, StatusSuccess, now)}
	got := ids(selectExpired(all, now, 30, 3))
	if len(got) != 1 || !got[4] {
		t.Fatalf("days and count both apply, got %v", got)
	}
}

func TestNextRun(t *testing.T) {
	loc := time.UTC
	at := func(h, m int) time.Time { return time.Date(2026, 10, 17, h, m, 0, 0, loc) }
	if nextRun("", at(1, 0), "") != nil {
		t.Fatal("empty schedule has no next run")
	}
	if n := nextRun("03:30", at(1, 0), ""); n == nil || !n.Equal(at(3, 30)) {
		t.Fatalf("before time -> today, got %v", n)
	}
	if n := nextRun("03:30", at(4, 0), ""); n == nil || !n.Equal(at(3, 30).AddDate(0, 0, 1)) {
		t.Fatalf("after time -> tomorrow, got %v", n)
	}
	if n := nextRun("03:30", at(1, 0), "2026-10-17"); n == nil || !n.Equal(at(3, 30).AddDate(0, 0, 1)) {
		t.Fatalf("already claimed today -> tomorrow, got %v", n)
	}
	if !dueNow("03:30", at(3, 30)) || dueNow("03:30", at(3, 29)) {
		t.Fatal("dueNow boundary")
	}
}

func TestConfigValidation(t *testing.T) {
	for _, bad := range []ConfigInput{{Schedule: "25:00"}, {Schedule: "3:5"}, {Schedule: "03:30 "}, {Schedule: "abc"}, {KeepDays: -1}, {KeepCount: -1}, {KeepDays: 99999}, {KeepCount: 99999}} {
		if bad.Validate() == nil {
			t.Errorf("%+v must be invalid", bad)
		}
	}
	for _, ok := range []ConfigInput{{}, {Schedule: "03:30", KeepDays: 30, KeepCount: 10}, {Schedule: "00:00"}, {Schedule: "23:59"}} {
		if err := ok.Validate(); err != nil {
			t.Errorf("%+v should be valid: %v", ok, err)
		}
	}
}

// ---- 工具缺失：available=false + reason，且不做任何降级导出 ----

func noTools(string) (string, error) { return "", errors.New("not found") }

func newTestService(look ctl.LookPath, dir string) *Service {
	cfg := &config.Config{}
	cfg.Backup.Dir = dir
	cfg.Database = config.Database{Type: "mysql", Host: "127.0.0.1", Port: 3306, User: "root", Password: "pw", Name: "nexadmin"}
	s := NewService(nil, zap.NewNop(), cfg, "1.0.0") // db 为 nil：这些用例不得访问数据库
	s.dumper = toolDumper{cfg: cfg.Database, look: look}
	return s
}

func TestToolMissingReportsUnavailableWithReason(t *testing.T) {
	s := newTestService(noTools, t.TempDir())
	st := s.ToolStatus()
	if st.Available || !strings.Contains(st.Reason, "mysqldump") {
		t.Fatalf("want available=false with reason naming mysqldump, got %+v", st)
	}
	b, _ := json.Marshal(st)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["available"] != false || m["reason"] == "" || m["reason"] == nil {
		t.Fatalf("JSON must carry available:false and reason, got %s", b)
	}
	// 立即备份在工具缺失时直接拒绝，不会创建记录或文件（db 为 nil，访问即 panic）
	_, err := s.Start(context.Background(), SourceManual, 1)
	var un *UnavailableError
	if !errors.As(err, &un) || un.Reason == "" {
		t.Fatalf("want UnavailableError with reason, got %v", err)
	}
}

func TestToolPresentIsAvailable(t *testing.T) {
	s := newTestService(func(string) (string, error) { return "/bin/x", nil }, t.TempDir())
	if st := s.ToolStatus(); !st.Available || st.Tool != "mysqldump" || st.Reason != "" {
		t.Fatalf("unexpected %+v", st)
	}
}

// ---- 下载路径：文件名防穿越 ----

func TestServicePathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	s := newTestService(noTools, dir)
	for _, n := range []string{"../../etc/passwd", "../x.sql.gz", "/etc/passwd", "a/../../b.sql.gz", "x.sql.gz/../../y.sql.gz", `..\..\x.sql.gz`} {
		if p, err := s.Path(&Record{FileName: n}); err == nil {
			t.Errorf("%q must be rejected, got %s", n, p)
		}
	}
	if p, err := s.Path(&Record{FileName: "nexadmin-mysql-v1.0.0-20261017-030405.sql.gz"}); err != nil || !strings.HasPrefix(p, dir) {
		t.Fatalf("valid name rejected: %v %s", err, p)
	}
}

func TestRoutesRegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	routes(e.Group("/api/v1"), NewHandler(nil)) // gin 在路由冲突时 panic
	want := map[string]bool{"GET /api/v1/backups": false, "POST /api/v1/backups": false, "GET /api/v1/backups/status": false,
		"GET /api/v1/backups/config": false, "PUT /api/v1/backups/config": false, "GET /api/v1/backups/:id/download": false, "DELETE /api/v1/backups/:id": false}
	for _, r := range e.Routes() {
		delete(want, r.Method+" "+r.Path)
	}
	if len(want) != 0 {
		t.Fatalf("missing routes: %v", want)
	}
}
