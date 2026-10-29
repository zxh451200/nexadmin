package pay

import (
	"time"

	"nexadmin/core/database"
)

// 支付单状态机（02-openapi-pay-v1.yaml OrderStatus）：
//
//	pending → succeeded | failed | closed
//	succeeded → refunding → partially_refunded | refunded
//
// 旧值 paid 在 nexMall 侧的兼容期由 Phase 1 平迁负责，中台只写新值。
const (
	StatusPending           = "pending"
	StatusSucceeded         = "succeeded"
	StatusFailed            = "failed"
	StatusClosed            = "closed"
	StatusRefunding         = "refunding"
	StatusPartiallyRefunded = "partially_refunded"
	StatusRefunded          = "refunded"
)

// 退款单状态（RefundStatus）。
const (
	RefundPending   = "pending"
	RefundSucceeded = "succeeded"
	RefundFailed    = "failed"
)

// 预授权状态（AuthStatus）。
const (
	AuthAuthorized      = "authorized"
	AuthPartialCaptured = "partial_captured"
	AuthCaptured        = "captured"
	AuthVoided          = "voided"
	AuthExpired         = "expired"
)

// Settleable 报告支付单是否处于可退款状态。
func Settleable(status string) bool {
	return status == StatusSucceeded || status == StatusRefunding || status == StatusPartiallyRefunded
}

// Freezable 报告预授权是否还能请款或撤销。
func Freezable(status string) bool {
	return status == AuthAuthorized || status == AuthPartialCaptured
}

// Order 是一笔支付单。资金事实一律租户级（TenantModel），由框架回调注入 tenant_id，
// 取消 nexMall 平台代收单的 TenantID=0 特例；平台账户建模为保留租户下的一个 subject。
// 金额一律最小货币单位（minor），汇率 1e-6 定点，费率基点——禁止 float 落库。
type Order struct {
	database.TenantModel
	PaymentNo       string `gorm:"size:40;not null;uniqueIndex:uk_pay_order_no" json:"paymentNo"`
	BizType         string `gorm:"size:32;not null;uniqueIndex:uk_pay_order_biz,priority:2" json:"bizType"`
	BizNo           string `gorm:"size:64;not null;uniqueIndex:uk_pay_order_biz,priority:3" json:"bizNo"`
	SubjectType     string `gorm:"size:32;not null;default:''" json:"subjectType"`
	SubjectID       string `gorm:"size:64;not null;default:'';index" json:"subjectId"`
	Subject         string `gorm:"size:200;not null" json:"subject"`
	AmountMinor     int64  `gorm:"not null" json:"amountMinor"`
	RefundedMinor   int64  `gorm:"not null;default:0" json:"refundedMinor"`
	FeeMinor        int64  `gorm:"not null;default:0" json:"feeMinor"`
	Currency        string `gorm:"size:8;not null" json:"currency"`
	FxRateMicro     int64  `gorm:"not null;default:1000000" json:"fxRateMicro"`
	AmountOrigMinor int64  `gorm:"not null;default:0" json:"amountOrigMinor"`
	OrigCurrency    string `gorm:"size:8;not null;default:''" json:"origCurrency"`
	ChannelCode     string `gorm:"size:32;not null;uniqueIndex:uk_pay_order_channel_txn,priority:1" json:"channelCode"`
	Method          string `gorm:"size:32;not null;default:''" json:"method"`
	Scene           string `gorm:"size:32;not null;default:''" json:"scene"`
	// ChannelTxnID 与渠道号组成唯一索引；NULL 表示渠道侧尚无流水（MySQL/PG 唯一索引都放行多个 NULL）。
	ChannelTxnID  *string    `gorm:"size:128;uniqueIndex:uk_pay_order_channel_txn,priority:2" json:"channelTxnId"`
	PrepayID      string     `gorm:"size:128;not null;default:''" json:"prepayId"`
	CodeURL       string     `gorm:"size:512;not null;default:''" json:"codeUrl"`
	GatewayStatus string     `gorm:"size:64;not null;default:''" json:"gatewayStatus"`
	FailReason    string     `gorm:"size:200;not null;default:''" json:"failReason"`
	PayerID       string     `gorm:"size:64;not null;default:''" json:"payerId"`
	PayerOpenID   string     `gorm:"size:128;not null;default:''" json:"payerOpenId"`
	OperatorID    string     `gorm:"size:64;not null;default:''" json:"operatorId"`
	MetadataJSON  string     `gorm:"type:text" json:"-"`
	Status        string     `gorm:"size:24;not null;index" json:"status"`
	PaidAt        *time.Time `json:"paidAt,omitempty"`
	ExpireAt      *time.Time `json:"expireAt,omitempty"`
	ClosedAt      *time.Time `json:"closedAt,omitempty"`
}

