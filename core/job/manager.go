package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/storage"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Options 配置管理器。零值字段取默认值。
type Options struct {
	Store        Store
	Files        storage.Storage // 结果文件存储（与文件模块同一套 storage.*）
	Log          *zap.Logger
	Node         string        // 本实例标识（重启恢复只处理本节点遗留的 running），多实例必须各不相同
	Workers      int           // job.workers，默认 2
	KeepDays     int           // job.keep_days，默认 7；结果文件与记录到期一并清理
	MaxPerUser   int           // job.max_per_user，默认 3（queued + running）
	PollEvery    time.Duration // 空闲 worker 轮询队列的间隔，默认 2s
	FlushEvery   time.Duration // 进度落库 / 检查是否被取消的间隔，默认 1s
	CleanupEvery time.Duration // 保留期清理间隔，默认 1h
	Now          func() time.Time
}

// Manager 是任务管理器：提交、查询、取消、重试、删除，以及 worker 与清理循环。
type Manager struct {
	o      Options
	ctx    context.Context
	stop   context.CancelFunc
	wg     sync.WaitGroup
	wake   chan struct{}
	mu     sync.Mutex
	cancel map[idgen.ID]context.CancelFunc // 本实例正在执行的任务
	once   sync.Once
	ready  atomic.Bool
}

// New 创建管理器（尚未启动）。
func New(o Options) *Manager {
	if o.Workers <= 0 {
		o.Workers = DefaultWorkers
	}
	if o.KeepDays <= 0 {
		o.KeepDays = DefaultKeepDays
	}
	if o.MaxPerUser <= 0 {
		o.MaxPerUser = DefaultMaxPerUser
	}
	if o.PollEvery <= 0 {
		o.PollEvery = 2 * time.Second
	}
	if o.FlushEvery <= 0 {
		o.FlushEvery = time.Second
	}
	if o.CleanupEvery <= 0 {
		o.CleanupEvery = time.Hour
	}
	if o.Now == nil {
		o.Now = func() time.Time { return time.Now().UTC() }
	}
	if o.Log == nil {
		o.Log = zap.NewNop()
	}
	if o.Node == "" {
		o.Node = "node-0"
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Manager{o: o, ctx: ctx, stop: stop, wake: make(chan struct{}, 16), cancel: map[idgen.ID]context.CancelFunc{}}
}

// Storage 返回结果文件存储。
func (m *Manager) Storage() storage.Storage { return m.o.Files }

// Workers 返回并发 worker 数。
func (m *Manager) Workers() int { return m.o.Workers }

// Ready reports whether workers started successfully and have not been stopped.
func (m *Manager) Ready() bool { return m != nil && m.ready.Load() && m.ctx.Err() == nil }

func (m *Manager) skip() context.Context { return tenant.Skip(m.ctx) }

// QueueLengths 返回 状态 -> 数量（queued / running，全租户），供 metrics.SetQueueSource 登记。
// Store 不支持统计或查询失败时返回空 map（指标只是缺这一次采样，不影响抓取）。
func (m *Manager) QueueLengths() map[string]int64 {
	qc, ok := m.o.Store.(QueueCounter)
	if !ok {
		return map[string]int64{}
	}
	ctx, cancel := context.WithTimeout(m.skip(), 3*time.Second)
	defer cancel()
	out, err := qc.CountByStatus(ctx)
	if err != nil {
		m.o.Log.Warn("job: count queue failed", zap.Error(err))
		return map[string]int64{}
	}
	return out
}

// Start 恢复本节点遗留的 running 任务（置为 failed），然后启动 worker 与清理循环。只生效一次。
func (m *Manager) Start() error {
	var err error
	m.once.Do(func() {
		var n int64
		n, err = m.o.Store.ResetRunning(m.skip(), m.o.Node, "服务重启，任务被中断，请重试", m.o.Now())
		if err != nil {
			return
		}
		if n > 0 {
			m.o.Log.Warn("job: reset interrupted running jobs", zap.Int64("count", n))
		}
		for i := 0; i < m.o.Workers; i++ {
			m.wg.Add(1)
			go m.worker()
		}
		m.wg.Add(1)
		go m.cleanupLoop()
		m.ready.Store(true)
	})
	return err
}

// Stop 取消所有执行中的任务并等待 worker 退出。
func (m *Manager) Stop() {
	m.ready.Store(false)
	m.stop()
	m.wg.Wait()
}

// ---- 提交与查询 ----

// Actor 是操作者：普通用户只能操作自己的任务，Admin（job:all）可以操作租户内全部任务。
type Actor struct {
	ID    idgen.ID
	Admin bool
}

func (a Actor) can(j *Job) bool { return a.Admin || j.OwnerID == a.ID }

// Submit 是提交任务的入参。
type Submit struct {
	Type   string
	Owner  idgen.ID
	Params any // 序列化为 JSON；键名含 password/secret/token 等敏感词会被拒绝
}

// Submit 创建一个 queued 任务。ctx 需带租户。超过每用户并发上限返回 ErrTooMany。
func (m *Manager) Submit(ctx context.Context, in Submit) (*Job, error) {
	tid, ok := tenant.From(ctx)
	if !ok {
		return nil, ErrNoTenant
	}
	if _, ok := handlerFor(in.Type); !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, in.Type)
	}
	raw := []byte("{}")
	if in.Params != nil {
		var err error
		if raw, err = json.Marshal(in.Params); err != nil {
			return nil, fmt.Errorf("job: marshal params: %w", err)
		}
	}
	if err := checkParams(raw); err != nil {
		return nil, err
	}
	if err := m.checkLimit(ctx, in.Owner); err != nil {
		return nil, err
	}
	j := &Job{OwnerID: in.Owner, Type: in.Type, Params: string(raw), Status: StatusQueued}
	j.TenantID = tid
	if err := m.o.Store.Insert(ctx, j); err != nil {
		return nil, err
	}
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return j, nil
}

