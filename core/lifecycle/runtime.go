package lifecycle

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"nexadmin/core/cluster"
	"nexadmin/core/event"
	"nexadmin/core/job"
)

// Runtime 持有插件一轮 Start 的进程内扩展；ctx 结束或 Close 后不再接受新工作。
type Runtime struct {
	ctx    context.Context
	owner  string
	mu     sync.Mutex
	closed atomic.Bool
	stops  []func()
}

func NewRuntime(ctx context.Context, owner string) *Runtime {
	return &Runtime{ctx: ctx, owner: owner}
}

func (r *Runtime) active() bool {
	return r != nil && r.ctx.Err() == nil && !r.closed.Load()
}

func (r *Runtime) add(stop func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed.Load() {
		stop()
		return
	}
	r.stops = append(r.stops, stop)
}

// Subscribe 订阅进程内事件，处理函数随插件运行期结束而注销。
func (r *Runtime) Subscribe(s event.Subscriber) {
	if !r.active() {
		return
	}
	r.add(event.Default().Subscribe(event.SubscriberFunc(func(e event.Event) {
		if r.active() {
			s.Handle(e)
		}
	})))
}

// SubscribeCluster 订阅跨实例事件，处理函数随插件运行期结束而注销。
func (r *Runtime) SubscribeCluster(bus *cluster.Bus, kind string, fn func(json.RawMessage), reset func()) {
	if !r.active() {
		return
	}
	r.add(bus.On(kind, func(raw json.RawMessage) {
		if r.active() {
			fn(raw)
		}
	}, func() {
		if r.active() && reset != nil {
			reset()
		}
	}))
}

// RegisterJob 注册插件任务处理器，并把插件取消信号接入 worker 执行上下文。
func (r *Runtime) RegisterJob(typ string, fn job.Handler) {
	if !r.active() {
		return
	}
	job.RegisterOwned(r.owner, typ, func(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
		if !r.active() {
			return job.ResultFile{}, context.Canceled
		}
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(r.ctx, cancel)
		defer stop()
		defer cancel()
		return fn(ctx, j, report)
	})
	r.add(func() { job.UnregisterOwner(r.owner) })
}

// Close 幂等撤销本轮全部注册；不等待已经进入处理器的任务，任务由上面的 ctx 联动取消。
func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed.Swap(true) {
		r.mu.Unlock()
		return
	}
	stops := r.stops
	r.stops = nil
	r.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}
