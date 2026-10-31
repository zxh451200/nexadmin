// Package workflow 是 NexAdmin 工作流插件：自研 BPMN 2.0 子集（开始 → 审批〔或签/会签〕
// → 排他网关 → 结束，加签 / 回退 / 撤回 / 超时自动升级 / BPMN XML 导出）。
// 引擎移植自 nexGrain 用户区实现；宿主系统通过 client.go 进程内 API 把业务单据挂上审批流，
// 审批结果经 workflow.* 事件订阅获得。只依赖 core/ 与 pkg/，表前缀 plg_workflow_，
// 权限码前缀 workflow:，接口路径 /api/v1/plugins/workflow/...。
package workflow

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/event"
	"nexadmin/core/plugin"
)

// 建表由运行时执行 plugins/workflow/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "workflow", Name: "工作流", Version: "0.2.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

// zapLog 把 *zap.Logger 适配成 Service 需要的最小日志面。
type zapLog struct{ l *zap.Logger }

func (z zapLog) Warn(msg string, kv ...any)  { z.l.Sugar().Warnw(msg, kv...) }
func (z zapLog) Error(msg string, kv ...any) { z.l.Sugar().Errorw(msg, kv...) }

type module struct {
	once sync.Once
	svc  *Service
}

func (m *module) service(d *core.Deps) *Service {
	m.once.Do(func() {
		l := d.Log
		if l == nil {
			l = zap.NewNop()
		}
		m.svc = NewService(d.DB, zapLog{l})
		SetDefault(m.svc)
	})
	return m.svc
}

func (*module) Name() string { return "workflow" }

// Register 注册路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d)}
	h.Register(r, d)
}

// Start 实现 plugin.Starter：每分钟清扫一次「审批节点配置了 TimeoutMinutes 且等待超时」
// 的运行中实例，自动升级到 EscalateTo。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	s := m.service(d)
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := s.SweepEscalate(ctx, time.Now().UTC()); err != nil {
					s.warn("workflow: sweep escalate failed", "err", err.Error())
				} else if n > 0 && d.Log != nil {
					d.Log.Sugar().Infow("workflow: auto escalated", "count", n)
				}
			}
		}
	}()
	return nil
}
