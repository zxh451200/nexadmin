package website

// 新留言通知：官网是匿名入口，管理员不盯着后台就等于没收到询盘。
//
//   - 站内信（顶栏铃铛）：发给所有能打开「在线留言」收件箱的人，按权限码 website:message 找；
//     始终发，零配置。
//   - 邮件抄送：站点设置开启且填了联系邮箱时，再发一封到该邮箱。SMTP 未配置按跳过处理，不刷屏报错。
//
// 留言是平台级数据（官网代表整个部署主体），但站内信 sys_message 是租户表，
// 所以收件人按租户分组后分别发送（与在线客服的坐席提醒同一套做法）。
// 每用户每渠道的限频在 core/notify 里兜底，这里不再自己加冷却。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/event"
	"nexadmin/core/notify"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
)

const (
	// messagePerm 是收件人条件：持有它的人能在后台看到留言收件箱（见 plugin.yaml 菜单）。
	messagePerm = "website:message"
	// notifyCategory 对应字典 message_category 的 website 项。
	notifyCategory = "website"
	// inboxLink 是站内信跳转的后台路径（SPA 内路由，不带域名）。
	inboxLink = "/website/messages"
	// notifyQueue 是待通知留言的缓冲；满了只丢通知并告警，不阻塞事件分发协程。
	notifyQueue = 64
)

// 测试缝用的最小面：core/notify 的 *Service 同时满足两者，nil 时方法是安全空操作。
type (
	msgSender interface {
		Send(ctx context.Context, m notify.Msg) error
	}
	mailSender interface {
		SendMail(ctx context.Context, to, tpl, lang string, d mail.Data) error
	}
)

type holder struct {
	UserID   idgen.ID `gorm:"column:user_id"`
	TenantID idgen.ID `gorm:"column:tenant_id"`
	IsSuper  bool     `gorm:"column:is_super"`
}

type notifier struct {
	ctx    context.Context // 插件运行 ctx：停用后续订变成空操作、worker 退出
	db     *gorm.DB
	sender msgSender
	mailer mailSender
	lang   string
	log    *zap.Logger
	queue  chan EventData

	findHolders func(ctx context.Context) ([]holder, error)
	siteOf      func(ctx context.Context) (*Setting, error)
}

func newNotifier(ctx context.Context, d *core.Deps, svc *Service) *notifier {
	if d == nil || d.DB == nil {
		if d != nil && d.Log != nil {
			d.Log.Warn("website: 留言通知未启用（数据库缺失）")
		}
		return nil
	}
	n := &notifier{
		ctx: ctx, db: d.DB, lang: "zh-CN", log: d.Log, queue: make(chan EventData, notifyQueue),
		siteOf: svc.Settings,
	}
	if d.Notify != nil {
		n.sender, n.mailer = d.Notify, d.Notify
	}
	if d.Config != nil && len(d.Config.Languages) > 0 {
		n.lang = d.Config.DefaultLanguage()
	}
	n.findHolders = n.queryHolders
	return n
}

// handle 在事件总线分发协程里被调用：只做名称过滤与入队，耗时工作交给 worker。
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
	select {
	case n.queue <- data:
	default:
		n.warn("留言通知队列已满，丢弃一条", zap.Any("messageId", data.ID))
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
	n.notifyInApp(data)
	n.notifyEmail(data)
}

func (n *notifier) notifyInApp(data EventData) {
	if n.sender == nil {
		return
	}
	holders, err := n.findHolders(n.ctx)
	if err != nil {
		n.warn("解析留言收件人失败", zap.Error(err))
		return
	}
	if len(holders) == 0 {
		// 没人有这个权限码：通知发不出去是配置问题，留下一行可读的告警而不是静默。
		n.warn("没有用户持有留言权限，无人可通知", zap.String("perm", messagePerm))
		return
	}
	title, content := n.compose(data)
	byTenant := make(map[idgen.ID][]idgen.ID, len(holders))
	for _, h := range holders {
		byTenant[h.TenantID] = append(byTenant[h.TenantID], h.UserID)
	}
	for tid, ids := range byTenant {
		if err := n.sender.Send(tenant.With(n.ctx, tid), notify.Msg{
			To: ids, Category: notifyCategory, Title: title, Content: content,
			Link: inboxLink, Channels: []string{notify.ChannelInApp},
		}); err != nil {
			n.warn("留言站内信发送失败", zap.Error(err))
		}
	}
}