func (m *Manager) checkLimit(ctx context.Context, owner idgen.ID) error {
	n, err := m.o.Store.CountActive(ctx, owner)
	if err != nil {
		return err
	}
	if int(n) >= m.o.MaxPerUser {
		return ErrTooMany
	}
	return nil
}

// Get 取任务；不是自己的（且非管理员）一律按不存在处理，避免枚举他人任务。
func (m *Manager) Get(ctx context.Context, a Actor, id idgen.ID) (*Job, error) {
	j, err := m.o.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !a.can(j) {
		return nil, ErrNotFound
	}
	return j, nil
}

// List 列出任务：非管理员强制只看自己的。
func (m *Manager) List(ctx context.Context, a Actor, f Filter, all bool) ([]*Job, int64, error) {
	if !a.Admin || !all {
		f.Owner = a.ID
	}
	return m.o.Store.List(ctx, f)
}

// Cancel 取消 queued / running 任务；running 的会被 context 取消。
func (m *Manager) Cancel(ctx context.Context, a Actor, id idgen.ID) error {
	j, err := m.Get(ctx, a, id)
	if err != nil {
		return err
	}
	if IsFinal(j.Status) {
		return ErrBadState
	}
	done, err := m.o.Store.Cancel(ctx, id, m.o.Now())
	if err != nil {
		return err
	}
	if !done {
		return ErrBadState
	}
	m.mu.Lock()
	cancel := m.cancel[id]
	m.mu.Unlock()
	if cancel != nil {
		cancel() // 本实例在执行：立即取消；其它实例由进度刷新循环发现状态变化后取消
	}
	return nil
}

// Retry 重新排队一个失败任务（仅 failed）。
func (m *Manager) Retry(ctx context.Context, a Actor, id idgen.ID) error {
	j, err := m.Get(ctx, a, id)
	if err != nil {
		return err
	}
	if j.Status != StatusFailed {
		return ErrBadState
	}
	if err := m.checkLimit(ctx, j.OwnerID); err != nil {
		return err
	}
	done, err := m.o.Store.Requeue(ctx, id)
	if err != nil {
		return err
	}
	if !done {
		return ErrBadState
	}
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return nil
}

// Delete 删除已结束的任务及其结果文件。
func (m *Manager) Delete(ctx context.Context, a Actor, id idgen.ID) error {
	j, err := m.Get(ctx, a, id)
	if err != nil {
		return err
	}
	if !IsFinal(j.Status) {
		return ErrBadState
	}
	if done, err := m.o.Store.Delete(ctx, id); err != nil {
		return err
	} else if !done {
		return ErrBadState
	}
	m.removeFile(j.ResultFile)
	return nil
}

// Downloadable 取可下载的任务（成功且有结果文件）；所有权校验同 Get。
func (m *Manager) Downloadable(ctx context.Context, a Actor, id idgen.ID) (*Job, error) {
	j, err := m.Get(ctx, a, id)
	if err != nil {
		return nil, err
	}
	if j.Status != StatusSuccess || j.ResultFile == "" {
		return nil, ErrBadState
	}
	return j, nil
}

