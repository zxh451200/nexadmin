// Package pay 是 NexAdmin 支付资金中台插件（02-openapi-pay-v1.yaml 的实现）：
// 收单状态机、渠道抽象与回调验签、退款、预授权、对账、结算打款、plg_pay_outbox
// 资金事件出站箱、请求级 Idempotency-Key。业务系统通过 client.go 的进程内 API
// 对接，渠道以 Provider 接口挂载（P0 内置 mock）。遵守插件规范：只依赖 core/ 与
// pkg/，表前缀 plg_pay_，权限码前缀 pay:，接口路径 /api/v1/plugins/pay/...。
package pay

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/event"
	"nexadmin/core/job"
	"nexadmin/core/plugin"
	"nexadmin/core/tenant"
)

// 建表由运行时执行 plugins/pay/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	// 插件向事件目录声明自己的事件（openapi 的事件列表 / webhook 表单校验用）。
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "pay", Name: "支付中台", Version: "0.1.0", Edition: plugin.Pro,
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
		m.svc = NewService(d.DB, zapLog{l}).WithTimezone(d.Config.App.Timezone)
		SetDefault(m.svc)
	})
	return m.svc
}

func (*module) Name() string { return "pay" }

// Register 注册管理端与公开路由。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := m.service(d)
	job.Register(jobTypeOrderExport, svc.RunOrderExportJob) // 导出中心任务处理器
	h := &handler{svc: svc}

	g := r.Group("/plugins/pay")
	// 订单
	g.POST("/orders", idempotency(d.DB), core.Perm("pay:edit"), h.createOrder)
	g.GET("/orders", core.Perm("pay:list"), h.listOrders)
	// 静态段注册在 /orders/:no 之前（与 /orders/stats 同一套匹配约定）。
	g.POST("/orders/export-async", core.Perm("pay:list"), h.exportOrdersAsync)
	g.GET("/orders/stats", core.Perm("pay:list"), h.Stats)
	g.GET("/orders/by-biz", core.Perm("pay:list"), h.orderByBiz)
	g.GET("/orders/:no", core.Perm("pay:list"), h.getOrder)
	g.POST("/orders/:no/close", core.Perm("pay:edit"), h.closeOrder)
	g.POST("/orders/:no/sync", core.Perm("pay:edit"), h.syncOrder)
	// 退款
	g.POST("/refunds", idempotency(d.DB), core.Perm("pay:refund"), h.createRefund)
	g.GET("/refunds", core.Perm("pay:list"), h.listRefunds)
	g.GET("/refunds/:no", core.Perm("pay:list"), h.getRefund)
	g.POST("/refunds/:no/sync", core.Perm("pay:edit"), h.syncRefund)
	// 预授权
	g.POST("/authorizations", idempotency(d.DB), core.Perm("pay:auth"), h.createAuth)
	g.GET("/authorizations", core.Perm("pay:list"), h.listAuths)
	g.GET("/authorizations/:no", core.Perm("pay:list"), h.getAuth)
	g.POST("/authorizations/:no/capture", core.Perm("pay:auth"), h.captureAuth)
	g.POST("/authorizations/:no/void", core.Perm("pay:auth"), h.voidAuth)
	// 渠道
	g.GET("/channels", core.Perm("pay:list"), h.listChannels)
	g.POST("/channels", core.Perm("pay:channel"), h.upsertChannel)
	g.DELETE("/channels/:id", core.Perm("pay:channel"), h.deleteChannel)
	// 批量删除走集合路径 + ?ids=，权限复用单条删除的 pay:channel（与 /channels/:id 共存，同 media/cron 形状）。
	g.DELETE("/channels", core.Perm("pay:channel"), h.deleteChannelsBatch)
	g.POST("/channels/:code/probe", core.Perm("pay:channel"), h.probeChannel)
	g.GET("/providers", core.Perm("pay:list"), h.providers)
	g.GET("/capabilities", core.Perm("pay:list"), h.capabilities)
	// 回调审计
	g.GET("/callbacks", core.Perm("pay:list"), h.listCallbacks)
	// 人工重放已验签但落账失败的回调（高危独立动作 + 操作日志）
	g.POST("/callbacks/:id/replay", core.Perm("pay:edit"), d.OperLogMiddleware("pay", "callback_replay"), h.replayCallback)
	// 资金事件出站箱（投递台账与死信处置）
	g.GET("/outbox", core.Perm("pay:list"), h.listOutbox)
	// 静态段注册在 /outbox/:eventId 之前（与 /orders/stats 同一套匹配约定）。
	g.GET("/outbox/counts", core.Perm("pay:list"), h.outboxCounts)
	g.GET("/outbox/:eventId", core.Perm("pay:list"), h.getOutbox)
	g.POST("/outbox/:eventId/retry", core.Perm("pay:outbox"), h.retryOutbox)
	g.POST("/outbox/:eventId/discard", core.Perm("pay:outbox"), h.discardOutbox)
	// 对账
	g.GET("/reconcile/tasks", core.Perm("pay:settle"), h.listReconcileTasks)
	g.POST("/reconcile/tasks", core.Perm("pay:settle"), h.runReconcileTask)
	g.GET("/reconcile/tasks/:id/diffs", core.Perm("pay:settle"), h.listReconcileDiffs)
	g.GET("/reconcile/tasks/:id/bill", core.Perm("pay:settle"), h.downloadReconcileBill)
	g.POST("/reconcile/diffs/:id/resolve", core.Perm("pay:settle"), h.resolveReconcileDiff)
	// 结算与打款
	g.GET("/commission-rules", core.Perm("pay:settle"), h.listCommissionRules)
	g.POST("/commission-rules", core.Perm("pay:settle"), h.upsertCommissionRule)
	g.GET("/settlements", core.Perm("pay:settle"), h.listSettlements)
	g.POST("/settlements/generate", core.Perm("pay:settle"), h.generateSettlement)
	g.POST("/settlements/:id/confirm", core.Perm("pay:settle"), h.confirmSettlement)
	g.GET("/payouts", core.Perm("pay:settle"), h.listPayouts)
	g.POST("/payouts", core.Perm("pay:settle"), h.createPayout)
	g.POST("/payouts/:id/mark-paid", core.Perm("pay:settle"), h.markPayoutPaid)
	// 渠道代付（能力矩阵 payout=true 的渠道）与批次内逐笔结论
	g.POST("/payouts/:id/pay", core.Perm("pay:settle"), h.payPayout)
	g.GET("/payouts/:id/items", core.Perm("pay:settle"), h.listPayoutItems)

	// 公开路由：渠道网关回调（验签兜底）与 mock 收银台。
	p := d.Public.Group("/plugins/pay")
	p.POST("/notify/:channelCode", h.notify)
	p.GET("/mock/cashier", h.mockCashier)
	p.POST("/mock/cashier/confirm", h.mockConfirm)
}

