package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// 内置处理器只做安全的事：http（带 SSRF 防护）、clean_logs（清理旧日志）、log（演示）。
// 不提供 shell / 任意 SQL 之类的处理器；业务方需要时按 handlers.go 里的说明自行注册。

// 表名常量：clean_logs 通过原生 SQL 操作框架的日志表，这样不需要 import 内置模块（插件不得依赖内置模块）。
// 如果框架改了表名，只需要改这里。
const (
	tableOperLog  = "sys_oper_log"
	tableLoginLog = "sys_login_log"
)

const (
	httpDefaultTimeout = 10 * time.Second
	httpMaxTimeout     = 30 * time.Second
	httpMaxBody        = 64 << 10 // 请求体与读取的响应体上限
)

func init() {
	RegisterHandler(HandlerSpec{Name: "http", Description: "Call an HTTP(S) URL (GET/POST, public addresses only)", Validate: validateHTTPParams, Run: runHTTP})
	RegisterHandler(HandlerSpec{Name: "clean_logs", Description: "Delete operation/login logs older than N days", Validate: validateCleanParams, Run: runCleanLogs})
	RegisterHandler(HandlerSpec{Name: "log", Description: "Write a log line (demo)", Validate: validateLogParams, Run: runLog})
}

func decodeParams(params string, v any) error {
	if strings.TrimSpace(params) == "" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(params))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

// ---- http ----

type httpParams struct {
	Method     string `json:"method"`     // GET（默认）或 POST
	URL        string `json:"url"`        // 必填
	Body       string `json:"body"`       // POST 请求体，最大 64KB
	TimeoutSec int    `json:"timeoutSec"` // 默认 10，最大 30
}

func parseHTTPParams(params string) (httpParams, error) {
	var p httpParams
	if err := decodeParams(params, &p); err != nil {
		return p, err
	}
	p.Method = strings.ToUpper(strings.TrimSpace(p.Method))
	if p.Method == "" {
		p.Method = http.MethodGet
	}
	if p.Method != http.MethodGet && p.Method != http.MethodPost {
		return p, errors.New("invalid params: method must be GET or POST")
	}
	if len(p.Body) > httpMaxBody {
		return p, errors.New("invalid params: body too large")
	}
	if p.TimeoutSec < 0 || time.Duration(p.TimeoutSec)*time.Second > httpMaxTimeout {
		return p, fmt.Errorf("invalid params: timeoutSec must be 0-%d", int(httpMaxTimeout/time.Second))
	}
	return p, nil
}

func validateHTTPParams(params string) error {
	p, err := parseHTTPParams(params)
	if err != nil {
		return err
	}
	_, err = validateURLStatic(p.URL)
	return err
}

func runHTTP(ctx context.Context, _ *Env, params string) (string, error) {
	return doHTTP(ctx, params, defaultLookup)
}

func doHTTP(ctx context.Context, params string, lookup lookupFunc) (string, error) {
	p, err := parseHTTPParams(params)
	if err != nil {
		return "", err
	}
	timeout := httpDefaultTimeout
	if p.TimeoutSec > 0 {
		timeout = time.Duration(p.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if _, err := validateURL(ctx, p.URL, lookup); err != nil { // 第 1、2 层
		return "", err
	}
	var body io.Reader
	if p.Method == http.MethodPost && p.Body != "" {
		body = bytes.NewBufferString(p.Body)
	}
	req, err := http.NewRequestWithContext(ctx, p.Method, strings.TrimSpace(p.URL), body)
	if err != nil {
		return "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "nexadmin-cron/1.0")

	resp, err := newSafeClient(timeout).Do(req) // 第 3 层在 Dialer.Control 里
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10)) // 日志里只留前 4KB
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, httpMaxBody))
	out := fmt.Sprintf("%s %s -> %d\n%s", p.Method, p.URL, resp.StatusCode, data)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return out, nil
}

// ---- clean_logs ----

type cleanParams struct {
	Days    int      `json:"days"`    // 清理 N 天前的日志，默认 30，范围 1-3650
	Targets []string `json:"targets"` // oper / login，默认两者都清
}

func parseCleanParams(params string) (cleanParams, error) {
	var p cleanParams
	if err := decodeParams(params, &p); err != nil {
		return p, err
	}
	if p.Days == 0 {
		p.Days = 30
	}
	if p.Days < 1 || p.Days > 3650 {
		return p, errors.New("invalid params: days must be 1-3650")
	}
	if len(p.Targets) == 0 {
		p.Targets = []string{"oper", "login"}
	}
	for _, t := range p.Targets {
		if t != "oper" && t != "login" {
			return p, fmt.Errorf("invalid params: unknown target %q (want oper or login)", t)
		}
	}
	return p, nil
}

func validateCleanParams(params string) error { _, err := parseCleanParams(params); return err }

func runCleanLogs(ctx context.Context, env *Env, params string) (string, error) {
	p, err := parseCleanParams(params)
	if err != nil {
		return "", err
	}
	cutoff := time.Now().AddDate(0, 0, -p.Days)
	var lines []string
	var failed []string
	for _, target := range p.Targets {
		table := tableOperLog
		if target == "login" {
			table = tableLoginLog
		}
		// 表可能不存在（对应内置模块被 disabled_modules 停用或尚未迁移）：优雅报错，不影响另一张表。
		if !env.DB.Migrator().HasTable(table) {
			failed = append(failed, table)
			lines = append(lines, fmt.Sprintf("%s: table does not exist, skipped", table))
			continue
		}
		// 原生 SQL 不经过租户 scope，必须显式带 tenant_id，只清理任务所属租户的日志。
		res := env.DB.WithContext(ctx).Exec("DELETE FROM "+table+" WHERE tenant_id = ? AND created_at < ?", env.TenantID, cutoff)
		if res.Error != nil {
			failed = append(failed, table)
			lines = append(lines, fmt.Sprintf("%s: delete failed: %v", table, res.Error))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: deleted %d rows older than %d days", table, res.RowsAffected, p.Days))
	}
	out := strings.Join(lines, "\n")
	if len(failed) > 0 {
		return out, fmt.Errorf("clean_logs incomplete: %s", strings.Join(failed, ", "))
	}
	return out, nil
}

// ---- log ----

type logParams struct {
	Message string `json:"message"`
}

func validateLogParams(params string) error {
	var p logParams
	if err := decodeParams(params, &p); err != nil {
		return err
	}
	if len(p.Message) > 500 {
		return errors.New("invalid params: message too long")
	}
	return nil
}

func runLog(_ context.Context, env *Env, params string) (string, error) {
	var p logParams
	if err := decodeParams(params, &p); err != nil {
		return "", err
	}
	if p.Message == "" {
		p.Message = "hello from cron"
	}
	env.Log.Info("cron log handler", zap.Int64("jobId", env.JobID), zap.String("message", p.Message))
	return p.Message, nil
}
