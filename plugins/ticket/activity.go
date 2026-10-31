package ticket

// 工单动态（审计轨迹）：状态、负责人、分组、优先级、标签、附件、评价与规则命中都留一行事实。
// 与回复分表：回复是「说的话」，动态是「发生的事」，混在一张表里未读数、最后摘要与访客可见性都会变形。
// 表里只存动作码与前后值，不存文案——渲染由前端按码查字典，第三种语言不必改 Go。

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 动作码（稳定契约：前端字典与开放事件都以它为准）。
const (
	ActCreated         = "created"
	ActStatusChanged   = "status_changed"
	ActPriorityChanged = "priority_changed"
	ActAssigneeChanged = "assignee_changed"
	ActGroupChanged    = "group_changed"
	ActTagsChanged     = "tags_changed"
	ActAttachmentAdded = "attachment_added"
	ActRatingAdded     = "rating_added"
	ActRuleApplied     = "rule_applied"
)

// 动作发起方，与回复的 Replier* 用同一套词。
const (
	ActorAgent    = ReplierAgent
	ActorReporter = ReplierReporter
	ActorSystem   = ReplierSystem
)

// activityLimit 是一次读取的动态条数上限（长工单取最近这些）。
const activityLimit = 200

// TicketActivity 是工单上的一次变更。
type TicketActivity struct {
	database.PlatformModel
	TicketID  idgen.ID `gorm:"not null;index:idx_plg_ticket_activity_ticket,priority:1" json:"-"`
	TicketNo  string   `gorm:"size:40;not null;default:''" json:"ticketNo"`
	ActorType string   `gorm:"size:16;not null" json:"actorType"`
	ActorID   string   `gorm:"size:64;not null;default:''" json:"actorId"`
	ActorName string   `gorm:"size:64;not null;default:''" json:"actorName"`
	Action    string   `gorm:"size:32;not null" json:"action"`
	FromValue string   `gorm:"size:64;not null;default:''" json:"fromValue"`
	ToValue   string   `gorm:"size:64;not null;default:''" json:"toValue"`
	Detail    string   `gorm:"size:255;not null;default:''" json:"detail"`
}

func (TicketActivity) TableName() string { return "plg_ticket_activity" }

// ActivityActor 是这条动态的发起人。
type ActivityActor struct {
	Type string
	ID   string
	Name string
}

// systemActor 是无人值守的变更（清扫器、宿主服务端调用）。
func systemActor(what string) ActivityActor {
	return ActivityActor{Type: ActorSystem, Name: truncate(what, 64)}
}

type activityActorKey struct{}

// agentActor 取坐席发起人：context 中间件放进来的登录态优先，没有则用接口参数里的操作人名
// （清扫器、宿主服务端调用这类无人值守的变更仍记为系统）。
func agentActor(ctx context.Context, name string) ActivityActor {
	a := actorFrom(ctx)
	if a.Type != ActorSystem {
		return a
	}
	if strings.TrimSpace(name) == "" {
		return a
	}
	return ActivityActor{Type: ActorAgent, Name: name}
}

// WithActor 把当前操作人带进 context：服务方法签名不变，否则每个入口都要多一个参数。
func WithActor(ctx context.Context, a ActivityActor) context.Context {
	return context.WithValue(ctx, activityActorKey{}, a)
}

// actorFrom 取 context 里的操作人；没有则记为系统。
func actorFrom(ctx context.Context) ActivityActor {
	if a, ok := ctx.Value(activityActorKey{}).(ActivityActor); ok && a.Type != "" {
		return a
	}
	return ActivityActor{Type: ActorSystem, Name: "system"}
}

// recordActivity 落一行动态。tx 非 nil 时与业务变更同事务：审计不能比变更先失败或后失败。
func (s *Service) recordActivity(ctx context.Context, tx *gorm.DB, t *Ticket, act ActivityActor, action, from, to, detail string) error {
	if t == nil || t.ID == 0 {
		return nil
	}
	if tx == nil {
		tx = s.db
	}
	row := TicketActivity{
		TicketID:  t.ID,
		TicketNo:  t.TicketNo,
		ActorType: act.Type,
		ActorID:   truncate(strings.TrimSpace(act.ID), 64),
		ActorName: truncate(strings.TrimSpace(act.Name), 64),
		Action:    action,
		FromValue: truncate(strings.TrimSpace(from), 64),
		ToValue:   truncate(strings.TrimSpace(to), 64),
		Detail:    truncate(strings.TrimSpace(detail), 255),
	}
	return tx.WithContext(ctx).Create(&row).Error
}

// RecordAttachmentAdded 给上传入口用：动态与附件行不在同一段代码里，单独露一个口。
// 附件动作对客户不可见（见 publicActivityActions），所以内部附件的文件名不会外泄。
func (s *Service) RecordAttachmentAdded(ctx context.Context, t *Ticket, act ActivityActor, name string) error {
	return s.recordActivity(ctx, nil, t, act, ActAttachmentAdded, "", "", name)
}

// TicketActivities 返回工单动态，按时间升序（与回复流同一方向，前端可直接合并）。
// publicOnly 用于客户自助页：只暴露提交与状态变化，内部协作细节不外泄。
func (s *Service) TicketActivities(ctx context.Context, ticketID idgen.ID, publicOnly bool) ([]TicketActivity, error) {
	if ticketID == 0 {
		return []TicketActivity{}, nil
	}
	db := s.db.WithContext(ctx).Where("ticket_id = ?", int64(ticketID))
	if publicOnly {
		db = db.Where("action IN ?", publicActivityActions())
	}
	var rows []TicketActivity
	if err := db.Order("id DESC").Limit(activityLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows, nil
}

// publicActivityActions 是客户可见的动作码集合（顺序稳定，便于断言与 SQL 复用）。
func publicActivityActions() []string {
	return []string{ActCreated, ActStatusChanged}
}