func (Order) TableName() string { return "plg_pay_order" }

// Channel 是一个渠道商户配置实例。Code 全局唯一（公开回调只带 channelCode，
// 必须能无租户定位到唯一配置）；Provider 指向已注册的渠道实现；凭据整体加密存放。
type Channel struct {
	database.TenantModel
	Code     string `gorm:"size:32;not null;uniqueIndex:uk_pay_channel_code" json:"code"`
	Name     string `gorm:"size:64;not null" json:"name"`
	Provider string `gorm:"size:32;not null" json:"provider"`
	Method   string `gorm:"size:32;not null;default:''" json:"method"`
	Scene    string `gorm:"size:32;not null;default:''" json:"scene"`
	// SubjectID 空 = 租户级共享渠道；非空 = 主体专属。
	SubjectID string `gorm:"size:64;not null;default:'';index" json:"subjectId"`
	FeeRateBp int64  `gorm:"not null;default:0" json:"feeRateBp"`
	NotifyURL string `gorm:"size:255;not null;default:''" json:"notifyUrl"`
	Enabled   bool   `gorm:"not null;default:true" json:"enabled"`
	Sort      int    `gorm:"not null;default:0" json:"sort"`
	// ConfigJSON 是加密后的渠道凭据 JSON，见 core/secret；任何读接口只回显 hasConfig。
	ConfigJSON string `gorm:"size:1024" json:"-"`
}

func (Channel) TableName() string { return "plg_pay_channel" }

// Refund 是一笔退款单。fx 沿用原单快照，不重新取牌价。
type Refund struct {
	database.TenantModel
	RefundNo        string     `gorm:"size:40;not null;uniqueIndex:uk_pay_refund_no" json:"refundNo"`
	PaymentNo       string     `gorm:"size:40;not null;index" json:"paymentNo"`
	SubjectID       string     `gorm:"size:64;not null;default:''" json:"subjectId"`
	BizType         string     `gorm:"size:32;not null;uniqueIndex:uk_pay_refund_biz,priority:2" json:"bizType"`
	BizNo           string     `gorm:"size:64;not null;uniqueIndex:uk_pay_refund_biz,priority:3" json:"bizNo"`
	AmountMinor     int64      `gorm:"not null" json:"amountMinor"`
	FeeRefundMinor  int64      `gorm:"not null;default:0" json:"feeRefundMinor"`
	Currency        string     `gorm:"size:8;not null" json:"currency"`
	FxRateMicro     int64      `gorm:"not null;default:1000000" json:"fxRateMicro"`
	Status          string     `gorm:"size:16;not null;index" json:"status"`
	ChannelRefundID string     `gorm:"size:128;not null;default:''" json:"channelRefundId"`
	Reason          string     `gorm:"size:200;not null;default:''" json:"reason"`
	FailReason      string     `gorm:"size:200;not null;default:''" json:"failReason"`
	OperatorID      string     `gorm:"size:64;not null;default:''" json:"operatorId"`
	SucceededAt     *time.Time `json:"succeededAt,omitempty"`
}

func (Refund) TableName() string { return "plg_pay_refund" }

// Callback 是一次渠道回调（或 mock 收银台触发）的审计记录，无论验签成败都落一行。
type Callback struct {
	database.TenantModel
	ChannelCode string `gorm:"size:32;not null;index:idx_pay_callback_no,priority:1" json:"channelCode"`
	PaymentNo   string `gorm:"size:40;not null;default:'';index:idx_pay_callback_no,priority:2" json:"paymentNo"`
	OK          bool   `json:"ok"`
	Result      string `gorm:"size:32;not null;default:''" json:"result"`
	Payload     string `gorm:"type:text" json:"-"`
}

func (Callback) TableName() string { return "plg_pay_callback" }

