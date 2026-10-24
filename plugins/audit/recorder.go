package audit

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	queueSize     = 4096             // 缓冲上限；满了丢弃并告警，绝不阻塞业务事务
	batchSize     = 100              // 单批写入条数
	flushInterval = time.Second      // 不满一批时的最长等待
	flushTimeout  = 5 * time.Second  // 退出时最后一次冲刷的上限
	scopeRefresh  = 30 * time.Second // 从库刷新审计范围（多实例收敛）
)

// event 是回调里产生的一条待写入记录。
type event struct {
	tenant   int64
	table    string
	pk       string
	action   string
	opID     int64
	opName   string
	changes  map[string]Change
	occurred time.Time
}

// Recorder 持有范围、缓冲队列和异步写入循环。
type Recorder struct {
	db      *gorm.DB
	log     *zap.Logger
	scope   *Scope
	ch      chan event
	dropped atomic.Int64
}

func NewRecorder(db *gorm.DB, log *zap.Logger, scope *Scope) *Recorder {
	if log == nil {
		log = zap.NewNop()
	}
	return &Recorder{db: db, log: log, scope: scope, ch: make(chan event, queueSize)}
}

// tenantOf 取 context 里的租户，没有就用默认租户。
func tenantOf(ctx context.Context) int64 {
	if id, ok := tenant.From(ctx); ok {
		return int64(id)
	}
	return int64(tenant.DefaultID)
}

// enqueue 非阻塞入队；队列满时丢弃并计数（Run 循环里统一告警）。
func (r *Recorder) enqueue(ctx context.Context, table, pk, action string, ch map[string]Change) {
	if len(ch) == 0 {
		return
	}
	id, name := operatorFrom(ctx)
	e := event{tenant: tenantOf(ctx), table: table, pk: pk, action: action, opID: id, opName: name, changes: ch, occurred: time.Now().UTC()}
	select {
	case r.ch <- e:
	default:
		r.dropped.Add(1)
	}
}

// Dropped 返回自上次 Run 告警以来被丢弃的条数（测试与监控用）。
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Run 阻塞运行写入循环，直到 ctx 结束；结束前会尽量把队列里剩下的写完。
func (r *Recorder) Run(ctx context.Context) {
	batch := make([]event, 0, batchSize)
	tick := time.NewTicker(flushInterval)
	defer tick.Stop()
	flush := func(c context.Context) {
		if len(batch) > 0 {
			r.write(c, batch)
			batch = batch[:0]
		}
	}
	for {
		select {
		case <-ctx.Done():
			for drained := false; !drained; {
				select {
				case e := <-r.ch:
					batch = append(batch, e)
					if len(batch) >= batchSize {
						fc, cancel := context.WithTimeout(context.Background(), flushTimeout)
						flush(fc)
						cancel()
					}
				default:
					drained = true
				}
			}
			fc, cancel := context.WithTimeout(context.Background(), flushTimeout)
			flush(fc)
			cancel()
			return
		case e := <-r.ch:
			batch = append(batch, e)
			if len(batch) >= batchSize {
				flush(ctx)
			}
		case <-tick.C:
			flush(ctx)
			if n := r.dropped.Swap(0); n > 0 {
				r.log.Warn("audit: queue full, change events dropped", zap.Int64("dropped", n), zap.Int("queueSize", queueSize))
			}
		}
	}
}

// write 批量写入；失败只记日志（并计入丢弃），不影响业务。
func (r *Recorder) write(ctx context.Context, batch []event) {
	rows := make([]Log, 0, len(batch))
	for _, e := range batch {
		raw, err := json.Marshal(e.changes)
		if err != nil {
			r.dropped.Add(1)
			continue
		}
		l := Log{
			Table: e.table, RowPK: e.pk, Action: e.action,
			OperatorID: idgen.ID(e.opID), OperatorName: e.opName,
			Fields: FieldNames(e.changes), Changes: string(raw),
		}
		l.TenantID = idgen.ID(e.tenant)
		l.CreatedAt, l.UpdatedAt = e.occurred, e.occurred
		rows = append(rows, l)
	}
	if len(rows) == 0 {
		return
	}
	wctx := markSkip(tenant.Skip(ctx))
	if err := r.db.Session(&gorm.Session{NewDB: true}).WithContext(wctx).CreateInBatches(rows, batchSize).Error; err != nil {
		r.dropped.Add(int64(len(rows)))
		r.log.Error("audit: write change records failed", zap.Int("count", len(rows)), zap.Error(err))
	}
}

// LoadScope 从库读全部租户的审计范围到内存。
func (r *Recorder) LoadScope(ctx context.Context) error {
	var rows []Config
	if err := r.db.Session(&gorm.Session{NewDB: true}).WithContext(markSkip(tenant.Skip(ctx))).Find(&rows).Error; err != nil {
		return err
	}
	all := make(map[int64][]string, len(rows))
	for _, c := range rows {
		all[int64(c.TenantID)] = decodeTables(c.TableList)
	}
	r.scope.Replace(all)
	return nil
}

// RefreshLoop 周期刷新审计范围，直到 ctx 结束。
func (r *Recorder) RefreshLoop(ctx context.Context) {
	t := time.NewTicker(scopeRefresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := r.LoadScope(c); err != nil {
				r.log.Warn("audit: refresh scope failed", zap.Error(err))
			}
			cancel()
		}
	}
}

func decodeTables(s string) []string {
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
