package message

// 站内消息模块：消息中心、分类、已读未读、铃铛红点、全部已读、SSE 实时推送。
// 一条消息一个收件人；用户只能读、改、删自己的消息，所有查询强制带当前用户条件。

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/notify"
	"nexadmin/core/tenant"
	"nexadmin/modules/internal/session"
	"nexadmin/modules/notice"
	"nexadmin/pkg/idgen"
)

func init() {
	core.RegisterBuiltin(module{})
	core.SetMessageCenterFactory(func(d *core.Deps) core.MessageCenter {
		return messageCenter{svc: NewService(d.DB, notice.NewService(d.DB))}
	})
}

// messageCenter 把内置消息服务适配成 core.MessageCenter，插件无需 import 本模块。
type messageCenter struct{ svc *Service }

func (c messageCenter) Unread(ctx context.Context, uid idgen.ID) (core.UnreadCount, error) {
	n, err := c.svc.Unread(ctx, uid)
	return core.UnreadCount{Messages: n.Messages, Notices: n.Notices}, err
}

type module struct{}

func (module) Name() string { return "message" }

// Seed 种入演示消息（setup.Seeder）。
func (module) Seed(db *gorm.DB) error { return seed(db) }

// Register 注册路由。消息只读写“自己的”数据，登录即可，不挂权限点；
// message:list 仅登记给菜单/角色模块展示（消息中心页面入口用）。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	auth.RegisterPerm("message:list")
	svc := NewService(d.DB, notice.NewService(d.DB))
	// 站内渠道：向 core/notify 注册（notify 不 import message）；本模块被停用时不会注册，站内发送即为空操作。
	d.Notify.Register(notify.ChannelInApp, notify.ChannelFunc(func(ctx context.Context, _ string, m notify.Msg, to []notify.Recipient) error {
		ids := make([]idgen.ID, 0, len(to))
		for _, r := range to {
			ids = append(ids, r.ID)
		}
		return svc.Send(ctx, ids, m.Category, m.Title, m.Content, m.Link)
	}))
	h := NewHandler(svc)
	defaultHub.attach(d.Bus) // 多实例：消息变化经 core/cache 发布订阅广播到其它实例的 SSE 连接（Bus 为 nil 时只推本实例）
	g := r.Group("", session.Middleware(session.Shared(d.Cache, d.Config.Auth.RefreshTTL)))

	g.GET("/messages", h.List)
	g.GET("/messages/unread-count", h.UnreadCount)
	g.GET("/messages/stream", h.Stream)
	g.PUT("/messages/read-all", h.ReadAll)
	g.PUT("/messages/:id/read", h.MarkRead)
	d.APIDoc.RegisterWriteTool("message_mark_read", "PUT", "/api/v1/messages/:id/read",
		"将当前用户的一条站内消息标为已读；必须在浏览器重新验证密码并确认后执行",
		func(ctx context.Context, uid idgen.ID, path map[string]string) error {
			id, err := idgen.Parse(path["id"])
			if err != nil {
				return err
			}
			return svc.ValidateMarkRead(ctx, uid, id)
		})
	g.DELETE("/messages", h.Clear)
	g.DELETE("/messages/:id", h.Delete)

	go cleanupLoop(d.DB, d.Log, d.Config.Message.RetentionDays)
}

// cleanupLoop 每天清理一次超过保留期的已读消息（message.retention_days，<=0 表示不清理）。
// 模块没有关闭钩子，goroutine 随进程退出。
func cleanupLoop(db *gorm.DB, log *zap.Logger, days int) {
	if days <= 0 {
		return
	}
	run := func() {
		ctx := tenant.Skip(context.Background())
		cutoff := time.Now().UTC().AddDate(0, 0, -days)
		res := db.WithContext(ctx).Where("read_at IS NOT NULL AND read_at < ?", cutoff).Delete(&Message{})
		if res.Error != nil {
			log.Warn("message: cleanup failed", zap.Error(res.Error))
		}
	}
	run()
	for range time.Tick(24 * time.Hour) {
		run()
	}
}
