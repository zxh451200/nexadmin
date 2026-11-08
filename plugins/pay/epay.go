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
	"time"
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

// Capabilities 如实声明易支付协议的能力边界：这是一套只负责「跳转收款 + MD5 通知」
// 的老协议，没有退款查询、没有预授权、没有账单下载、没有代付。
//   - 币种只有 CNY（网关按人民币计价，外币要业务侧先换汇）；
//   - 下单方式只有跳转类：submit.php 是电脑网站跳转，移动端网关给 wap/h5 跳转，
//     没有扫码串、没有 JSAPI、没有 APP 下单；
//   - 四个布尔能力全 false，前端据此禁用入口。
//
// 下面四组接口方法照样实现，但只有一条路径：capabilityGapError()（底座
// capabilityUnsupported 的文案 + 契约要求的 PAY_CAPABILITY_UNSUPPORTED）明确报错。
// 主链按 Capabilities 判能力是第一道闸，这里是第二道——被误调用时宁可报错，
// 也绝不静默返回一个「看起来像渠道流水号」的东西。
func (epayProvider) Capabilities() Capabilities {
	return Capabilities{
		Currencies: []string{"CNY"},
		Methods:    []string{"page", "wap", "h5"},
	}
}

// QueryRefund 易支付协议没有退款查询接口（老网关只有 act=refund 的一次性应答）。
func (epayProvider) QueryRefund(_ context.Context, _ RefundQueryRequest, _ map[string]string) (*RefundStatusResult, error) {
	return nil, capabilityGapError("epay", "退款结果查询")
}

// Freeze 等四个方法：协议里没有资金预授权。
func (epayProvider) Freeze(_ context.Context, _ AuthFreezeRequest, _ map[string]string) (*AuthResult, error) {
	return nil, capabilityGapError("epay", "资金预授权冻结")
}

func (epayProvider) CompleteAuth(_ context.Context, _ AuthCompleteRequest, _ map[string]string) (*AuthResult, error) {
	return nil, capabilityGapError("epay", "预授权请款")
}

func (epayProvider) CancelAuth(_ context.Context, _ AuthCancelRequest, _ map[string]string) (*AuthResult, error) {
	return nil, capabilityGapError("epay", "预授权撤销")
}

func (epayProvider) QueryAuth(_ context.Context, _ string, _ map[string]string) (*AuthResult, error) {
	return nil, capabilityGapError("epay", "预授权查询")
}

// FetchBill 协议不下发对账单：这类网关要做账单驱动对账只能靠商户后台导出，
// 中台侧只能保持 source=manual，绝不假装下载成功（0 行会被当成「账期没有交易」）。
func (epayProvider) FetchBill(_ context.Context, _ BillRequest, _ map[string]string) (*Bill, error) {
	return nil, capabilityGapError("epay", "渠道账单下载")
}

// Transfer / QueryTransfer：协议没有代付。结算打款在易支付渠道只能走人工回执。
func (epayProvider) Transfer(_ context.Context, _ TransferRequest, _ map[string]string) (*TransferResult, error) {
	return nil, capabilityGapError("epay", "代付/转账")
}

func (epayProvider) QueryTransfer(_ context.Context, _ string, _ map[string]string) (*TransferResult, error) {
	return nil, capabilityGapError("epay", "代付结果查询")
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
	// 防重放（时间窗）：易支付标准协议没有时间戳，但不少上游的通知里额外带
	// timestamp（秒）或 time，且 epaySign 会把所有非空参数（除 sign/sign_type）
	// 纳入签名，所以取到的一定是签过名的。
	//
	// 关键兼容判断：**不带**任何时间参数的历史报文一律照常放行。实测大量易支付系
	// 网关根本不下发时间戳，硬要它等于把合法通知全丢掉——时间窗在这里是
	// 「有则加固」的可选项，不是准入项；真正的重复入账防线仍是状态机幂等。
	if raw, strict := epayNotifyStamp(form); raw != "" {
		at, ok := parseEpayTime(raw)
		if !ok {
			return nil, fmt.Errorf("epay: 通知时间参数 %q 无法解析", raw)
		}
		// 超前方向两种都拒（签名过了也不可能是刚生成的通知，是伪造或时钟异常）；
		// 过去方向只有签名时间戳设硬窗，业务时间晚到是常态。
		if d := time.Since(at); d < -CallbackClockSkew || (strict && d > CallbackClockSkew) {
			return nil, staleWindowError("epay", d)
		}
	}
	paymentNo := form.Get("out_trade_no")
	if paymentNo == "" {
		return nil, fmt.Errorf("epay: 通知缺少 out_trade_no")
	}
	res := &CallbackResult{
		PaymentNo:    paymentNo,
		ChannelTxnID: form.Get("trade_no"),
		// NotifyID：易支付没有独立的通知流水号，渠道交易号（trade_no）是这条
		// 通知里唯一能定位到渠道侧事实的号（同一笔多次重试 trade_no 不变，与
		// 支付宝 notify_id 的行为一致），只做取证与排障检索，不做唯一约束。
		NotifyID:      form.Get("trade_no"),
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

// epayNotifyStamp 取通知里的时间参数，并回答它是哪种语义：
//   - timestamp：每次下发都重新生成、参与签名的通知时间戳 → strict=true，双向硬窗；
//   - time：不少上游拿它放业务时间（下单/成交时间），渠道重试并不刷新 →
//     strict=false，只拒超前于本地时钟的，过去方向不设硬限，否则晚到的合法
//     通知会被整批判成过期（铁律②：两种语义不能混为一谈）。
func epayNotifyStamp(form url.Values) (string, bool) {
	if v := strings.TrimSpace(form.Get("timestamp")); v != "" {
		return v, true
	}
	return strings.TrimSpace(form.Get("time")), false
}

// parseEpayTime 解析易支付时间戳，兼容三种上游口径：秒级/毫秒级 Unix 时间戳，
// 以及业务时区（UTC+8）的 "2006-01-02 15:04:05"。解析不了交给调用方判成非法报文，
// 不静默放行——参数参与了签名，说不清格式就是异常。
func parseEpayTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n > 1_000_000_000_000 { // 13 位起是毫秒
			return time.Unix(n/1000, 0), true
		}
		return time.Unix(n, 0), true
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, raw, epayBizLoc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// epayBizLoc 是易支付时间戳默认时区（协议按商户本地时间下发，国内网关都是 UTC+8）；
// 用 FixedZone 而不是 LoadLocation，Windows 无 tzdata 也不会退成 UTC 误判超窗。
var epayBizLoc = time.FixedZone("UTC+8", 8*3600)

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
