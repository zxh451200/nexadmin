// Package im 是 NexAdmin 在线客服/即时消息插件：会话（biz 三元组幂等）、消息
// （会话内单调序号增量拉取）、坐席未读游标、im.message.created 事件。
// 宿主系统用进程内事件订阅集成（P0 传输层为短轮询，WS/SSE 是 P1 升级点）。
// 遵守设计文档 §19：只依赖 core/ 与 pkg/，表前缀 plg_im_，权限码前缀 im:，
// 接口路径 /api/v1/plugins/im/...。
package im

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

// 建表由运行时执行 plugins/im/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "im", Name: "在线客服", Version: "0.1.0", Edition: plugin.Pro,
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

func (*module) Name() string { return "im" }

// Register 注册路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d), tickets: newStreamTicket(30 * time.Second)}
	h.Register(r, d)
}

// Start 实现 plugin.Starter（P0 无后台任务；SSE hub / 过期关闭在 P1 引入）。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	m.service(d)
	return nil
}
