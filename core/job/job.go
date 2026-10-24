// Package job 是通用异步任务运行器：任务类型注册表 + 数据库持久化队列 + 固定并发 worker。
//
// 业务模块新增一种异步任务只需要一行（通常写在模块的 Register 里）：
//
//	job.Register("report.export", func(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
//	    return job.WriteResult(ctx, j, "report.xlsx", contentType, func(w io.Writer) error { ...; report(50, "写入中"); return nil })
//	})
//
// 提交任务：job.Default().Submit(ctx, job.Submit{Type: "report.export", Owner: uid, Params: filters})。
// 多实例安全：任务由 UPDATE ... WHERE status='queued' 乐观抢占，影响行数为 1 的实例才执行。
package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 任务状态。
const (
	StatusQueued   = "queued"
	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"

	// StatusActive 只用于列表筛选：queued + running。
	StatusActive = "active"
)

// IsFinal 报告状态是否已结束（不会再被 worker 处理）。
func IsFinal(status string) bool {
	return status == StatusSuccess || status == StatusFailed || status == StatusCanceled
}

// 默认值（配置为 0 时使用）。
const (
	DefaultWorkers    = 2
	DefaultKeepDays   = 7
	DefaultMaxPerUser = 3
	maxMessageLen     = 200
	maxErrorLen       = 500
)

// 对外可识别的错误。
var (
	ErrNotFound        = errors.New("job: not found")
	ErrTooMany         = errors.New("job: too many active jobs for this user")
	ErrUnknownType     = errors.New("job: unknown job type")
	ErrBadState        = errors.New("job: operation not allowed in current state")
	ErrSensitiveParams = errors.New("job: params must not contain sensitive fields")
	ErrNoTenant        = errors.New("job: tenant required")
)

// Job 对应 sys_job。参数与结果只存业务筛选条件和对象键，不存密码、令牌、签名链接等敏感内容。
type Job struct {
	database.TenantModel
	OwnerID    idgen.ID   `gorm:"column:owner_id;not null" json:"ownerId"`
	Type       string     `gorm:"column:type;size:64;not null" json:"type"`
	Params     string     `gorm:"column:params;type:text" json:"-"`
	Status     string     `gorm:"column:status;size:16;not null" json:"status"`
	Progress   int        `gorm:"column:progress;not null" json:"progress"`
	Message    string     `gorm:"column:message;size:255" json:"message"`
	ResultFile string     `gorm:"column:result_file;size:500" json:"-"`
	ResultName string     `gorm:"column:result_name;size:255" json:"resultName"`
	ResultSize int64      `gorm:"column:result_size" json:"resultSize"`
	ResultMime string     `gorm:"column:result_mime;size:150" json:"-"`
	Error      string     `gorm:"column:error;size:500" json:"error"`
	Node       string     `gorm:"column:node;size:64" json:"-"`
	StartedAt  *time.Time `gorm:"column:started_at" json:"startedAt"`
	FinishedAt *time.Time `gorm:"column:finished_at" json:"finishedAt"`
}

func (Job) TableName() string { return "sys_job" }

// Decode 把 Params 解到 v。
func (j *Job) Decode(v any) error {
	if j.Params == "" {
		return nil
	}
	return json.Unmarshal([]byte(j.Params), v)
}

// Report 由处理器调用，汇报进度（0-100）与一句话说明。进度只增不减，由 worker 节流写库。
type Report func(progress int, msg string)

// ResultFile 是处理器产出的结果文件（对象存储里的键，不含签名链接）。
type ResultFile struct {
	Key         string
	Name        string // 下载文件名
	ContentType string
	Size        int64
}

// Handler 是任务类型处理器。ctx 已带租户，取消任务或停机时被取消；panic 会被 worker 捕获并标记失败。
type Handler func(ctx context.Context, j *Job, report Report) (ResultFile, error)

// UserError 是可以原样展示给用户的失败原因；其他错误只记日志，库里存通用提示。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// Errorf 构造 UserError。
func Errorf(format string, a ...any) error { return &UserError{Msg: fmt.Sprintf(format, a...)} }

// ---- 注册表 ----

var (
	regMu    sync.RWMutex
	registry = map[string]Handler{}
)

// Register 注册任务类型处理器（一行调用）。同一类型重复注册时后者覆盖前者，应用在同一进程内重建（测试、向导激活）也安全。
func Register(typ string, h Handler) {
	if typ == "" || h == nil {
		panic("job: Register needs a type and a handler")
	}
	regMu.Lock()
	registry[typ] = h
	regMu.Unlock()
}

func handlerFor(typ string) (Handler, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	h, ok := registry[typ]
	return h, ok
}

// Types 返回已注册的任务类型（升序不保证）。
func Types() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// unregister 仅测试使用。
func unregister(typ string) {
	regMu.Lock()
	delete(registry, typ)
	regMu.Unlock()
}

// ---- 默认管理器 ----

var def atomic.Pointer[Manager]

// Default 返回当前管理器；导出中心模块未启用时为 nil。
func Default() *Manager { return def.Load() }

// SetDefault 设置默认管理器。
func SetDefault(m *Manager) { def.Store(m) }

// ---- 敏感字段检查 ----

var sensitiveWords = []string{"password", "passwd", "secret", "token", "apikey", "api_key", "accesskey", "access_key", "privatekey", "private_key", "credential", "authorization", "signature"}

// checkParams 校验参数是合法 JSON 且任意层级的键名都不含敏感词。
func checkParams(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("job: params must be JSON: %w", err)
	}
	if hasSensitiveKey(v) {
		return ErrSensitiveParams
	}
	return nil
}

func hasSensitiveKey(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, sub := range x {
			lk := strings.ToLower(k)
			for _, w := range sensitiveWords {
				if strings.Contains(lk, w) {
					return true
				}
			}
			if hasSensitiveKey(sub) {
				return true
			}
		}
	case []any:
		for _, sub := range x {
			if hasSensitiveKey(sub) {
				return true
			}
		}
	}
	return false
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
