package pay

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"nexadmin/core/response"
)

// Provider 是支付渠道抽象。真实渠道（微信/支付宝/易支付）按市场实现后用
// RegisterProvider 挂载；所有实现必须满足三条铁律：
//  1. 金额由服务端订单决定，绝不接受客户端金额；
//  2. 回调必须验签，并按该渠道的时间戳语义做防重放：
//     「每次下发都重新生成、且参与签名」的时间戳（微信 APIv3 的 Wechatpay-Timestamp、
//     mock、易支付带 timestamp 的变体）用 CallbackClockSkew 双向硬窗；
//     「业务通知时间、渠道重试时不刷新」的（支付宝 notify_time）只能用于拒绝超前
//     于本地的时钟（伪造/时钟异常信号），过去方向不设硬限——否则渠道第 4 次以后的
//     合法重试会被丢单。两种语义不能混为一谈，接新渠道前先判断它属于哪一种。
//     真正的重复入账防线是签名内的 out_trade_no + 金额核对 + 状态机幂等。
//  3. 同一支付单重复回调/查询幂等。
type Provider interface {
	Name() string
	// Create 预创建渠道侧订单，返回拉起支付的参数（跳转 URL、二维码、JSAPI 参数等）；须按 paymentNo 幂等。
	Create(ctx context.Context, req PayRequest, cfg map[string]string) (*PayParams, error)
	// Query 主动查询渠道侧支付结果（事件丢失时的补偿路径）。
	Query(ctx context.Context, paymentNo string, cfg map[string]string) (*ChannelStatus, error)
	// Refund 发起渠道退款。同步渠道立即返回 OK/拒绝；异步渠道返回 Pending=true，由回调翻转。
	Refund(ctx context.Context, req RefundRequest, cfg map[string]string) (*RefundResult, error)
	// VerifyCallback 验签并解析渠道回调报文；验签失败必须返回 error。
	VerifyCallback(in CallbackInput, cfg map[string]string) (*CallbackResult, error)
}

// PayRequest 是 Create 的入参。
type PayRequest struct {
	PaymentNo   string
	Subject     string
	AmountMinor int64
	Currency    string
	Method      string // native/qr=扫码；page=电脑网站；wap/h5=手机网站；jsapi=公众号；app=APP
	Scene       string
	PayerOpenID string    // 微信 JSAPI 必填（公众号/小程序 openid）
	ClientIP    string    // 微信 H5 下单需要付款方 IP
	ReturnURL   string    // 支付完成后回到业务前端的地址（可为空）
	NotifyURL   string    // 渠道异步回调地址；真实网关必须是完整 URL（见 channels.notify_url）
	ExpireAt    time.Time // 支付单过期时间，渠道可据此设置 timeout_express / time_expire
}

// PayParams 是渠道返回的拉起支付参数；PayURL 为空表示用 Params 自行组装。
type PayParams struct {
	PayURL   string
	CodeURL  string
	PrepayID string
	Params   map[string]string
}

// ChannelStatus 是 Query 的结果；Paid/Failed 都为 false 表示渠道侧仍未定。
type ChannelStatus struct {
	Paid          bool
	Failed        bool
	ChannelTxnID  string
	GatewayStatus string
	Reason        string
}

// RefundRequest 是 Refund 的入参。
type RefundRequest struct {
	RefundNo     string
	PaymentNo    string
	ChannelTxnID string
	AmountMinor  int64
	// OrderAmountMinor 是原支付单金额，微信退款接口要求带上；其他渠道可忽略。
	OrderAmountMinor int64
	// IdempotencyKey 是渠道侧的退款幂等号（out_request_no / out_refund_no）。
	// 由支付单号与业务幂等键确定性派生：即使本地事务回滚后重试，也不会重复退款。
	IdempotencyKey string
	NotifyURL      string // 退款结果异步通知地址（可为空）
}

// RefundResult 是渠道退款结果；OK=false 且 Pending=false 表示渠道拒绝（落 failed），
// Pending=true 表示已受理待回调（落 pending，支付单进 refunding）。
type RefundResult struct {
	OK              bool
	Pending         bool
	ChannelRefundID string
	Reason          string
}

// CallbackInput 是渠道回调的原始输入（notify 端点收到什么就传什么）。
type CallbackInput struct {
	Channel string
	Body    []byte
	Headers map[string]string
}

// ---- 渠道可选能力 ----
// 用类型断言获取（同 SchemaProvider/AckProvider 的形态）。未实现的能力必须在服务层
// 返回 errCapabilityUnsupported（HTTP 400 + PAY_CAPABILITY_UNSUPPORTED），
// 禁止伪造渠道流水号、禁止本地假成功——资金中台里「看起来成功了但渠道侧没有」
// 比明确失败危险得多。前端按 Capabilities 禁用入口，服务端仍要独立判一次。

