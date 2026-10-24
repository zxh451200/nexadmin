package migrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeStore 是内存假实现：不连数据库，记录所有执行过的语句。
type fakeStore struct {
	engine   string
	atomic   bool
	tables   map[string]bool
	records  map[string]Record
	legacy   map[string]map[string]bool
	executed []string

	lockFails int    // 前 N 次 TryLock 失败
	lockCalls int    // TryLock 调用次数
	unlocks   int    //
	onLock    func() // 拿到锁时触发，用来模拟“别的实例刚好做完”
	failOn    string // 语句包含该子串时执行失败
}

func newFake(engine string) *fakeStore {
	return &fakeStore{engine: engine, tables: map[string]bool{}, records: map[string]Record{}, legacy: map[string]map[string]bool{}}
}

func (f *fakeStore) Engine() string { return f.engine }
func (f *fakeStore) Atomic() bool   { return f.atomic }
func (f *fakeStore) HasTable(n string) (bool, error) {
	return f.tables[n], nil
}
func (f *fakeStore) EnsureTable() error { f.tables[Table] = true; return nil }
func (f *fakeStore) Applied() ([]Record, error) {
	var out []Record
	for _, r := range f.records {
		out = append(out, r)
	}
	return out, nil
}
func (f *fakeStore) Legacy(scope string) (map[string]bool, error) { return f.legacy[scope], nil }
func (f *fakeStore) TryLock() (func(), bool, error) {
	f.lockCalls++
	if f.lockCalls <= f.lockFails {
		return nil, false, nil
	}
	if f.onLock != nil {
		f.onLock()
	}
	return func() { f.unlocks++ }, true, nil
}
func (f *fakeStore) Apply(rec Record, stmts []string) (int, error) {
	var ran []string
	for i, s := range stmts {
		if f.failOn != "" && strings.Contains(s, f.failOn) {
			if !f.atomic {
				f.executed = append(f.executed, ran...)
			}
			return i, errors.New("boom")
		}
		if m := createTableRe.FindStringSubmatch(s); m != nil {
			if f.tables[m[1]] {
				return i, fmt.Errorf("table %s already exists", m[1])
			}
			f.tables[m[1]] = true
		}
		ran = append(ran, s)
	}
	f.executed = append(f.executed, ran...)
	f.records[key(rec.Scope, rec.Version)] = rec
	return len(stmts), nil
}
func (f *fakeStore) Insert(rec Record) error {
	f.tables[Table] = true
	f.records[key(rec.Scope, rec.Version)] = rec
	return nil
}
func (f *fakeStore) DeleteManual(scope, version string) (bool, error) {
	r, ok := f.records[key(scope, version)]
	if !ok || r.Source != SourceManual {
		return false, nil
	}
	delete(f.records, key(scope, version))
	return true, nil
}

type testLog struct{ infos, warns []string }

func (l *testLog) Infof(f string, a ...any) { l.infos = append(l.infos, fmt.Sprintf(f, a...)) }
func (l *testLog) Warnf(f string, a ...any) { l.warns = append(l.warns, fmt.Sprintf(f, a...)) }

