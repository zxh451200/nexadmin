package job

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nexadmin/core/storage"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// memStore 是带互斥锁的内存 Store，状态迁移语义与 GormStore 的“带旧状态条件的 UPDATE”一致。
type memStore struct {
	mu   sync.Mutex
	jobs map[idgen.ID]*Job
	seq  int64
}

func newMemStore() *memStore { return &memStore{jobs: map[idgen.ID]*Job{}} }

func (s *memStore) cp(j *Job) *Job { c := *j; return &c }

func (s *memStore) Insert(_ context.Context, j *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	j.ID = idgen.ID(s.seq)
	j.CreatedAt = time.Now().Add(time.Duration(s.seq) * time.Microsecond)
	s.jobs[j.ID] = s.cp(j)
	return nil
}

func (s *memStore) Get(_ context.Context, id idgen.ID) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		return s.cp(j), nil
	}
	return nil, ErrNotFound
}

func (s *memStore) List(_ context.Context, f Filter) ([]*Job, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Job
	for _, j := range s.jobs {
		if (f.Owner == 0 || j.OwnerID == f.Owner) && (f.Type == "" || j.Type == f.Type) && (f.Status == "" || j.Status == f.Status) {
			out = append(out, s.cp(j))
		}
	}
	return out, int64(len(out)), nil
}

func (s *memStore) CountActive(_ context.Context, owner idgen.ID) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, j := range s.jobs {
		if j.OwnerID == owner && (j.Status == StatusQueued || j.Status == StatusRunning) {
			n++
		}
	}
	return n, nil
}

func (s *memStore) Claim(_ context.Context, types []string, node string, now time.Time) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *Job
	for _, j := range s.jobs {
		if j.Status != StatusQueued || !contains(types, j.Type) {
			continue
		}
		if best == nil || j.ID < best.ID {
			best = j
		}
	}
	if best == nil {
		return nil, nil
	}
	best.Status, best.Node, best.StartedAt = StatusRunning, node, &now
	return s.cp(best), nil
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func (s *memStore) SetProgress(_ context.Context, id idgen.ID, p int, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j := s.jobs[id]; j != nil && j.Status == StatusRunning {
		j.Progress, j.Message = p, msg
	}
	return nil
}

func (s *memStore) Finish(_ context.Context, id idgen.ID, status string, res ResultFile, errMsg string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	if j == nil || j.Status != StatusRunning {
		return false, nil
	}
	j.Status, j.FinishedAt, j.Error = status, &now, errMsg
	if status == StatusSuccess {
		j.Progress, j.ResultFile, j.ResultName, j.ResultSize = 100, res.Key, res.Name, res.Size
	}
	return true, nil
}

func (s *memStore) Cancel(_ context.Context, id idgen.ID, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	if j == nil || (j.Status != StatusQueued && j.Status != StatusRunning) {
		return false, nil
	}
	j.Status, j.FinishedAt = StatusCanceled, &now
	return true, nil
}

func (s *memStore) Requeue(_ context.Context, id idgen.ID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	if j == nil || j.Status != StatusFailed {
		return false, nil
	}
	j.Status, j.Progress, j.Error, j.Node, j.StartedAt, j.FinishedAt = StatusQueued, 0, "", "", nil, nil
	return true, nil
}

func (s *memStore) Status(_ context.Context, id idgen.ID) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j := s.jobs[id]; j != nil {
		return j.Status, nil
	}
	return "", ErrNotFound
}

func (s *memStore) ResetRunning(_ context.Context, node, msg string, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, j := range s.jobs {
		if j.Status == StatusRunning && j.Node == node {
			j.Status, j.Error, j.FinishedAt = StatusFailed, msg, &now
			n++
		}
	}
	return n, nil
}

func (s *memStore) Delete(_ context.Context, id idgen.ID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j := s.jobs[id]; j != nil && IsFinal(j.Status) {
		delete(s.jobs, id)
		return true, nil
	}
	return false, nil
}

func (s *memStore) Expired(_ context.Context, before time.Time, limit int) ([]*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Job
	for _, j := range s.jobs {
		if IsFinal(j.Status) && j.FinishedAt != nil && j.FinishedAt.Before(before) && len(out) < limit {
			out = append(out, s.cp(j))
		}
	}
	return out, nil
}

// ---- helpers ----

var typeSeq atomic.Int64

