package log

// 操作日志的异步缓冲写入：中间件每请求只做一次非阻塞入队，由单条冲刷协程批量落库，
// 队列满时丢弃并计数告警。口径对齐 plugins/audit/recorder.go（有界队列 + 冲刷协程 + 满时丢弃计数），
// 不另立一套语义；容量与批量按本模块的记录体积另算，见下面常量注释。

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/tenant"
)

const (
	// operQueueSize 队列容量：按生产 DB 连接池上限（defaultConfig 的 maxOpenConns=50）取值——
	// 50 连接 × 每批 50 条 = 2500，取整 2048 已够一个冲刷周期的积压，再大只会把过载推给数据库连接。
	// audit 用 4096：那边每条模型语句都产生记录，这边只有挂了审计中间件的写接口才产生，量级更低。
	operQueueSize = 2048
	// operBatchSize 比 audit 的 100 小：操作日志带 params/response 两个最大 8KB 文本列，
	// 单条体积远大于审计的字段级变更，批次小才不撑大单条 INSERT 语句。
	operBatchSize    = 50
	operFlushEvery   = 500 * time.Millisecond // 不满一批时的最长等待
	operFlushTimeout = 5 * time.Second        // 单次落库的 ctx 上限
	operDrainTimeout = 5 * time.Second        // 优雅关闭排空上限
)

type operTask struct {
	ctx context.Context
	log *OperLog
}

// operRecorder 持有操作日志的缓冲队列与冲刷协程；入队永不阻塞请求。
type operRecorder struct {
	db        *gorm.DB
	log       *zap.Logger
	ch        chan operTask
	wg        sync.WaitGroup // 在途落库：Close 用它确认排空
	closeNow  chan struct{}  // 立即中止冲刷循环（测试用它验证「停止后仍能排空」）
	done      chan struct{}  // Run 退出后关闭，供关闭流程确认冲刷协程真的结束
	closed    atomic.Bool
	closeOnce sync.Once
	dropped   atomic.Int64
	inflight  atomic.Int64
	warned    atomic.Bool // 首次丢弃立刻告警，之后按冲刷周期汇总
}

func newOperRecorder(db *gorm.DB, log *zap.Logger) *operRecorder {
	if log == nil {
		log = zap.NewNop()
	}
	return &operRecorder{db: db, log: log, ch: make(chan operTask, operQueueSize),
		closeNow: make(chan struct{}), done: make(chan struct{})}
}

// enqueue 非阻塞入队；已关闭或队满都只丢弃并计数，绝不阻塞、不 panic。
func (r *operRecorder) enqueue(ctx context.Context, l *OperLog) {
	if r.closed.Load() {
		r.dropped.Add(1)
		return
	}
	select {
	case r.ch <- operTask{ctx: ctx, log: l}:
	default:
		r.dropped.Add(1)
		r.warnDrop()
	}
}

// Dropped 返回当前窗口内被丢弃的条数（测试与监控用）。
func (r *operRecorder) Dropped() int64 { return r.dropped.Load() }

// Inflight 返回已提交但尚未落库的条数。
func (r *operRecorder) Inflight() int64 { return r.inflight.Load() }

// Done 在冲刷协程退出（含排空完成）后关闭。
func (r *operRecorder) Done() <-chan struct{} { return r.done }

// warnDrop 首次丢弃立刻告警一次，后续丢弃由 reportDrops 按周期汇总，避免刷屏。
func (r *operRecorder) warnDrop() {
	if r.warned.CompareAndSwap(false, true) {
		r.log.Warn("log: oper log queue full, records dropped",
			zap.Int64("dropped", r.dropped.Load()), zap.Int("queueSize", operQueueSize))
	}
}

// Run 持续冲刷入队任务，直到 ctx 结束或 Close 被调用；退出前把在途与队列剩余全部落库。
func (r *operRecorder) Run(ctx context.Context) {
	defer close(r.done)
	batch := make([]operTask, 0, operBatchSize)
	tick := time.NewTicker(operFlushEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			r.drain(batch)
			return
		case <-r.closeNow:
			r.drain(batch)
			return
		case t := <-r.ch:
			batch = append(batch, t)
			// 队列已积压到一批就立刻成批落库，不等冲刷周期。
		collect:
			for len(batch) < operBatchSize {
				select {
				case t2 := <-r.ch:
					batch = append(batch, t2)
				default:
					break collect
				}
			}
			r.writeBatch(batch)
			batch = make([]operTask, 0, operBatchSize)
			r.reportDrops()
		case <-tick.C:
			if len(batch) == 0 {
				r.reportDrops()
				continue
			}
			r.writeBatch(batch)
			batch = make([]operTask, 0, operBatchSize)
			r.reportDrops()
		}
	}
}

