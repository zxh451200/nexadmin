package pay

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// wechatProvider 是微信支付 APIv3 渠道：商户私钥签名请求，微信支付公钥验应答与回调，
// 回调解密用 APIv3 密钥（AES-256-GCM）。支持扫码（native）、公众号（jsapi）、
// H5（h5）、APP（app）四种拉起方式；退款异步回执走同一 notify 端点。
type wechatProvider struct{}

func (wechatProvider) Name() string { return "wechat" }

func (wechatProvider) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "appId", Type: "text", Required: true, Placeholder: "wx0000000000000000"},
		{Key: "mchId", Type: "text", Required: true, Placeholder: "1900000001"},
		{Key: "serialNo", Type: "text", Required: true, Placeholder: "商户 API 证书序列号"},
		{Key: "privateKey", Type: "password", Required: true, Secret: true, Placeholder: "apiclient_key.pem（PEM）"},
		{Key: "apiV3Key", Type: "password", Required: true, Secret: true, Placeholder: "APIv3 密钥（32 位）"},
		{Key: "platformPublicKey", Type: "password", Required: true, Secret: true, Placeholder: "微信支付公钥（PEM）"},
		{Key: "platformPublicKeyId", Type: "text", Required: true, Placeholder: "PUB_KEY_ID_0000000000000000"},
	}
}

func (wechatProvider) CallbackAck() (string, string) {
	return "application/json; charset=utf-8", `{"code":"SUCCESS","message":"成功"}`
}

const wechatAPIBase = "https://api.mch.weixin.qq.com"

func (p wechatProvider) Create(ctx context.Context, req PayRequest, cfg map[string]string) (*PayParams, error) {
	if err := cnyOnly("wechat", req.Currency); err != nil {
		return nil, err
	}
	notify, err := absoluteNotifyURL("wechat", req.NotifyURL)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"appid":        cfg["appId"],
		"mchid":        cfg["mchId"],
		"description":  truncate(req.Subject, 127),
		"out_trade_no": req.PaymentNo,
		"notify_url":   notify,
		"amount":       map[string]any{"total": req.AmountMinor, "currency": "CNY"},
	}
	if !req.ExpireAt.IsZero() {
		body["time_expire"] = req.ExpireAt.In(wechatLoc).Format(time.RFC3339)
	}
	method := strings.ToLower(strings.TrimSpace(req.Method))
	switch method {
	case "", "native", "qr", "scan":
		status, raw, err := p.call(ctx, cfg, http.MethodPost, "/v3/pay/transactions/native", body)
		if err != nil {
			return nil, err
		}
		res, err := wechatResult("wechat", status, raw)
		if err != nil {
			return nil, err
		}
		codeURL := res["code_url"]
		if codeURL == "" {
			return nil, channelGatewayError("wechat", "预下单未返回 code_url")
		}
		return &PayParams{CodeURL: codeURL, Params: map[string]string{"qrCode": codeURL}}, nil
	case "jsapi", "mp", "mini":
		if strings.TrimSpace(req.PayerOpenID) == "" {
			return nil, channelConfigError("wechat", "JSAPI 下单需要 payerOpenId")
		}
		body["payer"] = map[string]any{"openid": req.PayerOpenID}
		status, raw, err := p.call(ctx, cfg, http.MethodPost, "/v3/pay/transactions/jsapi", body)
		if err != nil {
			return nil, err
		}
		res, err := wechatResult("wechat", status, raw)
		if err != nil {
			return nil, err
		}
		prepay := res["prepay_id"]
		if prepay == "" {
			return nil, channelGatewayError("wechat", "预下单未返回 prepay_id")
		}
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := newRequestID(16)
		pkg := "prepay_id=" + prepay
		sign, err := p.signFront(cfg, strings.Join([]string{cfg["appId"], ts, nonce, pkg}, "\n")+"\n")
		if err != nil {
			return nil, err
		}
		return &PayParams{PrepayID: prepay, Params: map[string]string{
			"appId": cfg["appId"], "timeStamp": ts, "nonceStr": nonce,
			"package": pkg, "signType": "RSA", "paySign": sign,
		}}, nil
	case "h5", "wap", "mweb":
		ip := strings.TrimSpace(req.ClientIP)
		if ip == "" {
			ip = "127.0.0.1"
		}
		body["scene_info"] = map[string]any{
			"payer_client_ip": ip,
			"h5_info":         map[string]any{"type": "Wap"},
		}
		status, raw, err := p.call(ctx, cfg, http.MethodPost, "/v3/pay/transactions/h5", body)
		if err != nil {
			return nil, err
		}
		res, err := wechatResult("wechat", status, raw)
		if err != nil {
			return nil, err
		}
		h5 := res["h5_url"]
		if h5 == "" {
			return nil, channelGatewayError("wechat", "预下单未返回 h5_url")
		}
		return &PayParams{PayURL: h5}, nil
	case "app":
		status, raw, err := p.call(ctx, cfg, http.MethodPost, "/v3/pay/transactions/app", body)
		if err != nil {
			return nil, err
		}
		res, err := wechatResult("wechat", status, raw)
		if err != nil {
			return nil, err
		}
		prepay := res["prepay_id"]
		if prepay == "" {
			return nil, channelGatewayError("wechat", "预下单未返回 prepay_id")
		}
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := newRequestID(16)
		sign, err := p.signFront(cfg, strings.Join([]string{cfg["appId"], ts, nonce, prepay}, "\n")+"\n")
		if err != nil {
			return nil, err
		}
		return &PayParams{PrepayID: prepay, Params: map[string]string{
			"appid": cfg["appId"], "partnerid": cfg["mchId"], "prepayid": prepay,
			"package": "Sign=WXPay", "noncestr": nonce, "timestamp": ts, "sign": sign,
		}}, nil
	default:
		return nil, channelConfigError("wechat", "不支持的支付方式 "+method+"（可选 native/jsapi/h5/app）")
	}
}

