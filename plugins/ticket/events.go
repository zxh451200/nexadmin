package ticket

import (
	"context"
	"strings"
	"time"

	"nexadmin/core/event"
)

// 事件目录条目与载荷。宿主可订阅做通知（如新工单提醒、解决后回访）；
// 事件租户取自请求上下文，后台清扫器按负责人所属租户发布 ticket.overdue，
// 因此 openapi webhook 能按租户收到这些事件。
const (
	EventCreated  = "ticket.created"
	EventReplied  = "ticket.replied"
	EventResolved = "ticket.resolved"
	EventClosed   = "ticket.closed"
	EventRated    = "ticket.rated"
	EventOverdue  = "ticket.overdue"
)

// CatalogInfos 是插件声明的事件目录条目（plugin.go init 时调用 event.Register）。
func CatalogInfos() []event.Info {
	return []event.Info{
		{Name: EventCreated, Description: "工单已创建"},
		{Name: EventReplied, Description: "工单有新回复"},
		{Name: EventResolved, Description: "工单已标记解决"},
		{Name: EventClosed, Description: "工单已关闭"},
		{Name: EventRated, Description: "工单已评价"},
		{Name: EventOverdue, Description: "工单已超时（SLA 违约）"},
	}
}

// EventData 是 ticket.* 事件的载荷：只带定位字段与预览（前 120 字）。
type EventData struct {
	TicketNo   string `json:"ticketNo"`
	BizType    string `json:"bizType,omitempty"`
	BizNo      string `json:"bizNo,omitempty"`
	Status     string `json:"status"`
	Priority   string `json:"priority"`
	Title      string `json:"title"`
	SenderType string `json:"senderType"`
	SenderName string `json:"senderName,omitempty"`
	Seq        int64  `json:"seq,omitempty"`
	Preview    string `json:"preview,omitempty"`
	Rating     int    `json:"rating,omitempty"`
	// SLAKind 仅 ticket.overdue 使用：response=首响超时，resolution=解决超时。
	SLAKind    string    `json:"slaKind,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

func (s *Service) publish(ctx context.Context, name string, t *Ticket, data EventData) {
	data.TicketNo, data.BizType, data.BizNo = t.TicketNo, t.BizType, t.BizNo
	data.Status, data.Priority, data.Title = t.Status, t.Priority, t.Title
	data.OccurredAt = time.Now().UTC()
	event.Publish(ctx, name, data)
}

func preview(content string) string {
	content = strings.TrimSpace(content)
	if len(content) > 120 {
		return content[:120]
	}
	return content
}
