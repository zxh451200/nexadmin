package pay

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/pkg/idgen"
)

// 资金事件（02-openapi-pay-v1.yaml EventPaySucceeded）。与状态翻转同事务写入
// plg_pay_outbox 持久化，随后进程内 event.Publish；openapi webhook 分发器按
// TenantID 外发。tenantId 必填非 0——这是解除 nexMall 平台级 TenantID=0
// 导致 webhook 不外发限制的关键。
const (
	EventSucceeded    = "pay.succeeded"
	EventFailed       = "pay.failed"
	EventClosed       = "pay.closed"
	EventRefundOK     = "pay.refund.succeeded"
	EventRefundFailed = "pay.refund.failed"
	EventAuthCaptured = "pay.auth.captured"
)

// CatalogInfos 是插件声明的事件目录条目（plugin.go init 时调用 event.Register）。
func CatalogInfos() []event.Info {
	return []event.Info{
		{Name: EventSucceeded, Description: "支付单支付成功（落账幂等键 subKey = paymentNo）"},
		{Name: EventFailed, Description: "支付单支付失败"},
		{Name: EventClosed, Description: "支付单超时或手动关闭"},
		{Name: EventRefundOK, Description: "退款成功（落账幂等键 subKey = refundNo）"},
		{Name: EventRefundFailed, Description: "退款失败"},
		{Name: EventAuthCaptured, Description: "预授权请款成功"},
	}
}

// EventPayload 是事件 data：只含业务定位字段与金额，不含任何凭据。
// BizType/BizNo 是业务方的幂等键；订阅方在自己账本落一行时以
// (tenantId, ledgerId, bizDate, itemType, subKey) 幂等，subKey = paymentNo / refundNo。
type EventPayload struct {
	PaymentNo     string            `json:"paymentNo"`
	RefundNo      string            `json:"refundNo,omitempty"`
	BizType       string            `json:"bizType"`
	BizNo         string            `json:"bizNo"`
	SubjectID     string            `json:"subjectId,omitempty"`
	ChannelCode   string            `json:"channelCode"`
	AmountMinor   int64             `json:"amountMinor"`
	RefundedMinor int64             `json:"refundedMinor,omitempty"`
	FeeMinor      int64             `json:"feeMinor,omitempty"`
	Currency      string            `json:"currency"`
	FxRateMicro   int64             `json:"fxRateMicro,omitempty"`
	ChannelTxnID  string            `json:"channelTxnId,omitempty"`
	Status        string            `json:"status"`
	PaidAt        *time.Time        `json:"paidAt,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// EventData 是事件外层信封，字段与契约 EventPaySucceeded 一致。
type EventData struct {
	EventID    string       `json:"eventId"`
	EventName  string       `json:"eventName"`
	TenantID   idgen.ID     `json:"tenantId"`
	OccurredAt time.Time    `json:"occurredAt"`
	Data       EventPayload `json:"data"`
}

// EventTenant 实现租户路由：总线与 webhook 分发器据此取租户。
func (d EventData) EventTenant() idgen.ID { return d.TenantID }

// newEventID 生成事件幂等键（雪花 ID 字符串）。出站箱与进程内总线共用同一个 ID，
// 消费端据此判重；雪花自带单调性，不需要额外唯一性约束。
func newEventID() (string, error) {
	id, err := idgen.Next()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// buildEvent 组装一条带唯一幂等键的资金事件。出站箱与进程内总线共用同一
// EventID，消费端才能安全判重。
func buildEvent(name string, tenantID idgen.ID, payload EventPayload) (*EventData, error) {
	eventID, err := newEventID()
	if err != nil {
		return nil, err
	}
	return &EventData{
		EventID: eventID, EventName: name, TenantID: tenantID,
		OccurredAt: time.Now().UTC(), Data: payload,
	}, nil
}

// publishOutbox 在业务事务内调用（tx 上执行）：把事件写入出站箱。
// 状态翻转与事件落库必须同事务，否则翻转成功而事件丢失。
func publishOutbox(ctx context.Context, tx *gorm.DB, data *EventData) error {
	if data == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	row := Outbox{
		EventID: data.EventID, EventName: data.EventName, Payload: string(raw),
		Status: OutboxPending, NextRetryAt: time.Now().UTC(),
	}
	row.TenantID = data.TenantID
	return tx.Create(&row).Error
}
