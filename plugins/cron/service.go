package cron

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	jobTimeout    = 5 * time.Minute // 单次尝试的上限
	maxOutput     = 64 << 10        // 写入日志的输出上限
	maxError      = 4 << 10
	maxBackoff    = 30 * time.Second
	maxCatchUp    = 5 * time.Minute // 进程被挂起后最多补跑多久之内的调度分钟
	staleRunning  = time.Hour       // 超过这个时间仍是 running 的日志视为中断
	maxRetryCount = 10
)

// Service 是调度器 + 任务管理。单实例直接按分钟调度；多实例靠数据库抢占避免重复执行。
//
// 为什么不用 core/cache 加锁：Cache 接口只有 Get/Set/Del/Exists，没有 SetNX（原子"不存在才写入"），
// 用 Exists+Set 实现的锁有竞态，两个实例可能同时拿到。所以改用数据库乐观抢占：
//
//	UPDATE plg_cron_job SET last_slot = <本分钟> WHERE id = ? AND last_slot < <本分钟>
//
// 一条 UPDATE 在 MySQL / MariaDB / PostgreSQL 上都是原子的，多个实例同时执行时只有一个 RowsAffected == 1，
// 抢到的执行，其余跳过。同一分钟内重启、时钟小幅回拨也不会重复触发。
// cron 表达式按服务器本地时区解释。
type Service struct {
	db  *gorm.DB
	log *zap.Logger

	mu      sync.Mutex
	running map[idgen.ID]bool // 本实例内同一任务不并发
	base    context.Context   // 后台执行用的根 context；Start 后会换成框架传入的 ctx
}

// NewService 创建服务（不启动调度）。
func NewService(db *gorm.DB, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, log: log, running: map[idgen.ID]bool{}, base: context.Background()}
}

// ---- 调度循环 ----

// Run 阻塞运行调度循环，直到 ctx 结束。
func (s *Service) Run(ctx context.Context) {
	s.mu.Lock()
	s.base = ctx
	s.mu.Unlock()
	s.recoverStale(ctx)

	last := time.Now().Truncate(time.Minute)
	for {
		// 对齐到下一个整分钟后 1 秒，避开恰好跨分钟时的时钟抖动
		wait := time.Until(last.Add(time.Minute + time.Second))
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		now := time.Now().Truncate(time.Minute)
		for m := last.Add(time.Minute); !m.After(now); m = m.Add(time.Minute) {
			if now.Sub(m) > maxCatchUp {
				continue // 挂起太久的分钟不补跑
			}
			s.tick(ctx, m)
		}
		last = now
	}
}

// recoverStale 把长时间停在 running 的日志标记为中断（进程崩溃/被杀留下的）。
func (s *Service) recoverStale(ctx context.Context) {
	cutoff := time.Now().Add(-staleRunning)
	err := s.db.WithContext(tenant.Skip(ctx)).Model(&Log{}).
		Where("status = ? AND started_at < ?", StatusRunning, cutoff).
		Updates(map[string]any{"status": StatusFailed, "error": "interrupted (process stopped before the job finished)"}).Error
	if err != nil {
		s.log.Warn("cron: recover stale logs", zap.Error(err))
	}
}

// tick 处理一个调度分钟 m：找出命中的启用任务，抢占后异步执行。
func (s *Service) tick(ctx context.Context, m time.Time) {
	skip := tenant.Skip(ctx)
	var jobs []Job
	if err := s.db.WithContext(skip).Where("enabled = ?", true).Find(&jobs).Error; err != nil {
		s.log.Error("cron: load jobs", zap.Error(err))
		return
	}
	slot := m.Unix() / 60
	for i := range jobs {
		job := jobs[i]
		sch, err := ParseCron(job.CronExpr)
		if err != nil {
			s.log.Warn("cron: skip job with invalid expression", zap.String("job", job.Name), zap.Error(err))
			continue
		}
		if !sch.Matches(m) {
			continue
		}
		// 乐观抢占：只有把 last_slot 从 < slot 改成 slot 的那个实例（RowsAffected == 1）才执行。
		res := s.db.WithContext(skip).Model(&Job{}).Where("id = ? AND last_slot < ?", job.ID, slot).UpdateColumn("last_slot", slot)
		if res.Error != nil {
			s.log.Error("cron: claim job", zap.String("job", job.Name), zap.Error(res.Error))
			continue
		}
		if res.RowsAffected == 0 {
			continue // 其他实例已抢到，或本分钟已经跑过
		}
		if _, err := s.dispatch(job, TriggerSchedule); err != nil {
			s.log.Warn("cron: dispatch", zap.String("job", job.Name), zap.Error(err))
		}
	}
}

var errAlreadyRunning = errors.New("cron: job is already running")