// write 在 root/<engine>/<name> 写一个迁移文件。
func write(t *testing.T, root, engine, name, sql string) {
	t.Helper()
	dir := filepath.Join(root, engine)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(sql), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runner(f *fakeStore, log Logger, srcs ...Source) *Runner {
	r := NewRunner(f, log, srcs...)
	r.Sleep = func(time.Duration) {}
	return r
}

func fw(root string) Source { return Source{Scope: ScopeFramework, Root: root, Required: true} }

func TestDiscoverSortsAndFallsBack(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "b.sql", "CREATE TABLE b (id INT)")
	write(t, root, "mysql", "a.sql", "CREATE TABLE a (id INT)")
	write(t, root, "mysql", "notes.txt", "ignored")

	// mariadb 没有专属目录：回退 mysql，且按文件名排序
	files, err := Discover("mariadb", []Source{fw(root)})
	if err != nil || len(files) != 2 || files[0].Name != "a.sql" || files[1].Name != "b.sql" {
		t.Fatalf("mariadb 应回退 mysql 并排序: %+v %v", files, err)
	}
	// 有专属目录就用专属目录
	write(t, root, "mariadb", "only_maria.sql", "CREATE TABLE m (id INT)")
	files, _ = Discover("mariadb", []Source{fw(root)})
	if len(files) != 1 || files[0].Name != "only_maria.sql" {
		t.Fatalf("mariadb 专属目录优先: %+v", files)
	}
	// postgres 不回退 mysql：必需目录缺失要报错，并指出路径
	if _, err := Discover("postgres", []Source{fw(root)}); err == nil || !strings.Contains(err.Error(), filepath.Join(root, "postgres")) {
		t.Fatalf("找不到框架迁移目录应明确报错: %v", err)
	}
	// 非必需目录缺失当作没有迁移
	if files, err := Discover("mysql", []Source{{Scope: ScopeBusiness, Root: filepath.Join(root, "nope")}}); err != nil || len(files) != 0 {
		t.Fatalf("业务目录缺失应视为空: %v %v", files, err)
	}
}

func TestChecksumIgnoresLineEndings(t *testing.T) {
	if Checksum("a\r\nb") != Checksum("a\nb") {
		t.Error("CRLF 与 LF 的 checksum 应一致")
	}
	if Checksum("a") == Checksum("b") {
		t.Error("不同内容 checksum 应不同")
	}
}

func TestUpAppliesPendingInOrderAndSkipsApplied(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "002_b.sql", "CREATE TABLE b (id INT)")
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	write(t, root, "mysql", "003_c.sql", "CREATE TABLE c (id INT)")
	files, _ := Discover("mysql", []Source{fw(root)})

	st := newFake("mysql")
	st.tables[Table] = true
	st.records[key(ScopeFramework, "001_a.sql")] = Record{Scope: ScopeFramework, Version: "001_a.sql", Checksum: files[0].Checksum}
	res, err := runner(st, nil, fw(root)).Up()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 2 || res.Applied[0].File != "002_b.sql" || res.Applied[1].File != "003_c.sql" {
		t.Fatalf("应只按顺序执行未记录的文件: %+v", res.Applied)
	}
	if len(st.executed) != 2 || strings.Contains(strings.Join(st.executed, ";"), "TABLE a") {
		t.Errorf("已执行的 001 不应重跑: %v", st.executed)
	}
	for _, n := range []string{"002_b.sql", "003_c.sql"} {
		if r := st.records[key(ScopeFramework, n)]; r.Checksum == "" || r.AppliedAt.IsZero() {
			t.Errorf("%s 应写入版本表(checksum、applied_at): %+v", n, r)
		}
	}
	// 第二次：零执行，且不拿锁
	st.executed, st.lockCalls = nil, 0
	res, err = runner(st, nil, fw(root)).Up()
	if err != nil || len(res.Applied) != 0 || len(st.executed) != 0 || st.lockCalls != 0 {
		t.Errorf("已是最新时应零写入、不拿锁: %+v %v exec=%v locks=%d", res, err, st.executed, st.lockCalls)
	}
}

func TestChecksumMismatchRefuses(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	write(t, root, "mysql", "002_b.sql", "CREATE TABLE b (id INT)")
	st := newFake("mysql")
	st.tables[Table] = true
	st.records[key(ScopeFramework, "001_a.sql")] = Record{Scope: ScopeFramework, Version: "001_a.sql", Checksum: "deadbeef"}

	r := runner(st, nil, fw(root))
	_, err := r.Up()
	var ce *ChecksumError
	if !errors.As(err, &ce) || len(ce.Files) != 1 || ce.Files[0] != "framework/001_a.sql" {
		t.Fatalf("应拒绝并指出被改的文件名: %v", err)
	}
	if !strings.Contains(err.Error(), "001_a.sql") || len(st.executed) != 0 {
		t.Errorf("拒绝时不应执行任何迁移: %v exec=%v", err, st.executed)
	}
	// Status 不报错，只标出异常
	status, err := r.Status()
	if err != nil || len(status) != 2 || status[0].State != StateMismatch || status[1].State != StatePending {
		t.Errorf("status 应标出 mismatch / pending: %+v %v", status, err)
	}
	// 启动检查同样拒绝
	if _, err := r.Startup(true); !errors.As(err, &ce) {
		t.Errorf("Startup 也应拒绝: %v", err)
	}
}

