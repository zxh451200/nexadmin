// Package migrate 是统一的迁移运行器：框架（migrations/framework）、业务（migrations/business）、
// 插件（plugins/<id>/migrations）三类迁移共用一套规则，已执行的文件记录在版本表 sys_migration。
//
// 规则：
//   - 按文件名排序，只执行没记录的文件；已记录文件的 checksum 变了（违反“只增不改”）就拒绝继续。
//   - Postgres 每个文件一个事务；MySQL/MariaDB 的 DDL 不能回滚，逐条执行，失败时报告执行到第几条。
//   - 多实例并发靠数据库级锁，拿到锁后重新读版本表，别的实例已做完的直接跳过。
//   - 迁移里的 INSERT/UPDATE/DELETE 只给 WARN，不阻断（出厂数据应由 Seeder 种入）。
//
// 存量库基线：某个 scope 在版本表里一条记录都没有（升级前建的库）时，该 scope 的未记录文件按
// “CREATE TABLE 的目标表已存在就跳过该表及其索引，不存在才执行”处理；全部跳过则只记录不执行。
// 之后这个 scope 恢复严格模式。插件的 sys_plugin_migration 旧记录会被直接导入。
package migrate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Logger 只要 Infof / Warnf，zap.SugaredLogger 直接满足。
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Infof(string, ...any) {}
func (nopLogger) Warnf(string, ...any) {}

// Runner 执行迁移。
type Runner struct {
	Store    Store
	Sources  []Source
	Log      Logger
	LockPoll time.Duration       // 拿不到锁时的重试间隔，默认 500ms
	LockWait time.Duration       // 最长等待，默认 2 分钟
	Sleep    func(time.Duration) // 默认 time.Sleep，测试里替换
}

// NewRunner 用真实数据库之外的 Store 创建运行器（单测用），或配合 NewGormStore 使用。
func NewRunner(st Store, log Logger, sources ...Source) *Runner {
	if log == nil {
		log = nopLogger{}
	}
	return &Runner{Store: st, Sources: sources, Log: log, LockPoll: 500 * time.Millisecond, LockWait: 2 * time.Minute, Sleep: time.Sleep}
}

// Item 是一个待执行的迁移文件。
type Item struct {
	File
	Stmts    []string // 实际要执行的语句（基线时可能少于文件里的全部语句，甚至为空）
	Baseline bool     // 存量库基线：表已存在，只记录不执行（Stmts 为空）
	Warnings []string // DML 警告
}

// Applied 是本次实际处理过的文件。
type Applied struct {
	Scope, File string
	Baseline    bool
}

// Result 是一次 Up 的结果。
type Result struct{ Applied []Applied }

// ChecksumError 表示已执行的迁移文件被改过。
type ChecksumError struct{ Files []string } // 形如 framework/system_user.sql

func (e *ChecksumError) Error() string {
	return "迁移文件在执行后被修改（违反“只增不改”），拒绝启动: " + strings.Join(e.Files, ", ") +
		"。请还原这些文件，需要的改动请新增后续迁移文件"
}

// PendingError 表示 app.auto_migrate=false 时存在待执行迁移。
type PendingError struct{ Files []string }

func (e *PendingError) Error() string {
	return fmt.Sprintf("有 %d 个待执行迁移（%s），app.auto_migrate=false，请先运行 nexadmin-server migrate",
		len(e.Files), strings.Join(e.Files, ", "))
}

var (
	createTableRe = regexp.MustCompile("(?is)^CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[`\"]?([A-Za-z0-9_]+)")
	dmlRe         = regexp.MustCompile(`(?is)^(INSERT|UPDATE|DELETE)\b`)
)

// Split 去掉 “--” 注释行，按分号切分语句（迁移文件里不写含分号的字符串）。
func Split(sql string) []string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	var out []string
	for _, s := range strings.Split(b.String(), ";") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// DMLWarnings 找出语句里的 INSERT/UPDATE/DELETE。
