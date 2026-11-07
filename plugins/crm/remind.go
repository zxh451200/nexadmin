package crm

// 我的待跟进 + 到期站内信提醒。
//
// 「待跟进」= 未完成（done=false）且填了下次跟进日期（next_at 非空）并已到期的跟进记录，
// 可见性继承客户的归属（数据范围按 customer.owner 裁）——能看到客户的人才能看到他的跟进。
//
// 提醒是「一次即止」：跟进记录建后没有编辑接口（next_at 不变），清扫器把到期未提醒的
// 待办按（租户, 客户归属人）汇总发一条站内信，发过落 reminded_at，不再重发；
// 要再提醒得等新的待办到期。消息模块未注入时整个清扫是空操作，状态留在 NULL，
// 之后装上消息模块从零开始提醒——不丢提醒，只是晚到。

import (
	"context"
	"strings"
	"time"

	"nexadmin/core/datascope"
	"nexadmin/core/i18n"
	"nexadmin/core/notify"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// msgSender 是 core/notify 的最小面（测试注入假实现）。
type msgSender interface {
	Send(ctx context.Context, m notify.Msg) error
}

// notifyCategory 对应字典 message_category 的 business 项：跟进提醒属于业务消息。
const notifyCategory = "business"

// todosListLimit 待跟进清单单次返回上限：这是「先去处理」的入口清单，不是分页报表。
const todosListLimit = 200

// todoDigestMaxNames 站内信正文最多点名 3 个客户，剩下的并进「等 N 条」。
const todoDigestMaxNames = 3

// WithNotify 注入站内信发送器与默认语言；不注入时业务照常、不发提醒。
func (s *Service) WithNotify(n msgSender, lang string) *Service {
	s.notifier, s.lang = n, lang
	return s
}

// WithTimezone 设置应用时区（非法值退回 UTC）。
func (s *Service) WithTimezone(tz string) *Service {
	loc, err := time.LoadLocation(tz)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	s.loc = loc
	return s
}

func (s *Service) location() *time.Location {
	if s.loc != nil {
		return s.loc
	}
	return time.UTC
}

// notifyText 按应用默认语言取通知文案（清扫器没有请求上下文）。
func (s *Service) notifyText(ctx context.Context, key string, args ...any) string {
	return i18n.T(i18n.WithLang(ctx, s.lang), key, args...)
}

// TodoItem 是我的待跟进清单的一格。
type TodoItem struct {
	ID           idgen.ID `json:"id"`
	CustomerID   idgen.ID `json:"customerId"`
	CustomerName string   `json:"customerName"`
	Kind         string   `json:"kind"`
	Content      string   `json:"content"`
	NextAt       string   `json:"nextAt"`
	Author       string   `json:"author"`
	Overdue      bool     `json:"overdue"` // 已过期（next_at 早于今天）显示为红色
}

// todoRow 是清单查询的扫描行（customer_name 来自 JOIN，不是列名风格，单独落一层）。
type todoRow struct {
	ID           idgen.ID
	CustomerID   idgen.ID
	CustomerName string
	Kind         string
	Content      string
	NextAt       string
	Author       string
}

// ListDueTodos 我的待跟进清单：到期未完成的跟进记录 + 客户名，按到期日升序。
// total 是全量计数（清单最多返回 todosListLimit 条——看不完就先去处理最老的）。
func (s *Service) ListDueTodos(ctx context.Context, sc *datascope.Scope) ([]TodoItem, int64, error) {
	today := time.Now().In(s.location()).Format("2006-01-02")
	base := s.db.WithContext(ctx).Table("plg_crm_activity AS a").
		Joins("JOIN plg_crm_customer AS c ON c.id = a.customer_id AND c.deleted_at IS NULL").
		Where("a.done = ? AND a.next_at <> '' AND a.next_at <= ?", false, today)
	if cond, args, limited := sc.UsernameCond("c.owner"); limited {
		base = base.Where(cond, args...)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []todoRow{}
	if err := base.Select("a.id, a.customer_id, a.kind, a.content, a.next_at, a.author, c.name AS customer_name").
		Order("a.next_at ASC, a.id ASC").Limit(todosListLimit).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]TodoItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, TodoItem{
			ID: r.ID, CustomerID: r.CustomerID, CustomerName: r.CustomerName,
			Kind: r.Kind, Content: r.Content, NextAt: r.NextAt, Author: r.Author,
			Overdue: r.NextAt < today,
		})
	}
	return items, total, nil
}

