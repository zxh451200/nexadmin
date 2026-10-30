package im

// 离线提醒：访客发消息时，给能看会话的坐席写一条站内信（顶栏铃铛），链接直达该会话。
// 站内信是租户表（sys_message），而 IM 会话是平台级，所以收件人按租户分组后分别发送。
//
// 抑制规则（避免刷屏）：
//   - 只提醒访客消息，坐席自己的回复不提醒；
//   - 会话已关闭、或已有坐席读到了这条消息，不提醒；
//   - 坐席正在看这个会话（拉取过消息，agentActiveTTL 内），不提醒；
//   - 同一会话冷却窗口内只提醒一次（notifyCooldown）。

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"nexadmin/core"
	"nexadmin/core/cache"
	"nexadmin/core/event"
	"nexadmin/core/notify"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	notifyCooldown = 5 * time.Minute
	agentActiveTTL = 3 * time.Minute
	notifyQueue    = 256
	// 收件人：能打开会话列表的坐席权限码（插件菜单的 im:list）。
	agentPerm = "im:list"
	// 站内信分类，对应字典 message_category 的 im 项。
	notifyCategory = "im"
)

func activeKey(convNo string) string   { return "im:active:" + convNo }
func cooldownKey(convNo string) string { return "im:notify:" + convNo }

// msgSender 是 core/notify 的最小面（测试注入假实现）。
type msgSender interface {
	Send(ctx context.Context, m notify.Msg) error
}

type agentContact struct {
	UserID   idgen.ID `gorm:"column:user_id"`
	TenantID idgen.ID `gorm:"column:tenant_id"`
}

type notifier struct {
	ctx    context.Context // 插件运行 ctx：停用后续订者变成空操作、worker 退出
	db     *gorm.DB
	cache  cache.Cache
	sender msgSender
	lang   string
	log    logger
	queue  chan EventData

	findAgents func(ctx context.Context) ([]agentContact, error) // 测试缝
}

func newNotifier(ctx context.Context, d *core.Deps) *notifier {
	if d == nil || d.DB == nil || d.Notify == nil {
		if d != nil && d.Log != nil {
			d.Log.Warn("im: message notifications disabled (db or notify service missing)")
		}
		return nil
	}
	n := &notifier{
		ctx: ctx, db: d.DB, cache: d.Cache, sender: d.Notify,
		lang: "zh-CN", queue: make(chan EventData, notifyQueue),
	}
	if d.Log != nil {
		n.log = zapLog{d.Log}
	}
	if d.Config != nil && len(d.Config.Languages) > 0 {
		n.lang = d.Config.DefaultLanguage()
	}
	n.findAgents = n.queryAgents
	return n
}

// handle 在事件总线分发协程里被调用：只做过滤与入队，耗时工作交给 worker。
func (n *notifier) handle(e event.Event) {
	if e.Name != EventMessageCreated || n.ctx.Err() != nil {
		return
	}
	var data EventData
	switch v := e.Data.(type) {
	case EventData:
		data = v
	case *EventData:
		if v == nil {
			return
		}
		data = *v
	default:
		return
	}
	if data.SenderType != SenderVisitor || data.ConvNo == "" {
		return
	}
	select {
	case n.queue <- data:
	default:
		n.warn("im: notify queue full, dropped", "convNo", data.ConvNo)
	}
}

func (n *notifier) run() {
	for {
		select {
		case data := <-n.queue:
			n.notify(data)
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *notifier) notify(data EventData) {
	ctx := n.ctx
	var c Conversation
	if err := n.db.WithContext(ctx).Where("conv_no = ?", data.ConvNo).Take(&c).Error; err != nil {
		return
	}
	if c.Status != StatusOpen || c.AgentReadSeq >= data.Seq || n.suppress(ctx, data.ConvNo) {
		return
	}
	agents, err := n.findAgents(ctx)
	if err != nil {
		n.warn("im: resolve agents failed", "err", err.Error())
		return
	}
	if len(agents) == 0 {
		n.warn("im: no agent holds the permission, nobody to notify", "perm", agentPerm)
		return
	}
	title, content := n.compose(&c, data)
	link := "/im/conversations?conv=" + url.QueryEscape(c.ConvNo)
	byTenant := make(map[idgen.ID][]idgen.ID, len(agents))
	for _, a := range agents {
		byTenant[a.TenantID] = append(byTenant[a.TenantID], a.UserID)
	}
	for tid, ids := range byTenant {
		if err := n.sender.Send(tenant.With(ctx, tid), notify.Msg{
			To: ids, Category: notifyCategory, Title: title, Content: content, Link: link,
			Channels: []string{notify.ChannelInApp},
		}); err != nil {
			n.warn("im: notify send failed", "convNo", c.ConvNo, "err", err.Error())
		}
	}
}

// suppress 报告这次消息是否应跳过提醒：坐席在看在聊，或冷却窗口内已经提醒过。
func (n *notifier) suppress(ctx context.Context, convNo string) bool {
	if n.cache == nil {
		return false
	}
	if ok, _ := n.cache.Exists(ctx, activeKey(convNo)); ok {
		return true
	}
	if c, ok := n.cache.(cache.Counter); ok {
		v, _, err := c.Incr(ctx, cooldownKey(convNo), notifyCooldown)
		return err == nil && v > 1
	}
	if ok, _ := n.cache.Exists(ctx, cooldownKey(convNo)); ok {
		return true
	}
	_ = n.cache.Set(ctx, cooldownKey(convNo), "1", notifyCooldown)
	return false
}

func (n *notifier) compose(c *Conversation, data EventData) (string, string) {
	name := c.VisitorName
	if name == "" {
		name = c.VisitorToken
	}
	if strings.HasPrefix(strings.ToLower(n.lang), "en") {
		return fmt.Sprintf("Live chat: new message from %s", name), data.Preview
	}
	return fmt.Sprintf("在线客服：%s 发来新消息", name), data.Preview
}

// queryAgents 返回持有 im:list 的用户（含超管与内置角色），按租户分组由调用方处理。
// 平台级查询：线上客服会话跨租户共享，所以要跳过租户隔离看全部用户。
func (n *notifier) queryAgents(ctx context.Context) ([]agentContact, error) {
	db := n.db.WithContext(tenant.Skip(ctx))
	holders := db.Table("sys_user_role AS ur").Select("ur.user_id").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ?", "1").
		Joins("LEFT JOIN sys_role_menu rm ON rm.role_id = r.id").
		Joins("LEFT JOIN sys_menu m ON m.id = rm.menu_id AND m.status = ? AND m.perm = ?", "1", agentPerm).
		Where("r.builtin = ? OR m.id IS NOT NULL", true)
	var rows []agentContact
	err := db.Table("sys_user AS u").
		Select("DISTINCT u.id AS user_id, u.tenant_id AS tenant_id").
		Where("u.deleted_at IS NULL AND u.status = ?", "1").
		Where("u.is_super = ? OR u.id IN (?)", true, holders).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (n *notifier) warn(msg string, kv ...any) {
	if n.log != nil {
		n.log.Warn(msg, kv...)
	}
}
