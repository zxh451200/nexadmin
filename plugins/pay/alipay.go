package pay

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"nexadmin/core/response"
)

// alipayProvider 是支付宝开放平台渠道（RSA2）。签名用应用私钥，验签用支付宝公钥。
//
// 覆盖的能力（见 Capabilities，必须与这里真实实现的一致，不许虚报）：
//   - 下单：扫码（precreate）、电脑网站（page）、手机网站（wap）、APP、JSAPI/小程序
//     （trade.create）、付款码当面付（trade.pay）；
//   - 退款与退款结果主动查询（fastpay.refund.query）；
//   - 资金预授权冻结/请款/撤销/查询（fund.auth.*，需商户签约预授权产品）；
//   - 渠道账单下载与解析（data.dataservice.bill.downloadurl.query）；
//   - 单笔代付与代付查询（fund.trans.*，需商户签约转账产品）。
//
// 入参借用说明：PayRequest 目前没有「付款码」字段（底座待补，见泳道回票），当面付
// 复用 PayerOpenID 承载 auth_code——两种下单方式对「付款方凭据」的要求互斥，按 method
// 分流不会混；JSAPI 也用同一字段承载买家标识（2088 开头的 16 位按 buyer_id，含 @ 按
// buyer_logon_id，其余按 buyer_open_id）。
type alipayProvider struct{}

func (alipayProvider) Name() string { return "alipay" }

// Capabilities 是支付宝渠道真实能力矩阵。币种只有 CNY（外币必须业务侧换汇后下单，
// 见 cnyOnly）；Methods 逐项对应 Create 里的分支。Auth/Payout 虽然代码已实现，但
// 依赖商户在支付宝侧签约对应产品，未签约时渠道会明确报错（不本地假成功）。
func (alipayProvider) Capabilities() Capabilities {
	return Capabilities{
		Currencies:  []string{"CNY"},
		Methods:     []string{"native", "qr", "page", "wap", "h5", "app", "jsapi", "micropay"},
		RefundQuery: true,
		Auth:        true,
		Bill:        true,
		Payout:      true,
	}
}

func (alipayProvider) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "appId", Type: "text", Required: true, Placeholder: "2021000000000000"},
		{Key: "privateKey", Type: "password", Required: true, Secret: true, Placeholder: "应用私钥（PKCS8，PEM 或纯 Base64）"},
		{Key: "alipayPublicKey", Type: "password", Required: true, Secret: true, Placeholder: "支付宝公钥或公钥证书（PEM / 纯 Base64）"},
		{Key: "sellerId", Type: "text", Placeholder: "可选：校验回调的 seller_id"},
		{Key: "gateway", Type: "url", Placeholder: alipayDefaultGateway},
		{Key: "billType", Type: "select", Options: []string{"trade", "signCustomer"}, Placeholder: "可选：对账账单类型，默认 trade（交易账单）"},
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

// Create 按 method 选择拉起方式：默认扫码；page=电脑网站；wap/h5=手机网站；app=APP；
// jsapi=小程序/公众号（trade.create，返回 my.tradePay 用的 trade_no）；
// micropay=付款码当面付（trade.pay，可能同步成功）。
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
		res, err := p.call(ctx, cfg, key, "alipay.trade.precreate", map[string]any{
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
	case "jsapi", "mini", "miniprogram", "mp":
		// 小程序/公众号：先在渠道建单，再把 trade_no 交给 my.tradePay 拉起收银台。
		buyer := strings.TrimSpace(req.PayerOpenID)
		if buyer == "" {
			return nil, channelConfigError("alipay", "JSAPI/小程序下单缺少买家标识（buyer_id / buyer_logon_id / buyer_open_id 至少一个）")
		}
		biz := map[string]any{
			"out_trade_no": req.PaymentNo,
			"total_amount": amount,
			"subject":      truncate(req.Subject, 256),
			"product_code": "JSAPI_PAY",
		}
		if field, value := alipayBuyerIdentity(buyer); field != "" {
			biz[field] = value
		}
		res, err := p.call(ctx, cfg, key, "alipay.trade.create", biz, map[string]string{"notify_url": notify})
		if err != nil {
			return nil, err
		}
		if msg := alipayResponseError(res); msg != "" {
			return nil, channelGatewayError("alipay", msg)
		}
		tradeNo := res["trade_no"]
		if tradeNo == "" {
			// 渠道没给授权号就是没有这笔单：绝不本地编一个 trade_no 糊弄前端。
			return nil, channelGatewayError("alipay", "JSAPI 下单未返回 trade_no")
		}
		return &PayParams{
			PrepayID: tradeNo,
			Params: map[string]string{
				"tradeNo":    tradeNo,
				"outTradeNo": req.PaymentNo,
			},
		}, nil
	case "micropay", "facepay", "barcode", "authcode":
		// 付款码（当面付）：auth_code 是扫码枪读到的 18 位付款码，一次有效。
		authCode := strings.TrimSpace(req.PayerOpenID)
		if authCode == "" {
			return nil, channelConfigError("alipay", "付款码支付缺少 auth_code（扫码枪读到的付款码）")
		}
		res, err := p.call(ctx, cfg, key, "alipay.trade.pay", map[string]any{
			"out_trade_no": req.PaymentNo,
			"total_amount": amount,
			"subject":      truncate(req.Subject, 256),
			"product_code": "FACE_TO_FACE_PAYMENT",
			"scene":        "barcode",
			"auth_code":    authCode,
		}, map[string]string{"notify_url": notify})
		if err != nil {
			return nil, err
		}
		// 10003 = 渠道已受理但等待用户输密码：不是失败，也不是成功，只能继续查/等回调。
		if res["code"] == "10003" {
			return &PayParams{Params: map[string]string{
				"outTradeNo":  req.PaymentNo,
				"tradeStatus": "WAITING_PAYMENT",
			}}, nil
		}
		if msg := alipayResponseError(res); msg != "" {
			return nil, channelGatewayError("alipay", msg)
		}
		params := map[string]string{"outTradeNo": req.PaymentNo}
		if tn := res["trade_no"]; tn != "" {
			params["tradeNo"] = tn
		}
		// 当面付成功时渠道会同步回 trade_status；真实状态机仍由回调/查询翻转，
		// 这里只是把渠道原文透传给收银台，不做本地判定。
		if ts := res["trade_status"]; ts != "" {
			params["tradeStatus"] = ts
		}
		return &PayParams{Params: params}, nil
	case "page", "web", "pc":
		q := p.signedParams(cfg, key, "alipay.trade.page.pay", map[string]any{
			"out_trade_no": req.PaymentNo,
			"total_amount": amount,
			"subject":      truncate(req.Subject, 256),
			"product_code": "FAST_INSTANT_TRADE_PAY",
		}, notify, req.ReturnURL)
		return &PayParams{PayURL: alipayGateway(cfg) + "?" + q.Encode()}, nil
	case "wap", "h5", "mweb":
		q := p.signedParams(cfg, key, "alipay.trade.wap.pay", map[string]any{
			"out_trade_no": req.PaymentNo,
			"total_amount": amount,
			"subject":      truncate(req.Subject, 256),
			"product_code": "QUICK_WAP_WAY",
		}, notify, req.ReturnURL)
		return &PayParams{PayURL: alipayGateway(cfg) + "?" + q.Encode()}, nil
	case "app":
		q := p.signedParams(cfg, key, "alipay.trade.app.pay", map[string]any{
			"out_trade_no": req.PaymentNo,
			"total_amount": amount,
			"subject":      truncate(req.Subject, 256),
			"product_code": "QUICK_MSECURITY_PAY",
		}, notify, req.ReturnURL)
		return &PayParams{Params: map[string]string{"orderStr": q.Encode()}}, nil
	default:
		return nil, channelConfigError("alipay", "不支持的支付方式 "+method+"（可选 native/page/wap/app/jsapi/micropay）")
	}
}

