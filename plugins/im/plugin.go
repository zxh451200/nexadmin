// Package im 是 NexAdmin 在线客服/即时消息插件：会话（biz 三元组幂等）、消息
// （会话内单调序号增量拉取）、坐席未读与访客已读、认领、满意度评价、快捷回复、
// 结束闭环（系统消息 + 双通道推送）、空闲自动关闭与 im.message.created 事件。
// 实时通道：坐席端 WebSocket 优先、SSE 兜底；访客端 SSE + 轮询兜底。
// 遵守设计文档 §19：只依赖 core/ 与 pkg/，表前缀 plg_im_，权限码前缀 im:，
// 接口路径 /api/v1/plugins/im/...。
package im

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/event"
	"nexadmin/core/plugin"
	"nexadmin/core/storage"
)

// autoCloseIdle 是空闲会话的自动关闭阈值；autoCloseEvery 是巡检间隔。
const (
	autoCloseIdle  = 24 * time.Hour
	autoCloseEvery = 10 * time.Minute
	// retentionEvery 是保留期清理的巡检间隔：保留天数按天计，一小时一次足够。
	retentionEvery = time.Hour
	// retentionBatch 是单轮最多清理的会话数，避免一次扫太多拖慢库。
	retentionBatch = 200
)

// 建表由运行时执行 plugins/im/migrations/<引擎>/*.sql（安装时 CLI 拷贝），插件自己不做迁移。
var _ plugin.Starter = (*module)(nil)

func init() {
	event.Register(CatalogInfos()...)
	plugin.Register(plugin.Plugin{
		ID: "im", Name: "在线客服", Version: "0.6.0", Edition: plugin.Pro,
		Module: &module{},
	})
}

// zapLog 把 *zap.Logger 适配成 Service 需要的最小日志面。
type zapLog struct{ l *zap.Logger }

func (z zapLog) Info(msg string, kv ...any)  { z.l.Sugar().Infow(msg, kv...) }
func (z zapLog) Warn(msg string, kv ...any)  { z.l.Sugar().Warnw(msg, kv...) }
func (z zapLog) Error(msg string, kv ...any) { z.l.Sugar().Errorw(msg, kv...) }

type module struct {
	once    sync.Once
	sub     sync.Once
	wsGuard sync.Once
	svc     *Service
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

// Register 注册路由，并装配附件存储（local / S3 跟随 config.storage）。
func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := m.service(d)
	svc.SetBus(d.Bus)
	svc.SetCache(d.Cache)
	if d.Config != nil {
		st, err := storage.New(d.Config.Storage, filepath.Join(d.Config.DataDir, "uploads", "im"))
		if err != nil {
			if d.Log != nil {
				d.Log.Warn("im: attachment storage init failed", zap.Error(err))
			}
		} else {
			svc.SetStorage(st)
		}
	}
	h := &handler{svc: svc, tickets: newStreamTicket(30 * time.Second), cache: d.Cache, store: svc.Storage()}
	m.wsGuard.Do(func() { registerWSTopicGuard(svc, d.Auth) })
	h.Register(r, d)
}

// Start 实现 plugin.Starter：启动离线提醒（订阅 im.message.created，worker 随插件 ctx 退出）
// 与空闲会话自动关闭巡检。
func (m *module) Start(ctx context.Context, d *core.Deps) error {
	svc := m.service(d)
	m.sub.Do(func() {
		d.Bus.On(busKind, svc.Deliver, nil)
		go autoCloseLoop(ctx, svc)
		go retentionLoop(ctx, svc)
		n := newNotifier(ctx, d)
		if n == nil {
			return
		}
		event.Subscribe(event.SubscriberFunc(n.handle))
		go n.run()
	})
	return nil
}

// retentionLoop 按设置里的保留天数清理已结束会话（retentionDays=0 = 永久保留）。
// 每次运行都记一条「检查了多少、删了多少」的日志：只记删除数时，0 与「没轮到」无法区分。
func retentionLoop(ctx context.Context, svc *Service) {
	purge := func() {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		days := svc.Settings(cctx).RetentionDays
		if days <= 0 {
			return
		}
		cutoff := time.Now().AddDate(0, 0, -days)
		scanned, deleted, err := svc.PurgeConversations(cctx, cutoff, retentionBatch)
		if svc.log == nil {
			return
		}
		if err != nil {
			svc.log.Warn("im: purge conversations failed", "err", err.Error())
			return
		}
		svc.log.Info("im: purge conversations", "retentionDays", days, "scanned", scanned, "deleted", deleted)
	}
	// 启动后稍等再跑第一轮，避免与建库/初始化抢资源。
	first := time.NewTimer(5 * time.Minute)
	defer first.Stop()
	select {
	case <-ctx.Done():
		return
	case <-first.C:
		purge()
	}
	t := time.NewTicker(retentionEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			purge()
		}
	}
}

// autoCloseLoop 定期关闭超过 autoCloseIdle 没有新消息的会话；随插件 ctx 退出。
func autoCloseLoop(ctx context.Context, svc *Service) {
	sweep := func() {
		cctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		if n, err := svc.AutoClose(cctx, time.Now().Add(-autoCloseIdle), 50); err != nil {
			if svc.log != nil {
				svc.log.Warn("im: auto close sweep failed", "err", err.Error())
			}
		} else if n > 0 && svc.log != nil {
			svc.log.Warn("im: auto closed idle conversations", "count", n)
		}
		if n, err := svc.PurgeUnusedAttachments(cctx, time.Now().Add(-autoCloseIdle), 100); err == nil && n > 0 && svc.log != nil {
			svc.log.Warn("im: purged unused attachments", "count", n)
		}
	}
	sweep()
	t := time.NewTicker(autoCloseEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}