// regType 注册一个唯一类型的处理器并在测试结束时注销。
func regType(t *testing.T, h Handler) string {
	t.Helper()
	typ := "test." + strings.ReplaceAll(t.Name(), "/", "_") + "." + string(rune('a'+typeSeq.Add(1)%26))
	Register(typ, h)
	t.Cleanup(func() { unregister(typ) })
	return typ
}

func newMgr(t *testing.T, st Store, node string, files storage.Storage, mod func(*Options)) *Manager {
	t.Helper()
	if files == nil {
		l, err := storage.NewLocal(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		files = l
	}
	o := Options{Store: st, Files: files, Node: node, FlushEvery: 5 * time.Millisecond, PollEvery: 5 * time.Millisecond, MaxPerUser: 100}
	if mod != nil {
		mod(&o)
	}
	m := New(o)
	t.Cleanup(m.Stop)
	return m
}

func tctx() context.Context { return tenant.With(context.Background(), 1) }

func waitStatus(t *testing.T, st Store, id idgen.ID, want string) *Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := st.Get(context.Background(), id)
		if j != nil && j.Status == want {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	j, _ := st.Get(context.Background(), id)
	t.Fatalf("job %d: want status %s, got %+v", id, want, j)
	return nil
}

// ---- tests ----

func TestStateFlowSuccess(t *testing.T) {
	st := newMemStore()
	files, _ := storage.NewLocal(t.TempDir())
	m := newMgr(t, st, "n1", files, nil)
	var seen string
	typ := regType(t, func(ctx context.Context, j *Job, report Report) (ResultFile, error) {
		cur, _ := st.Status(ctx, j.ID)
		seen = cur // 执行中应是 running
		report(50, "half")
		return WriteResult(ctx, j, "a.xlsx", "application/octet-stream", func(w io.Writer) error {
			_, err := w.Write([]byte("hello"))
			return err
		})
	})
	j, err := m.Submit(tctx(), Submit{Type: typ, Owner: 7, Params: map[string]any{"keyword": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != StatusQueued {
		t.Fatalf("new job status = %s", j.Status)
	}
	if !m.RunOnce() {
		t.Fatal("RunOnce did nothing")
	}
	got, _ := st.Get(context.Background(), j.ID)
	if seen != StatusRunning || got.Status != StatusSuccess || got.Progress != 100 || got.StartedAt == nil || got.FinishedAt == nil {
		t.Fatalf("seen=%s job=%+v", seen, got)
	}
	if got.ResultSize != 5 || got.ResultName != "a.xlsx" || !strings.HasPrefix(got.ResultFile, "jobs/1/") {
		t.Fatalf("result = %+v", got)
	}
	if ok, _ := files.Exists(context.Background(), got.ResultFile); !ok {
		t.Fatal("result file not stored")
	}
	if m.RunOnce() {
		t.Fatal("finished job must not be claimed again")
	}
}

func TestClaimOnlyOneInstanceRuns(t *testing.T) {
	st := newMemStore()
	var runs sync.Map
	var total atomic.Int64
	typ := regType(t, func(ctx context.Context, j *Job, _ Report) (ResultFile, error) {
		n, _ := runs.LoadOrStore(j.ID, new(atomic.Int64))
		n.(*atomic.Int64).Add(1)
		total.Add(1)
		time.Sleep(time.Millisecond)
		return ResultFile{}, nil
	})
	a, b := newMgr(t, st, "a", nil, nil), newMgr(t, st, "b", nil, nil)
	const N = 60
	for i := 0; i < N; i++ {
		if _, err := a.Submit(tctx(), Submit{Type: typ, Owner: idgen.ID(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, m := range []*Manager{a, b, a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m.RunOnce() {
			}
		}()
	}
	wg.Wait()
	if total.Load() != N {
		t.Fatalf("handler ran %d times for %d jobs", total.Load(), N)
	}
	runs.Range(func(k, v any) bool {
		if v.(*atomic.Int64).Load() != 1 {
			t.Errorf("job %v ran %d times", k, v.(*atomic.Int64).Load())
		}
		return true
	})
}

func TestClaimIsExclusive(t *testing.T) {
	st := newMemStore()
	typ := regType(t, func(context.Context, *Job, Report) (ResultFile, error) { return ResultFile{}, nil })
	m := newMgr(t, st, "a", nil, nil)
	if _, err := m.Submit(tctx(), Submit{Type: typ, Owner: 1}); err != nil {
		t.Fatal(err)
	}
	j1, _ := st.Claim(context.Background(), []string{typ}, "a", time.Now())
	j2, _ := st.Claim(context.Background(), []string{typ}, "b", time.Now())
	if j1 == nil || j2 != nil {
		t.Fatalf("j1=%v j2=%v", j1, j2)
	}
}

func TestCancelRunningViaManager(t *testing.T) {
	st := newMemStore()
	m := newMgr(t, st, "n", nil, nil)
	started := make(chan struct{})
	typ := regType(t, func(ctx context.Context, j *Job, _ Report) (ResultFile, error) {
		close(started)
		<-ctx.Done()
		return ResultFile{}, ctx.Err()
	})
	j, _ := m.Submit(tctx(), Submit{Type: typ, Owner: 1})
	done := make(chan struct{})
	go func() { m.RunOnce(); close(done) }()
	<-started
	if err := m.Cancel(tctx(), Actor{ID: 1}, j.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler not canceled")
	}
	if got, _ := st.Get(context.Background(), j.ID); got.Status != StatusCanceled || got.ResultFile != "" {
		t.Fatalf("job = %+v", got)
	}
	if err := m.Cancel(tctx(), Actor{ID: 1}, j.ID); !errors.Is(err, ErrBadState) {
		t.Fatalf("cancel finished job err = %v", err)
	}
}

func TestCancelFromOtherInstanceStopsRunningHandler(t *testing.T) {
	st := newMemStore()
	m := newMgr(t, st, "n", nil, nil)
	started := make(chan struct{})
	typ := regType(t, func(ctx context.Context, j *Job, _ Report) (ResultFile, error) {
		close(started)
		<-ctx.Done()
		return ResultFile{}, ctx.Err()
	})
	j, _ := m.Submit(tctx(), Submit{Type: typ, Owner: 1})
	go m.RunOnce()
	<-started
	_, _ = st.Cancel(context.Background(), j.ID, time.Now()) // 另一个实例直接改库
	waitStatus(t, st, j.ID, StatusCanceled)
	time.Sleep(50 * time.Millisecond) // 等 flushLoop 发现并取消 ctx，handler 返回后不得把状态覆盖回去
	if got, _ := st.Get(context.Background(), j.ID); got.Status != StatusCanceled {
		t.Fatalf("status overwritten: %s", got.Status)
	}
}

func TestCancelQueued(t *testing.T) {
	st := newMemStore()
	m := newMgr(t, st, "n", nil, nil)
	typ := regType(t, func(context.Context, *Job, Report) (ResultFile, error) { return ResultFile{}, nil })
	j, _ := m.Submit(tctx(), Submit{Type: typ, Owner: 1})
	if err := m.Cancel(tctx(), Actor{ID: 1}, j.ID); err != nil {
		t.Fatal(err)
	}
	if m.RunOnce() {
		t.Fatal("canceled job must not run")
	}
}

func TestRestartResetsOwnRunningOnly(t *testing.T) {
	st := newMemStore()
	typ := regType(t, func(context.Context, *Job, Report) (ResultFile, error) { return ResultFile{}, nil })
	now := time.Now()
	st.jobs[1] = &Job{Type: typ, Status: StatusRunning, Node: "n1", StartedAt: &now}
	st.jobs[2] = &Job{Type: typ, Status: StatusRunning, Node: "n2", StartedAt: &now}
	st.jobs[3] = &Job{Type: typ, Status: StatusQueued}
	for id, j := range st.jobs {
		j.ID = id
	}
	m := newMgr(t, st, "n1", nil, func(o *Options) { o.PollEvery = time.Hour })
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	j1, _ := st.Get(context.Background(), 1)
	if j1.Status != StatusFailed || !strings.Contains(j1.Error, "重启") || j1.FinishedAt == nil {
		t.Fatalf("own running job = %+v", j1)
	}
	if j2, _ := st.Get(context.Background(), 2); j2.Status != StatusRunning {
		t.Fatalf("other node's job touched: %s", j2.Status)
	}
	if err := m.Retry(tctx(), Actor{ID: 0, Admin: true}, 1); err != nil {
		t.Fatalf("retry after reset: %v", err)
	}
}

func TestRetentionCleanup(t *testing.T) {
	st := newMemStore()
	files, _ := storage.NewLocal(t.TempDir())
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	m := newMgr(t, st, "n", files, func(o *Options) { o.Now = func() time.Time { return now }; o.KeepDays = 7 })
	put := func(key string) {
		if err := files.Put(context.Background(), key, strings.NewReader("x"), 1, storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	put("jobs/1/old.xlsx")
	put("jobs/1/new.xlsx")
	old, recent := now.AddDate(0, 0, -8), now.AddDate(0, 0, -6)
	st.jobs[1] = &Job{Status: StatusSuccess, ResultFile: "jobs/1/old.xlsx", FinishedAt: &old}
	st.jobs[2] = &Job{Status: StatusSuccess, ResultFile: "jobs/1/new.xlsx", FinishedAt: &recent}
	st.jobs[3] = &Job{Status: StatusRunning, StartedAt: &old}
	for id, j := range st.jobs {
		j.ID = id
	}
	if n := m.Cleanup(); n != 1 {
		t.Fatalf("cleaned %d", n)
	}
	if _, err := st.Get(context.Background(), 1); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired job kept")
	}
	if ok, _ := files.Exists(context.Background(), "jobs/1/old.xlsx"); ok {
		t.Fatal("expired file kept")
	}
	if ok, _ := files.Exists(context.Background(), "jobs/1/new.xlsx"); !ok {
		t.Fatal("fresh file removed")
	}
	if _, err := st.Get(context.Background(), 3); err != nil {
		t.Fatal("running job must never be cleaned")
	}
}

func TestOwnerIsolation(t *testing.T) {
	st := newMemStore()
	m := newMgr(t, st, "n", nil, nil)
	typ := regType(t, func(context.Context, *Job, Report) (ResultFile, error) { return ResultFile{}, nil })
	mine, _ := m.Submit(tctx(), Submit{Type: typ, Owner: 1})
	theirs, _ := m.Submit(tctx(), Submit{Type: typ, Owner: 2})
	u1, admin := Actor{ID: 1}, Actor{ID: 9, Admin: true}

	if _, err := m.Get(tctx(), u1, theirs.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get other's job err = %v", err)
	}
	for name, fn := range map[string]func() error{
		"cancel":   func() error { return m.Cancel(tctx(), u1, theirs.ID) },
		"delete":   func() error { return m.Delete(tctx(), u1, theirs.ID) },
		"retry":    func() error { return m.Retry(tctx(), u1, theirs.ID) },
		"download": func() error { _, err := m.Downloadable(tctx(), u1, theirs.ID); return err },
	} {
		if err := fn(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s other's job err = %v", name, err)
		}
	}
	list, _, _ := m.List(tctx(), u1, Filter{}, true) // 普通用户即使要求 all 也只看自己的
	if len(list) != 1 || list[0].ID != mine.ID {
		t.Fatalf("user list = %+v", list)
	}
	if all, _, _ := m.List(tctx(), admin, Filter{}, true); len(all) != 2 {
		t.Fatalf("admin all = %d", len(all))
	}
	if own, _, _ := m.List(tctx(), admin, Filter{}, false); len(own) != 0 {
		t.Fatalf("admin default must be own only, got %d", len(own))
	}
	if _, err := m.Get(tctx(), admin, theirs.ID); err != nil {
		t.Fatalf("admin get: %v", err)
	}
}

func TestPerUserLimit(t *testing.T) {
	st := newMemStore()
	m := newMgr(t, st, "n", nil, func(o *Options) { o.MaxPerUser = 2 })
	typ := regType(t, func(context.Context, *Job, Report) (ResultFile, error) { return ResultFile{}, nil })
	for i := 0; i < 2; i++ {
		if _, err := m.Submit(tctx(), Submit{Type: typ, Owner: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Submit(tctx(), Submit{Type: typ, Owner: 1}); !errors.Is(err, ErrTooMany) {
		t.Fatalf("third submit err = %v", err)
	}
	if _, err := m.Submit(tctx(), Submit{Type: typ, Owner: 2}); err != nil {
		t.Fatalf("other user blocked: %v", err)
	}
	m.RunOnce() // 做完一个，名额释放
	if _, err := m.Submit(tctx(), Submit{Type: typ, Owner: 1}); err != nil {
		t.Fatalf("submit after completion: %v", err)
	}
}

func TestPanicIsCaught(t *testing.T) {
	st := newMemStore()
	m := newMgr(t, st, "n", nil, nil)
	boom := regType(t, func(context.Context, *Job, Report) (ResultFile, error) { panic("secret-internal-detail") })
	ok := regType(t, func(context.Context, *Job, Report) (ResultFile, error) { return ResultFile{}, nil })
	j1, _ := m.Submit(tctx(), Submit{Type: boom, Owner: 1})
	j2, _ := m.Submit(tctx(), Submit{Type: ok, Owner: 1})
	for m.RunOnce() {
	}
	g1, _ := st.Get(context.Background(), j1.ID)
	if g1.Status != StatusFailed || g1.Error == "" || strings.Contains(g1.Error, "secret-internal-detail") {
		t.Fatalf("panicked job = %+v", g1)
	}
	if g2, _ := st.Get(context.Background(), j2.ID); g2.Status != StatusSuccess {
		t.Fatalf("next job status = %s", g2.Status)
	}
}

func TestFailureRetryAndUserError(t *testing.T) {
	st := newMemStore()
	m := newMgr(t, st, "n", nil, nil)
	var fail atomic.Bool
	fail.Store(true)
	typ := regType(t, func(context.Context, *Job, Report) (ResultFile, error) {
		if fail.Load() {
			return ResultFile{}, Errorf("没有符合条件的数据")
		}
		return ResultFile{}, nil
	})
	j, _ := m.Submit(tctx(), Submit{Type: typ, Owner: 1})
	m.RunOnce()
	g, _ := st.Get(context.Background(), j.ID)
	if g.Status != StatusFailed || g.Error != "没有符合条件的数据" {
		t.Fatalf("job = %+v", g)
	}
	fail.Store(false)
	if err := m.Retry(tctx(), Actor{ID: 1}, j.ID); err != nil {
		t.Fatal(err)
	}
	m.RunOnce()
	if g, _ = st.Get(context.Background(), j.ID); g.Status != StatusSuccess || g.Error != "" {
		t.Fatalf("after retry = %+v", g)
	}
	if err := m.Retry(tctx(), Actor{ID: 1}, j.ID); !errors.Is(err, ErrBadState) {
		t.Fatalf("retry success job err = %v", err)
	}
}

func TestSubmitValidation(t *testing.T) {
	st := newMemStore()
	m := newMgr(t, st, "n", nil, nil)
	typ := regType(t, func(context.Context, *Job, Report) (ResultFile, error) { return ResultFile{}, nil })
	if _, err := m.Submit(tctx(), Submit{Type: "nope", Owner: 1}); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("unknown type err = %v", err)
	}
	if _, err := m.Submit(tctx(), Submit{Type: typ, Owner: 1, Params: map[string]any{"filter": map[string]any{"passWord": "x"}}}); !errors.Is(err, ErrSensitiveParams) {
		t.Fatalf("sensitive params err = %v", err)
	}
	if _, err := m.Submit(context.Background(), Submit{Type: typ, Owner: 1}); !errors.Is(err, ErrNoTenant) {
		t.Fatalf("no tenant err = %v", err)
	}
}

func TestWriteResultFailureLeavesNoFile(t *testing.T) {
	st := newMemStore()
	files, _ := storage.NewLocal(t.TempDir())
	m := newMgr(t, st, "n", files, nil)
	var key string
	typ := regType(t, func(ctx context.Context, j *Job, _ Report) (ResultFile, error) {
		return WriteResult(ctx, j, "../../etc/x.xlsx", "x/y", func(w io.Writer) error {
			_, _ = w.Write([]byte("partial"))
			return errors.New("boom")
		})
	})
	j, _ := m.Submit(tctx(), Submit{Type: typ, Owner: 1})
	m.RunOnce()
	g, _ := st.Get(context.Background(), j.ID)
	if g.Status != StatusFailed || g.ResultFile != "" {
		t.Fatalf("job = %+v", g)
	}
	_ = key
	if n := countFiles(t, files, j); n != 0 {
		t.Fatalf("%d leftover objects", n)
	}
}

func countFiles(t *testing.T, files storage.Storage, j *Job) int {
	t.Helper()
	l := files.(*storage.Local)
	key := storage.ObjectKey("jobs/"+j.TenantID.String(), j.ID.String(), ".xlsx", time.Now())
	if ok, _ := l.Exists(context.Background(), key); ok {
		return 1
	}
	return 0
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{"../../etc/passwd": "passwd", `a"b.xlsx`: "a_b.xlsx", "": "download", "用户.xlsx": "用户.xlsx"} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}
