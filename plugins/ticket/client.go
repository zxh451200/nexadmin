package ticket

// 宿主侧进程内 API。业务模块（如商城）import 本包做服务端集成
// （比如订单详情页代客建单、订阅 ticket.* 事件推通知）；HTTP 接口给前端用。

import (
	"context"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
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

// Get 按工单号取工单本身（不含回复）。包级函数不能叫 Ticket：会和 Ticket 类型同名冲突。
func Get(ctx context.Context, ticketNo string) (*Ticket, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.Ticket(ctx, ticketNo)
}

// Activities 取工单动态；publicOnly=true 时只含可对客户展示的部分（提交与状态变化）。
func Activities(ctx context.Context, ticketNo string, publicOnly bool) ([]TicketActivity, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.ActivitiesByNo(ctx, ticketNo, publicOnly)
}

// BizTickets 按业务对象反查工单（订单详情页那种「这张单挂的工单」）。
// bizType 可为空（只按 bizNo 查）。truncated=true 表示命中超过默认上限，结果不完整。
func BizTickets(ctx context.Context, bizType, bizNo string) (tickets []Ticket, truncated bool, err error) {
	return SomeTickets(ctx, TicketQuery{BizType: bizType, BizNo: bizNo}, 0)
}

// ReporterTickets 按报告人取其全部工单（客户门户「我的工单」）。
// reporterType 用 ReplierReporter/ReplierAgent 那一套词，reporterID 是宿主侧身份（访客令牌、会员 ID…）。
func ReporterTickets(ctx context.Context, reporterType, reporterID string) (tickets []Ticket, truncated bool, err error) {
	return SomeTickets(ctx, TicketQuery{ReporterType: reporterType, ReporterID: reporterID}, 0)
}

// SomeTickets 是反查的通用入口：条件同后台列表，但不分页、自带 limit（<=0 用默认上限）。
func SomeTickets(ctx context.Context, q TicketQuery, limit int) ([]Ticket, bool, error) {
	s := current()
	if s == nil {
		return nil, false, responseUnavailable()
	}
	return s.HostTickets(ctx, q, limit)
}

// SetStatus 由宿主服务端驱动工单状态（如退款到账后自动关闭）；operator 会记进动态。
func SetStatus(ctx context.Context, ticketNo, to, operator string) (*Ticket, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.SetStatus(ctx, ticketNo, to, operator)
}

// Assign 由宿主服务端指定处理人（如按会员归属的销售）；assigneeID=0 会返回参数错误。
func Assign(ctx context.Context, ticketNo string, assigneeID idgen.ID, assigneeName string) (*Ticket, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.Assign(ctx, ticketNo, assigneeID, assigneeName, 0)
}
