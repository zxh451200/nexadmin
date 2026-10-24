package log

import (
	"bytes"
	"database/sql/driver"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/config"
)

func TestExportRoutesRequireExportPerm(t *testing.T) {
	e, mock, _ := newExportEnv(t, config.RateLimit{}, 0)
	for _, url := range []string{"/api/v1/oper-logs/export", "/api/v1/login-logs/export"} {
		// 只有列表权限、没有 export 权限：必须被服务端拒绝，且不触达数据库
		if w := doGet(e, url, "operLog:list,loginLog:list"); w.Code != http.StatusForbidden {
			t.Errorf("%s 仅有 list 权限 = %d，期望 403", url, w.Code)
		}
		// 交叉权限也不行
		perm := "loginLog:export"
		if strings.Contains(url, "login") {
			perm = "operLog:export"
		}
		if w := doGet(e, url, perm); w.Code != http.StatusForbidden {
			t.Errorf("%s 持有另一类 export 权限 = %d，期望 403", url, w.Code)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestExportOperPassesFiltersAndWhitelistsColumns(t *testing.T) {
	e, mock, cap := newExportEnv(t, config.RateLimit{}, 0)
	kw := "%abc%"
	args := []driver.Value{kw, kw, kw, "failed", sqlmock.AnyArg(), sqlmock.AnyArg(), "export"}
	where := "WHERE .*module LIKE .* OR operator LIKE .* OR url LIKE .*status = .*created_at >= .*created_at < .*type = "
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log` " + where).WithArgs(args...).WillReturnRows(countRows(2))
	ts := time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT `module`,`type`,`operator`,`ip`,`location`,`status`,`cost_ms`,`method`,`url`,`created_at` FROM `sys_oper_log` " + where).
		WithArgs(append(args, 50000)...).
		WillReturnRows(sqlmock.NewRows([]string{"module", "type", "operator", "ip", "location", "status", "cost_ms", "method", "url", "created_at"}).
			AddRow("用户管理", "export", "admin", "10.0.0.1", "深圳", "failed", 12, "GET", "/api/v1/users/export", ts).
			AddRow("角色管理", "export", "li.na", "10.0.0.2", "", "failed", 7, "GET", "=HYPERLINK(\"http://x\")", ts))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_oper_log`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := doGet(e, "/api/v1/oper-logs/export?format=xlsx&keyword=abc&status=failed&type=export&startDate=2026-10-01&endDate=2026-10-02&page=3&pageSize=1", "operLog:export")
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, truncate(w.Body.String(), 200))
	}
	if ct := w.Header().Get("Content-Type"); ct != xlsxContentType {
		t.Errorf("Content-Type=%q", ct)
	}
	cd := w.Header().Get("Content-Disposition")
	if !regexp.MustCompile(`^attachment; filename=oper-log-\d{8}-\d{6}\.xlsx$`).MatchString(cd) {
		t.Errorf("Content-Disposition=%q", cd)
	}
	rows := readXLSX(t, w)
	if len(rows) != 3 || strings.Join(rows[0], ",") != strings.Join(operExportHeader, ",") {
		t.Fatalf("rows=%v", rows)
	}
	if rows[1][0] != "用户管理" || rows[1][5] != "失败" || rows[2][8] != `=HYPERLINK("http://x")` {
		t.Errorf("行内容不对（公式文本应原样作为字符串保留）：%v", rows[1:])
	}
	// 敏感/大字段既不在 SELECT 里，也不在输出里
	for _, sel := range cap.selects() {
		for _, bad := range []string{"params", "response", "error_stack"} {
			if strings.Contains(sel, bad) {
				t.Errorf("导出查询包含敏感列 %s: %s", bad, sel)
			}
		}
	}
	for _, h := range rows[0] {
		if strings.Contains(h, "参数") || strings.Contains(h, "响应") || strings.Contains(h, "异常") {
			t.Errorf("表头含敏感列：%s", h)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestExportOperRecordsExportLog(t *testing.T) {
	e, mock, _ := newExportEnv(t, config.RateLimit{}, 0)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log`").WillReturnRows(countRows(0))
	mock.ExpectQuery("SELECT `module`.* FROM `sys_oper_log`").WillReturnRows(sqlmock.NewRows([]string{"module"}))
	mock.ExpectBegin()
	// 参数里必须出现 type=export 与 模块名；导出读的是写日志之前的快照，所以文件不含自己这一条
	anyv := sqlmock.AnyArg()
	mock.ExpectExec("INSERT INTO `sys_oper_log`").WithArgs(anyv, anyv, anyv, anyv, "操作日志", "export", "app:t", anyv, "", "success", anyv,
		"GET", "/api/v1/oper-logs/export", "", "", "").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	if w := doGet(e, "/api/v1/oper-logs/export", "operLog:export"); w.Code != 200 {
		t.Fatalf("code=%d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestExportRowLimit(t *testing.T) {
	for _, tc := range []struct{ url, perm, table string }{
		{"/api/v1/oper-logs/export", "operLog:export", "sys_oper_log"},
		{"/api/v1/login-logs/export", "loginLog:export", "sys_login_log"},
	} {
		t.Run(tc.table, func(t *testing.T) {
			e, mock, _ := newExportEnv(t, config.RateLimit{}, 2)
			mock.ExpectQuery("SELECT count\\(\\*\\) FROM `" + tc.table + "`").WillReturnRows(countRows(3))
			// 超限也记一条失败的导出日志，但不会再去查数据
			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO `sys_oper_log`").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
			w := doGet(e, tc.url, tc.perm)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code=%d，期望 400", w.Code)
			}
			body := w.Body.String()
			if !strings.Contains(body, "3") || !strings.Contains(body, "2") || !strings.Contains(body, "缩小") || !strings.Contains(body, "导出中心") {
				t.Errorf("提示不明确：%s", body)
			}
			if !strings.Contains(body, ExportTooManyReason) {
				t.Errorf("响应应带 reason %s：%s", ExportTooManyReason, body)
			}
			if strings.Contains(w.Header().Get("Content-Type"), "spreadsheet") || w.Header().Get("Content-Disposition") != "" {
				t.Errorf("超限不应输出文件头")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Run("等于上限放行", func(t *testing.T) {
		e, mock, _ := newExportEnv(t, config.RateLimit{}, 2)
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log`").WillReturnRows(countRows(2))
		mock.ExpectQuery("SELECT `module`.* FROM `sys_oper_log`.*LIMIT").WithArgs(2).WillReturnRows(sqlmock.NewRows([]string{"module"}))
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO `sys_oper_log`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		if w := doGet(e, "/api/v1/oper-logs/export", "operLog:export"); w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestExportBadFormatRejected(t *testing.T) {
	e, mock, _ := newExportEnv(t, config.RateLimit{}, 0)
	if w := doGet(e, "/api/v1/login-logs/export?format=csv", "loginLog:export"); w.Code != http.StatusBadRequest {
		t.Errorf("code=%d，期望 400", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestExportLoginColumnsAndFilters(t *testing.T) {
	e, mock, cap := newExportEnv(t, config.RateLimit{}, 0)
	where := "WHERE .*username LIKE .* OR ip LIKE .*status = .*login_type = "
	args := []driver.Value{"%zh%", "%zh%", "failed", "password"}
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_login_log` " + where).WithArgs(args...).WillReturnRows(countRows(1))
	mock.ExpectQuery("SELECT `username`,`login_type`,`ip`,`location`,`browser`,`os`,`status`,`fail_reason`,`created_at` FROM `sys_login_log` " + where).
		WithArgs(append(args, 50000)...).
		WillReturnRows(sqlmock.NewRows([]string{"username", "login_type", "ip", "location", "browser", "os", "status", "fail_reason", "created_at"}).
			AddRow("zhang.wei", "password", "10.0.0.3", "北京", "Chrome", "macOS", "failed", "credentials", time.Date(2026, 10, 2, 1, 2, 3, 0, time.UTC)))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_oper_log`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	w := doGet(e, "/api/v1/login-logs/export?keyword=zh&status=failed&loginType=password", "loginLog:export")
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, truncate(w.Body.String(), 200))
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "login-log-") {
		t.Errorf("Content-Disposition=%q", w.Header().Get("Content-Disposition"))
	}
	rows := readXLSX(t, w)
	if len(rows) != 2 || rows[1][0] != "zhang.wei" || rows[1][6] != "失败" || rows[1][7] != "credentials" {
		t.Errorf("rows=%v", rows)
	}
	for _, sel := range cap.selects() {
		if strings.Contains(sel, "tenant") || strings.Contains(sel, "`id`") {
			t.Errorf("登录日志导出查询了白名单之外的列: %s", sel)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 数据范围裁剪：受限操作者（部门 50）的导出，count 与数据查询都带“按账号归属”条件。
func TestExportAppliesDataScope(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log` WHERE .*" + inScopeSQL).WillReturnRows(countRows(0))
	mock.ExpectQuery("SELECT `module`.* FROM `sys_oper_log` WHERE .*" + inScopeSQL).WillReturnRows(sqlmock.NewRows([]string{"module"}))
	var buf bytes.Buffer
	if _, err := s.ExportOper(ctx, Query{Scope: limited}, func() io.Writer { return &buf }); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}

	s, mock = newSvc(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_login_log` WHERE .*username IN \\(SELECT username FROM sys_user").WillReturnRows(countRows(0))
	mock.ExpectQuery("SELECT `username`.* FROM `sys_login_log` WHERE .*username IN \\(SELECT username FROM sys_user").WillReturnRows(sqlmock.NewRows([]string{"username"}))
	if _, err := s.ExportLogin(ctx, Query{Scope: limited}, func() io.Writer { return &buf }); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}

	// 超管/全部：不带归属条件
	s, mock = newSvc(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_oper_log`$").WillReturnRows(countRows(0))
	mock.ExpectQuery("SELECT `module`.* FROM `sys_oper_log` ORDER BY").WillReturnRows(sqlmock.NewRows([]string{"module"}))
	if _, err := s.ExportOper(ctx, Query{Scope: all}, func() io.Writer { return &buf }); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestExportRateLimitRuleIsWired(t *testing.T) {
	rl := config.RateLimit{Enabled: true, LogExport: config.Rule{Limit: 1, Window: time.Minute}}
	e, mock, _ := newExportEnv(t, rl, 0)
	mock.ExpectQuery("SELECT count").WillReturnRows(countRows(0))
	mock.ExpectQuery("SELECT `module`").WillReturnRows(sqlmock.NewRows([]string{"module"}))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_oper_log`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	if w := doGet(e, "/api/v1/oper-logs/export", "operLog:export"); w.Code != 200 {
		t.Fatalf("第 1 次 = %d", w.Code)
	}
	w := doGet(e, "/api/v1/oper-logs/export", "operLog:export")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("第 2 次 = %d，期望 429（log_export 规则未生效）", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestLogExportRuleAndDefaults(t *testing.T) {
	if r := (config.RateLimit{Enabled: true, LogExport: config.Rule{Limit: 10, Window: time.Minute}}).Rule("log_export"); r.Limit != 10 || r.Window != time.Minute {
		t.Errorf("Rule(log_export)=%+v", r)
	}
	if r := (config.RateLimit{LogExport: config.Rule{Limit: 10}}).Rule("log_export"); r.Limit != 0 {
		t.Errorf("总开关关闭时应不限流：%+v", r)
	}
	if got := NewService(nil, "UTC", nil).exportMax(); got != 50000 {
		t.Errorf("默认上限=%d", got)
	}
	if got := NewService(nil, "UTC", nil).WithExportMaxRows(7).exportMax(); got != 7 {
		t.Errorf("上限=%d", got)
	}
}
