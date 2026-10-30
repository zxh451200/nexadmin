package pay

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// alipayProvider 是支付宝开放平台渠道（RSA2）。签名用应用私钥，验签用支付宝公钥；
// 支持扫码（precreate）、电脑网站、手机网站、APP 四种拉起方式，退款同步返回。
type alipayProvider struct{}

func (alipayProvider) Name() string { return "alipay" }

func (alipayProvider) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "appId", Type: "text", Required: true, Placeholder: "2021000000000000"},
		{Key: "privateKey", Type: "password", Required: true, Secret: true, Placeholder: "应用私钥（PKCS8，PEM 或纯 Base64）"},
		{Key: "alipayPublicKey", Type: "password", Required: true, Secret: true, Placeholder: "支付宝公钥或公钥证书（PEM / 纯 Base64）"},
		{Key: "sellerId", Type: "text", Placeholder: "可选：校验回调的 seller_id"},
		{Key: "gateway", Type: "url", Placeholder: alipayDefaultGateway},
	}
}

func (alipayProvider) CallbackAck() (string, string) {
	return "text/plain; charset=utf-8", "success"
}

const (
	alipayDefaultGateway = "https://openapi.alipay.com/gateway.do"
	alipayTimeLayout     = "2006-01-02 15:04:05"
)

// 支付宝网关的时间戳按北京时间，不跟随服务器时区。
var alipayLoc = time.FixedZone("CST", 8*3600)

// Create 按 method 选择拉起方式：默认扫码；page=电脑网站；wap/h5=手机网站；app=APP。
func (p alipayProvider) Create(ctx context.Context, req PayRequest, cfg map[string]string) (*PayParams, error) {
	if err := cnyOnly("alipay", req.Currency); err != nil {
		return nil, err
	}
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	if strings.TrimSpace(cfg["appId"]) == "" {
		return nil, channelConfigError("alipay", "缺少 appId")
	}
	notify, err := absoluteNotifyURL("alipay", req.NotifyURL)
	if err != nil {
		return nil, err
	}
	amount := yuanOf(req.AmountMinor)
	method := strings.ToLower(strings.TrimSpace(req.Method))

	switch method {
	case "", "native", "qr", "scan", "precreate", "offline":
		res, err := p.call(ctx, cfg, key, "alipay.trade.precreate", map[string]string{
			"out_trade_no":    req.PaymentNo,
			"total_amount":    amount,
			"subject":         truncate(req.Subject, 256),
			"timeout_express": expireExpress(req.ExpireAt, 30*time.Minute),
		}, map[string]string{"notify_url": notify})
		if err != nil {
			return nil, err
		}
		if msg := alipayResponseError(res); msg != "" {
			return nil, channelGatewayError("alipay", msg)
		}
		qr := res["qr_code"]
		if qr == "" {
			return nil, channelGatewayError("alipay", "预下单未返回 qr_code")
		}
		return &PayParams{CodeURL: qr, Params: map[string]string{"qrCode": qr}}, nil
	case "page", "web", "pc":
		q := p.signedParams(cfg, key, "alipay.trade.page.pay", map[string]string{
			"out_trade_no": req.PaymentNo,
			"total_amount": amount,
			"subject":      truncate(req.Subject, 256),
			"product_code": "FAST_INSTANT_TRADE_PAY",
		}, notify, req.ReturnURL)
		return &PayParams{PayURL: alipayGateway(cfg) + "?" + q.Encode()}, nil
	case "wap", "h5", "mweb":
		q := p.signedParams(cfg, key, "alipay.trade.wap.pay", map[string]string{
			"out_trade_no": req.PaymentNo,
			"total_amount": amount,
			"subject":      truncate(req.Subject, 256),
			"product_code": "QUICK_WAP_WAY",
		}, notify, req.ReturnURL)
		return &PayParams{PayURL: alipayGateway(cfg) + "?" + q.Encode()}, nil
	case "app":
		q := p.signedParams(cfg, key, "alipay.trade.app.pay", map[string]string{
			"out_trade_no": req.PaymentNo,
			"total_amount": amount,
			"subject":      truncate(req.Subject, 256),
			"product_code": "QUICK_MSECURITY_PAY",
		}, notify, req.ReturnURL)
		return &PayParams{Params: map[string]string{"orderStr": q.Encode()}}, nil
	default:
		return nil, channelConfigError("alipay", "不支持的支付方式 "+method+"（可选 native/page/wap/app）")
	}
}

