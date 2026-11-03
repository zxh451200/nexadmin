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
		// feed 是跨租户共用主题，守卫只验 ticket:list、明确不验数据范围：帧里不能带正文摘要，
		// 否则内部备注会发给范围外坐席和别的租户。前端只用 feed 当「列表有变化」的信号
		// （Preview 无人消费），列表数据本身按范围过滤。
		ws.Publish(wsTopicFeed, feedFrame(kind, t))
	}
}

// feedFrame 是 feed 主题用的帧：只有类型/工单号/状态，不带任何正文。见 publishWS 的说明。
func feedFrame(kind string, t *Ticket) wsFrame {
	return wsFrame{Type: kind, TicketNo: t.TicketNo, Status: t.Status}
}

// replyPreview 决定「回复」事件能带什么正文：内部备注一律空 ——
// 单工单主题的订阅者里就有报告人（客户），带摘要等于把坐席之间的悄悄话推给对面。
func replyPreview(internal bool, content string) string {
	if internal {
		return ""
	}
	return preview(content)
}

// marshalFrame 仅供测试断言帧形状。
func marshalFrame(f wsFrame) string {
	b, _ := json.Marshal(f)
	return string(b)
}
