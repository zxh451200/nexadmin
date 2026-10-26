package payment

import (
	"context"
	"time"

	"nexadmin/core/event"
)

// 事件目录注册与载荷。系统间集成走进程内订阅（event.Subscribe）；
// 注意支付单是平台级（无租户），事件 TenantID=0，openapi 的 webhook 分发器不会外发这些事件。
const (
	EventSucceeded    = "payment.succeeded"
	EventFailed       = "payment.failed"
	EventClosed       = "payment.closed"
	EventRefundOK     = "payment.refund.succeeded"
	EventRefundFailed = "payment.refund.failed"
)

// CatalogInfos 是插件声明的事件目录条目（plugin.go init 时调用 event.Register）。
func CatalogInfos() []event.Info {
	return []event.Info{
		{Name: EventSucceeded, Description: "支付单支付成功"},
		{Name: EventFailed, Description: "支付单支付失败"},
		{Name: EventClosed, Description: "支付单超时或手动关闭"},
		{Name: EventRefundOK, Description: "退款成功"},
		{Name: EventRefundFailed, Description: "退款被渠道拒绝"},
	}
}

// EventData 是 payment.* 事件载荷：只含业务定位字段与金额，不含任何凭据。
// BizType/BizNo 是业务方的幂等键，订阅方靠它定位自己的订单。
type EventData struct {
	PaymentNo     string    `json:"paymentNo"`
	BizType       string    `json:"bizType"`
	BizNo         string    `json:"bizNo"`
	AmountMinor   int64     `json:"amountMinor"`
	RefundedMinor int64     `json:"refundedMinor,omitempty"`
	Currency      string    `json:"currency"`
	Channel       string    `json:"channel"`
	ChannelTxnID  string    `json:"channelTxnId,omitempty"`
	Occurrence    time.Time `json:"occurredAt"`
}

func (s *Service) publish(ctx context.Context, name string, o *Order, now time.Time) {
	event.Publish(ctx, name, EventData{
		PaymentNo: o.PaymentNo, BizType: o.BizType, BizNo: o.BizNo,
		AmountMinor: o.AmountMinor, RefundedMinor: o.RefundedMinor,
		Currency: o.Currency, Channel: o.Channel, ChannelTxnID: o.ChannelTxnID,
		Occurrence: now,
	})
}
