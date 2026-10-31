package ticket

// 宿主侧进程内 API。业务模块（如商城）import 本包做服务端集成
// （比如订单详情页代客建单、订阅 ticket.* 事件推通知）；HTTP 接口给前端用。

import (
	"context"

	"nexadmin/core/response"
)

// defaultSvc 在插件 Register 时设置；宿主调用发生在请求期，此时插件已挂载。
var defaultSvc = make(chan *Service, 1)

// SetDefault 由插件装配层调用，注册进程级默认服务。
func SetDefault(s *Service) {
	select {
	case <-defaultSvc: // 已有则替换
	default:
	}
	defaultSvc <- s
}

func current() *Service {
	select {
	case s := <-defaultSvc:
		defaultSvc <- s
		return s
	default:
		return nil
	}
}

func responseUnavailable() error {
	return response.Unavailable("ticket.plugin_inactive")
}

// Create 建单（宿主代客或系统自建，ReporterType 按来源给 visitor/user）。
func Create(ctx context.Context, in CreateInput) (*Ticket, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.Create(ctx, in)
}

// Replies 增量拉取工单回复（宿主视角，含内部备注）。
func Replies(ctx context.Context, ticketNo string, since, limit int) (*Ticket, []TicketReply, error) {
	s := current()
	if s == nil {
		return nil, nil, responseUnavailable()
	}
	return s.Replies(ctx, ticketNo, since, limit, true)
}

// Reply 由宿主服务端代发回复（如系统备注用 ReplierSystem）。
func Reply(ctx context.Context, ticketNo string, in ReplyInput) (*TicketReply, *Ticket, error) {
	s := current()
	if s == nil {
		return nil, nil, responseUnavailable()
	}
	return s.Reply(ctx, ticketNo, in)
}
