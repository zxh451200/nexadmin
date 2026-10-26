// Package payment 是 NexAdmin 支付中心插件：收单核心（支付单状态机、渠道抽象、
// 回调验签、退款、payment.* 事件）。业务系统通过 client.go 的进程内 API 对接，
// 渠道以 Provider 接口挂载（P0 内置 mock）。遵守设计文档 §19：只依赖 core/ 与 pkg/，
// 表前缀 plg_payment_，权限码前缀 payment:，接口路径 /api/v1/plugins/payment/...。
package payment

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

// 建表由运行时执行 plugins/payment/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	// 插件向事件目录声明自己的事件（openapi 的事件列表 / webhook 表单校验用）。
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "payment", Name: "支付中心", Version: "0.1.0", Edition: plugin.Pro,
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
		m.svc = NewService(d.DB, zapLog{l})
		SetDefault(m.svc)
	})
	return m.svc
}

func (*module) Name() string { return "payment" }

// Register 注册管理端与公开路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{svc: m.service(d)}

	g := r.Group("/plugins/payment")
	g.GET("/orders", core.Perm("payment:list"), h.listOrders)
	g.GET("/orders/:no", core.Perm("payment:list"), h.getOrder)
	g.POST("/orders/:no/close", core.Perm("payment:edit"), h.closeOrder)
	g.GET("/channels", core.Perm("payment:list"), h.listChannels)
	g.POST("/channels", core.Perm("payment:channel"), h.createChannel)
	g.PUT("/channels/:id", core.Perm("payment:channel"), h.updateChannel)
	g.DELETE("/channels/:id", core.Perm("payment:channel"), h.deleteChannel)
	g.GET("/providers", core.Perm("payment:list"), h.providers)
	g.GET("/callbacks", core.Perm("payment:list"), h.listCallbacks)
	g.GET("/refunds", core.Perm("payment:list"), h.listRefunds)
	g.POST("/refunds", core.Perm("payment:refund"), h.createRefund)

	// 公开路由：渠道网关回调（验签兜底）与 mock 收银台。
	p := d.Public.Group("/plugins/payment")
	p.POST("/notify/:channel", h.notify)
	p.GET("/mock/cashier", h.mockCashier)
	p.POST("/mock/cashier/confirm", h.mockConfirm)
}

// Start 实现 plugin.Starter：确保默认 mock 渠道存在，并启动过期支付单清扫。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	s := m.service(d)
	if err := ensureDefaultChannel(ctx, s); err != nil && d.Log != nil {
		d.Log.Sugar().Warnw("payment: ensure default channel failed", "err", err.Error())
	}
	go s.runSweeper(ctx)
	return nil
}

// ensureDefaultChannel 首次启动时种一个启用的 mock 渠道（密钥随机生成，整体加密落库）。
func ensureDefaultChannel(ctx context.Context, s *Service) error {
	rows, err := s.ListChannels(ctx)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		return nil
	}
	_, err = s.SaveChannel(ctx, 0, "mock", "模拟渠道", true, 0, map[string]string{
		"secret": randomToken(32),
	})
	return err
}

// runSweeper 每分钟把过期 pending 支付单置为 closed。
func (s *Service) runSweeper(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.CloseExpired(ctx, time.Now().UTC()); err != nil {
				s.logWarn("payment: close expired failed", "err", err.Error())
			}
		}
	}
}
