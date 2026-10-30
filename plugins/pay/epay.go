package pay

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// epayProvider 是「易支付」协议的通用网关渠道：把支付宝/微信/QQ 钱包等上游统一成
// 一套 MD5 签名接口（submit.php 拉起、api.php 查询与退款）。长尾支付公司、海外
// 聚合网关多半兼容这套协议，商户只填网关地址与商户号/密钥，不需要写代码。
type epayProvider struct{}

func (epayProvider) Name() string { return "epay" }

func (epayProvider) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "gateway", Type: "url", Required: true, Placeholder: "https://pay.example.com"},
		{Key: "pid", Type: "text", Required: true, Placeholder: "商户 ID"},
		{Key: "key", Type: "password", Required: true, Secret: true, Placeholder: "商户密钥"},
		{Key: "payType", Type: "select", Required: true, Options: []string{"alipay", "wxpay", "qqpay"}},
	}
}

func (epayProvider) CallbackAck() (string, string) {
	return "text/plain; charset=utf-8", "success"
}

func (p epayProvider) Create(ctx context.Context, req PayRequest, cfg map[string]string) (*PayParams, error) {
	if err := cnyOnly("epay", req.Currency); err != nil {
		return nil, err
	}
	notify, err := absoluteNotifyURL("epay", req.NotifyURL)
	if err != nil {
		return nil, err
	}
	payType := strings.TrimSpace(cfg["payType"])
	if payType == "" {
		payType = "alipay"
	}
	params := map[string]string{
		"pid":          cfg["pid"],
		"type":         payType,
		"out_trade_no": req.PaymentNo,
		"notify_url":   notify,
		"name":         truncate(req.Subject, 120),
		"money":        yuanOf(req.AmountMinor),
	}
	if req.ReturnURL != "" {
		params["return_url"] = req.ReturnURL
	}
	params["sign"] = epaySign(params, cfg["key"])
	params["sign_type"] = "MD5"
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	return &PayParams{PayURL: strings.TrimRight(cfg["gateway"], "/") + "/submit.php?" + q.Encode()}, nil
}

func (p epayProvider) Query(ctx context.Context, paymentNo string, cfg map[string]string) (*ChannelStatus, error) {
	q := url.Values{}
	q.Set("act", "order")
	q.Set("pid", cfg["pid"])
	q.Set("key", cfg["key"])
	q.Set("out_trade_no", paymentNo)
	res, err := epayGet(ctx, strings.TrimRight(cfg["gateway"], "/")+"/api.php?"+q.Encode())
	if err != nil {
		return nil, err
	}
	if res["code"] != "1" {
		return &ChannelStatus{}, nil // 上游没有这笔单或查询未开放：维持未定
	}
	state := res["trade_status"]
	if state == "" && res["status"] == "1" {
		state = "TRADE_SUCCESS"
	}
	st := &ChannelStatus{ChannelTxnID: res["trade_no"], GatewayStatus: state}
	switch state {
	case "TRADE_SUCCESS", "SUCCESS":
		st.Paid = true
	}
	return st, nil
}

func (p epayProvider) Refund(ctx context.Context, req RefundRequest, cfg map[string]string) (*RefundResult, error) {
	form := url.Values{}
	form.Set("act", "refund")
	form.Set("pid", cfg["pid"])
	form.Set("key", cfg["key"])
	form.Set("out_trade_no", req.PaymentNo)
	form.Set("money", yuanOf(req.AmountMinor))
	form.Set("out_refund_no", epayRefundKey(req))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(cfg["gateway"], "/")+"/api.php", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	resp, err := gatewayClient.Do(httpReq)
	if err != nil {
		return nil, channelGatewayError("epay", "网关请求失败："+err.Error())
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, channelGatewayError("epay", "读取网关应答失败："+err.Error())
	}
	res, perr := epayJSON("epay", raw)
	if perr != nil {
		return nil, perr
	}
	if res["code"] != "1" {
		reason := res["msg"]
		if reason == "" {
			reason = "上游未受理退款（该网关可能不支持退款接口）"
		}
		return &RefundResult{OK: false, Reason: reason}, nil
	}
	return &RefundResult{OK: true, ChannelRefundID: res["refund_no"]}, nil
}

// VerifyCallback 校验易支付异步通知（MD5），成功应答纯文本 success。
func (epayProvider) VerifyCallback(in CallbackInput, cfg map[string]string) (*CallbackResult, error) {
	form, err := url.ParseQuery(string(in.Body))
	if err != nil {
		return nil, fmt.Errorf("epay: 通知报文不是合法表单")
	}
	sig := form.Get("sign")
	if sig == "" {
		return nil, fmt.Errorf("epay: 通知缺少 sign")
	}
	params := map[string]string{}
	for k := range form {
		params[k] = form.Get(k)
	}
	if !strings.EqualFold(epaySign(params, cfg["key"]), sig) {
		return nil, fmt.Errorf("epay: 通知验签失败")
	}
	paymentNo := form.Get("out_trade_no")
	if paymentNo == "" {
		return nil, fmt.Errorf("epay: 通知缺少 out_trade_no")
	}
	res := &CallbackResult{
		PaymentNo:     paymentNo,
		ChannelTxnID:  form.Get("trade_no"),
		GatewayStatus: form.Get("trade_status"),
	}
	if v, ok := minorFromYuan(form.Get("money")); ok {
		res.AmountMinor = v
	}
	switch res.GatewayStatus {
	case "TRADE_SUCCESS", "SUCCESS":
		res.Paid = true
	case "TRADE_CLOSED", "CLOSED":
		res.Failed = true
		res.Reason = "上游交易已关闭"
	case "WAIT_BUYER_PAY":
	default:
		return nil, fmt.Errorf("epay: 未知的 trade_status %q", res.GatewayStatus)
	}
	return res, nil
}

// epaySign 是易支付签名：非空参数按键名排序拼 k=v&...，末尾拼密钥后取 MD5 小写。
func epaySign(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || k == "sign_type" || strings.TrimSpace(v) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
		b.WriteByte('&')
	}
	b.WriteString(key)
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func epayGet(ctx context.Context, u string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := gatewayClient.Do(req)
	if err != nil {
		return nil, channelGatewayError("epay", "网关请求失败："+err.Error())
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, channelGatewayError("epay", "读取网关应答失败："+err.Error())
	}
	return epayJSON("epay", raw)
}

// epayJSON 解析上游 JSON（code 可能是数字 1 也可能是有引号的 "1"）。
func epayJSON(provider string, raw []byte) (map[string]string, error) {
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
	return out, nil
}

func epayRefundKey(req RefundRequest) string {
	if req.IdempotencyKey != "" {
		return req.IdempotencyKey
	}
	return req.RefundNo
}

func init() { RegisterProvider(epayProvider{}) }
