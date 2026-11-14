// Package kb 知识库插件（免费版）：文档向量化检索 + 在线客服 AI 自动回答的编排器。
// 建表由运行时执行 plugins/kb/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
package kb

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/plugin"
)

var _ plugin.Starter = (*module)(nil)

func init() {
	plugin.Register(plugin.Plugin{
		ID: "kb", Name: "知识库", Version: "0.1.0", Edition: plugin.Free,
		Module: &module{},
	})
}

// zapLog 把 *zap.Logger 适配成 Service 需要的最小日志面。
type zapLog struct{ l *zap.Logger }

func (z zapLog) Info(msg string, kv ...any)  { z.l.Sugar().Infow(msg, kv...) }
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
		m.svc = NewService(d.DB, core.AIGateway(d), zapLog{l})
	})
	return m.svc
}

func (*module) Name() string { return "kb" }

// Register 注册管理路由。AI 网关走 core.AIGateway 工厂（assistant 同款）：
// 与内置 /ai/* 接口同一套限流、租户策略、每日配额与审计。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d)}
	h.Register(r, d)
}

// Start 实现 plugin.Starter：订阅 im.message.created 驱动 AI 自动回答（worker 随插件 ctx 退出）。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	svc := m.service(d)
	b := newBot(ctx, d, svc)
	if b == nil {
		return nil
	}
	b.subscribe()
	go b.run()
	return nil
}
