package pay

import (
	"time"

	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

// ChannelForm 是渠道配置表单（契约 ChannelForm）。config 缺省/为空表示保留既有凭据。
type ChannelForm struct {
	Code      string            `json:"code"`
	Name      string            `json:"name"`
	Provider  string            `json:"provider"`
	Method    string            `json:"method"`
	Scene     string            `json:"scene"`
	SubjectID string            `json:"subjectId"`
	FeeRateBp int64             `json:"feeRateBp"`
	NotifyURL string            `json:"notifyUrl"`
	Enabled   bool              `json:"enabled"`
	Sort      int               `json:"sort"`
	Config    map[string]string `json:"config"`
}

// ChannelView 是渠道视图（凭据只回是否有配置，不回内容）。
type ChannelView struct {
	ID        idgen.ID  `json:"id"`
	TenantID  idgen.ID  `json:"tenantId"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Method    string    `json:"method"`
	Scene     string    `json:"scene"`
	SubjectID string    `json:"subjectId"`
	FeeRateBp int64     `json:"feeRateBp"`
	NotifyURL string    `json:"notifyUrl"`
	Enabled   bool      `json:"enabled"`
	Sort      int       `json:"sort"`
	HasConfig bool      `json:"hasConfig"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func channelToView(ch *Channel) *ChannelView {
	if ch == nil {
		return nil
	}
	return &ChannelView{
		ID: ch.ID, TenantID: ch.TenantID, Code: ch.Code, Name: ch.Name, Provider: ch.Provider,
		Method: ch.Method, Scene: ch.Scene, SubjectID: ch.SubjectID, FeeRateBp: ch.FeeRateBp,
		NotifyURL: ch.NotifyURL, Enabled: ch.Enabled, Sort: ch.Sort,
		HasConfig: ch.ConfigJSON != "", UpdatedAt: ch.UpdatedAt,
	}
}

// OrderView 是支付单视图（契约 PayOrder）。payUrl/payParams 只在下单响应返回，
// 查询接口一律脱敏。
type OrderView struct {
	ID              idgen.ID          `json:"id"`
	TenantID        idgen.ID          `json:"tenantId"`
	SubjectType     string            `json:"subjectType"`
	SubjectID       string            `json:"subjectId"`
	PaymentNo       string            `json:"paymentNo"`
	BizType         string            `json:"bizType"`
	BizNo           string            `json:"bizNo"`
	ChannelCode     string            `json:"channelCode"`
	Method          string            `json:"method"`
	Scene           string            `json:"scene"`
	Status          string            `json:"status"`
	AmountMinor     int64             `json:"amountMinor"`
	RefundedMinor   int64             `json:"refundedMinor"`
	FeeMinor        int64             `json:"feeMinor"`
	Currency        string            `json:"currency"`
	FxRateMicro     int64             `json:"fxRateMicro"`
	AmountOrigMinor int64             `json:"amountOrigMinor"`
	OrigCurrency    string            `json:"origCurrency"`
	ChannelTxnID    string            `json:"channelTxnId,omitempty"`
	PrepayID        string            `json:"prepayId,omitempty"`
	CodeURL         string            `json:"codeUrl,omitempty"`
	GatewayStatus   string            `json:"gatewayStatus,omitempty"`
	FailReason      string            `json:"failReason,omitempty"`
	PayerID         string            `json:"payerId,omitempty"`
	OperatorID      string            `json:"operatorId,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	PayURL          string            `json:"payUrl,omitempty"`
	PayParams       map[string]string `json:"payParams,omitempty"`
	PaidAt          *time.Time        `json:"paidAt,omitempty"`
	ExpireAt        *time.Time        `json:"expireAt,omitempty"`
	ClosedAt        *time.Time        `json:"closedAt,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
}

func orderView(o *Order, params *PayParams) OrderView {
	v := OrderView{
		ID: o.ID, TenantID: o.TenantID, SubjectType: o.SubjectType, SubjectID: o.SubjectID,
		PaymentNo: o.PaymentNo, BizType: o.BizType, BizNo: o.BizNo,
		ChannelCode: o.ChannelCode, Method: o.Method, Scene: o.Scene, Status: o.Status,
		AmountMinor: o.AmountMinor, RefundedMinor: o.RefundedMinor, FeeMinor: o.FeeMinor,
		Currency: o.Currency, FxRateMicro: o.FxRateMicro,
		AmountOrigMinor: o.AmountOrigMinor, OrigCurrency: o.OrigCurrency,
		ChannelTxnID: txnIDOf(o), PrepayID: o.PrepayID, CodeURL: o.CodeURL,
		GatewayStatus: o.GatewayStatus, FailReason: o.FailReason,
		PayerID: o.PayerID, OperatorID: o.OperatorID, Metadata: metaOf(o),
		PaidAt: o.PaidAt, ExpireAt: o.ExpireAt, ClosedAt: o.ClosedAt, CreatedAt: o.CreatedAt,
	}
	if params != nil {
		v.PayURL, v.CodeURL, v.PrepayID = params.PayURL, params.CodeURL, params.PrepayID
		v.PayParams = params.Params
	}
	return v
}

// RefundView 是退款单视图（契约 Refund）。
type RefundView struct {
	ID              idgen.ID   `json:"id"`
	TenantID        idgen.ID   `json:"tenantId"`
	RefundNo        string     `json:"refundNo"`
	PaymentNo       string     `json:"paymentNo"`
	SubjectID       string     `json:"subjectId"`
	BizType         string     `json:"bizType"`
	BizNo           string     `json:"bizNo"`
	Status          string     `json:"status"`
	AmountMinor     int64      `json:"amountMinor"`
	FeeRefundMinor  int64      `json:"feeRefundMinor"`
	Currency        string     `json:"currency"`
	FxRateMicro     int64      `json:"fxRateMicro"`
	ChannelRefundID string     `json:"channelRefundId,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	FailReason      string     `json:"failReason,omitempty"`
	OperatorID      string     `json:"operatorId,omitempty"`
	SucceededAt     *time.Time `json:"succeededAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

func refundView(r *Refund) RefundView {
	return RefundView{
		ID: r.ID, TenantID: r.TenantID, RefundNo: r.RefundNo, PaymentNo: r.PaymentNo,
		SubjectID: r.SubjectID, BizType: r.BizType, BizNo: r.BizNo, Status: r.Status,
		AmountMinor: r.AmountMinor, FeeRefundMinor: r.FeeRefundMinor,
		Currency: r.Currency, FxRateMicro: r.FxRateMicro,
		ChannelRefundID: r.ChannelRefundID, Reason: r.Reason, FailReason: r.FailReason,
		OperatorID: r.OperatorID, SucceededAt: r.SucceededAt, CreatedAt: r.CreatedAt,
	}
}

// CallbackView 是回调审计视图（payload 已截断，入库前不含凭据字段——由渠道实现保证）。
type CallbackView struct {
	ID          idgen.ID  `json:"id"`
	ChannelCode string    `json:"channelCode"`
	PaymentNo   string    `json:"paymentNo"`
	OK          bool      `json:"ok"`
	Result      string    `json:"result"`
	Payload     string    `json:"payload,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func callbackView(c *Callback) CallbackView {
	return CallbackView{
		ID: c.ID, ChannelCode: c.ChannelCode, PaymentNo: c.PaymentNo, OK: c.OK,
		Result: c.Result, Payload: c.Payload, CreatedAt: c.CreatedAt,
	}
}

// AuthView 是预授权视图（契约 Authorization）。
type AuthView struct {
	ID            idgen.ID   `json:"id"`
	TenantID      idgen.ID   `json:"tenantId"`
	AuthNo        string     `json:"authNo"`
	SubjectID     string     `json:"subjectId"`
	BizType       string     `json:"bizType"`
	BizNo         string     `json:"bizNo"`
	PaymentNo     string     `json:"paymentNo,omitempty"`
	Status        string     `json:"status"`
	AmountMinor   int64      `json:"amountMinor"`
	CapturedMinor int64      `json:"capturedMinor"`
	Currency      string     `json:"currency"`
	FxRateMicro   int64      `json:"fxRateMicro"`
	ChannelAuthNo string     `json:"channelAuthNo,omitempty"`
	CardLast4     string     `json:"cardLast4,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	VoidedAt      *time.Time `json:"voidedAt,omitempty"`
}

func authView(a *Authorization) AuthView {
	return AuthView{
		ID: a.ID, TenantID: a.TenantID, AuthNo: a.AuthNo, SubjectID: a.SubjectID,
		BizType: a.BizType, BizNo: a.BizNo, PaymentNo: a.PaymentNo, Status: a.Status,
		AmountMinor: a.AmountMinor, CapturedMinor: a.CapturedMinor,
		Currency: a.Currency, FxRateMicro: a.FxRateMicro,
		ChannelAuthNo: a.ChannelAuthNo, CardLast4: a.CardLast4,
		ExpiresAt: a.ExpiresAt, VoidedAt: a.VoidedAt,
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
