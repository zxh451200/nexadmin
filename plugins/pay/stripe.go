package pay

// Stripe 托管收单渠道（Checkout Session）：Visa / Master / Amex 等国际卡。
// 方法只有 page（跳转 Stripe 托管收银台）；卡组织/钱包在 Stripe 侧自动启用。
//
// 与中台契约的对应：
//   - Create    → POST /v1/checkout/sessions（client_reference_id 与 metadata 都带 paymentNo），
//     返回 session.url 作为 PayURL，session.id 存 PrepayID；
//   - Query     → POST /v1/checkout/sessions/search 按 metadata[paymentNo] 找回会话
//     （Query 只拿得到支付单号），再 expand payment_intent 看真实状态；
//   - Callback  → webhooks（checkout.session.completed / expired、payment_intent.succeeded /
//     payment_failed、charge.refunded），Stripe-Signature: t=…,v1=… HMAC-SHA256 验签，
//     时间戳属于「每次下发都重新生成」语义 → CallbackClockSkew 双向硬窗；
//   - Refund    → POST /v1/refunds（payment_intent + amount），退款单 metadata 带退款幂等键，
//     charge.refunded 回调翻转退款状态；RefundQuery → GET /v1/refunds/:id（回调丢失兜底）。
//
// 金额一律最小货币单位（Stripe 与中台口径一致）；凭据：secretKey + webhookSecret，
// apiBase 可覆盖（自建代理/测试桩）。

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/response"
)

// CallbackClockSkew 是回调时间戳的双向硬窗（秒）：Stripe-Signature 的 t 每次通知
// 都重新生成且参与签名，超窗即重放/伪造信号，直接验签失败让渠道重试。
const stripeClockSkew = 5 * 60

type stripeProvider struct{}

func (stripeProvider) Name() string { return "stripe" }

// Capabilities：Checkout 托管收银台覆盖的币种与方式。卡组织（Visa/Master/Amex/JCB）
// 与链接支付由 Stripe 侧自动启用，不逐个申报。
func (stripeProvider) Capabilities() Capabilities {
	return Capabilities{
		Currencies: []string{
			"USD", "EUR", "GBP", "CAD", "AUD", "CHF", "HKD", "SGD", "JPY", "NZD",
			"MXN", "BRL", "SEK", "NOK", "DKK", "PLN", "CZK", "HUF", "RON", "MYR", "TWD", "AED",
		},
		Methods:     []string{"page"},
		RefundQuery: true,
	}
}

func (stripeProvider) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "secretKey", Type: "password", Required: true, Secret: true, Placeholder: "sk_live_… / sk_test_…"},
		{Key: "webhookSecret", Type: "password", Required: true, Secret: true, Placeholder: "whsec_…"},
		{Key: "apiBase", Type: "url", Placeholder: "https://api.stripe.com（留空用官方端点）"},
	}
}

func (stripeProvider) stripeBase(cfg map[string]string) string {
	if v := strings.TrimSpace(cfg["apiBase"]); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.stripe.com"
}

// call 发 Stripe API 请求：Basic 鉴权（secretKey），表单编码，应答一律 JSON。
func (p stripeProvider) call(ctx context.Context, cfg map[string]string, method, path string, form url.Values, out any) error {
	secret := strings.TrimSpace(cfg["secretKey"])
	if secret == "" {
		return channelConfigError("stripe", "缺少 secretKey")
	}
	req, err := http.NewRequestWithContext(ctx, method, p.stripeBase(cfg)+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Stripe-Version", "2024-06-20")
	resp, err := gatewayClient.Do(req)
	if err != nil {
		return channelGatewayError("stripe", "网关请求失败："+err.Error())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return channelGatewayError("stripe", "读取网关应答失败："+err.Error())
	}
	var root struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &root)
	if root.Error != nil || resp.StatusCode >= 400 {
		msg := "网关拒绝"
		if root.Error != nil {
			msg = root.Error.Message
		}
		return channelGatewayError("stripe", msg+"（HTTP "+strconv.Itoa(resp.StatusCode)+"）")
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return channelGatewayError("stripe", "应答解析失败："+err.Error())
		}
	}
	return nil
}

// Create 创建 Checkout Session（page）：托管收银台页 URL 作为 PayURL。
func (p stripeProvider) Create(ctx context.Context, req PayRequest, cfg map[string]string) (*PayParams, error) {
	if req.ReturnURL == "" {
		return nil, channelConfigError("stripe", "业务侧未传 ReturnURL（Stripe success_url 必须是完整 URL）")
	}
	v := url.Values{}
	v.Set("mode", "payment")
	v.Set("success_url", req.ReturnURL)
	v.Set("cancel_url", req.ReturnURL)
	v.Set("client_reference_id", req.PaymentNo)
	v.Set("metadata[paymentNo]", req.PaymentNo)
	v.Set("line_items[0][quantity]", "1")
	v.Set("line_items[0][price_data][currency]", strings.ToLower(req.Currency))
	v.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(req.AmountMinor, 10))
	v.Set("line_items[0][price_data][product_data][name]", truncate(req.Subject, 120))
	var out struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := p.call(ctx, cfg, http.MethodPost, "/v1/checkout/sessions", v, &out); err != nil {
		return nil, err
	}
	if out.URL == "" {
		return nil, channelGatewayError("stripe", "应答缺少收银台 URL")
	}
	return &PayParams{PayURL: out.URL, PrepayID: out.ID}, nil
}