func TestLockRetryThenSuccess(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	st := newFake("mysql")
	st.lockFails = 3
	sleeps := 0
	r := runner(st, nil, fw(root))
	r.Sleep = func(d time.Duration) { sleeps++ }
	res, err := r.Up()
	if err != nil || len(res.Applied) != 1 {
		t.Fatalf("重试后应成功: %+v %v", res, err)
	}
	if st.lockCalls != 4 || sleeps != 3 || st.unlocks != 1 {
		t.Errorf("应失败 3 次后第 4 次拿到锁并释放: calls=%d sleeps=%d unlocks=%d", st.lockCalls, sleeps, st.unlocks)
	}
}

func TestLockTimeout(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	st := newFake("mysql")
	st.lockFails = 1 << 30
	r := runner(st, nil, fw(root))
	r.LockPoll, r.LockWait = 100*time.Millisecond, time.Second
	if _, err := r.Up(); err == nil || !strings.Contains(err.Error(), "等待迁移锁超时") {
		t.Fatalf("应超时报错: %v", err)
	}
	if len(st.executed) != 0 {
		t.Error("没拿到锁不能执行迁移")
	}
}

func TestOtherInstanceFinishedWhileWaiting(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	files, _ := Discover("mysql", []Source{fw(root)})
	st := newFake("mysql")
	st.lockFails = 2
	// 另一个实例在我们等锁期间做完了：拿到锁后重新读版本表，应全部跳过
	st.onLock = func() {
		st.tables[Table], st.tables["a"] = true, true
		st.records[key(ScopeFramework, "001_a.sql")] = Record{Scope: ScopeFramework, Version: "001_a.sql", Checksum: files[0].Checksum}
	}
	res, err := runner(st, nil, fw(root)).Up()
	if err != nil || len(res.Applied) != 0 || len(st.executed) != 0 {
		t.Fatalf("别的实例已完成，应跳过: %+v %v exec=%v", res, err, st.executed)
	}
}

func TestBaselineForExistingDatabase(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "system_user.sql", "CREATE TABLE sys_user (id INT);\nCREATE INDEX idx_u ON sys_user (id);\nCREATE TABLE sys_user_role (id INT);\nCREATE INDEX idx_ur ON sys_user_role (id)")
	write(t, root, "mysql", "system_role.sql", "CREATE TABLE sys_role (id INT)")
	// 升级前已初始化：没有版本表，但 sys_user / sys_role 已存在，sys_user_role 缺失
	st := newFake("mysql")
	st.tables["sys_user"], st.tables["sys_role"] = true, true

	res, err := runner(st, nil, fw(root)).Up()
	if err != nil {
		t.Fatal(err)
	}
	// system_role.sql：表已存在 -> 只记录；system_user.sql：只补缺失的 sys_user_role 及其索引
	if len(res.Applied) != 2 {
		t.Fatalf("两个文件都应被记录: %+v", res.Applied)
	}
	got := strings.Join(st.executed, ";")
	if strings.Contains(got, "idx_u ") || strings.Contains(got, "TABLE sys_user (") || !strings.Contains(got, "sys_user_role") || len(st.executed) != 2 {
		t.Errorf("只应执行缺失表的那一组: %v", st.executed)
	}
	var baseline []string
	for _, a := range res.Applied {
		if a.Baseline {
			baseline = append(baseline, a.File)
		}
	}
	if len(baseline) != 1 || baseline[0] != "system_role.sql" {
		t.Errorf("全部表已存在的文件应标记为基线(只记录不执行): %v", baseline)
	}
	if _, ok := st.records[key(ScopeFramework, "system_role.sql")]; !ok {
		t.Error("基线文件也要写入版本表")
	}

	// 基线只用一次：之后同 scope 恢复严格模式，新文件建已存在的表就该报错而不是悄悄跳过
	write(t, root, "mysql", "system_dup.sql", "CREATE TABLE sys_role (id INT)")
	if _, err := runner(st, nil, fw(root)).Up(); err == nil || !strings.Contains(err.Error(), "system_dup.sql") {
		t.Errorf("基线之后应严格执行并报错: %v", err)
	}
}

