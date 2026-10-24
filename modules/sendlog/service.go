package sendlog

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/notify"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	queueSize    = 2048
	workers      = 2
	writeTimeout = 5 * time.Second
	maxDeleteIDs = 500
)

// Service 既是 notify.Recorder（写入），也服务管理端查询。
//
// 写入是异步的：Record 只做脱敏、校验并把记录放进内存队列，立刻返回；后台 worker 落库。
// 队列满了就丢弃并记 zap 警告，落库失败只记 zap 警告 —— 发送日志任何时候都不能拖慢或拖垮发送主流程。
// 读取和删除每次都显式带 tenant_id，并且在 ctx 里没有租户时直接报错（不依赖 GORM 租户插件这一层保护）。
type Service struct {
	db  *gorm.DB
	loc *time.Location
	log *zap.Logger

	queue  chan entry
	stop   chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool

	dropped atomic.Int64
	insert  func(ctx context.Context, l *SendLog) error // 测试缝
}

type entry struct {
	ctx context.Context
	row SendLog
}

var _ notify.Recorder = (*Service)(nil)

// NewService 创建 Service 并启动写入 worker。tz 是应用时区（解释日期筛选），非法时按 UTC。
func NewService(db *gorm.DB, tz string, log *zap.Logger) *Service {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	if log == nil {
		log = zap.NewNop()
	}
	s := &Service{db: db, loc: loc, log: log, queue: make(chan entry, queueSize), stop: make(chan struct{})}
	s.insert = func(ctx context.Context, l *SendLog) error { return s.db.WithContext(ctx).Create(l).Error }
	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	return s
}

func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case e := <-s.queue:
			s.write(e)
		case <-s.stop:
			for { // 退出前把队列里剩下的写完
				select {
				case e := <-s.queue:
					s.write(e)
				default:
					return
				}
			}
		}
	}
}

// write 落一条记录。失败只记日志，绝不向上传播；panic 也被吞掉，worker 不会因为一条坏记录退出。
func (s *Service) write(e entry) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Warn("sendlog: write panicked, record dropped", zap.Any("panic", r))
		}
	}()
	ctx, cancel := context.WithTimeout(e.ctx, writeTimeout)
	defer cancel()
	if err := s.insert(ctx, &e.row); err != nil {
		s.log.Warn("sendlog: write failed, record dropped", zap.String("channel", e.row.Channel),
			zap.String("status", e.row.Status), zap.Error(err))
	}
}

// Close 停止接收新记录并把队列里的写完。之后到达的记录（服务关闭时 notify 还在排空自己的队列）改为同步写入，不丢。
func (s *Service) Close() {
	if s == nil || !s.closed.CompareAndSwap(false, true) {
		return
	}
	close(s.stop)
	s.wg.Wait()
}

// Record 实现 notify.Recorder：脱敏、限长、入队，立即返回。ctx 里必须有租户（notify 的任务 ctx 保留了请求的租户），
// 否则丢弃并记警告 —— 宁可不记，也不能记到别的租户下。
func (s *Service) Record(ctx context.Context, r notify.Record) {
	if s == nil {
		return
	}
	tid, ok := tenant.From(ctx)
	if !ok {
		s.log.Warn("sendlog: no tenant in context, record dropped", zap.String("channel", r.Channel))
		return
	}
	row, ok := buildRow(tid, r)
	if !ok {
		s.log.Warn("sendlog: invalid record dropped", zap.String("channel", r.Channel))
		return
	}
	// 只带租户的全新 ctx：不持有请求的任何东西，请求结束 / 取消不影响落库。
	e := entry{ctx: tenant.With(context.Background(), tid), row: row}
	if s.closed.Load() {
		s.write(e)
		return
	}
	select {
	case s.queue <- e:
	default:
		if n := s.dropped.Add(1); n == 1 || n%100 == 0 {
			s.log.Warn("sendlog: queue full, record dropped", zap.Int64("dropped_total", n))
		}
	}
}

// buildRow 把 notify.Record 变成可落库的行：收件人脱敏，标题和错误清理并截断。
func buildRow(tid idgen.ID, r notify.Record) (SendLog, bool) {
	if !oneOf(r.Channel, channels) {
		return SendLog{}, false
	}
	status := r.Status
	if !oneOf(status, statuses) {
		status = notify.StatusFail
	}
	scene := SanitizeText(r.Scene, MaxScene)
	if scene == "" {
		scene = notify.SceneNotice
	}
	ms := int(r.Duration / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	var row SendLog
	row.TenantID = tid
	row.Channel, row.Scene, row.Status, row.DurationMs = r.Channel, scene, status, ms
	row.Recipient = MaskRecipient(r.Channel, r.Recipient)
	row.Subject = SanitizeText(r.Subject, MaxSubject)
	row.Error = SanitizeError(r.Err)
	return row, true
}

func likeArg(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

// base 返回限定当前租户的查询。ctx 里没有租户时报 database.ErrNoTenant，不会发出任何 SQL。
func (s *Service) base(ctx context.Context) (*gorm.DB, error) {
	tid, ok := tenant.From(ctx)
	if !ok {
		return nil, database.ErrNoTenant
	}
	return s.db.WithContext(ctx).Model(&SendLog{}).Where("tenant_id = ?", int64(tid)), nil
}

// List 分页，最新的在前。
func (s *Service) List(ctx context.Context, q Query) ([]View, int64, error) {
	tx, err := s.base(ctx)
	if err != nil {
		return nil, 0, err
	}
	if q.Channel != "" {
		tx = tx.Where("channel = ?", q.Channel)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if kw := strings.TrimSpace(q.Recipient); kw != "" {
		tx = tx.Where("recipient LIKE ?", likeArg(kw))
	}
	if t, err := time.ParseInLocation("2006-01-02", q.StartDate, s.loc); err == nil {
		tx = tx.Where("created_at >= ?", t.UTC())
	}
	if t, err := time.ParseInLocation("2006-01-02", q.EndDate, s.loc); err == nil {
		tx = tx.Where("created_at < ?", t.AddDate(0, 0, 1).UTC())
	}
	tx = tx.Session(&gorm.Session{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []SendLog
	if err := tx.Order("created_at DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]View, len(rows))
	for i, r := range rows {
		out[i] = toView(r)
	}
	return out, total, nil
}

// Get 取一条；不存在（或属于别的租户）返回 gorm.ErrRecordNotFound。
func (s *Service) Get(ctx context.Context, id idgen.ID) (*View, error) {
	tx, err := s.base(ctx)
	if err != nil {
		return nil, err
	}
	var row SendLog
	if err := tx.Where("id = ?", int64(id)).Take(&row).Error; err != nil {
		return nil, err
	}
	v := toView(row)
	return &v, nil
}

// Delete 按 ID 批量删除（只限当前租户），返回删除条数。
func (s *Service) Delete(ctx context.Context, ids []idgen.ID) (int64, error) {
	tx, err := s.base(ctx)
	if err != nil {
		return 0, err
	}
	raw := make([]int64, len(ids))
	for i, id := range ids {
		raw[i] = int64(id)
	}
	res := tx.Where("id IN ?", raw).Delete(&SendLog{})
	return res.RowsAffected, res.Error
}

// Clean 清理 days 天前的记录（只限当前租户），返回删除条数。days 必须 >= 1。
func (s *Service) Clean(ctx context.Context, days int) (int64, error) {
	tx, err := s.base(ctx)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	res := tx.Where("created_at < ?", cutoff).Delete(&SendLog{})
	return res.RowsAffected, res.Error
}