// alipayBuyerIdentity 把中台侧的买家标识映射到 trade.create 要求的字段：
// 2088 开头的 16 位数字是支付宝 user_id；含 @ 是登录号；其余按开放平台 open_id。
func alipayBuyerIdentity(payer string) (string, string) {
	payer = strings.TrimSpace(payer)
	if payer == "" {
		return "", ""
	}
	if alipayUserIDRe.MatchString(payer) {
		return "buyer_id", payer
	}
	if strings.Contains(payer, "@") {
		return "buyer_logon_id", payer
	}
	return "buyer_open_id", payer
}

var alipayUserIDRe = regexp.MustCompile(`^2088\d{12}$`)

func (p alipayProvider) Query(ctx context.Context, paymentNo string, cfg map[string]string) (*ChannelStatus, error) {
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	res, err := p.call(ctx, cfg, key, "alipay.trade.query", map[string]any{"out_trade_no": paymentNo}, nil)
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
	biz := map[string]any{
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

// VerifyCallback 校验异步通知：先拒绝 sign_type 降级，再去掉 sign 与 sign_type 后按参数名
// 排序验 RSA2，再核对 app_id / seller_id / notify_time 与金额；支付宝要求应答纯文本
// success（见 CallbackAck）。
//
// notify_time 的防重放是不对称的（对应 provider.go 铁律②的第二种语义）：
//   - 超前本地时钟超过 CallbackClockSkew：拒。这个时间不参与请求签名拼接之外的重试刷新，
//     一个「未来」的通知只可能是伪造或商户服务器时钟异常，没有合法解释。
//   - 过去方向不设硬限：支付宝在收到非 success 应答后会用**同一个 notify_time**重投，
//     间隔从 4 分钟一直拉到 15 小时量级（25m/15m/30m/1h/2h/6h/15h 序列）。套微信式的
//     5 分钟双向硬窗，会把第 4 次以后的合法重投全部丢掉——表现为「用户已付款、订单永远
//     不落账」，比多收一次重复通知危险得多。重复入账的真实防线是签名内的 out_trade_no
//     与金额核对加上单据状态机幂等，不是时间窗。
func (alipayProvider) VerifyCallback(in CallbackInput, cfg map[string]string) (*CallbackResult, error) {
	form, err := url.ParseQuery(string(in.Body))
	if err != nil {
		return nil, fmt.Errorf("alipay: 通知报文不是合法表单")
	}
	// 防降级：支付宝的异步通知只可能是 RSA2；报文显式声明别的算法就拒，
	// 绝不"兼容"成 SHA1 去验（RSA 报文即便验不过也不该走到验签分支）。
	if st := strings.TrimSpace(form.Get("sign_type")); st != "" && !strings.EqualFold(st, "RSA2") {
		return nil, fmt.Errorf("alipay: 通知 sign_type=%q 非 RSA2，拒绝降级验签", st)
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
	// notify_time 是北京时间「业务通知时间」，格式固定、不可解析即拒（报文不是真渠道发的
	// 才会缺这种基础字段）；过去多久都不据此拒收，理由见函数头注释。
	raw := strings.TrimSpace(form.Get("notify_time"))
	if raw == "" {
		return nil, fmt.Errorf("alipay: 通知缺少 notify_time")
	}
	notifyAt, err := time.ParseInLocation(alipayTimeLayout, raw, alipayLoc)
	if err != nil {
		return nil, fmt.Errorf("alipay: 通知 notify_time=%q 不可解析", raw)
	}
	if ahead := notifyAt.Sub(time.Now()); ahead > CallbackClockSkew {
		// 超窗：主文案走字典，哨兵用底座那一个（HandleCallback 靠 errors.Is 把它记成
		// stale_notify 而不是 sign_invalid——前者通常是服务器时钟/渠道时间配置问题）。
		return nil, response.BadRequest(errNotifyStaleKey).Wrap(
			fmt.Errorf("alipay: notify_time=%s 超前本地时钟 %s，超过 %s: %w",
				raw, ahead.Round(time.Second), CallbackClockSkew, errAlipayStaleNotify))
	}
	res := &CallbackResult{
		PaymentNo:     paymentNo,
		ChannelTxnID:  form.Get("trade_no"),
		GatewayStatus: form.Get("trade_status"),
		// 渠道给这条通知的流水号：只做取证与排障检索（渠道重投时同一个号），不做唯一约束。
		NotifyID: strings.TrimSpace(form.Get("notify_id")),
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

// errAlipayStaleNotify 是底座哨兵 errNotifyStale 的别名：四个渠道报「通知时间超窗」都指向
// 同一个目标，HandleCallback 只需 errors.Is(err, errNotifyStale) 就能把审计记成
// stale_notify 而不是 sign_invalid（前者多半是服务器时钟/渠道时间配置问题）。
var errAlipayStaleNotify = errNotifyStale

// signedParams 组装公共参数（含 sign_type 与签名），供跳转类接口拼 URL。
// biz 是 map[string]any：转账/预授权接口的 biz_content 有嵌套对象（receive_info 等），
// 纯字符串映射写不出来。
func (p alipayProvider) signedParams(cfg map[string]string, key *rsa.PrivateKey, method string, biz any, notify, returnURL string) url.Values {
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
func (p alipayProvider) call(ctx context.Context, cfg map[string]string, key *rsa.PrivateKey, method string, biz any, extra map[string]string) (map[string]string, error) {
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

// ---- 退款结果主动查询（RefundQueryProvider）----

// QueryRefund 用 alipay.trade.fastpay.refund.query 收敛长期 pending 的退款单。
// out_request_no 必须是发起退款时同一个渠道幂等号（= Refund.IdempotencyKey），
// 否则渠道会把它当成另一笔退款来查，永远查不到。
func (p alipayProvider) QueryRefund(ctx context.Context, req RefundQueryRequest, cfg map[string]string) (*RefundStatusResult, error) {
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	outRequestNo := strings.TrimSpace(req.IdempotencyKey)
	if outRequestNo == "" {
		outRequestNo = strings.TrimSpace(req.RefundNo)
	}
	res, err := p.call(ctx, cfg, key, "alipay.trade.fastpay.refund.query", map[string]any{
		"out_trade_no":   req.PaymentNo,
		"out_request_no": outRequestNo,
	}, nil)
	if err != nil {
		return nil, err
	}
	if alipayNotExist(res) {
		return &RefundStatusResult{Found: false, Reason: alipayResponseError(res)}, nil
	}
	if msg := alipayResponseError(res); msg != "" {
		return nil, channelGatewayError("alipay", msg)
	}
	refunded, _ := minorFromYuan(res["refund_amount"])
	out := &RefundStatusResult{Found: true, ChannelRefundID: res["trade_no"], Reason: res["refund_status"]}
	switch {
	case strings.EqualFold(res["refund_status"], "REFUND_IN_PROCESS"):
		// 渠道受理了但资金还没动（部分产品形态才有 refund_status）：继续等。
		out.Pending = true
	case strings.EqualFold(res["fund_change"], "Y") || refunded > 0:
		out.OK = true
	default:
		// 查到的是这笔交易，但这个 out_request_no 对应的退款金额为 0：按「查无该笔退款」处理，
		// 不能报成功（报成功就等于本地凭空退了一笔钱）。
		out.Found = false
		out.Pending = false
		out.Reason = "渠道应答退款金额为 0（查无该笔退款）"
	}
	return out, nil
}

// alipayNotExist 判断渠道应答是「查无此单」而非「这笔操作失败」。
// 支付宝常见形态是 40004 + ACQ.*_NOT_EXIST；也有接口只在 sub_msg 里说「单据不存在」
// （转账查询就是这样）。⚠ 精确的 sub_code 清单要真商户号核对：这里宁可把「查不到」
// 判成未定（Found=false / Pending），也不能判成终态——把已发生的资金动作读成失败
// 会诱发重复出金或重复退款。
func alipayNotExist(res map[string]string) bool {
	if strings.Contains(strings.ToUpper(res["sub_code"]), "NOT_EXIST") {
		return true
	}
	return strings.Contains(res["sub_msg"], "不存在")
}

// ---- 资金预授权（AuthProvider）----
//
// 走支付宝资金授权接口族：
//   - 冻结 alipay.fund.auth.order.freeze（product_code=PRE_AUTH_ONLINE）
//   - 请款 alipay.fund.auth.order.confirm
//   - 撤销 alipay.fund.auth.operation.cancel
//   - 查询 alipay.fund.auth.order.query
//
// ChannelAuthNo 一律取渠道应答里的 auth_no。应答没有 auth_no 就直接报错——auth_no 是
// 后续请款/撤销的唯一凭据，本地编一个只会在请款时炸在渠道侧，还留下一条假的授权账。
// 商户未签约该产品时，渠道回 40004 + sub_code（ACQ.PERMISSION_NOT_MATCH / PRODUCT_NOT_SIGN
// 之类），这里原样包成 PAY_CHANNEL_ERROR（含 sub_code 与 sub_msg）回传，不做本地假成功。
//
// ⚠ 需真商户号端到端验证：本泳道只能用桩网关验协议形状（接口名、字段名、幂等号、
// 错误分类），验不了签约前置、buyer 标识的真实取值与 auth_no 格式。上线前必须在
// 签约了预授权产品的沙箱/生产商户号上跑一遍冻结→请款→撤销。

func (p alipayProvider) Freeze(ctx context.Context, in AuthFreezeRequest, cfg map[string]string) (*AuthResult, error) {
	if err := cnyOnly("alipay", in.Currency); err != nil {
		return nil, err
	}
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	outOrderNo := strings.TrimSpace(in.AuthNo)
	if outOrderNo == "" {
		return nil, channelConfigError("alipay", "冻结缺少授权业务号（out_order_no，中台侧 authNo）")
	}
	if in.AmountMinor <= 0 {
		return nil, channelConfigError("alipay", "冻结金额必须大于 0")
	}
	biz := map[string]any{
		"out_order_no": outOrderNo,
		"product_code": "PRE_AUTH_ONLINE",
		"order_title":  truncate(in.Subject, 256),
		"total_amount": yuanOf(in.AmountMinor),
	}
	// 支付宝的同步冻结接口必须有买家标识；缺失时报凭据错误，而不是发一个渠道必拒的请求。
	field, value := alipayBuyerIdentity(strings.TrimSpace(in.PayerID))
	if field == "" {
		return nil, channelConfigError("alipay", "资金授权冻结缺少买家标识（buyer_id / buyer_logon_id / buyer_open_id）")
	}
	biz[field] = value
	if !in.ExpireAt.IsZero() {
		biz["expiry_time"] = in.ExpireAt.In(alipayLoc).Format(alipayTimeLayout)
	}
	res, err := p.call(ctx, cfg, key, "alipay.fund.auth.order.freeze", biz, alipayNotifyExtra(in.NotifyURL))
	if err != nil {
		return nil, err
	}
	if msg := alipayResponseError(res); msg != "" {
		return nil, channelGatewayError("alipay", "冻结被渠道拒绝："+msg)
	}
	authNo := strings.TrimSpace(res["auth_no"])
	if authNo == "" {
		return nil, channelGatewayError("alipay", "冻结应答缺少 auth_no（渠道侧没有这笔授权，不能本地造号）")
	}
	return alipayAuthResult(res, authNo, in.AmountMinor), nil
}

func (p alipayProvider) CompleteAuth(ctx context.Context, in AuthCompleteRequest, cfg map[string]string) (*AuthResult, error) {
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	authNo := strings.TrimSpace(in.ChannelAuthNo)
	if authNo == "" {
		return nil, channelConfigError("alipay", "请款缺少渠道授权号 auth_no（冻结未拿到授权号就说明渠道侧没有这笔授权）")
	}
	outRequestNo := strings.TrimSpace(in.PaymentNo)
	if outRequestNo == "" {
		return nil, channelConfigError("alipay", "请款缺少中台收款单号（作为渠道幂等号 out_request_no）")
	}
	if in.AmountMinor <= 0 {
		return nil, channelConfigError("alipay", "请款金额必须大于 0")
	}
	biz := map[string]any{
		"auth_no":        authNo,
		"out_request_no": outRequestNo,
		"amount":         yuanOf(in.AmountMinor),
	}
	if outOrderNo := strings.TrimSpace(in.AuthNo); outOrderNo != "" {
		biz["out_order_no"] = outOrderNo
	}
	if remark := strings.TrimSpace(in.Remark); remark != "" {
		biz["remark"] = truncate(remark, 256)
	}
	res, err := p.call(ctx, cfg, key, "alipay.fund.auth.order.confirm", biz, alipayNotifyExtra(in.NotifyURL))
	if err != nil {
		return nil, err
	}
	if msg := alipayResponseError(res); msg != "" {
		return nil, channelGatewayError("alipay", "请款被渠道拒绝："+msg)
	}
	// 请款应答里的 trade_no 是这笔请款生成的渠道交易号。AuthResult 没有放渠道交易号的
	// 字段（需要底座补），这里只回授权号与金额，trade_no 由收款单的回调/查询回填。
	echo := strings.TrimSpace(res["auth_no"])
	if echo == "" {
		echo = authNo
	}
	out := alipayAuthResult(res, echo, in.AmountMinor)
	out.Status = alipayFirstNonEmpty(res["order_status"], "CONFIRMED")
	return out, nil
}

func (p alipayProvider) CancelAuth(ctx context.Context, in AuthCancelRequest, cfg map[string]string) (*AuthResult, error) {
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	authNo := strings.TrimSpace(in.ChannelAuthNo)
	outOrderNo := strings.TrimSpace(in.AuthNo)
	if authNo == "" && outOrderNo == "" {
		return nil, channelConfigError("alipay", "撤销授权缺少 auth_no 与 out_order_no（至少一个）")
	}
	biz := map[string]any{"operation_type": "CANCEL"}
	if authNo != "" {
		biz["auth_no"] = authNo
	}
	if outOrderNo != "" {
		biz["out_order_no"] = outOrderNo
		// 撤销也是资金动作：用 authNo 派生确定性幂等号，重试不会重复解冻。
		biz["out_request_no"] = "CANCEL-" + outOrderNo
	}
	if remark := strings.TrimSpace(in.Reason); remark != "" {
		biz["remark"] = truncate(remark, 256)
	}
	res, err := p.call(ctx, cfg, key, "alipay.fund.auth.operation.cancel", biz, nil)
	if err != nil {
		return nil, err
	}
	if msg := alipayResponseError(res); msg != "" {
		return nil, channelGatewayError("alipay", "撤销被渠道拒绝："+msg)
	}
	echo := strings.TrimSpace(res["auth_no"])
	if echo == "" {
		echo = authNo
	}
	return &AuthResult{ChannelAuthNo: echo, Status: alipayFirstNonEmpty(res["order_status"], "CANCELLED")}, nil
}

// QueryAuth 按中台侧授权业务号查渠道授权状态（authNo = 冻结时下发的 out_order_no）。
// 渠道查无此授权时回 Status=NOT_FOUND 而不是 error：调用方要的是「渠道侧现在怎么说」，
// 把它算成失败会让补偿逻辑误判成一笔真实的资金异常。
func (p alipayProvider) QueryAuth(ctx context.Context, authNo string, cfg map[string]string) (*AuthResult, error) {
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	outOrderNo := strings.TrimSpace(authNo)
	if outOrderNo == "" {
		return nil, channelConfigError("alipay", "查询授权缺少授权业务号")
	}
	res, err := p.call(ctx, cfg, key, "alipay.fund.auth.order.query", map[string]any{
		"out_order_no": outOrderNo,
		"product_code": "PRE_AUTH_ONLINE",
	}, nil)
	if err != nil {
		return nil, err
	}
	if alipayNotExist(res) {
		return &AuthResult{Status: "NOT_FOUND", Reason: alipayResponseError(res)}, nil
	}
	if msg := alipayResponseError(res); msg != "" {
		return nil, channelGatewayError("alipay", "查询授权失败："+msg)
	}
	rest, ok := minorFromYuan(res["rest_amount"])
	if !ok {
		rest, ok = minorFromYuan(res["amount"])
	}
	frozen := int64(0)
	if ok {
		frozen = rest
	}
	return alipayAuthResult(res, strings.TrimSpace(res["auth_no"]), frozen), nil
}

// alipayAuthResult 把授权类应答转成结论：授权号必须来自应答本身。
func alipayAuthResult(res map[string]string, authNo string, fallbackAmountMinor int64) *AuthResult {
	amount, ok := minorFromYuan(res["amount"])
	if !ok || amount <= 0 {
		amount = fallbackAmountMinor
	}
	return &AuthResult{
		ChannelAuthNo: authNo,
		Status:        alipayFirstNonEmpty(res["order_status"], res["status"], "AUTHORIZED"),
		AmountMinor:   amount,
		Reason:        res["operation_id"],
	}
}

func alipayNotifyExtra(notifyURL string) map[string]string {
	v := strings.TrimSpace(notifyURL)
	if v == "" {
		return nil
	}
	return map[string]string{"notify_url": v}
}

func alipayFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// ---- 渠道账单下载（BillProvider）----

const (
	// alipayBillMaxBytes 是账单下载的硬上限：账单可达数 MB，但绝不能让一次对账把内存吃穿。
	alipayBillMaxBytes = 10 << 20
)

var alipayBillDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// FetchBill 先用 alipay.data.dataservice.bill.downloadurl.query 换 download_url，再 HTTP 下载
// 并解析 CSV。账单本体（raw）既不返回也不落库，只留 sha256 摘要与行数用于审计复现。
//
// 解析按「表头名」定位列，绝不按下标：支付宝账单前面有说明段、后面有汇总段，
// 且不同账单类型（trade / signCustomer）列序不同，下标定位会在某次账单改版后静默错位。
// 金额是元、可能带千分位，统一去符号后用 minorFromYuan 转分（全程不经 float）。
//
// 「解析不出任何行」必须报错而不是返回 0 行成功：对账任务里 0 行和「已跳过」长得一样，
// 静默返回会把渠道侧多日的对账空洞化。
func (p alipayProvider) FetchBill(ctx context.Context, in BillRequest, cfg map[string]string) (*Bill, error) {
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	date := strings.TrimSpace(in.BillDate)
	if !alipayBillDateRe.MatchString(date) {
		return nil, channelConfigError("alipay", "账单日期必须是 YYYY-MM-DD，当前 "+date)
	}
	billType := strings.TrimSpace(cfg["billType"])
	if billType == "" {
		billType = "trade"
	}
	res, err := p.call(ctx, cfg, key, "alipay.data.dataservice.bill.downloadurl.query", map[string]any{
		"bill_type": billType,
		"bill_date": date,
	}, nil)
	if err != nil {
		return nil, err
	}
	if msg := alipayResponseError(res); msg != "" {
		return nil, channelGatewayError("alipay", "账单下载地址获取被渠道拒绝："+msg)
	}
	downloadURL := strings.TrimSpace(res["bill_download_url"])
	if downloadURL == "" {
		return nil, channelGatewayError("alipay", "账单应答缺少 bill_download_url")
	}
	raw, err := downloadAlipayBill(ctx, downloadURL)
	if err != nil {
		return nil, err
	}
	rows, err := parseAlipayBill(raw)
	if err != nil {
		return nil, channelGatewayError("alipay", err.Error())
	}
	sum := sha256.Sum256(raw)
	return &Bill{Rows: rows, Digest: hex.EncodeToString(sum[:]), RowCount: len(rows)}, nil
}

// downloadAlipayBill 拉账单文件原文。账单下载链接由支付宝 HTTPS 直发、没有独立签名，
// digest 只能用于事后复现与审计，不能当完整性证明。链接来自验过签的网关应答，
// 但仍然只接受 http/https，且不做磁盘落地（ZIP 条目只在内存里按内容读，
// 条目名不参与任何路径拼接，因此不存在 zip-slip）。
func downloadAlipayBill(ctx context.Context, downloadURL string) ([]byte, error) {
	u, err := url.Parse(downloadURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, channelGatewayError("alipay", "账单下载地址不是合法的 http(s) URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, channelGatewayError("alipay", "账单下载地址非法："+err.Error())
	}
	resp, err := gatewayClient.Do(req)
	if err != nil {
		return nil, channelGatewayError("alipay", "账单下载失败："+err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, channelGatewayError("alipay", fmt.Sprintf("账单下载返回 HTTP %d", resp.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, alipayBillMaxBytes))
	if err != nil {
		return nil, channelGatewayError("alipay", "读取账单失败："+err.Error())
	}
	if len(raw) >= alipayBillMaxBytes {
		return nil, channelGatewayError("alipay", fmt.Sprintf("账单超过 %d 字节上限", alipayBillMaxBytes))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, channelGatewayError("alipay", "账单内容为空")
	}
	return raw, nil
}

// parseAlipayBill 解析下载的账单原文：支付宝会把多个 CSV 打成 ZIP，
// 所以要区分「压缩包」与「单个 CSV」两种载荷。返回的是流水行，不含说明段与汇总段。
func parseAlipayBill(raw []byte) ([]BillRow, error) {
	files, err := splitAlipayBillFiles(raw)
	if err != nil {
		return nil, err
	}
	var rows []BillRow
	for _, data := range files {
		parsed, err := parseAlipayBillCSV(data)
		if err != nil {
			return nil, err
		}
		rows = append(rows, parsed...)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("alipay: 账单未解析出任何流水行（表头缺失或不是 UTF-8 CSV）：%s", truncate(string(raw), 160))
	}
	return rows, nil
}

// splitAlipayBillFiles 返回账单里所有需要解析的 CSV 内容（ZIP 会取其中每个 .csv 条目，
// 跳过 __MACOSX 与 AppleDouble 之类噪声）。非 ZIP 载荷原样返回。
func splitAlipayBillFiles(raw []byte) ([][]byte, error) {
	if !bytes.HasPrefix(raw, []byte("PK\x03\x04")) {
		return [][]byte{raw}, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("alipay: 账单是 ZIP 但打不开：%w", err)
	}
	var out [][]byte
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		base := name[strings.LastIndex(name, "/")+1:]
		if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(base), ".csv") {
			continue
		}
		if strings.HasPrefix(base, "._") || strings.Contains(name, "__MACOSX") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("alipay: 账单 ZIP 条目 %s 打不开：%w", base, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, alipayBillMaxBytes))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("alipay: 账单 ZIP 条目 %s 读取失败：%w", base, err)
		}
		out = append(out, data)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("alipay: 账单 ZIP 里没有 CSV 条目")
	}
	return out, nil
}

// alipayBillBOM 是账单 CSV 常见的前导字节序标记（UTF-8 BOM），不去掉会让第一列表头
// 匹配不上。用 rune 构造，避免源文件里出现不可见字符。
var alipayBillBOM = string(rune(0xFEFF))

// 账单列名（去掉单位括号、空白后的小写形态）。同义词按支付宝历史表头枚举，
// 命中第一个即用；都没命中就当作该列不存在。
var alipayBillAliases = map[string][]string{
	"paymentNo": {"商户订单号", "商户支付单号", "商家订单号", "商户单据号"},
	"refundNo":  {"商户退款单号", "商户退款编号", "退款编号", "退款单号", "退款序号"},
	"txn":       {"支付宝交易号", "支付宝流水号", "交易号"},
	"bizName":   {"业务名称", "交易类型", "业务类型"},
	"amount":    {"金额", "交易金额", "订单金额", "收入金额", "支付金额"},
	"refundAmt": {"退款金额", "实际退款金额", "退款"},
	"fee":       {"服务费", "服务费金额", "手续费", "交易服务费", "退款服务费"},
}

// parseAlipayBillCSV 在一份 CSV 里定位表头后逐行取值。
// Comment='#' 让新版账单的 # 说明段被 csv 读取器跳过；FieldsPerRecord=-1 容忍
// 说明段/汇总段的列数与表头不一致。
func parseAlipayBillCSV(data []byte) ([]BillRow, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.Comment = '#'
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true

	var idx map[string]int
	var rows []BillRow
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("alipay: 账单 CSV 解析失败: %w", err)
		}
		if idx == nil {
			if m := alipayBillHeaderIndex(rec); m != nil {
				idx = m
			}
			continue
		}
		if alipayBillSectionEnd(rec) {
			break // 汇总段：后面的都不是流水行
		}
		row, ok, err := alipayBillRowOf(rec, idx)
		if err != nil {
			return nil, err
		}
		if ok {
			rows = append(rows, row)
		}
	}
	if idx == nil {
		// 找不到表头基本就是渠道把 HTML 错误页当成账单返回了：必须报错，
		// 不能返回 0 行——0 行会被对账当成「渠道今天没有交易」。
		return nil, fmt.Errorf("alipay: 账单里找不到表头行（前若干行都不是列名，内容可能是渠道的错误页）：%s", truncate(string(data), 160))
	}
	return rows, nil
}

// alipayBillHeaderIndex 判断这一行是不是表头：至少要能认出「一个单号列 + 一个金额列」。
func alipayBillHeaderIndex(rec []string) map[string]int {
	m := map[string]int{}
	for i, cell := range rec {
		name := alipayBillColumnName(cell)
		if name == "" {
			continue
		}
		for col, aliases := range alipayBillAliases {
			if _, seen := m[col]; seen {
				continue
			}
			for _, alias := range aliases {
				if name == alias {
					m[col] = i
					break
				}
			}
		}
	}
	_, hasID := m["paymentNo"]
	if !hasID {
		_, hasID = m["refundNo"]
	}
	if !hasID {
		_, hasID = m["txn"]
	}
	_, hasAmount := m["amount"]
	_, hasRefund := m["refundAmt"]
	if !hasID || (!hasAmount && !hasRefund) {
		return nil
	}
	return m
}

// alipayBillColumnName 归一化表头：去 BOM/空白、去掉单位括号、小写。
// 「金额（元）」「服务费(元) 」「 商户订单号 」都归一到同一个键。
func alipayBillColumnName(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(s, alipayBillBOM))
	if i := strings.IndexAny(s, "（("); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return strings.ToLower(s)
}

// alipayBillSectionEnd 识别汇总段/分隔行：一旦遇到就不再往后读。
func alipayBillSectionEnd(rec []string) bool {
	for _, cell := range rec {
		v := strings.TrimSpace(cell)
		if v == "" {
			continue
		}
		if strings.Contains(v, "合计") || strings.Contains(v, "总计") {
			return true
		}
		if strings.Trim(v, "-—=") == "" && len(v) >= 3 {
			return true
		}
	}
	return false
}

func alipayBillRowOf(rec []string, idx map[string]int) (BillRow, bool, error) {
	cell := func(col string) string {
		i, ok := idx[col]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	row := BillRow{
		PaymentNo:    cell("paymentNo"),
		RefundNo:     cell("refundNo"),
		ChannelTxnID: cell("txn"),
	}
	if row.PaymentNo == "" && row.RefundNo == "" && row.ChannelTxnID == "" {
		return BillRow{}, false, nil // 说明段/空行：不是流水行
	}
	refundAmt := cell("refundAmt")
	isRefund := row.RefundNo != "" || strings.Contains(cell("bizName"), "退款")
	row.Kind = BillKindTrade
	raw := cell("amount")
	if isRefund {
		row.Kind = BillKindRefund
		if refundAmt != "" {
			raw = refundAmt
		}
	} else if refundAmt != "" && raw == "" {
		// 只有退款金额列有值的行就是退款行（有的账单类型不给业务名称列）。
		row.Kind = BillKindRefund
		raw = refundAmt
	}
	amount, err := alipayBillAmount(raw)
	if err != nil {
		return BillRow{}, false, fmt.Errorf("alipay: 账单行金额不可解析（%s / %s）: %w", row.PaymentNo, row.RefundNo, err)
	}
	row.AmountMinor = amount
	fee, err := alipayBillAmount(cell("fee"))
	if err != nil {
		return BillRow{}, false, fmt.Errorf("alipay: 账单行手续费不可解析（%s / %s）: %w", row.PaymentNo, row.RefundNo, err)
	}
	row.FeeMinor = fee
	return row, true, nil
}

// alipayBillAmount 把账单里的金额文本转成分：去 BOM/空白/货币符号/千分位分隔（含全角逗号），
// 再 minorFromYuan——全程字符串运算，绝不经 float。空值按 0。
func alipayBillAmount(s string) (int64, error) {
	s = strings.TrimSpace(strings.TrimPrefix(s, alipayBillBOM))
	if s == "" {
		return 0, nil
	}
	for _, cut := range []string{"￥", "¥", "元", "\"", "'", "，", ","} {
		s = strings.ReplaceAll(s, cut, "")
	}
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "+")
	if s == "" || s == "-" {
		return 0, nil
	}
	v, ok := minorFromYuan(s)
	if !ok {
		return 0, fmt.Errorf("金额 %q 不是数字", strings.TrimSpace(s))
	}
	return v, nil
}

// ---- 代付 / 单笔转账（PayoutProvider）----
//
// 走 alipay.fund.trans.uni.transfer（单笔、无密码转账）与 alipay.fund.trans.common.query。
// out_biz_no 用批次内的 ItemNo，保证重试不会重复打款；收款方标识按渠道要求映射成
// payee_info（ALIPAY_USER_ID / ALIPAY_LOGON_ID）。
// 转账产品需要商户签约（TRANS_ACCOUNT_NO_PWD + DIRECT_TRANSFER），未签约时渠道回
// 40004 + sub_code，这里原样包成 PAY_CHANNEL_ERROR 回传，绝不本地记「已打款」。
// ⚠ 需真商户号端到端验证：本泳道只验协议形状与错误分类，验不了签约、实名校验
// 与到账状态语义；代发工资这类需要场景报备（transfer_scene_info）的场景未实现。

func (p alipayProvider) Transfer(ctx context.Context, in TransferRequest, cfg map[string]string) (*TransferResult, error) {
	if err := cnyOnly("alipay", in.Currency); err != nil {
		return nil, err
	}
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	outBizNo := strings.TrimSpace(in.ItemNo)
	if outBizNo == "" {
		// 幂等号缺失时退回批次号：单批一次的打款仍然确定，不会因缺字段而每次换号重打。
		outBizNo = strings.TrimSpace(in.BatchNo)
	}
	if outBizNo == "" {
		return nil, channelConfigError("alipay", "代付缺少渠道幂等号（ItemNo / BatchNo 至少一个）")
	}
	if in.AmountMinor <= 0 {
		return nil, channelConfigError("alipay", "代付金额必须大于 0")
	}
	accountType, err := alipayTransferAccountType(in.ReceiverID)
	if err != nil {
		return nil, err
	}
	payee := map[string]any{"account_type": accountType, "identity": strings.TrimSpace(in.ReceiverID)}
	if name := strings.TrimSpace(in.ReceiverName); name != "" {
		payee["name"] = name
	}
	biz := map[string]any{
		"product_code": "TRANS_ACCOUNT_NO_PWD",
		"biz_scene":    "DIRECT_TRANSFER",
		"out_biz_no":   outBizNo,
		"trans_amount": yuanOf(in.AmountMinor),
		"order_title":  truncate(alipayFirstNonEmpty(strings.TrimSpace(in.Remark), "结算打款"), 128),
		"payee_info":   payee,
	}
	res, err := p.call(ctx, cfg, key, "alipay.fund.trans.uni.transfer", biz, alipayNotifyExtra(in.NotifyURL))
	if err != nil {
		return nil, err
	}
	if msg := alipayResponseError(res); msg != "" {
		return nil, channelGatewayError("alipay", "转账被渠道拒绝："+msg)
	}
	return alipayTransferResult(res), nil
}

// QueryTransfer 用 alipay.fund.trans.common.query 按 out_biz_no 查转账结果。
// 接口签名只给批次号（底座可考虑补成 batchNo+itemNo，见泳道回票），这里把它当
// out_biz_no 用——与 Transfer 的幂等号口径一致（ItemNo 缺失时同样是批次号）。
// 渠道查无此单时保持 Pending：代付单已经被我们提交过，「查不到」不能当成「没打款」，
// 否则重试会重复出金。
func (p alipayProvider) QueryTransfer(ctx context.Context, batchNo string, cfg map[string]string) (*TransferResult, error) {
	key, err := alipayPrivateKey(cfg["privateKey"])
	if err != nil {
		return nil, channelConfigError("alipay", "应用私钥不可用："+err.Error())
	}
	outBizNo := strings.TrimSpace(batchNo)
	if outBizNo == "" {
		return nil, channelConfigError("alipay", "查询转账缺少渠道幂等号（out_biz_no）")
	}
	res, err := p.call(ctx, cfg, key, "alipay.fund.trans.common.query", map[string]any{
		"product_code": "TRANS_ACCOUNT_NO_PWD",
		"biz_scene":    "DIRECT_TRANSFER",
		"out_biz_no":   outBizNo,
	}, nil)
	if err != nil {
		return nil, err
	}
	if alipayNotExist(res) {
		return &TransferResult{Pending: true, State: "NOT_FOUND", Reason: "渠道查无此转账（保持未定，需人工核对）"}, nil
	}
	if msg := alipayResponseError(res); msg != "" {
		return nil, channelGatewayError("alipay", "查询转账失败："+msg)
	}
	return alipayTransferResult(res), nil
}

// alipayTransferAccountType 按渠道要求把收款方标识映射成 account_type：
// 2088 开头的 16 位是 user_id，邮箱/手机号是登录号；其他形态（银行卡、openid 之类）
// 这个接口不收，直接报凭据错误而不是发一个渠道必拒的请求。
// ReceiverType（personal/merchant）在这里不改变映射：转账到支付宝账户两种主体都用同一组
// account_type，只有「转账到银行卡」才需要另一个 product_code，本渠道未实现。
func alipayTransferAccountType(receiverID string) (string, error) {
	id := strings.TrimSpace(receiverID)
	if id == "" {
		return "", channelConfigError("alipay", "代付缺少收款方标识（支付宝 user_id 或登录号）")
	}
	switch {
	case alipayUserIDRe.MatchString(id):
		return "ALIPAY_USER_ID", nil
	case strings.Contains(id, "@") || alipayMobileRe.MatchString(id):
		return "ALIPAY_LOGON_ID", nil
	}
	return "", channelConfigError("alipay", "代付收款方标识 "+truncate(id, 40)+" 不是支付宝 user_id/登录号（本渠道只支持转账到支付宝账户）")
}

var alipayMobileRe = regexp.MustCompile(`^1\d{10}$`)

// alipayTransferResult 把转账应答的 status 映射成结论。
// 渠道状态未知时按「已受理、结果未定」处理：代付的两种误判里，把已打款记成失败会重复出金。
func alipayTransferResult(res map[string]string) *TransferResult {
	status := strings.ToUpper(strings.TrimSpace(res["status"]))
	out := &TransferResult{
		ChannelBatchNo:  res["order_id"],
		ChannelDetailNo: res["pay_fund_order_id"],
		State:           status,
		Accepted:        true,
		Pending:         true,
	}
	switch status {
	case "SUCCESS":
		out.Pending = false
	case "FAIL", "CLOSED":
		out.Accepted = false
		out.Pending = false
		out.Reason = alipayFirstNonEmpty(res["fail_reason"], res["sub_msg"], "渠道返回转账失败")
	case "":
		out.Reason = "渠道应答缺少 status（按未定处理）"
	}
	return out
}

func init() { RegisterProvider(alipayProvider{}) }