func TestLegacyPluginRecordsImported(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_init.sql", "CREATE TABLE plg_x (id INT)")
	write(t, root, "mysql", "002_more.sql", "CREATE TABLE plg_y (id INT)")
	scope := ScopePrefix + "x"
	st := newFake("mysql")
	st.tables["plg_x"] = true
	st.legacy[scope] = map[string]bool{"001_init.sql": true} // 旧的 sys_plugin_migration 里已有

	res, err := runner(st, nil, Source{Scope: scope, Root: root}).Up()
	if err != nil || len(res.Applied) != 2 || !res.Applied[0].Baseline || res.Applied[1].Baseline {
		t.Fatalf("旧记录导入为基线，新文件照常执行: %+v %v", res.Applied, err)
	}
	if len(st.executed) != 1 || !strings.Contains(st.executed[0], "plg_y") {
		t.Errorf("只应执行 002: %v", st.executed)
	}
}

func TestDMLWarnsButDoesNotBlock(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT);\n-- backfill\nINSERT INTO a (id) VALUES (1);\nupdate a SET id = 2;\nDELETE FROM a")
	st := newFake("mysql")
	lg := &testLog{}
	r := runner(st, lg, fw(root))

	items, err := r.Plan()
	if err != nil || len(items) != 1 || len(items[0].Warnings) != 3 {
		t.Fatalf("dry-run 应给出 3 条 DML 警告: %+v %v", items, err)
	}
	if len(st.executed) != 0 {
		t.Error("Plan 不应执行任何语句")
	}
	res, err := r.Up()
	if err != nil || len(res.Applied) != 1 {
		t.Fatalf("DML 警告不应阻断: %v", err)
	}
	if len(lg.warns) != 3 || !strings.Contains(lg.warns[0], "INSERT") || !strings.Contains(lg.warns[0], "framework/001_a.sql") {
		t.Errorf("日志应有 WARN: %v", lg.warns)
	}
}

func TestFailureReporting(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT);\nCREATE TABLE bad (id INT);\nCREATE INDEX i ON a (id)")

	// MySQL：DDL 不能回滚，报告执行到哪一条，文件不记录
	st := newFake("mysql")
	st.failOn = "bad"
	_, err := runner(st, nil, fw(root)).Up()
	if err == nil || !strings.Contains(err.Error(), "已执行 1/3 条") || !strings.Contains(err.Error(), "001_a.sql") {
		t.Fatalf("应报告执行到第几条: %v", err)
	}
	if len(st.records) != 0 {
		t.Error("失败的文件不应记录")
	}

	// Postgres：整个文件回滚
	pg := newFake("postgres")
	pg.atomic, pg.failOn = true, "bad"
	root2 := t.TempDir()
	write(t, root2, "postgres", "001_a.sql", "CREATE TABLE a (id INT);\nCREATE TABLE bad (id INT)")
	_, err = runner(pg, nil, fw(root2)).Up()
	if err == nil || !strings.Contains(err.Error(), "整体回滚") || len(pg.executed) != 0 || len(pg.records) != 0 {
		t.Errorf("事务引擎应整体回滚: %v exec=%v", err, pg.executed)
	}
}