func (p alipayProvider) Query(ctx context.Context, paymentNo string, cfg map[string]string) (*ChannelStatus, error) {
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	res, err := p.call(ctx, cfg, key, "alipay.trade.query", map[string]string{"out_trade_no": paymentNo}, nil)
	if err != nil {
		return nil, err
	}
	if res["sub_code"] == "ACQ.TRADE_NOT_EXIST" || res["code"] == "40004" {
		return &ChannelStatus{}, nil // 渠道侧还没有这笔单：维持未定，不报错
	}
	if msg := alipayResponseError(res); msg != "" {
		return nil, channelGatewayError("alipay", msg)
	}
	st := &ChannelStatus{ChannelTxnID: res["trade_no"], GatewayStatus: res["trade_status"]}
	switch res["trade_status"] {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		st.Paid = true
	case "TRADE_CLOSED":
		st.Failed = true
		st.Reason = "支付宝交易已关闭"
	}
	return st, nil
}

func (p alipayProvider) Refund(ctx context.Context, req RefundRequest, cfg map[string]string) (*RefundResult, error) {
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	biz := map[string]string{
		"out_trade_no":   req.PaymentNo,
		"refund_amount":  yuanOf(req.AmountMinor),
		"out_request_no": alipayRefundKey(req),
	}
	res, err := p.call(ctx, cfg, key, "alipay.trade.refund", biz, nil)
	if err != nil {
		return nil, err
	}
	if msg := alipayResponseError(res); msg != "" {
		return &RefundResult{OK: false, Reason: msg}, nil
	}
	return &RefundResult{OK: true, ChannelRefundID: res["trade_no"]}, nil
}

// VerifyCallback 校验异步通知：去掉 sign 与 sign_type 后按参数名排序验 RSA2，
// 再核对 app_id / seller_id 与金额；支付宝要求应答纯文本 success（见 CallbackAck）。
func (alipayProvider) VerifyCallback(in CallbackInput, cfg map[string]string) (*CallbackResult, error) {
	form, err := url.ParseQuery(string(in.Body))
	if err != nil {
		return nil, fmt.Errorf("alipay: 通知报文不是合法表单")
	}
	pub, err := alipayPublicKey(cfg["alipayPublicKey"])
	if err != nil {
		return nil, fmt.Errorf("alipay: 支付宝公钥不可用: %w", err)
	}
	sig := form.Get("sign")
	if sig == "" {
		return nil, fmt.Errorf("alipay: 通知缺少 sign")
	}
	if !alipayVerify(pub, alipayCanonical(form, true), sig) {
		return nil, fmt.Errorf("alipay: 通知验签失败")
	}
	if appID := strings.TrimSpace(cfg["appId"]); appID != "" && form.Get("app_id") != appID {
		return nil, fmt.Errorf("alipay: 通知 app_id 与渠道配置不一致")
	}
	if seller := strings.TrimSpace(cfg["sellerId"]); seller != "" && form.Get("seller_id") != seller {
		return nil, fmt.Errorf("alipay: 通知 seller_id 与渠道配置不一致")
	}
	paymentNo := form.Get("out_trade_no")
	if paymentNo == "" {
		return nil, fmt.Errorf("alipay: 通知缺少 out_trade_no")
	}
	res := &CallbackResult{
		PaymentNo:     paymentNo,
		ChannelTxnID:  form.Get("trade_no"),
		GatewayStatus: form.Get("trade_status"),
	}
	if v, ok := minorFromYuan(form.Get("total_amount")); ok {
		res.AmountMinor = v
	}
	switch res.GatewayStatus {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		res.Paid = true
	case "TRADE_CLOSED":
		res.Failed = true
		res.Reason = "支付宝交易已关闭"
	case "WAIT_BUYER_PAY":
		// 买家尚未付款：只应答，不翻状态。
	default:
		return nil, fmt.Errorf("alipay: 未知的 trade_status %q", res.GatewayStatus)
	}
	return res, nil
}

// signedParams 组装公共参数（含 sign_type 与签名），供跳转类接口拼 URL。
func (p alipayProvider) signedParams(cfg map[string]string, key *rsa.PrivateKey, method string, biz map[string]string, notify, returnURL string) url.Values {
	v := url.Values{}
	v.Set("app_id", cfg["appId"])
	v.Set("method", method)
	v.Set("format", "JSON")
	v.Set("charset", "utf-8")
	v.Set("sign_type", "RSA2")
	v.Set("timestamp", time.Now().In(alipayLoc).Format(alipayTimeLayout))
	v.Set("version", "1.0")
	if notify != "" {
		v.Set("notify_url", notify)
	}
	if returnURL != "" {
		v.Set("return_url", returnURL)
	}
	raw, _ := json.Marshal(biz)
	v.Set("biz_content", string(raw))
	v.Set("sign", alipaySign(key, v))
	return v
}

