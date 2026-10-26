package market

import (
	"fmt"
	"sync"
	"time"
)

// Job 是一次市场操作（安装/移除/重建）的执行记录。进程内存态：
// 自重启后任务自然消失——前端约定“轮询 404 且之前是 running/restarting”按已完成处理。
type Job struct {
	ID    string    `json:"id"`
	Kind  string    `json:"kind"`  // install | remove | rebuild
	State string    `json:"state"` // running | restarting | done | failed
	Log   []string  `json:"log"`
	At    time.Time `json:"at"`
}

const maxJobLog = 400

// Runner 串行执行市场任务并保存最近的任务记录。
type Runner struct {
	mu   sync.Mutex
	seq  int
	jobs map[string]*Job
	next chan struct{} // 信号量：同一时刻只跑一个构建任务
}

// NewRunner 返回可用的任务执行器。
func NewRunner() *Runner {
	return &Runner{jobs: map[string]*Job{}, next: make(chan struct{}, 1)}
}

// Run 异步执行 fn；立即返回任务 id。fn 通过 Ctx 写日志、标记自重启。
// 构建任务必须串行（同时跑两个 go build 会互相踩产物），超出并行的排队等待。
func (r *Runner) Run(kind, id string, fn func(*Ctx) error) string {
	r.mu.Lock()
	r.seq++
	j := &Job{ID: fmt.Sprintf("%s-%d", kind, r.seq), Kind: kind, State: "running", At: time.Now()}
	if id != "" {
		j.ID += "-" + id
	}
	r.jobs[j.ID] = j
	r.mu.Unlock()

	logf := func(format string, a ...any) {
		line := time.Now().Format("15:04:05 ") + fmt.Sprintf(format, a...)
		r.mu.Lock()
		j.Log = append(j.Log, line)
		if len(j.Log) > maxJobLog {
			j.Log = j.Log[len(j.Log)-maxJobLog:]
		}
		r.mu.Unlock()
	}
	ctx := &Ctx{
		Log:            logf,
		markRestarting: func() { r.MarkRestarting(j.ID) },
	}

	go func() {
		r.next <- struct{}{}        // 排队拿构建位
		defer func() { <-r.next }() // 释放构建位
		defer func() {
			if p := recover(); p != nil {
				logf("任务异常: %v", p)
				r.finish(j.ID, "failed")
			}
		}()
		if err := fn(ctx); err != nil {
			logf("失败: %v", err)
			r.finish(j.ID, "failed")
			return
		}
		r.finish(j.ID, "done") // fn 内部若已推进到 restarting，这里不再覆盖
	}()
	return j.ID
}

// Ctx 是任务函数的执行上下文。
type Ctx struct {
	// Log 追加一行进度日志（自动带时间戳）。
	Log func(format string, a ...any)

	markRestarting func()
}

// MarkRestarting 声明任务即将自重启：进程随时会被停止，先把状态推进到 restarting，
// 前端看到后转入“等待服务恢复”分支。
func (c *Ctx) MarkRestarting() { c.markRestarting() }

// LogPlain 写一行不带格式化的日志（Rebuild/Restart 的 func(string) 回调用它对接）。
func (c *Ctx) LogPlain(s string) { c.Log("%s", s) }

// Get 返回任务快照。
func (r *Runner) Get(id string) (Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return Job{}, false
	}
	snap := *j
	snap.Log = append([]string(nil), j.Log...)
	return snap, true
}

// MarkRestarting 供任务 fn 在触发自重启前调用：状态推进后进程随时可能被 ctl 停掉。
func (r *Runner) MarkRestarting(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok && j.State == "running" {
		j.State = "restarting"
	}
}

func (r *Runner) finish(id, state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok && j.State == "running" {
		j.State = state
	}
}
