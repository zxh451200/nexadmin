package payment

import (
	"time"

	"nexadmin/core/database"
)

// 支付单状态：pending → paid | failed | closed；paid → partially_refunded | refunded。
// refunding 预留给异步退款渠道（P0 的 mock 退款是同步返回的）。
const (
	StatusPending           = "pending"
	StatusPaid              = "paid"
	StatusFailed            = "failed"
	StatusClosed            = "closed"
	StatusRefunding         = "refunding"
	StatusPartiallyRefunded = "partially_refunded"
	StatusRefunded          = "refunded"
)

// 退款单状态。
const (
	RefundPending   = "pending"
	RefundSucceeded = "succeeded"
	RefundFailed    = "failed"
)

// Settleable 报告支付单是否处于可退款状态。
func Settleable(status string) bool {
	return status == StatusPaid || status == StatusRefunding || status == StatusPartiallyRefunded
}

// Order 是一笔支付单（平台级：渠道收款方是平台自身）。
// 金额一律 minor units（分）；biz_type + biz_no 是业务方的幂等键，联合唯一。
type Order struct {
	database.PlatformModel
	PaymentNo     string     `gorm:"size:40;not null;uniqueIndex:uk_plg_payment_order_no" json:"paymentNo"`
	BizType       string     `gorm:"size:32;not null;uniqueIndex:uk_plg_payment_order_biz,priority:1" json:"bizType"`
	BizNo         string     `gorm:"size:64;not null;uniqueIndex:uk_plg_payment_order_biz,priority:2" json:"bizNo"`
	Subject       string     `gorm:"size:200;not null" json:"subject"`
	AmountMinor   int64      `gorm:"not null" json:"amountMinor"`
	Currency      string     `gorm:"size:8;not null" json:"currency"`
	Channel       string     `gorm:"size:32;not null" json:"channel"`
	ChannelTxnID  string     `gorm:"size:128" json:"channelTxnId"`
	Status        string     `gorm:"size:24;not null;index" json:"status"`
	FailReason    string     `gorm:"size:200" json:"failReason,omitempty"`
	RefundedMinor int64      `gorm:"not null;default:0" json:"refundedMinor"`
	PaidAt        *time.Time `json:"paidAt,omitempty"`
	ExpireAt      *time.Time `json:"expireAt,omitempty"`
}

func (Order) TableName() string { return "plg_payment_order" }

// Channel 是一个支付渠道配置。Code 即 Provider 名（如 mock），凭据整体加密存放。
type Channel struct {
	database.PlatformModel
	Code    string `gorm:"size:32;not null;uniqueIndex:uk_plg_payment_channel_code" json:"code"`
	Name    string `gorm:"size:64;not null" json:"name"`
	Enabled bool   `json:"enabled"`
	Sort    int    `json:"sort"`
	// ConfigJSON 是加密后的渠道配置 JSON（含密钥），见 core/secret。
	ConfigJSON string `gorm:"size:512" json:"-"`
}

func (Channel) TableName() string { return "plg_payment_channel" }

// Callback 是一次渠道回调（或 mock 收银台触发）的审计记录，无论验签成败都落一行。
type Callback struct {
	database.PlatformModel
	Channel   string `gorm:"size:32;not null;index" json:"channel"`
	PaymentNo string `gorm:"size:40;index" json:"paymentNo"`
	OK        bool   `json:"ok"`
	Result    string `gorm:"size:200" json:"result"`
	Payload   string `gorm:"type:text" json:"payload"`
}

func (Callback) TableName() string { return "plg_payment_callback" }

// Refund 是一笔退款单；biz_type + biz_no 由退款发起方给幂等键。
type Refund struct {
	database.PlatformModel
	RefundNo        string     `gorm:"size:40;not null;uniqueIndex:uk_plg_payment_refund_no" json:"refundNo"`
	PaymentNo       string     `gorm:"size:40;not null;index" json:"paymentNo"`
	BizType         string     `gorm:"size:32;not null;uniqueIndex:uk_plg_payment_refund_biz,priority:1" json:"bizType"`
	BizNo           string     `gorm:"size:64;not null;uniqueIndex:uk_plg_payment_refund_biz,priority:2" json:"bizNo"`
	AmountMinor     int64      `gorm:"not null" json:"amountMinor"`
	Currency        string     `gorm:"size:8;not null" json:"currency"`
	Reason          string     `gorm:"size:200" json:"reason"`
	Status          string     `gorm:"size:16;not null;index" json:"status"`
	ChannelRefundID string     `gorm:"size:128" json:"channelRefundId"`
	SucceededAt     *time.Time `json:"succeededAt,omitempty"`
	Operator        string     `gorm:"size:64" json:"operator,omitempty"`
}

func (Refund) TableName() string { return "plg_payment_refund" }