// Capabilities 是渠道能力矩阵：决定币种/支付方式下拉与退款查询、预授权、账单、
// 代付四个动作在该渠道是否可用。
type Capabilities struct {
	// Currencies 是该渠道可直接收款的币种；空表示只收 CNY。
	Currencies []string `json:"currencies"`
	// Methods 是支持的下单方式取值：native/qr=扫码、page=电脑网站、wap/h5=手机网站、
	// jsapi=公众号或小程序、app=APP、micropay=付款码当面付。
	Methods []string `json:"methods"`
	// RefundQuery 表示能主动向渠道查退款结果（回调丢失时的补偿路径）。
	RefundQuery bool `json:"refundQuery"`
	// Auth 表示支持资金预授权冻结/请款/撤销（需商户在该渠道签约资金授权产品）。
	Auth bool `json:"auth"`
	// Bill 表示能下载渠道账单，对账可用 source=channel。
	Bill bool `json:"bill"`
	// Payout 表示能向收款方代付/转账，结算打款可走渠道而不是人工回执。
	Payout bool `json:"payout"`
}

// CapabilityProvider 声明自己的真实能力；未实现的 Provider 用零值（全不支持）。
type CapabilityProvider interface {
	Capabilities() Capabilities
}

// RefundQueryRequest 是退款结果查询的入参；IdempotencyKey 必须与发起退款时同一个，
// 渠道才认得出这是同一笔退款。
type RefundQueryRequest struct {
	RefundNo         string
	PaymentNo        string
	ChannelTxnID     string
	IdempotencyKey   string
	AmountMinor      int64
	OrderAmountMinor int64
}

// RefundStatusResult 是渠道侧退款结论。Found=false 表示渠道查无此退款
// （既不是成功也不是失败，服务层保持 pending 并告警，等人工核对）。
type RefundStatusResult struct {
	Found           bool
	OK              bool
	Pending         bool
	ChannelRefundID string
	Reason          string
}

// RefundQueryProvider 支持主动查退款结果。
type RefundQueryProvider interface {
	QueryRefund(ctx context.Context, req RefundQueryRequest, cfg map[string]string) (*RefundStatusResult, error)
}

// AuthFreezeRequest 是资金预授权冻结入参。PayerID 是渠道侧买家标识
// （支付宝 buyer_id / buyer_logon_id）；渠道要求买家存在时为空即报凭据错误。
type AuthFreezeRequest struct {
	AuthNo      string
	AmountMinor int64
	Currency    string
	Subject     string
	PayerID     string
	NotifyURL   string
	ExpireAt    time.Time
	Remark      string
}

// AuthCompleteRequest 是请款（解冻扣款）入参；AmountMinor 可小于冻结额（部分请款）。
// PaymentNo 是中台侧请款生成的收款单号，作为渠道侧幂等号。
type AuthCompleteRequest struct {
	AuthNo        string
	ChannelAuthNo string
	AmountMinor   int64
	PaymentNo     string
	NotifyURL     string
	Remark        string
}

// AuthCancelRequest 是撤销冻结入参，释放剩余额度。
type AuthCancelRequest struct {
	AuthNo        string
	ChannelAuthNo string
	Reason        string
}

// AuthResult 是渠道侧授权动作结论。ChannelAuthNo 必须是渠道真实返回的授权号。
type AuthResult struct {
	ChannelAuthNo string
	Status        string
	AmountMinor   int64
	Reason        string
}

// AuthProvider 支持资金预授权的四个渠道动作。
type AuthProvider interface {
	Freeze(ctx context.Context, in AuthFreezeRequest, cfg map[string]string) (*AuthResult, error)
	CompleteAuth(ctx context.Context, in AuthCompleteRequest, cfg map[string]string) (*AuthResult, error)
	CancelAuth(ctx context.Context, in AuthCancelRequest, cfg map[string]string) (*AuthResult, error)
	QueryAuth(ctx context.Context, authNo string, cfg map[string]string) (*AuthResult, error)
}

// BillRequest 是要拉取的渠道账单。BillDate 是账期日 YYYY-MM-DD（业务时区的自然日）。
type BillRequest struct {
	BillDate string
}

// 账单行种类。
const (
	BillKindTrade  = "trade"
	BillKindRefund = "refund"
)

// BillRow 是账单里的一笔渠道流水。PaymentNo/RefundNo 是中台单号（渠道回传的
// out_trade_no / out_refund_no）；两者都空表示渠道侧多出来的长款行。
type BillRow struct {
	Kind         string
	PaymentNo    string
	RefundNo     string
	AmountMinor  int64
	FeeMinor     int64
	ChannelTxnID string
}

// Bill 是一次账单下载的结果。Raw 不进返回值也不落库——账单可达数 MB，
// 只留 Digest 与行数用于审计与对账复现。
type Bill struct {
	Rows     []BillRow
	Digest   string
	RowCount int
}

// BillProvider 支持下载渠道账单。渠道不支持时返回 errCapabilityUnsupported。
type BillProvider interface {
	FetchBill(ctx context.Context, in BillRequest, cfg map[string]string) (*Bill, error)
}

