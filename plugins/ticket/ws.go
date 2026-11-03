package ticket

// WS 实时推送：工单变化即时通知在线坐席，替换「轮询 3 秒」的主通道（轮询保留作兜底）。
//
// 两个主题（订阅授权见 ws_guard.go）：
//   - ticket:<工单号>：单张工单的回复/状态/认领/评价帧，详情会话页订阅；
//   - ticket:feed：全量列表帧，队列页订阅后刷新列表。
//
// 平台级口径与 IM 一致：发布侧不带租户，守卫按「主体持 ticket:list」放行（fail closed）。
// 发布是尽力而为：hub 未装配时是空操作，任何失败不影响业务主流程。

import (
	"encoding/json"

	"nexadmin/core/ws"
)

// wsTopicPrefix / wsTopicFeed 与守卫、前端订阅端保持一致，改动要三处同步。
const (
	wsTopicPrefix = "ticket:"
	wsTopicFeed   = "ticket:feed"
)

// wsFrame 是推给前端的帧；字段刻意保持最小，详情数据由前端命中帧后拉增量（since=seq 已有）。
type wsFrame struct {
	Type     string `json:"type"`
	TicketNo string `json:"ticketNo"`
	Status   string `json:"status,omitempty"`
	Preview  string `json:"preview,omitempty"`
}

// WS 帧类型。
const (
	wsKindCreated  = "created"
	wsKindReplied  = "replied"
	wsKindStatus   = "status"
	wsKindAssigned = "assigned"
)

// publishWS 发布单帧 + 列表帧。rated 不进 feed（列表不按评价变化），其余都会影响队列。
func (s *Service) publishWS(kind string, t *Ticket, previewText string) {
	if t == nil {
		return
	}
	f := wsFrame{Type: kind, TicketNo: t.TicketNo, Status: t.Status, Preview: truncate(previewText, 120)}
	ws.Publish(wsTopicPrefix+t.TicketNo, f)
	if kind != "rated" {
		ws.Publish(wsTopicFeed, f)
	}
}

// marshalFrame 仅供测试断言帧形状。
func marshalFrame(f wsFrame) string {
	b, _ := json.Marshal(f)
	return string(b)
}