// Start 实现 plugin.Starter：启动过期单清扫与 outbox 重投。渠道是租户级配置，
// 不在启动期代种——首次使用在管理端「渠道配置」新建一个 mock 渠道即可联调。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	s := m.service(d)
	go s.runSweeper(ctx)
	go s.runOutboxDispatcher(ctx)
	return nil
}

// runSweeper 每分钟清扫：过期 pending 支付单置 closed，过期预授权置 expired，
// 卡住不动的退款单主动向渠道补查一次（回调丢失时退款不能永远停在 refunding）。
func (s *Service) runSweeper(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now().UTC()
			if _, err := s.CloseExpired(tenant.Skip(ctx), now); err != nil {
				s.logWarn("pay: close expired failed", "err", err.Error())
			}
			if _, err := s.ExpireAuths(tenant.Skip(ctx), now); err != nil {
				s.logWarn("pay: expire auths failed", "err", err.Error())
			}
			// 退款补查：只处理 10 分钟前发起、仍未收到回执的退款单，单轮上限 50 笔；
			// 服务层另有节流，同一笔不会每分钟打一次渠道。
			summary, err := s.SyncStuckRefunds(tenant.Skip(ctx), now, refundSweepOlderThan, refundSweepLimit)
			if err != nil {
				s.logWarn("pay: sync stuck refunds failed", "err", err.Error())
			} else if summary.Checked > 0 {
				s.logInfo("pay: stuck refunds swept", "checked", summary.Checked,
					"flipped", summary.Flipped, "notFound", summary.NotFound,
					"failed", summary.Failed, "skipped", summary.Skipped)
			}
			// 预授权残留补查：停在 authorizing 的占位行（进程崩溃或渠道受理未回执）
			// 只能问渠道才知道钱到底冻没冻，本地猜不得。
			auths, err := s.ReconcileStuckAuths(tenant.Skip(ctx), now)
			if err != nil {
				s.logWarn("pay: reconcile stuck auths failed", "err", err.Error())
			} else if auths.Checked > 0 {
				s.logInfo("pay: stuck auths swept", "checked", auths.Checked,
					"adopted", auths.Adopted, "failed", auths.Failed,
					"unresolved", auths.Unresolved, "skipped", auths.Skipped)
			}
			// 代付终局补查：停在 initiated 的转账笔只有渠道能给终局。
			payouts, err := s.SweepStuckPayouts(tenant.Skip(ctx), now)
			if err != nil {
				s.logWarn("pay: sweep stuck payouts failed", "err", err.Error())
			} else if payouts.Batches > 0 {
				s.logInfo("pay: stuck payouts swept", "batches", payouts.Batches,
					"resolved", payouts.Resolved, "unresolved", payouts.Unresolved,
					"skipped", payouts.Skipped)
			}
		}
	}
}

// 退款补查清扫的口径：卡住多久算卡住、单轮最多查多少笔。
const (
	refundSweepOlderThan = 10 * time.Minute
	refundSweepLimit     = 50
)