// TransferRequest 是代付/转账入参。ItemNo 是批次内单笔的幂等号。
type TransferRequest struct {
	BatchNo      string
	ItemNo       string
	AmountMinor  int64
	Currency     string
	ReceiverType string // personal|merchant
	ReceiverID   string // 渠道侧收款方标识（openid / 银行卡号 / 商户账号）
	ReceiverName string
	Remark       string
	NotifyURL    string
}

// TransferResult 是渠道代付结论。Accepted=false 且 Pending=false 表示渠道拒绝。
type TransferResult struct {
	Accepted        bool
	Pending         bool
	ChannelBatchNo  string
	ChannelDetailNo string
	State           string
	Reason          string
}

// PayoutProvider 支持渠道代付与代付结果查询。
type PayoutProvider interface {
	Transfer(ctx context.Context, in TransferRequest, cfg map[string]string) (*TransferResult, error)
	QueryTransfer(ctx context.Context, batchNo string, cfg map[string]string) (*TransferResult, error)
}

// ProviderCaps 返回渠道声明的能力；未声明 CapabilityProvider 的按零值
// （只 CNY、无可选能力），保证新渠道不会凭空获得能力。
func ProviderCaps(p Provider) Capabilities {
	if cp, ok := p.(CapabilityProvider); ok {
		return cp.Capabilities()
	}
	return Capabilities{}
}

// capsOfDB 按渠道实例名取能力，供服务层与前端矩阵展示复用。
func capsOf(providerName string) (Capabilities, error) {
	p, err := LookupProvider(providerName)
	if err != nil {
		return Capabilities{}, err
	}
	return ProviderCaps(p), nil
}

// supportsCurrency 报告渠道是否收款该币种；空币种按 CNY 处理（与下单口径一致）。
func (c Capabilities) supportsCurrency(currency string) bool {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "CNY"
	}
	if len(c.Currencies) == 0 {
		return currency == "CNY"
	}
	for _, v := range c.Currencies {
		if strings.EqualFold(strings.TrimSpace(v), currency) {
			return true
		}
	}
	return false
}

// supportsMethod 报告渠道是否支持该下单方式；空方式表示由渠道自决，放行。
func (c Capabilities) supportsMethod(method string) bool {
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" || len(c.Methods) == 0 {
		return true
	}
	for _, v := range c.Methods {
		if strings.EqualFold(strings.TrimSpace(v), method) {
			return true
		}
	}
	return false
}

// capabilityUnsupported 是渠道能力缺口的统一错误：主文案走 i18n（字典里那条
// PAY_CAPABILITY_UNSUPPORTED），具体是哪个渠道缺哪项能力用 Wrap 挂进 Err 只进日志——
// WithMsg 会顶掉 i18n 文案，前端就变成不可翻译的中文串了。
func capabilityUnsupported(provider, what string) error {
	return response.BadRequest(errCapabilityUnsupported).
		Wrap(fmt.Errorf("%s: 不支持%s", provider, what))
}

// CallbackResult 是验签解析后的回调结论。RefundNo 非空时是退款回执。
type CallbackResult struct {
	PaymentNo    string
	RefundNo     string
	Paid         bool
	Failed       bool
	RefundOK     bool
	AmountMinor  int64 // 渠道回调里的金额（分）；>0 时服务层会与本地单据核对
	ChannelTxnID string
	// NotifyID 是渠道给这条通知的流水号（支付宝 notify_id、微信 id/serial 等）。
	// 只写进回调审计用于排障与重放取证，不做唯一约束：渠道在收到非 success 应答时
	// 会用同一个 notify_id 重试几十小时，唯一索引会把「验签失败后的一次配置修复」
	// 变成永久丢单。真正的重复入账防线是状态机幂等。
	NotifyID string
	// GatewayStatus 是渠道原始状态串（trade_status / trade_state），写进单据便于排障。
	GatewayStatus string
	Reason        string
}

var (
	providerMu sync.RWMutex
	providers  = map[string]Provider{}
)

// RegisterProvider 注册一个支付渠道实现；同名覆盖（测试用）。
func RegisterProvider(p Provider) {
	providerMu.Lock()
	defer providerMu.Unlock()
	providers[p.Name()] = p
}

// LookupProvider 取一个已注册渠道实现。
func LookupProvider(name string) (Provider, error) {
	providerMu.RLock()
	defer providerMu.RUnlock()
	p, ok := providers[name]
	if !ok {
		return nil, fmt.Errorf("pay: unknown provider %q", name)
	}
	return p, nil
}

// ProviderNames 返回已注册渠道类型（新建渠道时给前端下拉用）。
func ProviderNames() []string {
	providerMu.RLock()
	defer providerMu.RUnlock()
	names := make([]string, 0, len(providers))
	for n := range providers {
		names = append(names, n)
	}
	return names
}

func init() { RegisterProvider(mockProvider{}) }
