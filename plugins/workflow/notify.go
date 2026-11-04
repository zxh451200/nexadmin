package workflow

// 待办通知：审批人不在流程中心盯着，实例到了他桌上他不知道——待办就等于没有。
//
//   - 订阅事件总线上的 workflow.todo（引擎每次推进都带「待审批人并集」发布）；
//   - 只发站内信（顶栏铃铛），跳转 /workflow/center；邮件不抄——审批是高频事件，
//     每一步一封邮件很快就会被人设成免打扰，反而毁掉这个渠道。
//
// 去重口径：同一实例同一审批人只通知一次（instID|assignee），实例结束后清账。
// 节点粒度不去重——或签下别人先批、轮到你时实例还没变，这种「再看一眼」的提醒价值不大，
// 而逐节点提醒的噪音大得多。去重表在进程内：重启后同实例的下一次流转会再多提醒一次，
// 可接受；多实例部署各自持有去重表，最坏重复发送（与留言通知同一量级的取舍）。

import (
	"context"
	"strings"
	"sync"

	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/event"
	"nexadmin/core/i18n"
	"nexadmin/core/notify"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	// notifyCategory 是站内信分类；inboxLink 是站内信跳转的后台路径（SPA 内路由）。
	notifyCategory = "workflow"
	inboxLink      = "/workflow/center"
	// notifyQueue 是待通知事件的缓冲；满了只丢通知并告警，不阻塞事件分发协程。
	notifyQueue = 64
)

type todoNotifier struct {
	ctx    context.Context // 插件运行 ctx：停用后订阅变成空操作、worker 退出
	sender interface {
		Send(ctx context.Context, m notify.Msg) error
	}
	log   *zap.Logger
	queue chan EventData

	mu   sync.Mutex
	seen map[string]bool // instID|assignee → 已通知
}

func newTodoNotifier(ctx context.Context, d *core.Deps) *todoNotifier {
	if d == nil || d.Notify == nil {
		if d != nil && d.Log != nil {
			d.Log.Warn("workflow: 待办通知未启用（通知服务缺失）")
		}
		return nil
	}
	return &todoNotifier{
		ctx: ctx, sender: d.Notify, log: d.Log,
		queue: make(chan EventData, notifyQueue),
		seen:  map[string]bool{},
	}
}

// handle 在事件总线分发协程里被调用：过滤名称、完成即清账、其余入队给 worker。
func (n *todoNotifier) handle(e event.Event) {
	if n.ctx.Err() != nil {
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
	switch e.Name {
	case EventCompleted:
		// 实例已终结：清掉它的去重账（撤回/驳回后可能重新发起同主题的新实例，ID 不同不受影响）。
		n.forget(data.InstID)
	case EventTodo:
		select {
		case n.queue <- data:
		default:
			n.warn("待办通知队列已满，丢弃一条", zap.Any("instId", data.InstID))
		}
	}
}

func (n *todoNotifier) run() {
	for {
		select {
		case data := <-n.queue:
			n.notify(data)
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *todoNotifier) notify(data EventData) {
	if n.sender == nil || data.TenantID == "" {
		return
	}
	ids := make([]idgen.ID, 0, len(data.Approvers))
	n.mu.Lock()
	for _, a := range data.Approvers {
		a = strings.TrimSpace(a)
		if a == "" {
			continue // 公开任务没有具体审批人，通知无从谈起
		}
		key := idText(data.InstID) + "|" + a
		if n.seen[key] {
			continue
		}
		id, err := idgen.Parse(a)
		if err != nil {
			continue // 审批人是宿主自定义主体：解析不出用户 ID 就发不了站内信
		}
		n.seen[key] = true
		ids = append(ids, id)
	}
	n.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	tid, err := idgen.Parse(data.TenantID)
	if err != nil {
		n.warn("待办通知租户解析失败", zap.String("tenantId", data.TenantID))
		return
	}
	// 收件人各自的语言拿不到（站内信只存一份文本），按应用默认语言出：
	// 英文部署不再收到硬写死的中文。
	tctx := tenant.With(n.ctx, tid)
	name := titleOr(data)
	if err := n.sender.Send(tctx, notify.Msg{
		To: ids, Category: notifyCategory,
		Title:   i18n.T(tctx, "workflow.notify_todo_title", name),
		Content: i18n.T(tctx, "workflow.notify_todo_content", name),
		Link:    inboxLink, Channels: []string{notify.ChannelInApp},
	}); err != nil {
		n.warn("待办站内信发送失败", zap.Error(err))
	}
}

// forget 清掉某实例的全部去重记录（实例终结时调用）。
func (n *todoNotifier) forget(instID idgen.ID) {
	prefix := idText(instID) + "|"
	n.mu.Lock()
	defer n.mu.Unlock()
	for k := range n.seen {
		if strings.HasPrefix(k, prefix) {
			delete(n.seen, k)
		}
	}
}

func (n *todoNotifier) warn(msg string, fields ...zap.Field) {
	if n.log != nil {
		n.log.Warn("workflow: "+msg, fields...)
	}
}

// titleOr 兜底标题：实例标题为空时退回流程标识，绝不发「审批待办：」空标题。
func titleOr(data EventData) string {
	if t := strings.TrimSpace(data.Title); t != "" {
		return t
	}
	return data.DefKey
}