func DMLWarnings(f File, stmts []string) []string {
	var out []string
	for _, s := range stmts {
		if m := dmlRe.FindString(s); m != "" {
			out = append(out, fmt.Sprintf("迁移 %s/%s 含数据操作 %s：设计规则禁止在迁移里写 INSERT/UPDATE/DELETE，出厂数据请用 Seeder",
				f.Scope, f.Name, strings.ToUpper(m)))
		}
	}
	return out
}

func key(scope, name string) string { return scope + "/" + name }

// scan 比对磁盘文件与版本表，返回每个文件的状态与待执行项。tableExists=false 时视为一条记录都没有。
func (r *Runner) scan() (status []FileStatus, items []Item, err error) {
	files, err := Discover(r.Store.Engine(), r.Sources)
	if err != nil {
		return nil, nil, err
	}
	applied := map[string]string{}
	manual := map[string]bool{}
	hasScope := map[string]bool{}
	if ok, err := r.Store.HasTable(Table); err != nil {
		return nil, nil, err
	} else if ok {
		recs, err := r.Store.Applied()
		if err != nil {
			return nil, nil, err
		}
		for _, rec := range recs {
			applied[key(rec.Scope, rec.Version)] = rec.Checksum
			manual[key(rec.Scope, rec.Version)] = rec.Source == SourceManual
			hasScope[rec.Scope] = true
		}
	}
	legacy := map[string]map[string]bool{}
	for _, f := range files {
		sum, done := applied[key(f.Scope, f.Name)]
		switch {
		case done && sum != f.Checksum:
			status = append(status, FileStatus{Scope: f.Scope, Name: f.Name, Checksum: f.Checksum, Recorded: sum, State: StateMismatch, Manual: manual[key(f.Scope, f.Name)]})
			continue
		case done:
			status = append(status, FileStatus{Scope: f.Scope, Name: f.Name, Checksum: f.Checksum, Recorded: sum, State: StateApplied, Manual: manual[key(f.Scope, f.Name)]})
			continue
		}
		status = append(status, FileStatus{Scope: f.Scope, Name: f.Name, Checksum: f.Checksum, State: StatePending})
		it := Item{File: f, Stmts: Split(f.SQL)}
		if !hasScope[f.Scope] { // 该 scope 还没有任何记录：存量库基线
			if _, ok := legacy[f.Scope]; !ok {
				if legacy[f.Scope], err = r.Store.Legacy(f.Scope); err != nil {
					return nil, nil, err
				}
			}
			if legacy[f.Scope][f.Name] {
				it.Stmts, it.Baseline = nil, true
			} else if it.Stmts, it.Baseline, err = r.baseline(it.Stmts); err != nil {
				return nil, nil, err
			}
		}
		it.Warnings = DMLWarnings(f, it.Stmts)
		items = append(items, it)
	}
	var bad []string
	for _, s := range status {
		if s.State == StateMismatch {
			bad = append(bad, key(s.Scope, s.Name))
		}
	}
	if len(bad) > 0 {
		err = &ChecksumError{Files: bad}
	}
	return status, items, err
}

// baseline 只保留需要执行的语句：CREATE TABLE 的目标表已存在，就连同它后面的 CREATE INDEX 整组跳过。
// 第一个 CREATE TABLE 之前的语句无从判断，照常执行。全部跳过返回 baseline=true。
func (r *Runner) baseline(stmts []string) (run []string, baseline bool, err error) {
	keep := true
	for _, s := range stmts {
		if m := createTableRe.FindStringSubmatch(s); m != nil {
			exists, err := r.Store.HasTable(m[1])
			if err != nil {
				return nil, false, err
			}
			keep = !exists
		}
		if keep {
			run = append(run, s)
		}
	}
	return run, len(run) == 0 && len(stmts) > 0, nil
}

// Plan 列出待执行的迁移（只读，不拿锁、不建表），供 --dry-run 与 auto_migrate=false 的检查使用。
func (r *Runner) Plan() ([]Item, error) {
	_, items, err := r.scan()
	return items, err
}