func (p wechatProvider) Query(ctx context.Context, paymentNo string, cfg map[string]string) (*ChannelStatus, error) {
	path := "/v3/pay/transactions/out-trade-no/" + url.PathEscape(paymentNo) + "?mchid=" + url.QueryEscape(cfg["mchId"])
	status, raw, err := p.call(ctx, cfg, http.MethodGet, path, "")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return &ChannelStatus{}, nil // 渠道侧还没有这笔单
	}
	res, err := wechatResult("wechat", status, raw)
	if err != nil {
		return nil, err
	}
	st := &ChannelStatus{ChannelTxnID: res["transaction_id"], GatewayStatus: res["trade_state"]}
	switch res["trade_state"] {
	case "SUCCESS", "REFUND":
		st.Paid = true
	case "CLOSED", "REVOKED", "PAYERROR":
		st.Failed = true
		st.Reason = "微信支付交易已关闭"
	}
	return st, nil
}

func (p wechatProvider) Refund(ctx context.Context, req RefundRequest, cfg map[string]string) (*RefundResult, error) {
	notify, err := absoluteNotifyURL("wechat", req.NotifyURL)
	if err != nil {
		return nil, err
	}
	total := req.OrderAmountMinor
	if total <= 0 {
		total = req.AmountMinor
	}
	body := map[string]any{
		"out_trade_no":  req.PaymentNo,
		"out_refund_no": wechatRefundKey(req),
		"reason":        truncate("退款", 80),
		"notify_url":    notify,
		"amount":        map[string]any{"refund": req.AmountMinor, "total": total, "currency": "CNY"},
	}
	status, raw, err := p.call(ctx, cfg, http.MethodPost, "/v3/refund/domestic/refunds", body)
	if err != nil {
		return nil, err
	}
	res, err := wechatResult("wechat", status, raw)
	if err != nil {
		return nil, err
	}
	switch res["status"] {
	case "SUCCESS":
		return &RefundResult{OK: true, ChannelRefundID: res["refund_id"]}, nil
	case "PROCESSING":
		return &RefundResult{Pending: true, ChannelRefundID: res["refund_id"]}, nil
	default:
		reason := res["status"]
		if reason == "" {
			reason = "退款被微信拒绝"
		}
		return &RefundResult{OK: false, Reason: reason}, nil
	}
}