// call 走网关换 JSON 应答：先验应答签名，再返回节点字段。
func (p alipayProvider) call(ctx context.Context, cfg map[string]string, key *rsa.PrivateKey, method string, biz, extra map[string]string) (map[string]string, error) {
	pub, err := alipayPublicKey(cfg["alipayPublicKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "支付宝公钥不可用："+err.Error())
	}
	q := p.signedParams(cfg, key, method, biz, extra["notify_url"], "")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, alipayGateway(cfg), strings.NewReader(q.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	resp, err := gatewayClient.Do(req)
	if err != nil {
		return nil, channelGatewayError("alipay", "网关请求失败："+err.Error())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, channelGatewayError("alipay", "读取网关应答失败："+err.Error())
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, channelGatewayError("alipay", "网关应答不是 JSON："+truncate(string(body), 200))
	}
	nodeKey := strings.ReplaceAll(method, ".", "_") + "_response"
	raw, ok := root[nodeKey]
	if !ok {
		return nil, channelGatewayError("alipay", "网关应答缺少节点 "+nodeKey)
	}
	sig := rawString(root["sign"])
	if sig == "" {
		return nil, channelGatewayError("alipay", "网关应答缺少签名")
	}
	if !alipayVerify(pub, string(raw), sig) {
		return nil, channelGatewayError("alipay", "网关应答验签失败")
	}
	return alipayFields(raw), nil
}

func alipayGateway(cfg map[string]string) string {
	if g := strings.TrimSpace(cfg["gateway"]); g != "" {
		return g
	}
	return alipayDefaultGateway
}

// alipayResponseError 把非 10000 的应答转成可读原因；10000 返回空串。
func alipayResponseError(res map[string]string) string {
	if res["code"] == "10000" {
		return ""
	}
	detail := res["sub_msg"]
	if detail == "" {
		detail = res["msg"]
	}
	if detail == "" {
		detail = "网关返回 code=" + res["code"]
	}
	if sc := res["sub_code"]; sc != "" {
		return sc + ": " + detail
	}
	return detail
}

// alipayCanonical 是待签串：按参数名排序的 k=v 用 & 连接。
// 请求签名包含 sign_type，异步通知验签须同时去掉 sign 和 sign_type。
func alipayCanonical(v url.Values, dropSignType bool) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		if k == "sign" || (dropSignType && k == "sign_type") {
			continue
		}
		if strings.TrimSpace(v.Get(k)) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v.Get(k))
	}
	return b.String()
}

func alipaySign(key *rsa.PrivateKey, v url.Values) string {
	return rsaSignSHA256(key, alipayCanonical(v, false))
}

func alipayVerify(pub *rsa.PublicKey, content, sigB64 string) bool {
	return rsaVerifySHA256(pub, content, sigB64)
}

// alipayRefundKey 用渠道幂等号；缺失时退回本地退款单号（老调用方）。
func alipayRefundKey(req RefundRequest) string {
	if req.IdempotencyKey != "" {
		return req.IdempotencyKey
	}
	return req.RefundNo
}

// alipayFields 把应答节点的每个字段转成字符串（字符串去引号，数字/布尔保留原文）。
func alipayFields(raw json.RawMessage) map[string]string {
	var m map[string]json.RawMessage
	out := map[string]string{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return out
	}
	for k, v := range m {
		if s := rawString(v); s != "" || string(v) == `""` {
			out[k] = s
			continue
		}
		out[k] = strings.TrimSpace(string(v))
	}
	return out
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// expireExpress 把支付单过期时间换算成支付宝的 timeout_express（如 30m）。
func expireExpress(at time.Time, fallback time.Duration) string {
	d := fallback
	if !at.IsZero() {
		if left := time.Until(at); left > time.Minute {
			d = left
		}
	}
	if d < time.Minute {
		d = time.Minute
	}
	return fmt.Sprintf("%dm", int64(d.Round(time.Minute)/time.Minute))
}

func init() { RegisterProvider(alipayProvider{}) }
