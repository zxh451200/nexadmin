// Package ticket 是 NexAdmin 工单插件：工单生命周期（open → processing → resolved →
// closed，报告人再回复自动重开）、优先级、认领/改派、坐席首次响应时间、ticket.* 事件。
// 宿主系统用进程内事件订阅集成；遵守设计文档 §19：只依赖 core/ 与 pkg/，
// 表前缀 plg_ticket_，权限码前缀 ticket:，接口路径 /api/v1/plugins/ticket/...。
package ticket

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

// 建表由运行时执行 plugins/ticket/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "ticket", Name: "工单中心", Version: "0.1.0", Edition: plugin.Pro,
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
		m.svc = NewService(d.DB, zapLog{l}).WithTimezone(d.Config.App.Timezone)
		SetDefault(m.svc)
	})
	return m.svc
}

func (*module) Name() string { return "ticket" }

// Register 注册路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d)}
	h.Register(r, d)
}

// Start 实现 plugin.Starter：启动 SLA 清扫（每分钟把过期未响应工单标记 overdue）。
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
				if n, err := s.MarkOverdue(ctx, time.Now().UTC()); err != nil {
					s.logWarn("ticket: mark overdue failed", "err", err.Error())
				} else if n > 0 && d.Log != nil {
					d.Log.Sugar().Infow("ticket: marked overdue", "count", n)
				}
			}
		}
	}()
	return nil
}

func (s *Service) logWarn(msg string, keysAndValues ...any) {
	if s.log != nil {
		s.log.Warn(msg, keysAndValues...)
	}
}