// stripeSession 是 Checkout Session 的最小应答。
type stripeSession struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	PaymentStatus  string `json:"payment_status"` // paid / unpaid / no_payment_required
	Status         string `json:"status"`         // complete / open / expired
	AmountTotal    int64  `json:"amount_total"`
	PaymentIntent  string `json:"payment_intent"`
	ClientReferenceID string `json:"client_reference_id"`
	Metadata       map[string]string `json:"metadata"`
}

// sessionStatus 把会话状态折算成渠道结论：paid=已支付，expired=已关，其余未定。
func stripeSessionStatus(s stripeSession) ChannelStatus {
	out := ChannelStatus{ChannelTxnID: s.PaymentIntent, GatewayStatus: s.PaymentStatus}
	if s.PaymentIntent == "" {
		out.ChannelTxnID = s.ID
	}
	switch s.PaymentStatus {
	case "paid", "no_payment_required":
		out.Paid = true
	case "unpaid":
		if s.Status == "expired" {
			out.Failed = true
			out.Reason = "收银台会话已过期"
		}
	}
	return out
}

// Query 主动查渠道结果：先按 metadata[paymentNo] 搜会话，再展开 payment_intent 看状态。
// Search API 在部分账户未开启时返回错误 → 按未定处理（回调仍是主路径）。
func (p stripeProvider) Query(ctx context.Context, paymentNo string, cfg map[string]string) (*ChannelStatus, error) {
	v := url.Values{}
	v.Set("query", fmt.Sprintf("metadata['paymentNo']:%q", paymentNo))
	v.Set("limit", "1")
	var out struct {
		Data []stripeSession `json:"data"`
	}
	if err := p.call(ctx, cfg, http.MethodPost, "/v1/checkout/sessions/search", v, &out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return &ChannelStatus{}, nil
	}
	return ptr(stripeSessionStatus(out.Data[0])), nil
}

// Refund 发起退款。ChannelTxnID 可能是支付意图（pi_…）也可能是会话（cs_…）——
// 会话先展开取支付意图再退款。结果 succeeded=OK，pending=Pending，其余拒绝。
func (p stripeProvider) Refund(ctx context.Context, req RefundRequest, cfg map[string]string) (*RefundResult, error) {
	pi := strings.TrimSpace(req.ChannelTxnID)
	if pi == "" || strings.HasPrefix(pi, "cs_") {
		var out stripeSession
		if err := p.call(ctx, cfg, http.MethodGet, "/v1/checkout/sessions/"+url.PathEscape(req.ChannelTxnID)+"?expand[]=payment_intent", nil, &out); err != nil {
			return nil, err
		}
		if out.PaymentIntent == "" {
			return nil, channelGatewayError("stripe", "会话尚未生成支付意图，无法退款")
		}
		pi = out.PaymentIntent
	}
	v := url.Values{}
	v.Set("payment_intent", pi)
	v.Set("amount", strconv.FormatInt(req.AmountMinor, 10))
	if req.IdempotencyKey != "" {
		v.Set("metadata[refundNo]", req.IdempotencyKey)
	}
	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := p.call(ctx, cfg, http.MethodPost, "/v1/refunds", v, &out); err != nil {
		return nil, err
	}
	res := &RefundResult{ChannelRefundID: out.ID}
	switch out.Status {
	case "succeeded":
		res.OK = true
	case "pending":
		res.Pending = true
	default:
		res.Reason = "退款状态 " + out.Status
	}
	return res, nil
}

// RefundQuery 主动查退款结果（回调丢失兜底）：按退款单 Stripe id 查状态。
func (p stripeProvider) RefundQuery(ctx context.Context, req RefundQueryRequest, cfg map[string]string) (*ChannelStatus, error) {
	var out struct {
		Status     string `json:"status"`
		Charge     string `json:"charge"`
		FailureReason string `json:"failure_reason"`
	}
	if err := p.call(ctx, cfg, http.MethodGet, "/v1/refunds/"+url.PathEscape(req.RefundNo), nil, &out); err != nil {
		return nil, err
	}
	out2 := &ChannelStatus{GatewayStatus: out.Status}
	switch out.Status {
	case "succeeded":
		out2.Paid = true
	case "pending":
		// 受理未回执：Paid/Failed 都不置位，按未定处理（等回调或再查）
	default:
		out2.Failed = true
		out2.Reason = out.FailureReason
	}
	return out2, nil
}