// drain 退出前把在途任务与队列剩余全部落库。
func (r *operRecorder) drain(batch []operTask) {
	r.writeBatch(batch)
	for len(r.ch) > 0 {
		tasks := make([]operTask, 0, operBatchSize)
	poll:
		for len(tasks) < operBatchSize {
			select {
			case t := <-r.ch:
				tasks = append(tasks, t)
			default:
				break poll
			}
		}
		if len(tasks) == 0 {
			break
		}
		r.writeBatch(tasks)
	}
	r.waitInflight()
}

func (r *operRecorder) waitInflight() {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(operDrainTimeout):
		r.log.Warn("log: oper log drain timed out", zap.Int64("inflight", r.inflight.Load()))
	}
}

// writeBatch 异步批量落库；失败只记日志并计入丢弃，不影响业务响应（与旧直写口径一致）。
// 一批可能跨多个租户，所以 ctx 走 tenant.Skip：每条记录的租户由它自带的 TenantID 决定
// （TenantModel.BeforeCreate 见到非 0 值就不再用 ctx 覆盖），落库超时也不会取消已入队日志。
func (r *operRecorder) writeBatch(batch []operTask) {
	if len(batch) == 0 {
		return
	}
	rows := make([]OperLog, 0, len(batch))
	base := context.Background()
	for i, t := range batch {
		if t.log == nil {
			continue
		}
		if i == 0 && t.ctx != nil {
			base = t.ctx
		}
		rows = append(rows, *t.log)
	}
	if len(rows) == 0 {
		return
	}
	wctx, cancel := context.WithTimeout(tenant.Skip(context.WithoutCancel(base)), operFlushTimeout)
	r.wg.Add(1)
	r.inflight.Add(int64(len(rows)))
	go func() {
		defer cancel()
		defer r.wg.Done()
		defer r.inflight.Add(-int64(len(rows)))
		if err := r.db.Session(&gorm.Session{NewDB: true}).WithContext(wctx).Create(&rows).Error; err != nil {
			r.dropped.Add(int64(len(rows)))
			r.log.Warn("log: write oper log failed", zap.Int("count", len(rows)), zap.Error(err))
		}
	}()
}

// reportDrops 汇总冲刷窗口内的丢弃并清零计数（与 audit 的 dropped.Swap 同口径）。
func (r *operRecorder) reportDrops() {
	if n := r.dropped.Swap(0); n > 0 {
		r.log.Warn("log: oper log queue full, records dropped", zap.Int64("dropped", n), zap.Int("queueSize", operQueueSize))
		r.warned.Store(true)
	} else {
		r.warned.Store(false)
	}
}

// Close 幂等地停止冲刷并等 Run 排空，返回超时后仍未落库的条数供调用方告警。
// 排空由 Run 的 drain 负责：Close 不能自己从队列取任务，否则会和 drain 抢条目、把日志“吃掉”。
func (r *operRecorder) Close(timeout time.Duration) (remaining int) {
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		close(r.closeNow)
	})
	select {
	case <-r.done:
	case <-time.After(timeout):
	}
	return len(r.ch) + int(r.inflight.Load())
}

// —— 与 Service / 模块注册的接线 ——

// startOperQueue 建队并启动冲刷协程（非阻塞），由模块注册时调用；重复调用无害。
func (s *Service) startOperQueue() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec != nil {
		return // 已启动（或已排空停止）都不再重建：重复调用无害
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := newOperRecorder(s.db, s.log)
	s.rec, s.cancelRun = r, cancel
	go r.Run(ctx)
}

// stopOperQueue 停止队列并等冲刷协程排空在途日志，返回仍未落库的条数；未启动时返回 0。幂等。
// 优雅关闭时它排在 core 连接池关闭之前（OnClose 后注册者先执行），所以排空仍写得进库。
func (s *Service) stopOperQueue() (remaining int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec == nil {
		return 0
	}
	// 保留 rec：关闭后的入队由它的 closed 分支计数丢弃，既不阻塞也不回落成「直写正在关闭的库」。
	remaining = s.rec.Close(operDrainTimeout)
	if s.cancelRun != nil {
		s.cancelRun()
		s.cancelRun = nil
	}
	return remaining
}

// operQueue 返回当前队列（未启动时为 nil），供同包测试与监控读取计数。
func (s *Service) operQueue() *operRecorder {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec
}

// enqueue 是中间件的写入入口：队列已启动就走队列（非阻塞，关闭后只计数丢弃），
// 从未启动过队列的手工装配 Service（单测里 NewService 直接用）沿用旧的每请求直写，行为不变。
func (s *Service) enqueue(ctx context.Context, l *OperLog) {
	s.mu.Lock()
	r := s.rec
	s.mu.Unlock()
	if r != nil {
		r.enqueue(ctx, l)
		return
	}
	go func() {
		if err := s.WriteOper(ctx, l); err != nil && s.log != nil {
			s.log.Warn("log: write oper log failed", zap.Error(err))
		}
	}()
}