func TestStartupAutoMigrateFalseRefusesWhenPending(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	st := newFake("mysql")
	r := runner(st, nil, fw(root))

	_, err := r.Startup(false)
	var pe *PendingError
	if !errors.As(err, &pe) || len(pe.Files) != 1 || pe.Files[0] != "framework/001_a.sql" ||
		!strings.Contains(err.Error(), "nexadmin-server migrate") {
		t.Fatalf("auto_migrate=false 且有待执行迁移应拒绝并提示 migrate 命令: %v", err)
	}
	if len(st.executed) != 0 {
		t.Error("auto_migrate=false 不能执行迁移")
	}
	// 执行之后再检查：通过
	if _, err := r.Up(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Startup(false); err != nil {
		t.Errorf("没有待执行迁移时应正常启动: %v", err)
	}
}

func TestSplit(t *testing.T) {
	got := Split("-- c\nCREATE TABLE a (id INT);\n\n-- x;\nCREATE INDEX i ON a (id);\n")
	if len(got) != 2 || !strings.HasPrefix(got[0], "CREATE TABLE a") || !strings.HasPrefix(got[1], "CREATE INDEX") {
		t.Errorf("Split: %q", got)
	}
}

func yes(string) (bool, error) { return true, nil }

func TestMarkAppliedSuccess(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT);\nCREATE TABLE bad (id INT)")
	files, _ := Discover("mysql", []Source{fw(root)})
	st := newFake("mysql")
	st.failOn = "bad"
	lg := &testLog{}
	r := runner(st, lg, fw(root))
	if _, err := r.Up(); err == nil { // 中途失败，文件未记录
		t.Fatal("应失败")
	}
	st.executed, st.lockCalls, st.unlocks = nil, 0, 0

	var prompt string
	err := r.MarkApplied("framework/001_a.sql", "alice@host", func(p string) (bool, error) { prompt = p; return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "framework/001_a.sql") || !strings.Contains(prompt, "不会运行其中的任何 SQL") {
		t.Errorf("确认提示不对: %q", prompt)
	}
	rec := st.records[key(ScopeFramework, "001_a.sql")]
	if rec.Source != SourceManual || rec.Checksum != files[0].Checksum || rec.AppliedAt.IsZero() {
		t.Errorf("应按当前 checksum 写入并标记来源: %+v", rec)
	}
	if len(st.executed) != 0 || st.lockCalls == 0 || st.unlocks != 1 {
		t.Errorf("不得执行 SQL，且要拿锁并释放: exec=%v locks=%d unlocks=%d", st.executed, st.lockCalls, st.unlocks)
	}
	if len(lg.infos) == 0 || !strings.Contains(lg.infos[len(lg.infos)-1], "alice@host") ||
		!strings.Contains(lg.infos[len(lg.infos)-1], "framework/001_a.sql") {
		t.Errorf("日志应含操作者与文件: %v", lg.infos)
	}
	// 不再待执行；status 标出手工
	if items, err := r.Plan(); err != nil || len(items) != 0 {
		t.Errorf("标记后不应再待执行: %+v %v", items, err)
	}
	if s, _ := r.Status(); len(s) != 1 || s[0].State != StateApplied || !s[0].Manual {
		t.Errorf("status 应显示手工标记: %+v", s)
	}
	// 重复标记拒绝
	if err := r.MarkApplied("framework/001_a.sql", "x", yes); err == nil || !strings.Contains(err.Error(), "已在版本表中记录") {
		t.Errorf("重复标记应拒绝: %v", err)
	}
}

func TestMarkAppliedRejectsMissingFileAndBadRef(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	st := newFake("mysql")
	r := runner(st, nil, fw(root))
	if err := r.MarkApplied("framework/nope.sql", "x", yes); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Errorf("文件不存在应拒绝: %v", err)
	}
	for _, bad := range []string{"001_a.sql", "other/001_a.sql", "plugin:/001_a.sql", "framework/"} {
		if err := r.MarkApplied(bad, "x", yes); err == nil {
			t.Errorf("非法参数 %q 应拒绝", bad)
		}
	}
	if len(st.records) != 0 {
		t.Error("拒绝时不得写入")
	}
}

func TestMarkAppliedRejectsChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	st := newFake("mysql")
	st.tables[Table] = true
	st.records[key(ScopeFramework, "001_a.sql")] = Record{Scope: ScopeFramework, Version: "001_a.sql", Checksum: "deadbeef"}
	err := runner(st, nil, fw(root)).MarkApplied("framework/001_a.sql", "x", yes)
	if err == nil || !strings.Contains(err.Error(), "checksum 异常") || !strings.Contains(err.Error(), "还原") {
		t.Fatalf("checksum 异常应拒绝并提示: %v", err)
	}
	if st.records[key(ScopeFramework, "001_a.sql")].Checksum != "deadbeef" {
		t.Error("不得改动原记录")
	}
}