func (m *Manager) removeFile(key string) {
	if key == "" || m.o.Files == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.o.Files.Delete(ctx, key); err != nil && !errors.Is(err, storage.ErrNotFound) {
		m.o.Log.Warn("job: delete result file failed", zap.String("key", key), zap.Error(err))
	}
}

// ---- 结果文件 ----

type envKey struct{}

// WriteResult 把 fn 写出的内容流式存入对象存储（不落内存、不落本地临时大文件），返回 ResultFile。
// 失败或被取消时已写入的对象会被清理。只能在 Handler 内调用。
func WriteResult(ctx context.Context, j *Job, name, contentType string, fn func(w io.Writer) error) (ResultFile, error) {
	m, _ := ctx.Value(envKey{}).(*Manager)
	if m == nil || m.o.Files == nil {
		return ResultFile{}, errors.New("job: result storage unavailable")
	}
	name = safeName(name)
	key := storage.ObjectKey("jobs/"+j.TenantID.String(), j.ID.String(), filepath.Ext(name), m.o.Now())
	pr, pw := io.Pipe()
	cw := &countWriter{w: pw}
	done := make(chan error, 1)
	go func() {
		err := m.o.Files.PutStream(ctx, key, pr, storage.PutOptions{
			ContentType:        contentType,
			ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": name}),
		})
		if err != nil {
			pr.CloseWithError(err) // 存储提前失败时让写入方立刻返回，而不是阻塞
		} else {
			_ = pr.Close()
		}
		done <- err
	}()
	ferr := fn(cw)
	if ferr != nil {
		pw.CloseWithError(ferr)
	} else {
		_ = pw.Close()
	}
	perr := <-done
	if ferr != nil || perr != nil {
		m.removeFile(key)
		if ferr != nil {
			return ResultFile{}, ferr
		}
		return ResultFile{}, perr
	}
	if err := ctx.Err(); err != nil { // 写完时恰好被取消：不要留文件
		m.removeFile(key)
		return ResultFile{}, err
	}
	return ResultFile{Key: key, Name: name, ContentType: contentType, Size: cw.n}, nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// safeName 去掉路径分隔符与控制字符，限制长度，避免进入响应头或存储键时出问题。
func safeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\"`, r) {
			return '_'
		}
		return r
	}, filepath.Base(name))
	name = truncate(strings.TrimSpace(name), 120)
	if name == "" || name == "." {
		return "download"
	}
	return name
}

// ---- worker ----

func (m *Manager) worker() {
	defer m.wg.Done()
	t := time.NewTicker(m.o.PollEvery)
	defer t.Stop()
	for {
		for m.ctx.Err() == nil && m.runOne() {
		}
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case <-t.C:
		}
	}
}

// RunOnce 抢占并执行一个任务，返回是否执行了任务（测试与手动驱动用）。
func (m *Manager) RunOnce() bool { return m.runOne() }

func (m *Manager) runOne() bool {
	j, err := m.o.Store.Claim(m.skip(), Types(), m.o.Node, m.o.Now())
	if err != nil {
		m.o.Log.Warn("job: claim failed", zap.Error(err))
		return false
	}
	if j == nil {
		return false
	}
	m.execute(j)
	return true
}