// Authorization 是一笔预授权（源自 nexHotel biz_preauth，泛化 hotel_id → subject_id）。
// 只存卡号末四位，禁存卡号（pkg/mask BankCard 口径）。
type Authorization struct {
	database.TenantModel
	AuthNo        string     `gorm:"size:40;not null;uniqueIndex:uk_pay_auth_no" json:"authNo"`
	BizType       string     `gorm:"size:32;not null;uniqueIndex:uk_pay_auth_biz,priority:2" json:"bizType"`
	BizNo         string     `gorm:"size:64;not null;uniqueIndex:uk_pay_auth_biz,priority:3" json:"bizNo"`
	SubjectID     string     `gorm:"size:64;not null;default:''" json:"subjectId"`
	ChannelCode   string     `gorm:"size:32;not null" json:"channelCode"`
	Method        string     `gorm:"size:32;not null;default:''" json:"method"`
	Status        string     `gorm:"size:20;not null;index" json:"status"`
	AmountMinor   int64      `gorm:"not null" json:"amountMinor"`
	CapturedMinor int64      `gorm:"not null;default:0" json:"capturedMinor"`
	Currency      string     `gorm:"size:8;not null" json:"currency"`
	FxRateMicro   int64      `gorm:"not null;default:1000000" json:"fxRateMicro"`
	ChannelAuthNo string     `gorm:"size:128;not null;default:''" json:"channelAuthNo"`
	CardLast4     string     `gorm:"size:8;not null;default:''" json:"cardLast4"`
	Remark        string     `gorm:"size:500;not null;default:''" json:"remark"`
	PaymentNo     string     `gorm:"size:40;not null;default:''" json:"paymentNo"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	VoidedAt      *time.Time `json:"voidedAt,omitempty"`
}

func (Authorization) TableName() string { return "plg_pay_auth" }

// ReconcileTask 是一次对账任务（源自 nexHotel biz_reconcile_task 骨架）。
// Phase 0 渠道金额由入参提供（source=manual），Phase 4 改为渠道账单 API 拉取。
type ReconcileTask struct {
	database.TenantModel
	SubjectID         string `gorm:"size:64;not null;default:'';uniqueIndex:uk_pay_reconcile_task,priority:2" json:"subjectId"`
	ChannelCode       string `gorm:"size:32;not null;uniqueIndex:uk_pay_reconcile_task,priority:3" json:"channelCode"`
	BillDate          string `gorm:"size:10;not null;uniqueIndex:uk_pay_reconcile_task,priority:4" json:"billDate"`
	Status            string `gorm:"size:16;not null" json:"status"`
	TotalLocalMinor   int64  `gorm:"not null;default:0" json:"totalLocalMinor"`
	TotalChannelMinor int64  `gorm:"not null;default:0" json:"totalChannelMinor"`
	DiffCount         int    `gorm:"not null;default:0" json:"diffCount"`
	Source            string `gorm:"size:16;not null;default:'manual'" json:"source"`
}

func (ReconcileTask) TableName() string { return "plg_pay_reconcile_task" }

// ReconcileDiff 是一条对账差异：long 渠道多于本地（长款）、short 渠道少于本地（短款）、
// missing 单边缺失。DiffAmountMinor = local - channel。
type ReconcileDiff struct {
	database.TenantModel
	TaskID             int64      `gorm:"not null;index" json:"taskId"`
	Kind               string     `gorm:"size:16;not null" json:"kind"`
	PaymentNo          string     `gorm:"size:40;not null;default:''" json:"paymentNo"`
	RefundNo           string     `gorm:"size:40;not null;default:''" json:"refundNo"`
	LocalAmountMinor   int64      `gorm:"not null;default:0" json:"localAmountMinor"`
	ChannelAmountMinor int64      `gorm:"not null;default:0" json:"channelAmountMinor"`
	DiffAmountMinor    int64      `gorm:"not null;default:0" json:"diffAmountMinor"`
	Status             string     `gorm:"size:16;not null;default:'open'" json:"status"`
	ResolveNote        string     `gorm:"size:500;not null;default:''" json:"resolveNote"`
	ResolvedBy         string     `gorm:"size:64;not null;default:''" json:"resolvedBy"`
	ResolvedAt         *time.Time `json:"resolvedAt,omitempty"`
}

func (ReconcileDiff) TableName() string { return "plg_pay_reconcile_diff" }

// CommissionRule 是抽佣费率：维度 = 主体 + 业务类型（mall 的商品类目维度泛化而来）。
// SubjectID 空 = 租户默认规则；命中优先级为「主体精确 > 租户默认」。
type CommissionRule struct {
	database.TenantModel
	SubjectID  string `gorm:"size:64;not null;default:'';uniqueIndex:uk_pay_commission_rule,priority:2" json:"subjectId"`
	BizType    string `gorm:"size:32;not null;uniqueIndex:uk_pay_commission_rule,priority:3" json:"bizType"`
	RateBp     int64  `gorm:"not null;default:0" json:"rateBp"`
	FixedMinor int64  `gorm:"not null;default:0" json:"fixedMinor"`
	Currency   string `gorm:"size:8;not null;default:'CNY'" json:"currency"`
	Status     string `gorm:"size:16;not null;default:'active'" json:"status"`
}

func (CommissionRule) TableName() string { return "plg_pay_commission_rule" }

// Settlement 是一期结算单：金额生成后不随订单变化，后发退款进下期。
type Settlement struct {
	database.TenantModel
	SubjectID       string `gorm:"size:64;not null;default:'';uniqueIndex:uk_pay_settlement,priority:2" json:"subjectId"`
	PeriodStart     string `gorm:"size:10;not null;uniqueIndex:uk_pay_settlement,priority:3" json:"periodStart"`
	PeriodEnd       string `gorm:"size:10;not null;uniqueIndex:uk_pay_settlement,priority:4" json:"periodEnd"`
	GrossMinor      int64  `gorm:"not null;default:0" json:"grossMinor"`
	CommissionMinor int64  `gorm:"not null;default:0" json:"commissionMinor"`
	RefundMinor     int64  `gorm:"not null;default:0" json:"refundMinor"`
	FeeMinor        int64  `gorm:"not null;default:0" json:"feeMinor"`
	NetMinor        int64  `gorm:"not null;default:0" json:"netMinor"`
	OrderCount      int64  `gorm:"not null;default:0" json:"orderCount"`
	Currency        string `gorm:"size:8;not null" json:"currency"`
	Status          string `gorm:"size:16;not null;default:'draft'" json:"status"`
}

func (Settlement) TableName() string { return "plg_pay_settlement" }

// PayoutBatch 是打款批次（人工回执号；渠道自动分账为 Phase 4）。
type PayoutBatch struct {
	database.TenantModel
	BatchNo         string     `gorm:"size:40;not null;uniqueIndex:uk_pay_payout_no" json:"batchNo"`
	Status          string     `gorm:"size:16;not null;default:'draft'" json:"status"`
	TotalMinor      int64      `gorm:"not null;default:0" json:"totalMinor"`
	Currency        string     `gorm:"size:8;not null" json:"currency"`
	SettlementCount int        `gorm:"not null;default:0" json:"settlementCount"`
	Method          string     `gorm:"size:32;not null;default:''" json:"method"`
	Note            string     `gorm:"size:500;not null;default:''" json:"note"`
	ReceiptNo       string     `gorm:"size:64;not null;default:''" json:"receiptNo"`
	PaidAt          *time.Time `json:"paidAt,omitempty"`
}

func (PayoutBatch) TableName() string { return "plg_pay_payout_batch" }

// PayoutItem 是批次明细；SettlementID 唯一，保证一张结算单只进一个批次。
type PayoutItem struct {
	database.TenantModel
	BatchID      int64 `gorm:"not null;index" json:"batchId"`
	SettlementID int64 `gorm:"not null;uniqueIndex:uk_pay_payout_item_settlement" json:"settlementId"`
	NetMinor     int64 `gorm:"not null;default:0" json:"netMinor"`
}

func (PayoutItem) TableName() string { return "plg_pay_payout_item" }

// Outbox 是资金事件的持久化出站箱：与状态翻转同事务落库，随后进程内发布；
// 进程内总线丢事件（缓冲满）时由 dispatcher 重投。事件载荷里禁放凭据。
type Outbox struct {
	database.TenantModel
	EventID     string     `gorm:"size:40;not null;uniqueIndex:uk_pay_outbox_event" json:"eventId"`
	EventName   string     `gorm:"size:48;not null" json:"eventName"`
	Payload     string     `gorm:"type:text" json:"-"`
	Status      string     `gorm:"size:16;not null;index:idx_pay_outbox_retry,priority:1" json:"status"`
	Attempts    int        `gorm:"not null;default:0" json:"attempts"`
	NextRetryAt time.Time  `gorm:"not null;index:idx_pay_outbox_retry,priority:2" json:"nextRetryAt"`
	SentAt      *time.Time `json:"sentAt,omitempty"`
}

func (Outbox) TableName() string { return "plg_pay_outbox" }

// IdemKey 是请求级幂等记录（Idempotency-Key 头）。同键同体重放首次响应，
// 同键异体 422。业务键（biz_type+biz_no）幂等仍由唯一索引兜底，两者互补。
type IdemKey struct {
	database.TenantModel
	IdemKey    string `gorm:"size:64;not null;uniqueIndex:uk_pay_idem,priority:2" json:"idemKey"`
	Path       string `gorm:"size:128;not null;default:''" json:"path"`
	ReqHash    string `gorm:"size:64;not null" json:"reqHash"`
	RespStatus int    `gorm:"not null;default:0" json:"respStatus"`
	RespBody   string `gorm:"type:text" json:"-"`
}

func (IdemKey) TableName() string { return "plg_pay_idem" }
