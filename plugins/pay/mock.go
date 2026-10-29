package pay

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// mockProvider 是 P0 的模拟渠道：收银台页面模拟用户扫码支付，回调报文与真实网关
// 同构（JSON + HMAC-SHA256 + 时间窗），把验签、幂等、状态机的整条链路真实跑通。
type mockProvider struct{}

func (mockProvider) Name() string { return "mock" }

// MockCallbackHeaders 是 mock 渠道回调使用的签名头（与框架 webhook 的签名方案一致）。
const (
	mockTimestampHeader = "X-Timestamp"
	mockSignatureHeader = "X-Signature"
	// 回调时间窗统一 5 分钟（契约：mall wechat 5min / mock 10min，取严）。
	CallbackClockSkew = 5 * time.Minute
)

// Create 返回插件自带的模拟收银台页面（/api/v1 下的相对路径，与宿主前端同源）。
func (mockProvider) Create(_ context.Context, req PayRequest, _ map[string]string) (*PayParams, error) {
	u := "/api/v1/plugins/pay/mock/cashier?no=" + url.QueryEscape(req.PaymentNo)
	if req.ReturnURL != "" {
		u += "&return=" + url.QueryEscape(req.ReturnURL)
	}
	return &PayParams{PayURL: u}, nil
}

// Query 模拟渠道没有外部状态：未收到（模拟）回调前，渠道侧永远是"未支付"。
func (mockProvider) Query(_ context.Context, _ string, _ map[string]string) (*ChannelStatus, error) {
	return &ChannelStatus{}, nil
}

// Refund 模拟渠道同步退款成功。
func (mockProvider) Refund(_ context.Context, req RefundRequest, _ map[string]string) (*RefundResult, error) {
	if req.AmountMinor <= 0 {
		return &RefundResult{OK: false, Reason: "amount must be positive"}, nil
	}
	return &RefundResult{OK: true, ChannelRefundID: "mockrf_" + randomToken(8)}, nil
}

// mockCallbackBody 是 mock 渠道回调的报文格式。
type mockCallbackBody struct {
	PaymentNo       string `json:"paymentNo"`
	RefundNo        string `json:"refundNo,omitempty"`
	Result          string `json:"result"` // success | fail
	ChannelTxnID    string `json:"channelTxnId"`
	ChannelRefundID string `json:"channelRefundId,omitempty"`
	Timestamp       int64  `json:"timestamp"`
}

// SignMockCallback 用渠道密钥生成 mock 回调签名（收银台处理器与测试共用）。
func SignMockCallback(secret string, body []byte, timestamp int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", timestamp)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// headerOf 大小写不敏感地取回调头（notify 端点已统一转小写，渠道原始大小写也要兜住）。
func headerOf(headers map[string]string, name string) string {
	if v, ok := headers[name]; ok {
		return v
	}
	return headers[strings.ToLower(name)]
}

// VerifyCallback 验签（HMAC-SHA256 over "timestamp.body"）+ 时间窗防重放，再解析报文。
func (mockProvider) VerifyCallback(in CallbackInput, cfg map[string]string) (*CallbackResult, error) {
	secret := cfg["secret"]
	if secret == "" {
		return nil, fmt.Errorf("pay: mock channel has no secret")
	}
	tsStr := headerOf(in.Headers, mockTimestampHeader)
	sig := headerOf(in.Headers, mockSignatureHeader)
	if tsStr == "" || sig == "" {
		return nil, fmt.Errorf("pay: missing signature headers")
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("pay: bad timestamp")
	}
	if d := time.Since(time.Unix(ts, 0)); d > CallbackClockSkew || d < -CallbackClockSkew {
		return nil, fmt.Errorf("pay: callback timestamp out of window")
	}
	want := SignMockCallback(secret, in.Body, ts)
	if subtle.ConstantTimeCompare([]byte(want), []byte(sig)) != 1 {
		return nil, fmt.Errorf("pay: invalid signature")
	}
	var body mockCallbackBody
	if err := json.Unmarshal(in.Body, &body); err != nil {
		return nil, fmt.Errorf("pay: bad callback body")
	}
	if body.PaymentNo == "" {
		return nil, fmt.Errorf("pay: callback missing paymentNo")
	}
	res := &CallbackResult{PaymentNo: body.PaymentNo, ChannelTxnID: body.ChannelTxnID}
	switch body.Result {
	case "success":
		if body.RefundNo != "" {
			res.RefundNo = body.RefundNo
			res.RefundOK = true
			if body.ChannelRefundID != "" {
				res.ChannelTxnID = body.ChannelRefundID
			}
			return res, nil
		}
		res.Paid = true
		if res.ChannelTxnID == "" {
			res.ChannelTxnID = "mock_" + randomToken(8)
		}
	case "fail":
		res.Failed = true
		res.Reason = "user closed cashier"
	default:
		return nil, fmt.Errorf("pay: unknown mock result %q", body.Result)
	}
	return res, nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
