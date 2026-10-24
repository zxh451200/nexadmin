package cron

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Env 是处理器执行时拿到的运行环境。
type Env struct {
	DB  *gorm.DB // 已带任务所属租户的 context，但原生 SQL 不经过租户 scope，必须自己带 tenant_id 条件
	Log *zap.Logger
	// TenantID / JobID 是任务所属租户与任务本身的 ID。
	TenantID int64
	JobID    int64
}

// HandlerFunc 执行一次任务。params 是任务上配置的参数（约定为 JSON 对象字符串，可为空）。
// 返回的 output 会写入执行日志（超长自动截断）；返回 error 表示本次失败，会按任务的重试次数重试。
// ctx 带超时，处理器必须遵守 ctx.Done()。
type HandlerFunc func(ctx context.Context, env *Env, params string) (output string, err error)

// HandlerSpec 描述一个内置处理器。
type HandlerSpec struct {
	Name        string                    // 任务类型标识，如 "http"，全局唯一
	Description string                    // 下拉里显示的说明（英文/中文均可，前端会按 plugin_cron.handlers.<name> 翻译）
	Validate    func(params string) error // 可选：保存任务时校验参数，不要做耗时/联网操作
	Run         HandlerFunc
}

// ---- 处理器注册表 ----
//
// 业务方如何追加自己的处理器（不需要改本插件的任何代码）：
//
//	// 业务项目里任意包，例如 internal/jobs/jobs.go
//	package jobs
//
//	import (
//	    "context"
//	    cron "nexadmin/plugins/cron"
//	)
//
//	func init() {
//	    cron.RegisterHandler(cron.HandlerSpec{
//	        Name:        "sync_orders",
//	        Description: "同步订单",
//	        Run: func(ctx context.Context, env *cron.Env, params string) (string, error) {
//	            // env.DB 访问数据库（原生 SQL 记得带 tenant_id），env.Log 写日志
//	            return "synced 42 orders", nil
//	        },
//	    })
//	}
//
// 然后在 cmd 的入口 import _ "yourproject/internal/jobs" 即可；
// 任务表单里的「任务类型」下拉会自动出现 sync_orders（前端文案可在 locales 里加 plugin_cron.handlers.sync_orders）。
// 注册必须在 init() 或启动早期完成；重名会 panic，以便在启动时就暴露问题。

var (
	regMu    sync.RWMutex
	registry = map[string]HandlerSpec{}
)

// RegisterHandler 注册一个处理器。
func RegisterHandler(h HandlerSpec) {
	if h.Name == "" || h.Run == nil {
		panic("cron: handler needs Name and Run")
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[h.Name]; dup {
		panic(fmt.Sprintf("cron: handler %q already registered", h.Name))
	}
	registry[h.Name] = h
}

func lookupHandler(name string) (HandlerSpec, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	h, ok := registry[name]
	return h, ok
}

// Handlers 返回已注册处理器（按名称排序）。
func Handlers() []HandlerSpec {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]HandlerSpec, 0, len(registry))
	for _, h := range registry {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