// notifyEmail 抄送站点联系邮箱。开关关、邮箱空、SMTP 未配置都按跳过处理。
func (n *notifier) notifyEmail(data EventData) {
	if n.mailer == nil || n.siteOf == nil {
		return
	}
	set, err := n.siteOf(n.ctx)
	if err != nil {
		n.warn("读取站点设置失败，跳过留言邮件抄送", zap.Error(err))
		return
	}
	if !set.MessageNotifyEmail || strings.TrimSpace(set.ContactEmail) == "" {
		return
	}
	title, content := n.compose(data)
	err = n.mailer.SendMail(n.ctx, set.ContactEmail, mail.TplNotice, n.lang, mail.Data{
		Title: title,
		// 邮件里没有站内信那样的相对路由可点，把入口写成菜单位置。
		Content: content + "\n\n后台「官网 → 在线留言」查看全部询盘。",
	})
	switch {
	case err == nil:
	case errors.Is(err, notify.ErrMailNotConfigured), errors.Is(err, notify.ErrSkip):
		// 新装站点没配 SMTP 是常态：一条 Debug 足够，warn 会变成长期噪音。
		if n.log != nil {
			n.log.Debug("website: 留言邮件抄送跳过", zap.Error(err))
		}
	default:
		n.warn("留言邮件抄送失败", zap.Error(err))
	}
}

// compose 生成通知标题与正文；语言跟随站点默认语言（收件人是内部用户，不按访客语言）。
func (n *notifier) compose(data EventData) (string, string) {
	content := data.Preview
	if content == "" {
		content = "（空）"
	}
	if strings.HasPrefix(strings.ToLower(n.lang), "en") {
		return fmt.Sprintf("Website enquiry from %s", data.Name), content
	}
	return fmt.Sprintf("官网留言：%s 发来新询盘", data.Name), content
}

// queryHolders 返回持有 website:message 的用户（含超管与内置角色）中的平台级调用方。
// 平台级查询：留言不随租户隔离，所以取候选要跳过租户隔离看全部用户。
// 收件人只留打得开官网后台的人（plugin.go 的 RequirePlatformFeature 整组收口）——
// 别的租户点进来只有「平台级功能」解释页，那封站内信就是死信。
// 判据同 plugins/im/notify.go：走 tenant.IsPlatformCallerID，写在 Go 侧才有真测的余地。
func (n *notifier) queryHolders(ctx context.Context) ([]holder, error) {
	db := n.db.WithContext(tenant.Skip(ctx))
	holders := db.Table("sys_user_role AS ur").Select("ur.user_id").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ?", "1").
		Joins("LEFT JOIN sys_role_menu rm ON rm.role_id = r.id").
		Joins("LEFT JOIN sys_menu m ON m.id = rm.menu_id AND m.status = ? AND m.perm = ?", "1", messagePerm).
		Where("r.builtin = ? OR m.id IS NOT NULL", true)
	var rows []holder
	err := db.Table("sys_user AS u").
		Select("DISTINCT u.id AS user_id, u.tenant_id AS tenant_id, u.is_super AS is_super").
		Where("u.deleted_at IS NULL AND u.status = ?", "1").
		Where("u.is_super = ? OR u.id IN (?)", true, holders).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]holder, 0, len(rows))
	for _, r := range rows {
		if tenant.IsPlatformCallerID(r.TenantID, r.IsSuper) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (n *notifier) warn(msg string, fields ...zap.Field) {
	if n.log != nil {
		n.log.Warn("website: "+msg, fields...)
	}
}