// dispatch 创建一条 running 日志并在后台执行任务。
func (s *Service) dispatch(job Job, trigger string) (*Log, error) {
	s.mu.Lock()
	if s.running[job.ID] {
		s.mu.Unlock()
		return nil, errAlreadyRunning
	}
	s.running[job.ID] = true
	base := s.base
	s.mu.Unlock()

	ctx := tenant.With(base, job.TenantID)
	row := &Log{JobID: job.ID, JobName: job.Name, Handler: job.Handler, TriggerType: trigger,
		Status: StatusRunning, StartedAt: time.Now().UTC()}
	row.TenantID = job.TenantID
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		s.release(job.ID)
		return nil, err
	}
	go func() {
		defer s.release(job.ID)
		s.execute(ctx, job, row)
	}()
	return row, nil
}

func (s *Service) release(id idgen.ID) {
	s.mu.Lock()
	delete(s.running, id)
	s.mu.Unlock()
}

// execute 执行任务（含失败重试，指数退避 1s、2s、4s…最多 30s），并回写日志与任务的最近状态。
func (s *Service) execute(ctx context.Context, job Job, row *Log) {
	env := &Env{DB: s.db.WithContext(ctx), Log: s.log.With(zap.String("job", job.Name)), TenantID: int64(job.TenantID), JobID: int64(job.ID)}
	var out string
	var err error
	attempts := 0
	spec, ok := lookupHandler(job.Handler)
	if !ok {
		attempts, err = 1, fmt.Errorf("handler %q is not registered", job.Handler)
	} else {
		for attempts < job.RetryCount+1 {
			attempts++
			actx, cancel := context.WithTimeout(ctx, jobTimeout)
			out, err = safeRun(actx, spec, env, job.Params)
			cancel()
			if err == nil || ctx.Err() != nil || attempts > job.RetryCount {
				break
			}
			select { // 退避等待，期间收到停止信号就放弃重试
			case <-ctx.Done():
			case <-time.After(min(maxBackoff, time.Second<<(attempts-1))):
			}
		}
	}

	finished := time.Now().UTC()
	status := StatusSuccess
	errText := ""
	if err != nil {
		status, errText = StatusFailed, truncate(err.Error(), maxError)
	}
	// 收尾写库用独立的 context：即使因停机 ctx 已取消，也要把结果记下来。
	wctx, cancel := context.WithTimeout(tenant.With(context.Background(), job.TenantID), 10*time.Second)
	defer cancel()
	if e := s.db.WithContext(wctx).Model(&Log{}).Where("id = ?", row.ID).Updates(map[string]any{
		"status": status, "attempts": attempts, "finished_at": finished,
		"cost_ms": int(finished.Sub(row.StartedAt).Milliseconds()),
		"output":  truncate(out, maxOutput), "error": errText,
	}).Error; e != nil {
		s.log.Error("cron: write log", zap.Error(e))
	}
	if e := s.db.WithContext(wctx).Model(&Job{}).Where("id = ?", job.ID).
		UpdateColumns(map[string]any{"last_run_at": row.StartedAt, "last_status": status}).Error; e != nil {
		s.log.Error("cron: update job status", zap.Error(e))
	}
}

// safeRun 调用处理器并把 panic 转成错误，避免一个坏处理器带崩整个进程。
func safeRun(ctx context.Context, spec HandlerSpec, env *Env, params string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return spec.Run(ctx, env, params)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) && len(s) > 0 { // 不截断在多字节字符中间
		s = s[:len(s)-1]
	}
	return s + "\n…(truncated)"
}

// ---- 任务管理 ----

// JobForm 是创建/更新任务的请求体。
type JobForm struct {
	Name       string `json:"name"`
	CronExpr   string `json:"cronExpr"`
	Handler    string `json:"handler"`
	Params     string `json:"params"`
	Enabled    bool   `json:"enabled"`
	RetryCount int    `json:"retryCount"`
}

// JobView 是任务列表项：任务本身 + 下次执行时间。
type JobView struct {
	Job
	NextRunAt *time.Time `json:"nextRunAt"`
}

func badReq(msg string) error { return response.BadRequest("error.bad_request").WithMsg(msg) }

func (f *JobForm) normalize() error {
	f.Name = strings.TrimSpace(f.Name)
	f.CronExpr = strings.Join(strings.Fields(f.CronExpr), " ")
	f.Handler = strings.TrimSpace(f.Handler)
	f.Params = strings.TrimSpace(f.Params)
	if n := utf8.RuneCountInString(f.Name); n < 1 || n > 100 {
		return badReq("name must be 1-100 characters")
	}
	if _, err := ParseCron(f.CronExpr); err != nil {
		return badReq(err.Error())
	}
	spec, ok := lookupHandler(f.Handler)
	if !ok {
		return badReq(fmt.Sprintf("unknown handler %q", f.Handler))
	}
	if f.RetryCount < 0 || f.RetryCount > maxRetryCount {
		return badReq(fmt.Sprintf("retryCount must be 0-%d", maxRetryCount))
	}
	if len(f.Params) > 4<<10 {
		return badReq("params too long")
	}
	if spec.Validate != nil {
		if err := spec.Validate(f.Params); err != nil {
			return badReq(err.Error())
		}
	}
	return nil
}

func view(j Job, now time.Time) JobView {
	v := JobView{Job: j}
	if j.Enabled {
		if sch, err := ParseCron(j.CronExpr); err == nil {
			if n := sch.Next(now); !n.IsZero() {
				v.NextRunAt = &n
			}
		}
	}
	return v
}