func (m *Manager) execute(j *Job) {
	ctx, cancel := context.WithCancel(m.ctx)
	defer cancel()
	m.mu.Lock()
	m.cancel[j.ID] = cancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.cancel, j.ID)
		m.mu.Unlock()
	}()

	var prog atomic.Int64
	var msg atomic.Value
	msg.Store("")
	report := func(p int, s string) {
		p = min(max(p, 0), 100)
		for {
			old := prog.Load()
			if int64(p) <= old || prog.CompareAndSwap(old, int64(p)) {
				break
			}
		}
		msg.Store(truncate(s, maxMessageLen))
	}
	flushDone := make(chan struct{})
	go m.flushLoop(ctx, cancel, j.ID, &prog, &msg, flushDone)

	// 处理器里的写操作（审计、操作日志）需要归属：以任务提交者为操作人，名称标明来自哪类任务。
	// 只放归属信息（ContextWithUser），不构造 Claims，处理器拿不到可通过权限校验的登录态。
	hctx := auth.ContextWithUser(context.WithValue(tenant.With(ctx, j.TenantID), envKey{}, m), j.OwnerID, "job:"+j.Type)
	res, herr := m.invoke(hctx, j, report)
	cancel()
	<-flushDone

	status, errMsg := StatusSuccess, ""
	switch {
	case herr != nil && m.ctx.Err() != nil:
		status, errMsg = StatusFailed, "服务停止，任务被中断，请重试"
	case herr != nil:
		status = StatusFailed
		var ue *UserError
		if errors.As(herr, &ue) {
			errMsg = truncate(ue.Msg, maxErrorLen)
		} else if !errors.Is(herr, context.Canceled) {
			errMsg = "任务执行失败，请稍后重试或联系管理员"
		}
		if !errors.Is(herr, context.Canceled) {
			m.o.Log.Warn("job: handler failed", zap.String("type", j.Type), zap.String("id", j.ID.String()), zap.Error(herr))
		}
	}
	fctx, fcancel := context.WithTimeout(tenant.Skip(context.Background()), 10*time.Second)
	defer fcancel()
	ok, err := m.o.Store.Finish(fctx, j.ID, status, res, errMsg, m.o.Now())
	if err != nil {
		m.o.Log.Error("job: finish failed", zap.String("id", j.ID.String()), zap.Error(err))
	}
	if (!ok || status != StatusSuccess) && res.Key != "" {
		m.removeFile(res.Key) // 已被取消 / 落库失败 / 失败：不留孤儿文件
	}
}

// invoke 执行处理器，捕获 panic（只记日志，不拖垮进程）。
func (m *Manager) invoke(ctx context.Context, j *Job, report Report) (res ResultFile, err error) {
	h, ok := handlerFor(j.Type)
	if !ok {
		return ResultFile{}, Errorf("不支持的任务类型")
	}
	defer func() {
		if r := recover(); r != nil {
			m.o.Log.Error("job: handler panic", zap.String("type", j.Type), zap.String("id", j.ID.String()),
				zap.Any("panic", r), zap.Stack("stack"))
			res, err = ResultFile{}, fmt.Errorf("handler panic: %v", r)
		}
	}()
	return h(ctx, j, report)
}

// flushLoop 定期把进度写库，并检查任务是否已被（其它实例）取消。
func (m *Manager) flushLoop(ctx context.Context, cancel context.CancelFunc, id idgen.ID, prog *atomic.Int64, msg *atomic.Value, done chan<- struct{}) {
	defer close(done)
	t := time.NewTicker(m.o.FlushEvery)
	defer t.Stop()
	last, lastMsg := int64(-1), ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		sctx, scancel := context.WithTimeout(m.skip(), 5*time.Second)
		if st, err := m.o.Store.Status(sctx, id); err == nil && st != StatusRunning {
			scancel()
			cancel()
			return
		}
		if p, s := prog.Load(), msg.Load().(string); p != last || s != lastMsg {
			if err := m.o.Store.SetProgress(sctx, id, int(p), s); err == nil {
				last, lastMsg = p, s
			}
		}
		scancel()
	}
}

// ---- 保留期清理 ----

func (m *Manager) cleanupLoop() {
	defer m.wg.Done()
	m.Cleanup()
	t := time.NewTicker(m.o.CleanupEvery)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.Cleanup()
		}
	}
}

// Cleanup 删除结束时间超过 keep_days 的任务及其结果文件，返回清理条数。
func (m *Manager) Cleanup() int {
	cutoff := m.o.Now().AddDate(0, 0, -m.o.KeepDays)
	total := 0
	for m.ctx.Err() == nil {
		rows, err := m.o.Store.Expired(m.skip(), cutoff, 100)
		if err != nil {
			m.o.Log.Warn("job: list expired failed", zap.Error(err))
			return total
		}
		if len(rows) == 0 {
			return total
		}
		progressed := 0
		for _, j := range rows {
			if j.ResultFile != "" && m.o.Files != nil {
				if err := m.o.Files.Delete(m.skip(), j.ResultFile); err != nil && !errors.Is(err, storage.ErrNotFound) {
					m.o.Log.Warn("job: delete expired file failed", zap.String("key", j.ResultFile), zap.Error(err))
					continue // 文件没删掉就保留记录，下一轮再试
				}
			}
			if ok, err := m.o.Store.Delete(m.skip(), j.ID); err == nil && ok {
				progressed++
			}
		}
		total += progressed
		if progressed == 0 {
			return total
		}
	}
	return total
}
