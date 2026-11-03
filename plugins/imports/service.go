package imports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Service 是导入中心业务层。
type Service struct {
	db  *gorm.DB
	log logger
	// cancels 是运行中异步任务的取消注册表(jobID → CancelFunc)；任务结束即移除。
	// 进程重启后注册表丢失——残留的 running 台账无法取消，管理员可删除重建。
	cancels sync.Map
}

// logger 是最小日志面(便于测试注入空实现)。
type logger interface {
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService 创建导入中心服务。
func NewService(db *gorm.DB, log logger) *Service { return &Service{db: db, log: log} }

var defaultSvc = make(chan *Service, 1)

// SetDefault 由插件装配层调用,注册进程级默认服务。
func SetDefault(s *Service) {
	select {
	case <-defaultSvc: // 已有则替换
	default:
	}
	defaultSvc <- s
}

// JobView 是导入台账视图。
type JobView struct {
	ID         idgen.ID   `json:"id"`
	Source     string     `json:"source"`
	FileName   string     `json:"fileName"`
	TotalRows  int        `json:"totalRows"`
	OkRows     int        `json:"okRows"`
	FailRows   int        `json:"failRows"`
	Status     string     `json:"status"`
	DurationMs int64      `json:"durationMs"`
	Errors     []RowError `json:"errors,omitempty"`
	CreatedBy  string     `json:"createdBy"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func jobView(m *Job, errs []RowError) JobView {
	v := JobView{
		ID: m.ID, Source: m.Source, FileName: m.FileName,
		TotalRows: m.TotalRows, OkRows: m.OkRows, FailRows: m.FailRows,
		Status: m.Status, DurationMs: m.DurationMs, CreatedBy: m.CreatedBy, CreatedAt: m.CreatedAt,
	}
	if len(errs) > 0 {
		v.Errors = errs
	}
	return v
}

func (s *Service) jobGet(ctx context.Context, id idgen.ID) (*Job, []RowError, error) {
	var m Job
	err := s.db.WithContext(ctx).First(&m, "id = ?", int64(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, response.NotFound("imports.job_not_found")
	}
	var errs []RowError
	if strings.TrimSpace(m.ErrorJSON) != "" {
		if err := json.Unmarshal([]byte(m.ErrorJSON), &errs); err != nil {
			errs = nil
		}
	}
	return &m, errs, nil
}

// JobQuery 是导入台账过滤条件。
type JobQuery struct {
	Source   string
	Status   string
	Page     int
	PageSize int
}

// ListJobs 分页列出导入台账。
func (s *Service) ListJobs(ctx context.Context, q JobQuery) ([]JobView, int64, error) {
	base := s.db.WithContext(ctx).Model(&Job{})
	if q.Source != "" {
		base = base.Where("source = ?", q.Source)
	}
	if q.Status != "" {
		base = base.Where("status = ?", q.Status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Job
	if err := base.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]JobView, len(rows))
	for i := range rows {
		var errs []RowError
		if strings.TrimSpace(rows[i].ErrorJSON) != "" {
			_ = json.Unmarshal([]byte(rows[i].ErrorJSON), &errs)
		}
		out[i] = jobView(&rows[i], errs)
	}
	return out, total, nil
}

// RunInput 是执行一次导入的入参(content 为上传文件原始字节)。
type RunInput struct {
	Source   string
	FileName string
	Content  []byte
}

// Run 执行一次导入。≤ SyncMaxRows 同步完成并返回 done;> SyncMaxRows 创建 pending 任务
// 并启动后台协程异步处理,立即返回(pending)——前端靠列表轮询感知状态迁移。
// 文件不可解析 / 缺必填列时 400,不创建台账。
func (s *Service) Run(ctx context.Context, in RunInput, actor string) (*JobView, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return nil, response.BadRequest("imports.actor_required")
	}
	src, err := getSource(strings.TrimSpace(in.Source))
	if err != nil {
		return nil, response.BadRequest("imports.source_unknown").Wrap(err)
	}
	if len(in.Content) == 0 {
		return nil, response.BadRequest("imports.file_required")
	}
	if len(in.Content) > MaxBytes {
		return nil, response.BadRequest("imports.file_too_large")
	}

	rows, perr := ParseUpload(src, in.Content, in.FileName)
	if perr != nil {
		if errors.Is(perr, ErrTooManyRows) {
			return nil, response.BadRequest("imports.too_many_rows_limit", AsyncMaxRows)
		}
		return nil, response.BadRequest("imports.parse_failed").Wrap(perr)
	}
	if len(rows) == 0 {
		return nil, response.BadRequest("imports.no_rows")
	}
	if len(rows) > AsyncMaxRows {
		return nil, response.BadRequest("imports.too_many_rows_limit", AsyncMaxRows)
	}

	// 表头必须覆盖全部必填列(缺列时整批失败,提示补列,而不是静默丢数据)。
	missing := []string{}
	for _, c := range src.Columns {
		if !c.Required {
			continue
		}
		found := false
		for _, row := range rows {
			if _, ok := row[c.Key]; ok {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, c.Label)
		}
	}
	if len(missing) > 0 {
		return nil, response.BadRequest("imports.missing_columns_detail", strings.Join(missing, "、"))
	}

	fileName := truncate(strings.TrimSpace(in.FileName), 200)
	if len(rows) <= SyncMaxRows {
		return s.runSync(ctx, src, rows, in.FileName, actor)
	}
	return s.runAsync(ctx, src, rows, fileName, actor)
}

// runSync 同步执行:请求内完成,返回 status=done。
func (s *Service) runSync(ctx context.Context, src Source, rows []map[string]string, fileName, actor string) (*JobView, error) {
	start := time.Now()
	okRows, failRows, errs := s.processRows(ctx, src, rows, 0, len(rows))

	raw, err := json.Marshal(errs)
	if err != nil {
		return nil, err
	}
	m := Job{
		Source: src.Name, FileName: truncate(fileName, 200), Lang: i18n.Lang(ctx),
		TotalRows: len(rows), OkRows: okRows, FailRows: failRows,
		Status: StatusDone, DurationMs: time.Since(start).Milliseconds(),
		ErrorJSON: string(raw), CreatedBy: truncate(actor, 64),
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	s.publishCompleted(ctx, &m, actor)
	v := jobView(&m, errs)
	return &v, nil
}

// runAsync 大文件异步执行:先落 pending 台账并立即返回,后台协程逐批处理并更新进度。
// 前端 useJobs 已有 refetchInterval,列表会自动感知 pending → running → done。
func (s *Service) runAsync(ctx context.Context, src Source, rows []map[string]string, fileName, actor string) (*JobView, error) {
	m := Job{
		Source: src.Name, FileName: truncate(fileName, 200),
		TotalRows: len(rows),
		Status:    StatusPending, CreatedBy: truncate(actor, 64), Lang: i18n.Lang(ctx),
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	v := jobView(&m, nil)
	go s.processAsync(src, &m, rows, actor)
	return &v, nil
}

// processAsync 在后台协程里按批次处理行,每批更新一次台账(前端可感知进度)。
// 不使用请求 ctx(请求可能已断开),改用可取消的后台 ctx;但租户必须从台账行还原,
// 否则租户表会在默认租户下读写(小文件走同步路径没这个问题,大文件会写错租户)。
// 取消：CancelJob 触发 ctx 取消，已落库的部分保留，台账标记 cancelled。
func (s *Service) processAsync(src Source, m *Job, rows []map[string]string, actor string) {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancels.Store(m.ID, cancel)
	defer func() {
		cancel()
		s.cancels.Delete(m.ID)
	}()
	if m.TenantID != 0 {
		ctx = tenant.With(ctx, m.TenantID)
	}
	if m.Lang != "" {
		ctx = i18n.WithLang(ctx, m.Lang)
	}
	const batch = 500
	start := time.Now()
	s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", int64(m.ID)).Update("status", StatusRunning)

	errs := make([]RowError, 0, 8)
	okRows, failRows := 0, 0
	for i, row := range rows {
		if ctx.Err() != nil {
			// 已取消：处理过的批次已经落库，如实收尾，不虚报 done。
			raw, _ := json.Marshal(errs)
			s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", int64(m.ID)).Updates(map[string]any{
				"ok_rows": okRows, "fail_rows": failRows, "status": StatusCancelled,
				"duration_ms": time.Since(start).Milliseconds(), "error_json": string(raw),
			})
			s.logWarn("imports: async job cancelled", "jobId", m.ID, "processed", okRows+failRows)
			return
		}
		bad := ""
		for _, c := range src.Columns {
			if c.Required && strings.TrimSpace(row[c.Key]) == "" {
				bad = c.Key
				break
			}
		}
		if bad != "" {
			failRows++
			if len(errs) < MaxErrors {
				errs = append(errs, RowError{Row: i + 1, Key: bad, Message: i18n.T(ctx, "imports.required_empty")})
			}
			continue
		}
		if err := safeCall(src.Handler, ctx, i+1, row); err != nil {
			failRows++
			if len(errs) < MaxErrors {
				errs = append(errs, RowError{Row: i + 1, Message: truncate(err.Error(), 200)})
			}
			continue
		}
		okRows++
		// 每处理 batch 行更新一次台账,让前端轮询能看到进度。
		if (i+1)%batch == 0 || i == len(rows)-1 {
			s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", int64(m.ID)).Updates(map[string]any{
				"ok_rows": okRows, "fail_rows": failRows,
			})
		}
	}

	raw, _ := json.Marshal(errs)
	updates := map[string]any{
		"ok_rows": okRows, "fail_rows": failRows, "status": StatusDone,
		"duration_ms": time.Since(start).Milliseconds(), "error_json": string(raw),
	}
	if err := s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", int64(m.ID)).Updates(updates).Error; err != nil {
		s.logWarn("imports: async update failed", "jobId", m.ID, "err", err.Error())
		return
	}
	finished := Job{
		Source: m.Source, FileName: m.FileName, TotalRows: len(rows),
		OkRows: okRows, FailRows: failRows, Status: StatusDone,
	}
	finished.ID = m.ID
	s.publishCompleted(ctx, &finished, actor)
}

// CancelJob 取消一个运行中/pending 的异步导入：已处理的部分保留，状态记为 cancelled。
// 任务不在注册表里（已完成、进程重启过或根本不存在）按 404 处理。
func (s *Service) CancelJob(ctx context.Context, id idgen.ID) error {
	m, _, err := s.jobGet(ctx, id)
	if err != nil {
		return err
	}
	if m.Status != StatusPending && m.Status != StatusRunning {
		return response.Conflict("imports.job_not_running")
	}
	v, ok := s.cancels.Load(id)
	if !ok {
		// 进程重启后注册表丢失，后台协程已不存在：直接把台账收尾为 cancelled。
		s.db.WithContext(ctx).Model(&Job{}).Where("id = ? AND status IN ?", int64(id), []string{StatusPending, StatusRunning}).
			Update("status", StatusCancelled)
		return nil
	}
	v.(context.CancelFunc)()
	return nil
}

// processRows 同步路径的逐行处理(小文件 ≤ SyncMaxRows)。
func (s *Service) processRows(ctx context.Context, src Source, rows []map[string]string, from, to int) (int, int, []RowError) {
	errs := make([]RowError, 0, 8)
	okRows, failRows := 0, 0
	for i := from; i < to && i < len(rows); i++ {
		row := rows[i]
		bad := ""
		for _, c := range src.Columns {
			if c.Required && strings.TrimSpace(row[c.Key]) == "" {
				bad = c.Key
				break
			}
		}
		if bad != "" {
			failRows++
			if len(errs) < MaxErrors {
				errs = append(errs, RowError{Row: i + 1, Key: bad, Message: i18n.T(ctx, "imports.required_empty")})
			}
			continue
		}
		if err := safeCall(src.Handler, ctx, i+1, row); err != nil {
			failRows++
			if len(errs) < MaxErrors {
				errs = append(errs, RowError{Row: i + 1, Message: truncate(err.Error(), 200)})
			}
			continue
		}
		okRows++
	}
	return okRows, failRows, errs
}

func (s *Service) logWarn(msg string, keysAndValues ...any) {
	if s.log != nil {
		s.log.Warn(msg, keysAndValues...)
	}
}

// safeCall 隔离 Handler 的 panic:单行 panic 只算该行失败,不拖垮整批。
func safeCall(fn func(context.Context, int, map[string]string) error, ctx context.Context, row int, r map[string]string) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("handler panicked: %v", rec)
		}
	}()
	return fn(ctx, row, r)
}

// GetJob 返回导入台账详情(含错误明细)。
func (s *Service) GetJob(ctx context.Context, id idgen.ID) (*JobView, error) {
	m, errs, err := s.jobGet(ctx, id)
	if err != nil {
		return nil, err
	}
	v := jobView(m, errs)
	return &v, nil
}

// DeleteJob 删除一条导入台账（不影响已入库的数据）。
// 排队中/执行中的任务不允许删：删掉会让后台协程没有目标行可写（先取消再删）。
func (s *Service) DeleteJob(ctx context.Context, id idgen.ID) error {
	m, _, err := s.jobGet(ctx, id)
	if err != nil {
		return err
	}
	if m.Status == StatusPending || m.Status == StatusRunning {
		return response.Conflict("imports.job_running")
	}
	return s.db.WithContext(ctx).Delete(&Job{}, "id = ?", int64(id)).Error
}

// DeleteJobs 批量删除导入台账：判断逐条照搬单条 DeleteJob——
// 排队中/执行中的任务不允许删（先取消再删），且这一条是「在跑」的就不能只把它挑出来跳过：
// 批次里有任何一条在跑，整批失败回滚，不做「删掉能删的」（部分成功会让调用方以为删的就是点名的几条）。
// 越权 / 不存在的 id 落在租户 scope 外（core/database 的 tenantPlugin 给查询与 DELETE 追加 tenant_id 条件，
// 与单条同一条路径），查不出来也就不计入实删数；一条都没命中报不存在。
// 台账只存行数据与 ErrorJSON 文本，上传内容当场解析完即丢、不落文件（见 handler.run），所以没有连带清理。
func (s *Service) DeleteJobs(ctx context.Context, ids []idgen.ID) (int64, error) {
	list := make([]int64, 0, len(ids))
	for _, id := range ids {
		list = append(list, int64(id))
	}
	var deleted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []Job
		if err := tx.Find(&rows, "id IN ?", list).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return response.NotFound("error.not_found")
		}
		found := make([]int64, 0, len(rows))
		for _, r := range rows {
			if r.Status == StatusPending || r.Status == StatusRunning {
				return response.Conflict("imports.job_running")
			}
			found = append(found, int64(r.ID))
		}
		res := tx.Where("id IN ?", found).Delete(&Job{})
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// GetJobErrors 取错误明细(供 errors.csv 生成)。
func (s *Service) GetJobErrors(ctx context.Context, id idgen.ID) ([]RowError, error) {
	_, errs, err := s.jobGet(ctx, id)
	return errs, err
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
