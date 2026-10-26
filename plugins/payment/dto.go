package payment

import (
	"time"

	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

// ChannelForm 是渠道配置表单。
type ChannelForm struct {
	Code    string            `json:"code"`
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Sort    int               `json:"sort"`
	Config  map[string]string `json:"config"`
}

// ChannelView 是渠道列表视图（凭据只回是否有配置，不回内容）。
type ChannelView struct {
	ID        idgen.ID  `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	Sort      int       `json:"sort"`
	HasConfig bool      `json:"hasConfig"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// OrderView 是支付单视图；PayURL 仅在 pending 时由 Create/查询路径填充。
type OrderView struct {
	ID            idgen.ID   `json:"id"`
	PaymentNo     string     `json:"paymentNo"`
	BizType       string     `json:"bizType"`
	BizNo         string     `json:"bizNo"`
	Subject       string     `json:"subject"`
	AmountMinor   int64      `json:"amountMinor"`
	RefundedMinor int64      `json:"refundedMinor"`
	Currency      string     `json:"currency"`
	Channel       string     `json:"channel"`
	ChannelTxnID  string     `json:"channelTxnId,omitempty"`
	Status        string     `json:"status"`
	FailReason    string     `json:"failReason,omitempty"`
	PayURL        string     `json:"payUrl,omitempty"`
	PaidAt        *time.Time `json:"paidAt,omitempty"`
	ExpireAt      *time.Time `json:"expireAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// RefundView 是退款单视图。
type RefundView struct {
	ID              idgen.ID   `json:"id"`
	RefundNo        string     `json:"refundNo"`
	PaymentNo       string     `json:"paymentNo"`
	BizType         string     `json:"bizType"`
	BizNo           string     `json:"bizNo"`
	AmountMinor     int64      `json:"amountMinor"`
	Currency        string     `json:"currency"`
	Reason          string     `json:"reason,omitempty"`
	Status          string     `json:"status"`
	ChannelRefundID string     `json:"channelRefundId,omitempty"`
	SucceededAt     *time.Time `json:"succeededAt,omitempty"`
	Operator        string     `json:"operator,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

// CallbackView 是回调审计视图。
type CallbackView struct {
	ID        idgen.ID  `json:"id"`
	Channel   string    `json:"channel"`
	PaymentNo string    `json:"paymentNo"`
	OK        bool      `json:"ok"`
	Result    string    `json:"result"`
	Payload   string    `json:"payload,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func orderView(o *Order, payURL string) OrderView {
	return OrderView{
		ID: o.ID, PaymentNo: o.PaymentNo, BizType: o.BizType, BizNo: o.BizNo,
		Subject: o.Subject, AmountMinor: o.AmountMinor, RefundedMinor: o.RefundedMinor,
		Currency: o.Currency, Channel: o.Channel, ChannelTxnID: o.ChannelTxnID,
		Status: o.Status, FailReason: o.FailReason, PayURL: payURL,
		PaidAt: o.PaidAt, ExpireAt: o.ExpireAt, CreatedAt: o.CreatedAt,
	}
}

func refundView(r *Refund) RefundView {
	return RefundView{
		ID: r.ID, RefundNo: r.RefundNo, PaymentNo: r.PaymentNo, BizType: r.BizType, BizNo: r.BizNo,
		AmountMinor: r.AmountMinor, Currency: r.Currency, Reason: r.Reason, Status: r.Status,
		ChannelRefundID: r.ChannelRefundID, SucceededAt: r.SucceededAt, Operator: r.Operator, CreatedAt: r.CreatedAt,
	}
}

func callbackView(c *Callback) CallbackView {
	return CallbackView{
		ID: c.ID, Channel: c.Channel, PaymentNo: c.PaymentNo, OK: c.OK,
		Result: c.Result, Payload: c.Payload, CreatedAt: c.CreatedAt,
	}
}

// ---- core/secret 包装：测试环境没有默认密钥时退化为明文/原样 ----

func secretEncrypt(plain string) (string, error) {
	if secret.Default() == nil {
		return plain, nil
	}
	return secret.Encrypt(plain)
}

func secretDecrypt(enc string) (string, error) {
	if secret.Default() == nil || !secret.IsEncrypted(enc) {
		return enc, nil
	}
	return secret.Decrypt(enc)
}