// JobQuery 是任务列表筛选条件。
type JobQuery struct {
	Keyword, Handler, Enabled string
	Page, PageSize            int
}

func (s *Service) ListJobs(ctx context.Context, q JobQuery) ([]JobView, int64, error) {
	tx := s.db.WithContext(ctx).Model(&Job{})
	if q.Keyword != "" {
		tx = tx.Where("name LIKE ?", "%"+escapeLike(q.Keyword)+"%")
	}
	if q.Handler != "" {
		tx = tx.Where("handler = ?", q.Handler)
	}
	switch q.Enabled {
	case "1", "true":
		tx = tx.Where("enabled = ?", true)
	case "0", "false":
		tx = tx.Where("enabled = ?", false)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Job
	if err := tx.Order("created_at DESC, id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	now := time.Now()
	out := make([]JobView, len(rows))
	for i, r := range rows {
		out[i] = view(r, now)
	}
	return out, total, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *Service) getJob(ctx context.Context, id idgen.ID) (*Job, error) {
	var j Job
	if err := s.db.WithContext(ctx).First(&j, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("error.not_found")
		}
		return nil, err
	}
	return &j, nil
}

func (s *Service) SaveJob(ctx context.Context, id idgen.ID, f JobForm) (*JobView, error) {
	if err := f.normalize(); err != nil {
		return nil, err
	}
	var j *Job
	if id == 0 {
		j = &Job{}
	} else {
		var err error
		if j, err = s.getJob(ctx, id); err != nil {
			return nil, err
		}
	}
	j.Name, j.CronExpr, j.Handler, j.Params, j.Enabled, j.RetryCount = f.Name, f.CronExpr, f.Handler, f.Params, f.Enabled, f.RetryCount
	if err := s.db.WithContext(ctx).Save(j).Error; err != nil {
		return nil, err
	}
	v := view(*j, time.Now())
	return &v, nil
}

func (s *Service) SetEnabled(ctx context.Context, id idgen.ID, enabled bool) (*JobView, error) {
	j, err := s.getJob(ctx, id)
	if err != nil {
		return nil, err
	}
	j.Enabled = enabled
	if err := s.db.WithContext(ctx).Model(j).Update("enabled", enabled).Error; err != nil {
		return nil, err
	}
	v := view(*j, time.Now())
	return &v, nil
}

func (s *Service) DeleteJob(ctx context.Context, id idgen.ID) error {
	if _, err := s.getJob(ctx, id); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(&Job{}, "id = ?", id).Error // 执行日志保留，job_name 已冗余存储
}

// RunNow 立即异步执行一次（不受 cron 表达式与启用状态限制），返回 running 状态的日志行。
func (s *Service) RunNow(ctx context.Context, id idgen.ID) (*Log, error) {
	j, err := s.getJob(ctx, id)
	if err != nil {
		return nil, err
	}
	row, err := s.dispatch(*j, TriggerManual)
	if errors.Is(err, errAlreadyRunning) {
		return nil, response.Conflict("error.conflict").WithMsg("job is already running")
	}
	return row, err
}

// ---- 执行日志 ----

// LogQuery 是执行日志筛选条件。
type LogQuery struct {
	JobID, Status  string
	Page, PageSize int
}

// 列表里 output / error 只取前 500 个字符（SUBSTR 三种数据库通用），完整内容用 GetLog 取。
const logListColumns = "id, tenant_id, job_id, job_name, handler, trigger_type, status, attempts, started_at, finished_at, cost_ms, " +
	"SUBSTR(output, 1, 500) AS output, SUBSTR(error, 1, 500) AS error, created_at, updated_at"

func (s *Service) ListLogs(ctx context.Context, q LogQuery) ([]Log, int64, error) {
	tx := s.db.WithContext(ctx).Model(&Log{})
	if q.JobID != "" {
		jid, err := idgen.Parse(q.JobID)
		if err != nil {
			return nil, 0, badReq("invalid jobId")
		}
		tx = tx.Where("job_id = ?", jid)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Log
	err := tx.Select(logListColumns).Order("started_at DESC, id DESC").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error
	return rows, total, err
}

func (s *Service) GetLog(ctx context.Context, id idgen.ID) (*Log, error) {
	var l Log
	if err := s.db.WithContext(ctx).First(&l, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("error.not_found")
		}
		return nil, err
	}
	return &l, nil
}

// CleanLogs 删除 days 天前的日志（days = 0 表示全部已结束的日志）；可限定某个任务；不会删除仍在运行的记录。
func (s *Service) CleanLogs(ctx context.Context, days int, jobID idgen.ID) (int64, error) {
	tx := s.db.WithContext(ctx).Where("status <> ?", StatusRunning)
	if days > 0 {
		tx = tx.Where("started_at < ?", time.Now().AddDate(0, 0, -days))
	}
	if jobID != 0 {
		tx = tx.Where("job_id = ?", jobID)
	}
	res := tx.Delete(&Log{})
	return res.RowsAffected, res.Error
}