// Check 是 app.auto_migrate=false 时的启动检查：checksum 异常或有待执行迁移都返回错误。
func (r *Runner) Check() error {
	items, err := r.Plan()
	if err != nil {
		return err
	}
	if len(items) > 0 {
		e := &PendingError{}
		for _, it := range items {
			e.Files = append(e.Files, key(it.Scope, it.Name))
		}
		return e
	}
	return nil
}

// 文件状态。
const (
	StateApplied  = "applied"
	StatePending  = "pending"
	StateMismatch = "mismatch"
)

// FileStatus 是 migrate --status 的一行。
type FileStatus struct {
	Scope, Name, Checksum, Recorded, State string
	Manual                                 bool // 记录是 --mark-applied 手工标记的（没有真实执行）
}

// Status 列出所有迁移文件的状态（checksum 异常不当作错误返回，状态里标出）。
func (r *Runner) Status() ([]FileStatus, error) {
	st, _, err := r.scan()
	var ce *ChecksumError
	if err != nil && !errors.As(err, &ce) {
		return nil, err
	}
	return st, nil
}

// Exclusive 在持有数据库级锁期间运行 fn。拿不到锁就每隔 LockPoll 重试，最长 LockWait。
func (r *Runner) Exclusive(fn func() error) error {
	wait, poll := r.LockWait, r.LockPoll
	attempts := 1
	if poll > 0 && wait > poll {
		attempts = int(wait/poll) + 1
	}
	for i := 0; ; i++ {
		unlock, ok, err := r.Store.TryLock()
		if err != nil {
			return fmt.Errorf("migrate: 获取迁移锁: %w", err)
		}
		if ok {
			defer unlock()
			break
		}
		if i == 0 {
			r.Log.Infof("别的实例正在迁移，等待迁移锁…")
		}
		if i+1 >= attempts {
			return fmt.Errorf("migrate: 等待迁移锁超时（%s），另一个实例可能仍在迁移", wait)
		}
		r.Sleep(poll)
	}
	return fn()
}

// Up 执行待迁移文件（自己拿锁）。没有待执行项时不拿锁，直接返回。
func (r *Runner) Up() (*Result, error) {
	if items, err := r.Plan(); err != nil || len(items) == 0 {
		return &Result{}, err
	}
	var res *Result
	err := r.Exclusive(func() (err error) { res, err = r.RunPending(); return })
	return res, err
}

// RunPending 在已持有锁的前提下建版本表、重新读取状态并逐个执行。
func (r *Runner) RunPending() (*Result, error) {
	res := &Result{}
	if err := r.Store.EnsureTable(); err != nil {
		return res, fmt.Errorf("migrate: 创建版本表 %s: %w", Table, err)
	}
	items, err := r.Plan() // 拿到锁之后重新比对：别的实例做完的会被跳过
	if err != nil {
		return res, err
	}
	for _, it := range items {
		for _, w := range it.Warnings {
			r.Log.Warnf("%s", w)
		}
		rec := Record{Scope: it.Scope, Version: it.Name, Checksum: it.Checksum, AppliedAt: time.Now().UTC()}
		done, err := r.Store.Apply(rec, it.Stmts)
		if err != nil {
			where := fmt.Sprintf("已执行 %d/%d 条，第 %d 条失败，该文件未记录，需先手工修复", done, len(it.Stmts), done+1)
			if r.Store.Atomic() {
				where = fmt.Sprintf("第 %d/%d 条失败，该文件已整体回滚", done+1, len(it.Stmts))
			}
			return res, fmt.Errorf("migrate: %s/%s: %s: %w", it.Scope, it.Name, where, err)
		}
		res.Applied = append(res.Applied, Applied{Scope: it.Scope, File: it.Name, Baseline: it.Baseline})
	}
	return res, nil
}

// Startup 是服务启动时的迁移入口（调用方已持有锁，或自行决定是否加锁）：
// auto=true 执行待迁移文件；auto=false 只检查，有待执行迁移就返回 *PendingError。
func (r *Runner) Startup(auto bool) (*Result, error) {
	if !auto {
		return &Result{}, r.Check()
	}
	return r.RunPending()
}