// VerifyCallback 验签并解析 Stripe webhook。签名头：Stripe-Signature: t=…,v1=…（可多个），
// 签名串 = t + "." + 原始报文，HMAC-SHA256(webhookSecret)。
func (p stripeProvider) VerifyCallback(in CallbackInput, cfg map[string]string) (*CallbackResult, error) {
	secret := strings.TrimSpace(cfg["webhookSecret"])
	if secret == "" {
		return nil, channelConfigError("stripe", "缺少 webhookSecret（webhook 验签需要）")
	}
	sig := in.Headers["stripe-signature"]
	ts, sigs, err := stripeParseSignature(sig)
	if err != nil {
		return nil, err
	}
	if now := time.Now().Unix(); absDuration(time.Duration(now-ts)*time.Second) > stripeClockSkew {
		return nil, errNotifyStale
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(in.Body)
	want := hex.EncodeToString(mac.Sum(nil))
	ok := false
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			ok = true
			break
		}
	}
	if !ok {
		return nil, response.BadRequest(errSignInvalid)
	}
	var ev struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Obj json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(in.Body, &ev); err != nil {
		return nil, response.BadRequest(errSignInvalid)
	}
	res := &CallbackResult{NotifyID: ev.ID, GatewayStatus: ev.Type}
	switch ev.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		var s stripeSession
		if err := json.Unmarshal(ev.Data.Obj, &s); err != nil {
			return nil, response.BadRequest(errSignInvalid)
		}
		res.PaymentNo = s.Metadata["paymentNo"]
		if s.PaymentIntent != "" {
			res.ChannelTxnID = s.PaymentIntent
		} else {
			res.ChannelTxnID = s.ID
		}
		res.AmountMinor = s.AmountTotal
		res.Paid = s.PaymentStatus == "paid" || s.PaymentStatus == "no_payment_required"
	case "checkout.session.expired", "checkout.session.async_payment_failed":
		var s stripeSession
		if err := json.Unmarshal(ev.Data.Obj, &s); err != nil {
			return nil, response.BadRequest(errSignInvalid)
		}
		res.PaymentNo = s.Metadata["paymentNo"]
		res.ChannelTxnID = s.ID
		res.Failed = true
		res.Reason = ev.Type
	case "payment_intent.succeeded":
		var pi struct {
			ID       string            `json:"id"`
			Amount   int64             `json:"amount_received"`
			Metadata map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(ev.Data.Obj, &pi); err != nil {
			return nil, response.BadRequest(errSignInvalid)
		}
		res.PaymentNo = pi.Metadata["paymentNo"]
		res.ChannelTxnID = pi.ID
		res.AmountMinor = pi.Amount
		res.Paid = true
	case "payment_intent.payment_failed":
		var pi struct {
			ID                string            `json:"id"`
			Metadata          map[string]string `json:"metadata"`
			LastPaymentError  struct {
				Message string `json:"message"`
			} `json:"last_payment_error"`
		}
		if err := json.Unmarshal(ev.Data.Obj, &pi); err != nil {
			return nil, response.BadRequest(errSignInvalid)
		}
		res.PaymentNo = pi.Metadata["paymentNo"]
		res.ChannelTxnID = pi.ID
		res.Failed = true
		res.Reason = pi.LastPaymentError.Message
	case "charge.refunded", "charge.refund.updated":
		var rf struct {
			ID        string            `json:"id"`
			Status    string            `json:"status"`
			Amount    int64             `json:"amount"`
			Metadata  map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(ev.Data.Obj, &rf); err != nil {
			return nil, response.BadRequest(errSignInvalid)
		}
		res.RefundNo = rf.Metadata["refundNo"]
		res.ChannelTxnID = rf.ID
		res.AmountMinor = rf.Amount
		switch rf.Status {
		case "succeeded":
			res.RefundOK = true
		case "pending":
			// 受理未回执：不翻转，等后续 refund.updated 或 RefundQuery 兜底
		default:
			res.RefundOK = false
			res.Reason = "退款状态 " + rf.Status
		}
	default:
		// 未申报的事件类型：只落审计（GatewayStatus），不翻转任何状态。
	}
	return res, nil
}

// stripeParseSignature 解析 Stripe-Signature 头：t=时间戳，v1=签名（可多个）。
func stripeParseSignature(sig string) (int64, []string, error) {
	ts, sigs := int64(0), []string{}
	for _, part := range strings.Split(sig, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			v, err := strconv.ParseInt(kv[1], 10, 64)
			if err != nil {
				return 0, nil, response.BadRequest(errSignInvalid)
			}
			ts = v
		case "v1":
			sigs = append(sigs, kv[1])
		}
	}
	if ts == 0 || len(sigs) == 0 {
		return 0, nil, response.BadRequest(errSignInvalid)
	}
	return ts, sigs, nil
}

func init() { RegisterProvider(stripeProvider{}) }