// SweepReminders 扫一遍到期未提醒的待办，按（租户, 客户归属人）发汇总站内信，
// 发过的落 reminded_at。返回发出的记录条数（不是消息条数）。notifier 未注入时是空操作。
// 清扫器没有租户上下文，租户来自行数据；发送与落状态都按租户分组串行做。
func (s *Service) SweepReminders(ctx context.Context, now time.Time) (int, error) {
	if s.notifier == nil {
		return 0, nil
	}
	today := now.In(s.location()).Format("2006-01-02")
	// 只挑有归属人的：公海客户（owner 为空）没有收件人，提醒无处可投。
	rows := []struct {
		ID           int64
		TenantID     int64
		Owner        string
		CustomerName string
	}{}
	err := s.db.WithContext(ctx).Table("plg_crm_activity AS a").
		Joins("JOIN plg_crm_customer AS c ON c.id = a.customer_id AND c.deleted_at IS NULL").
		Select("a.id, a.tenant_id, c.owner, c.name AS customer_name").
		Where("a.done = ? AND a.next_at <> '' AND a.next_at <= ? AND a.reminded_at IS NULL AND c.owner <> ''", false, today).
		Order("a.next_at ASC, a.id ASC").
		Limit(todosListLimit).
		Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return 0, err
	}

	type groupKey struct {
		tenant int64
		owner  string
	}
	idsOf := map[groupKey][]idgen.ID{}
	namesOf := map[groupKey][]string{}
	for _, r := range rows {
		k := groupKey{r.TenantID, r.Owner}
		idsOf[k] = append(idsOf[k], idgen.ID(r.ID))
		if len(namesOf[k]) < todoDigestMaxNames {
			namesOf[k] = append(namesOf[k], r.CustomerName)
		}
	}

	sent := 0
	for k, ids := range idsOf {
		uids, err := s.userIDsByNames(ctx, k.tenant, []string{k.owner})
		if err != nil {
			s.log.Warn("crm: resolve reminder recipients failed", "owner", k.owner, "err", err.Error())
			continue
		}
		if len(uids) == 0 {
			// 归属人已删除/改名：这批永远没有收件人，标成已提醒避免每次清扫白扫一遍。
			if err := s.markReminded(ctx, now, ids); err != nil {
				s.log.Warn("crm: mark reminded failed", "err", err.Error())
			}
			continue
		}
		names := strings.Join(namesOf[k], ", ")
		title := s.notifyText(ctx, "crm.todo_notify_title", len(ids))
		content := s.notifyText(ctx, "crm.todo_notify_content", names)
		msg := notify.Msg{
			To: uids, Category: notifyCategory, Title: title, Content: content,
			Link: "/crm/customers?todo=1", Channels: []string{notify.ChannelInApp},
		}
		if err := s.notifier.Send(tenant.With(ctx, idgen.ID(k.tenant)), msg); err != nil {
			// 发送失败不落状态：下次清扫重试（notify 内部已有限频与重试，这里兜底）。
			s.log.Warn("crm: todo reminder send failed", "owner", k.owner, "err", err.Error())
			continue
		}
		if err := s.markReminded(ctx, now, ids); err != nil {
			s.log.Warn("crm: mark reminded failed", "err", err.Error())
			continue
		}
		sent += len(ids)
	}
	return sent, nil
}

// userIDsByNames 在指定租户内把账号解析成用户 ID（在职用户）。
func (s *Service) userIDsByNames(ctx context.Context, tenantID int64, names []string) ([]idgen.ID, error) {
	var ids []int64
	err := s.db.WithContext(ctx).Table("sys_user").
		Where("tenant_id = ? AND deleted_at IS NULL AND status = ? AND username IN ?", tenantID, "1", names).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	out := make([]idgen.ID, 0, len(ids))
	for _, id := range ids {
		out = append(out, idgen.ID(id))
	}
	return out, nil
}

// markReminded 落提醒时间：UpdateColumns 只碰这一列，不动 updated_at——
// 提醒是系统行为，不该让时间线看起来像有人改过这条跟进。
func (s *Service) markReminded(ctx context.Context, now time.Time, ids []idgen.ID) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&Activity{}).
		Where("id IN ?", ids).
		UpdateColumn("reminded_at", now.UTC()).
		Error
}