// VerifyCallback 验微信支付签名 → 用 APIv3 密钥解密 resource → 按事件类型给结论。
func (wechatProvider) VerifyCallback(in CallbackInput, cfg map[string]string) (*CallbackResult, error) {
	apiKey := []byte(strings.TrimSpace(cfg["apiV3Key"]))
	if len(apiKey) != 32 {
		return nil, fmt.Errorf("wechat: apiV3Key 必须是 32 位")
	}
	pub, err := parseRSAPublicKey(cfg["platformPublicKey"])
	if err != nil {
		return nil, fmt.Errorf("wechat: 微信支付公钥不可用: %w", err)
	}
	ts := headerOf(in.Headers, "Wechatpay-Timestamp")
	nonce := headerOf(in.Headers, "Wechatpay-Nonce")
	sig := headerOf(in.Headers, "Wechatpay-Signature")
	serial := headerOf(in.Headers, "Wechatpay-Serial")
	if ts == "" || nonce == "" || sig == "" {
		return nil, fmt.Errorf("wechat: 回调缺少签名头")
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("wechat: 回调时间戳非法")
	}
	if d := time.Since(time.Unix(sec, 0)); d > CallbackClockSkew || d < -CallbackClockSkew {
		return nil, fmt.Errorf("wechat: 回调时间戳超出允许窗口")
	}
	if want := strings.TrimSpace(cfg["platformPublicKeyId"]); want != "" && serial != want {
		return nil, fmt.Errorf("wechat: 回调公钥 ID 与渠道配置不一致")
	}
	if !rsaVerifySHA256(pub, ts+"\n"+nonce+"\n"+string(in.Body)+"\n", sig) {
		return nil, fmt.Errorf("wechat: 回调验签失败")
	}

	var ev struct {
		EventType string `json:"event_type"`
		Resource  struct {
			AssociatedData string `json:"associated_data"`
			Nonce          string `json:"nonce"`
			Ciphertext     string `json:"ciphertext"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(in.Body, &ev); err != nil {
		return nil, fmt.Errorf("wechat: 回调报文不是 JSON")
	}
	plain, err := wechatDecrypt(apiKey, ev.Resource.AssociatedData, ev.Resource.Nonce, ev.Resource.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("wechat: 回调解密失败: %w", err)
	}

	switch ev.EventType {
	case "TRANSACTION.SUCCESS":
		var tx struct {
			OutTradeNo    string `json:"out_trade_no"`
			TransactionID string `json:"transaction_id"`
			TradeState    string `json:"trade_state"`
			Amount        struct {
				Total int64 `json:"total"`
			} `json:"amount"`
		}
		if err := json.Unmarshal(plain, &tx); err != nil {
			return nil, fmt.Errorf("wechat: 支付回调解密内容非法")
		}
		if tx.OutTradeNo == "" {
			return nil, fmt.Errorf("wechat: 支付回调缺少 out_trade_no")
		}
		return &CallbackResult{
			PaymentNo: tx.OutTradeNo, Paid: tx.TradeState == "SUCCESS",
			AmountMinor: tx.Amount.Total, ChannelTxnID: tx.TransactionID,
			GatewayStatus: tx.TradeState,
		}, nil
	case "REFUND.SUCCESS", "REFUND.ABNORMAL", "REFUND.CLOSED":
		var rf struct {
			OutTradeNo   string `json:"out_trade_no"`
			OutRefundNo  string `json:"out_refund_no"`
			RefundID     string `json:"refund_id"`
			RefundStatus string `json:"refund_status"`
			Amount       struct {
				Refund int64 `json:"refund"`
			} `json:"amount"`
		}
		if err := json.Unmarshal(plain, &rf); err != nil {
			return nil, fmt.Errorf("wechat: 退款回调解密内容非法")
		}
		if rf.OutRefundNo == "" {
			return nil, fmt.Errorf("wechat: 退款回调缺少 out_refund_no")
		}
		return &CallbackResult{
			PaymentNo: rf.OutTradeNo, RefundNo: rf.OutRefundNo,
			RefundOK:    ev.EventType == "REFUND.SUCCESS" && rf.RefundStatus == "SUCCESS",
			AmountMinor: rf.Amount.Refund, ChannelTxnID: rf.RefundID,
			GatewayStatus: rf.RefundStatus,
		}, nil
	default:
		// 其他事件（如优惠/授权变更）只应答，不改本地状态。
		return &CallbackResult{}, nil
	}
}

// call 发签名请求并验应答签名。status 交给调用方判定（查询 404 是正常业务态）。
func (p wechatProvider) call(ctx context.Context, cfg map[string]string, method, path string, payload any) (int, []byte, error) {
	body := ""
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = string(raw)
	}
	auth, err := wechatAuthorization(cfg, method, path, body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, wechatAPIBase+path, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := gatewayClient.Do(req)
	if err != nil {
		return 0, nil, channelGatewayError("wechat", "网关请求失败："+err.Error())
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, channelGatewayError("wechat", "读取网关应答失败："+err.Error())
	}
	if err := wechatVerifyResponse(cfg, resp.Header, raw); err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// signFront 生成给前端 SDK 的签名（JSAPI / APP 拉起参数）。
func (p wechatProvider) signFront(cfg map[string]string, message string) (string, error) {
	key, err := parseRSAPrivateKey(cfg["privateKey"])
	if err != nil {
		return "", channelConfigError("wechat", "商户私钥不可用："+err.Error())
	}
	return rsaSignSHA256(key, message), nil
}

func wechatAuthorization(cfg map[string]string, method, path, body string) (string, error) {
	key, err := parseRSAPrivateKey(cfg["privateKey"])
	if err != nil {
		return "", channelConfigError("wechat", "商户私钥不可用："+err.Error())
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := newRequestID(16)
	msg := method + "\n" + path + "\n" + ts + "\n" + nonce + "\n" + body + "\n"
	return fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`,
		cfg["mchId"], nonce, rsaSignSHA256(key, msg), ts, cfg["serialNo"]), nil
}

func wechatVerifyResponse(cfg map[string]string, h http.Header, body []byte) error {
	sig := h.Get("Wechatpay-Signature")
	if sig == "" {
		return channelGatewayError("wechat", "网关应答缺少签名")
	}
	ts := h.Get("Wechatpay-Timestamp")
	nonce := h.Get("Wechatpay-Nonce")
	if ts == "" || nonce == "" {
		return channelGatewayError("wechat", "网关应答缺少签名时间戳/随机串")
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || absDuration(time.Since(time.Unix(sec, 0))) > CallbackClockSkew {
		return channelGatewayError("wechat", "网关应答签名时间戳超出允许窗口")
	}
	if want := strings.TrimSpace(cfg["platformPublicKeyId"]); want != "" && h.Get("Wechatpay-Serial") != want {
		return channelGatewayError("wechat", "网关应答公钥 ID 与渠道配置不一致")
	}
	pub, err := parseRSAPublicKey(cfg["platformPublicKey"])
	if err != nil {
		return channelConfigError("wechat", "微信支付公钥不可用："+err.Error())
	}
	if !rsaVerifySHA256(pub, ts+"\n"+nonce+"\n"+string(body)+"\n", sig) {
		return channelGatewayError("wechat", "网关应答验签失败")
	}
	return nil
}

// wechatResult 解析网关应答：2xx 返回字段表，其余把 code/message 变成可读原因。
func wechatResult(provider string, status int, raw []byte) (map[string]string, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, channelGatewayError(provider, "网关应答不是 JSON："+truncate(string(raw), 200))
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch t := v.(type) {
		case string:
			out[k] = t
		case float64:
			out[k] = strconv.FormatInt(int64(t), 10)
		case bool:
			out[k] = strconv.FormatBool(t)
		}
	}
	if status < 200 || status >= 300 {
		msg := out["message"]
		if msg == "" {
			msg = truncate(string(raw), 200)
		}
		if code := out["code"]; code != "" {
			return nil, channelGatewayError(provider, code+": "+msg)
		}
		return nil, channelGatewayError(provider, msg)
	}
	return out, nil
}

func wechatDecrypt(apiKey []byte, aad, nonce, ciphertext string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(apiKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, []byte(nonce), ct, []byte(aad))
}

func wechatRefundKey(req RefundRequest) string {
	if req.IdempotencyKey != "" {
		return req.IdempotencyKey
	}
	return req.RefundNo
}

// wechatLoc 用于 time_expire：微信要求带时区的 RFC3339。
var wechatLoc = time.FixedZone("CST", 8*3600)

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func init() { RegisterProvider(wechatProvider{}) }
