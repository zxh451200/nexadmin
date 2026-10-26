package im

import (
	"context"
	"strings"
	"time"

	"nexadmin/core/event"
)

// 事件目录条目与载荷。宿主可订阅 im.message.created 做站内通知、机器人应答等；
// 会话是平台级（无租户），openapi webhook 分发器不会外发，集成走进程内订阅。
const EventMessageCreated = "im.message.created"

// CatalogInfos 是插件声明的事件目录条目（plugin.go init 时调用 event.Register）。
func CatalogInfos() []event.Info {
	return []event.Info{
		{Name: EventMessageCreated, Description: "IM 会话有新消息"},
	}
}

// EventData 是 im.message.created 的载荷：不含完整消息内容，只带预览（前 120 字）。
type EventData struct {
	ConvNo     string    `json:"convNo"`
	BizType    string    `json:"bizType,omitempty"`
	BizNo      string    `json:"bizNo,omitempty"`
	Seq        int64     `json:"seq"`
	SenderType string    `json:"senderType"`
	SenderID   string    `json:"senderId,omitempty"`
	SenderName string    `json:"senderName,omitempty"`
	Preview    string    `json:"preview"`
	OccurredAt time.Time `json:"occurredAt"`
}

func (s *Service) publishCreated(ctx context.Context, c *Conversation, m *Message) {
	event.Publish(ctx, EventMessageCreated, EventData{
		ConvNo: c.ConvNo, BizType: c.BizType, BizNo: c.BizNo,
		Seq: m.Seq, SenderType: m.SenderType, SenderID: m.SenderID, SenderName: m.SenderName,
		Preview: preview(m.Content), OccurredAt: m.CreatedAt,
	})
}

func preview(content string) string {
	content = strings.TrimSpace(content)
	if len(content) > 120 {
		return content[:120]
	}
	return content
}
