package payment

import (
	"context"
	"fmt"
	"sync"
)

// Provider 是支付渠道抽象。真实渠道（微信/支付宝/Stripe）按市场实现后用
// RegisterProvider 挂载；所有实现必须满足三条铁律：
//  1. 金额由服务端订单决定，绝不接受客户端金额；
//  2. 回调必须验签（签名 + 时间窗防重放）；
//  3. 同一支付单重复回调/查询幂等。
type Provider interface {
	Name() string
	// Create 预创建渠道侧订单，返回拉起支付的参数（跳转 URL、二维码等）；须按 paymentNo 幂等。
	Create(ctx context.Context, req PayRequest, cfg map[string]string) (*PayParams, error)
	// Query 主动查询渠道侧支付结果（事件丢失时的补偿路径）。
	Query(ctx context.Context, paymentNo string, cfg map[string]string) (*ChannelStatus, error)
	// Refund 发起渠道退款。P0 渠道同步返回结果；异步渠道返回 Pending 并由回调翻转。
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
	ReturnURL   string // 支付完成后回到业务前端的地址（可为空）
}

// PayParams 是渠道返回的拉起支付参数；PayURL 为空表示用 Params 自行组装。
type PayParams struct {
	PayURL string
	Params map[string]string
}

// ChannelStatus 是 Query 的结果；Paid/Failed 都为 false 表示渠道侧仍未定。
type ChannelStatus struct {
	Paid         bool
	Failed       bool
	ChannelTxnID string
	Reason       string
}

// RefundRequest 是 Refund 的入参。
type RefundRequest struct {
	RefundNo    string
	PaymentNo   string
	AmountMinor int64
}

// RefundResult 是渠道退款结果；OK=false 时 Reason 说明原因。
type RefundResult struct {
	OK              bool
	ChannelRefundID string
	Reason          string
}

// CallbackInput 是渠道回调的原始输入（notify 端点收到什么就传什么）。
type CallbackInput struct {
	Channel string
	Body    []byte
	Headers map[string]string
}

// CallbackResult 是验签解析后的回调结论。
type CallbackResult struct {
	PaymentNo    string
	Paid         bool
	Failed       bool
	ChannelTxnID string
	Reason       string
}

var (
	providerMu sync.RWMutex
	providers  = map[string]Provider{}
)

// RegisterProvider 注册一个支付渠道；同名覆盖（测试用）。
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
		return nil, fmt.Errorf("payment: unknown provider %q", name)
	}
	return p, nil
}

// ProviderNames 返回已注册渠道名（新建渠道时给前端下拉用）。
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
