package website

import (
	"context"
	"time"

	"nexadmin/core/event"
	"nexadmin/pkg/idgen"
)

// 事件目录条目与载荷。插件自身订阅 website.message.created 给后台收件箱的人写站内信、
// 抄送站点邮箱（见 notify.go）；宿主二开也可订阅它把询盘推进自己的 CRM，不用改插件。
// 留言是平台级数据（官网代表整个部署主体，没有租户），所以事件不带 tenantId。
const EventMessageCreated = "website.message.created"

// CatalogInfos 是插件声明的事件目录条目（plugin.go init 时调用 event.Register）。
func CatalogInfos() []event.Info {
	return []event.Info{
		{Name: EventMessageCreated, Description: "官网收到一条在线留言"},
	}
}

// EventData 是 website.message.created 的载荷：只带 id、称呼与正文预览（前 120 字）。
// 不带邮箱、电话、IP、UA：事件目录是公开的（GET /openapi/events），载荷会进 webhook 投递记录。
type EventData struct {
	ID         idgen.ID  `json:"id"`
	Name       string    `json:"name"`
	Preview    string    `json:"preview"`
	OccurredAt time.Time `json:"occurredAt"`
}

// messagePreviewRunes 是预览长度，与 IM 会话预览保持一致。
const messagePreviewRunes = 120

func (s *Service) publishCreated(ctx context.Context, m *Message) {
	event.Publish(ctx, EventMessageCreated, EventData{
		ID: m.ID, Name: m.Name,
		Preview: trimRunes(m.Content, messagePreviewRunes),
		OccurredAt: m.CreatedAt,
	})
}