func TestMarkAppliedNeedsConfirmation(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	st := newFake("mysql")
	r := runner(st, nil, fw(root))
	for name, c := range map[string]Confirm{
		"nil":  nil,
		"no":   func(string) (bool, error) { return false, nil },
		"fail": func(string) (bool, error) { return false, errors.New("read error") },
	} {
		if err := r.MarkApplied("framework/001_a.sql", "x", c); err == nil {
			t.Errorf("%s: 未确认不应成功", name)
		}
	}
	if len(st.records) != 0 || len(st.executed) != 0 || st.lockCalls != 0 {
		t.Errorf("未确认时不得写入也不拿锁: %v locks=%d", st.records, st.lockCalls)
	}
	if err := r.MarkApplied("framework/001_a.sql", "x", func(string) (bool, error) { return false, nil }); !errors.Is(err, ErrAborted) {
		t.Errorf("拒绝确认应返回 ErrAborted: %v", err)
	}
}

func TestMarkAppliedLockTimeout(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	st := newFake("mysql")
	st.lockFails = 1 << 30
	r := runner(st, nil, fw(root))
	r.LockPoll, r.LockWait = 100*time.Millisecond, time.Second
	if err := r.MarkApplied("framework/001_a.sql", "x", yes); err == nil || !strings.Contains(err.Error(), "等待迁移锁超时") {
		t.Fatalf("拿不到锁应报错: %v", err)
	}
	if len(st.records) != 0 {
		t.Error("没拿到锁不得写入")
	}
}

func TestUnmarkOnlyManualRecords(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mysql", "001_a.sql", "CREATE TABLE a (id INT)")
	write(t, root, "mysql", "002_b.sql", "CREATE TABLE b (id INT)")
	st := newFake("mysql")
	r := runner(st, nil, fw(root))
	if _, err := r.Up(); err != nil { // 两个都是真实执行
		t.Fatal(err)
	}
	if err := r.Unmark("framework/001_a.sql", "x", yes); err == nil || !strings.Contains(err.Error(), "真实执行") {
		t.Fatalf("不能撤销真实执行的记录: %v", err)
	}
	if _, ok := st.records[key(ScopeFramework, "001_a.sql")]; !ok {
		t.Fatal("真实执行的记录不得被删")
	}
	// 手工标记的可撤销，且需要确认
	delete(st.records, key(ScopeFramework, "002_b.sql"))
	if err := r.MarkApplied("framework/002_b.sql", "x", yes); err != nil {
		t.Fatal(err)
	}
	if err := r.Unmark("framework/002_b.sql", "x", func(string) (bool, error) { return false, nil }); !errors.Is(err, ErrAborted) {
		t.Errorf("未确认不应撤销: %v", err)
	}
	if _, ok := st.records[key(ScopeFramework, "002_b.sql")]; !ok {
		t.Fatal("未确认时记录应保留")
	}
	if err := r.Unmark("framework/002_b.sql", "x", yes); err != nil {
		t.Fatal(err)
	}
	if items, _ := r.Plan(); len(items) != 1 || items[0].Name != "002_b.sql" {
		t.Errorf("撤销后应重新待执行: %+v", items)
	}
	if err := r.Unmark("framework/002_b.sql", "x", yes); err == nil || !strings.Contains(err.Error(), "没有记录") {
		t.Errorf("无记录应拒绝: %v", err)
	}
}

func TestRootsFromEnv(t *testing.T) {
	base := t.TempDir()
	fwDir := filepath.Join(base, "migrations", "framework")
	if err := os.MkdirAll(fwDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvDir, fwDir)
	f, b, err := Roots()
	if err != nil || f != fwDir || b != filepath.Join(base, "migrations", "business") {
		t.Fatalf("环境变量指向框架目录，业务目录取其同级 business: %q %q %v", f, b, err)
	}
	srcs, err := DefaultSources()
	if err != nil || len(srcs) != 2 || !srcs[0].Required || srcs[1].Required {
		t.Errorf("框架必需、业务可选: %+v %v", srcs, err)
	}
}
