package pay

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/sha1" // 微信账单 hash_type=SHA1 是渠道规定，不是我们选的摘要算法
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"nexadmin/core/response"
)

// wechatProvider 是微信支付 APIv3 渠道：商户私钥签名请求，平台证书验应答与回调，
// 回调解密用 APIv3 密钥（AES-256-GCM）。支持扫码（native）、公众号（jsapi）、
// H5（h5）、APP（app）四种拉起方式；退款异步回执走同一 notify 端点。
//
// 平台证书有两种来源，优先手工、其次自动（见「平台证书自动下载与轮换」一节）：
//   - 手工：渠道配置里填 platformPublicKey（+ platformPublicKeyId），永远生效；
//   - 自动：调 GET /v3/certificates 下载并按序列号缓存、到期前轮换。
type wechatProvider struct{}

func (wechatProvider) Name() string { return "wechat" }

// 编译期把「这个渠道到底实现了哪些可选能力」钉在代码里：
// 少实现一个接口就编译不过或能力矩阵不一致，比靠注释记忆可靠。
// 注意这里 _故意_ 没有 `_ AuthProvider = wechatProvider{}`：微信支付没有通用开放的
// 资金预授权接口（理由见 Capabilities），服务层按类型断言失败报 errCapabilityUnsupported。
var (
	_ Provider            = wechatProvider{}
	_ SchemaProvider      = wechatProvider{}
	_ AckProvider         = wechatProvider{}
	_ CapabilityProvider  = wechatProvider{}
	_ RefundQueryProvider = wechatProvider{}
	_ BillProvider        = wechatProvider{}
	_ PayoutProvider      = wechatProvider{}
)

func (wechatProvider) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "appId", Type: "text", Required: true, Placeholder: "wx0000000000000000"},
		{Key: "mchId", Type: "text", Required: true, Placeholder: "1900000001"},
		{Key: "serialNo", Type: "text", Required: true, Placeholder: "商户 API 证书序列号"},
		{Key: "privateKey", Type: "password", Required: true, Secret: true, Placeholder: "apiclient_key.pem（PEM）"},
		{Key: "apiV3Key", Type: "password", Required: true, Secret: true, Placeholder: "APIv3 密钥（32 位）：回调解密与平台证书自动下载都依赖它"},
		// 平台证书兜底项：留空即走自动下载。轮换日手工抄的公钥必然失效，所以默认自动。
		{Key: "platformPublicKey", Type: "password", Required: false, Secret: true, Placeholder: "（可选兜底）手工配置的微信支付公钥/平台证书（PEM）；留空则自动调 GET /v3/certificates 下载并轮换"},
		{Key: "platformPublicKeyId", Type: "text", Required: false, Placeholder: "（可选兜底）手工公钥对应的 PUB_KEY_ID_xxx，只在填了上面的公钥时生效"},
		{Key: "gateway", Type: "url", Required: false, Placeholder: wechatAPIBase},
	}
}

func (wechatProvider) CallbackAck() (string, string) {
	return "application/json; charset=utf-8", `{"code":"SUCCESS","message":"成功"}`
}

const wechatAPIBase = "https://api.mch.weixin.qq.com"

// wechatGateway 取渠道网关根地址：cfg["gateway"] 只用于联调/测试桩指向别处，
// 生产留空即微信官方网关。尾部斜杠一律去掉，避免拼出 //v3/... 的路径。
func wechatGateway(cfg map[string]string) string {
	if g := strings.TrimRight(strings.TrimSpace(cfg["gateway"]), "/"); g != "" {
		return g
	}
	return wechatAPIBase
}

// Capabilities 如实声明微信 APIv3 渠道的能力矩阵。
//
// Auth=false 的判断依据（不要「顺手补上」）：微信支付没有通用开放的资金预授权接口。
// 「冻结—请款—撤销」这套语义在微信侧只存在于两个需要单独签约的场景产品里——
// 微信支付分（服务分/先享后付）与押金类预授权，二者都不是商户可以在中台里自助开通的
// 通用资金授权，接口形态（service_order/押金单号）也和 AuthProvider 的
// Freeze/CompleteAuth/CancelAuth 对不上，且要求付款人在微信侧已完成授权签约。
// 因此本渠道不实现 AuthProvider，让服务层用类型断言失败报 errCapabilityUnsupported
// （PAY_CAPABILITY_UNSUPPORTED，语义是「换渠道或去渠道侧签约」）。
// 绝不允许伪造一个渠道授权号（此前 auths.go 的 mockauth_ 就是这个缺陷）——
// 资金中台里「看起来冻结了但微信侧没有」比明确失败危险得多。
//
// Methods 只列四种已实现的规范取值；Create 内部还把 qr/scan→native、
// mp/mini→jsapi、wap/mweb→h5 归一进来，服务层若要按 caps 拦截下单方式，
// 请先归一再比对（否则别名会被误判成不支持）。
func (wechatProvider) Capabilities() Capabilities {
	return Capabilities{
		Currencies:  []string{"CNY"},
		Methods:     []string{"native", "jsapi", "h5", "app"},
		RefundQuery: true,
		Auth:        false,
		Bill:        true,
		Payout:      true,
	}
}

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

// QueryRefund 实现 RefundQueryProvider：回调丢失时主动收敛退款状态。
// 路径 GET /v3/refund/domestic/refunds/{out_refund_no}，其中的 out_refund_no 必须与
// 发起退款时同一个（req.IdempotencyKey），否则微信认不出是同一笔退款。
// 渠道原始状态一律放进 Reason，方便对着微信商户平台排障。
func (p wechatProvider) QueryRefund(ctx context.Context, req RefundQueryRequest, cfg map[string]string) (*RefundStatusResult, error) {
	outRefundNo := wechatRefundQueryKey(req)
	if outRefundNo == "" {
		return nil, channelConfigError("wechat", "退款查询缺少 out_refund_no（IdempotencyKey 与 RefundNo 都为空）")
	}
	path := "/v3/refund/domestic/refunds/" + url.PathEscape(outRefundNo)
	status, raw, err := p.call(ctx, cfg, http.MethodGet, path, "")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		// 查无此退款：既不成功也不失败，服务层应保持 pending 并告警等人工核对。
		return &RefundStatusResult{Found: false, Reason: "微信查无此退款单（out_refund_no=" + outRefundNo + "）"}, nil
	}
	res, err := wechatResult("wechat", status, raw)
	if err != nil {
		return nil, err
	}
	out := &RefundStatusResult{Found: true, ChannelRefundID: res["refund_id"]}
	switch st := res["status"]; st {
	case "SUCCESS":
		out.OK = true
		out.Pending = false
		out.Reason = "微信退款状态 SUCCESS"
	case "PROCESSING":
		out.Pending = true
		out.Reason = "微信退款状态 PROCESSING（渠道仍在处理）"
	case "ABNORMAL", "CLOSED":
		out.OK = false
		out.Pending = false
		out.Reason = "微信退款状态 " + st
	default:
		// 渠道给了没见过的状态：不下结论，保持 pending（假完成比挂住危险）。
		out.Pending = true
		out.Reason = "微信退款状态未知: " + st
	}
	return out, nil
}

// wechatRefundQueryKey 与 Refund 用同一个口径取渠道侧退款幂等号，
// 保证「发起」与「补偿查询」在微信看来是同一笔。
func wechatRefundQueryKey(req RefundQueryRequest) string {
	if k := strings.TrimSpace(req.IdempotencyKey); k != "" {
		return k
	}
	return strings.TrimSpace(req.RefundNo)
}

// VerifyCallback 验微信支付签名 → 用 APIv3 密钥解密 resource → 按事件类型给结论。
//
// 验签用的平台证书：填了 platformPublicKey 就用手工的（兜底路径），否则按报文头
// Wechatpay-Serial 从自动下载的证书缓存里选；缓存未命中会强制重下载一次再判一次。
// 两条路都拿不到可用证书时直接报错——不存在「跳过验签继续」的分支。
func (p wechatProvider) VerifyCallback(in CallbackInput, cfg map[string]string) (*CallbackResult, error) {
	apiKey := []byte(strings.TrimSpace(cfg["apiV3Key"]))
	if len(apiKey) != 32 {
		return nil, fmt.Errorf("wechat: apiV3Key 必须是 32 位")
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
	// 铁律②里「每次下发都重新生成、且参与签名」的那一类：微信重试会刷新
	// timestamp/nonce/签名，所以 5 分钟双向硬窗对它成立（不同于支付宝 notify_time）。
	// 真正的重复入账防线仍是 out_trade_no + 金额核对 + 状态机幂等。
	if d := time.Since(time.Unix(sec, 0)); d > CallbackClockSkew || d < -CallbackClockSkew {
		return nil, fmt.Errorf("wechat: 回调时间戳超出允许窗口")
	}
	// Provider 契约没有 ctx，证书下载用独立超时上下文，不让回调挂死。
	dlCtx, cancel := context.WithTimeout(context.Background(), wechatCertDownloadTimeout)
	defer cancel()
	pub, err := p.platformKey(dlCtx, cfg, serial)
	if err != nil {
		return nil, err
	}
	if !rsaVerifySHA256(pub, ts+"\n"+nonce+"\n"+string(in.Body)+"\n", sig) {
		return nil, fmt.Errorf("wechat: 回调验签失败")
	}

	var ev struct {
		ID        string `json:"id"`
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
	// 取证号：微信给这条通知的事件 id；缺失时用 serial+timestamp+nonce 拼指纹。
	notifyID := wechatNotifyID(ev.ID, serial, ts, nonce)
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
			NotifyID: notifyID, GatewayStatus: tx.TradeState,
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
			NotifyID: notifyID, GatewayStatus: rf.RefundStatus,
		}, nil
	default:
		// 其他事件（如优惠/授权变更）只应答，不改本地状态；NotifyID 仍要留给审计。
		return &CallbackResult{NotifyID: notifyID, GatewayStatus: ev.EventType}, nil
	}
}

// ---- 平台证书自动下载与轮换 ----
//
// 微信用「平台证书」私钥给应答与回调签名，证书会轮换；轮换当天手工抄的公钥必然验签失败。
// 协议自带的 GET /v3/certificates 返回加密证书列表：每张证书的密文用 APIv3 密钥做
// AES-256-GCM，GCM tag 校验通过本身就证明「只有持有同一 APIv3 密钥的微信才能给出这份
// 列表」，所以这一步不再用平台证书验应答签名（那一刻手里还没有证书，是协议上的鸡生蛋）。
// 拿到证书之后，所有应答与回调一律按 Wechatpay-Serial 选证书验签；缺密钥、缺序列号、
// 序列号查不到都是明确报错，没有任何「不验签也能走通」的路径。
const (
	wechatCertPath            = "/v3/certificates"
	wechatCertRefreshEvery    = 12 * time.Hour   // 定时刷新节奏（文档允许更长，取 12h 兼顾轮换及时性与网关调用量）
	wechatCertExpireLead      = 1 * time.Hour    // 比最早到期时间提前刷新，避开轮换窗口
	wechatCertRetryBackoff    = 30 * time.Second // 下载失败后的退避，防止每次回调都打网关
	wechatCertDownloadTimeout = 10 * time.Second
	wechatCertGCMAlgorithm    = "AEAD_AES_256_GCM"
)

type wechatPlatformCert struct {
	Serial    string
	PublicKey *rsa.PublicKey
	NotAfter  time.Time
}

type wechatCertSet struct {
	certs     map[string]*wechatPlatformCert // key：大写证书序列号
	fetchedAt time.Time
	nextFetch time.Time
}

type wechatCertFailure struct {
	err     error
	retryAt time.Time
}

// 证书缓存按商户号分桶：同一进程可能挂多个微信渠道实例，凭据不同证书也不同。
var wechatCertCache = struct {
	mu       sync.Mutex
	byMch    map[string]*wechatCertSet
	failures map[string]*wechatCertFailure
}{
	byMch:    make(map[string]*wechatCertSet),
	failures: make(map[string]*wechatCertFailure),
}

// wechatCertCacheReset 清空证书缓存（测试换桩网关、或运维改凭据后强制重新下载）。
func wechatCertCacheReset() {
	wechatCertCache.mu.Lock()
	defer wechatCertCache.mu.Unlock()
	wechatCertCache.byMch = make(map[string]*wechatCertSet)
	wechatCertCache.failures = make(map[string]*wechatCertFailure)
}

// certSetOf 取（必要时下载）商户号对应的平台证书集合；force=true 用于「序列号未命中」
// 之后的一次性重下载。下载在锁内串行执行：这是有意的单飞（single-flight），
// 避免轮换瞬间每个回调都去敲 /v3/certificates 被限流。
func (p wechatProvider) certSetOf(ctx context.Context, cfg map[string]string, force bool) (*wechatCertSet, error) {
	mch := strings.TrimSpace(cfg["mchId"])
	if mch == "" {
		return nil, channelConfigError("wechat", "自动下载平台证书需要 mchId")
	}
	if len(strings.TrimSpace(cfg["apiV3Key"])) != 32 {
		// 凭据缺失要说清楚：这是配置问题，同时堵住「没密钥就不验签」的降级口子。
		return nil, channelConfigError("wechat",
			"自动下载平台证书需要 32 位 apiV3Key（或改用 platformPublicKey 手工兜底），拒绝降级为不验签")
	}
	wechatCertCache.mu.Lock()
	defer wechatCertCache.mu.Unlock()
	if !force {
		if set, ok := wechatCertCache.byMch[mch]; ok && time.Now().Before(set.nextFetch) {
			return set, nil
		}
		if f, ok := wechatCertCache.failures[mch]; ok && time.Now().Before(f.retryAt) {
			return nil, f.err
		}
	}
	set, err := p.downloadCerts(ctx, cfg)
	if err != nil {
		wechatCertCache.failures[mch] = &wechatCertFailure{err: err, retryAt: time.Now().Add(wechatCertRetryBackoff)}
		delete(wechatCertCache.byMch, mch)
		return nil, err
	}
	delete(wechatCertCache.failures, mch)
	wechatCertCache.byMch[mch] = set
	return set, nil
}

func (p wechatProvider) downloadCerts(ctx context.Context, cfg map[string]string) (*wechatCertSet, error) {
	status, raw, err := p.do(ctx, cfg, http.MethodGet, wechatCertPath, "", false)
	if err != nil {
		return nil, err
	}
	if _, err := wechatResult("wechat", status, raw); err != nil {
		return nil, err // 非 2xx：把微信给的 code/message 原样带出去
	}
	var doc struct {
		Certificates []struct {
			SerialNo      string `json:"serial_no"`
			EffectiveTime string `json:"effective_time"`
			ExpireTime    string `json:"expire_time"`
			Encrypt       struct {
				Algorithm      string `json:"algorithm"`
				Nonce          string `json:"nonce"`
				AssociatedData string `json:"associated_data"`
				Ciphertext     string `json:"ciphertext"`
			} `json:"encrypt_certificate"`
		} `json:"certificates"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, channelGatewayError("wechat", "平台证书应答不是 JSON")
	}
	if len(doc.Certificates) == 0 {
		return nil, channelGatewayError("wechat", "平台证书列表为空")
	}
	apiKey := []byte(strings.TrimSpace(cfg["apiV3Key"]))
	now := time.Now()
	set := &wechatCertSet{certs: make(map[string]*wechatPlatformCert, len(doc.Certificates)), fetchedAt: now}
	next := now.Add(wechatCertRefreshEvery)
	for _, item := range doc.Certificates {
		alg := strings.ToUpper(strings.TrimSpace(item.Encrypt.Algorithm))
		if alg != "" && alg != wechatCertGCMAlgorithm {
			return nil, channelGatewayError("wechat", "平台证书加密算法不支持: "+alg)
		}
		plain, err := wechatDecrypt(apiKey, item.Encrypt.AssociatedData, item.Encrypt.Nonce, item.Encrypt.Ciphertext)
		if err != nil {
			// GCM tag 不通过 = 不是同一 APIv3 密钥签发的列表：拒绝，绝不拿明文凑合验签。
			return nil, channelGatewayError("wechat", "平台证书解密失败（APIv3 密钥不匹配或应答被篡改）")
		}
		cert, err := wechatParseCertificate(plain)
		if err != nil {
			return nil, channelGatewayError("wechat", "平台证书不可用: "+err.Error())
		}
		pub, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, channelGatewayError("wechat", "平台证书不是 RSA 公钥")
		}
		serial := strings.ToUpper(strings.TrimSpace(item.SerialNo))
		certSerial := wechatCertSerialHex(cert)
		if certSerial != "" && serial != "" && serial != certSerial {
			// 应答里的 serial_no 与证书自身序列号不一致：证书列表被动过手脚的信号。
			return nil, channelGatewayError("wechat", "平台证书序列号与证书自身不一致: "+serial)
		}
		if serial == "" {
			serial = certSerial
		}
		notAfter := cert.NotAfter
		if t, terr := time.Parse(time.RFC3339, strings.TrimSpace(item.ExpireTime)); terr == nil && !t.IsZero() {
			notAfter = t
		}
		set.certs[serial] = &wechatPlatformCert{Serial: serial, PublicKey: pub, NotAfter: notAfter}
		if until := notAfter.Add(-wechatCertExpireLead); until.Before(next) {
			next = until
		}
	}
	if len(set.certs) == 0 {
		return nil, channelGatewayError("wechat", "平台证书列表解密后为空")
	}
	// 最早到期时间可能已经过去（证书在架但即将下线）：刷新节奏仍留一个最小间隔，
	// 否则每个回调都会去打 /v3/certificates。
	if floor := now.Add(wechatCertRetryBackoff); next.Before(floor) {
		next = floor
	}
	set.nextFetch = next
	return set, nil
}

// wechatParseCertificate 解析证书 PEM（也容忍纯 DER），供 x509 校验。
func wechatParseCertificate(b []byte) (*x509.Certificate, error) {
	if block, _ := pem.Decode(b); block != nil {
		return x509.ParseCertificate(block.Bytes)
	}
	return x509.ParseCertificate(bytes.TrimSpace(b))
}

func wechatCertSerialHex(cert *x509.Certificate) string {
	if cert == nil || cert.SerialNumber == nil {
		return ""
	}
	return strings.ToUpper(cert.SerialNumber.Text(16))
}

// platformKey 按报文头序列号给出验签用的平台公钥。
// 手工 platformPublicKey 优先（兜底路径），否则用自动下载的证书缓存。
func (p wechatProvider) platformKey(ctx context.Context, cfg map[string]string, serial string) (*rsa.PublicKey, error) {
	serial = strings.TrimSpace(serial)
	if manual := strings.TrimSpace(cfg["platformPublicKey"]); manual != "" {
		pub, err := parseRSAPublicKey(manual) // parseRSAPublicKey 同时支持 PUBLIC KEY PEM 与 CERTIFICATE PEM
		if err != nil {
			return nil, channelConfigError("wechat", "微信支付公钥/平台证书不可用: "+err.Error())
		}
		if want := strings.TrimSpace(cfg["platformPublicKeyId"]); want != "" && serial != "" && serial != want {
			return nil, channelGatewayError("wechat", "报文 Wechatpay-Serial 与渠道配置的公钥 ID 不一致")
		}
		return pub, nil
	}
	set, err := p.certSetOf(ctx, cfg, false)
	if err != nil {
		return nil, err
	}
	if serial == "" {
		return nil, channelGatewayError("wechat", "报文缺少 Wechatpay-Serial，无法选择平台证书（拒绝，不降级为不验签）")
	}
	want := strings.ToUpper(serial)
	if c, ok := set.certs[want]; ok {
		return c.PublicKey, nil
	}
	// 未命中多半是刚轮换：强制重下载一次再判一次。
	set, err = p.certSetOf(ctx, cfg, true)
	if err != nil {
		return nil, err
	}
	if c, ok := set.certs[want]; ok {
		return c.PublicKey, nil
	}
	return nil, channelGatewayError("wechat", "平台证书序列号 "+serial+" 在微信证书列表中找不到")
}

// call 发签名请求并验应答签名。status 交给调用方判定（查询 404 是正常业务态）。
func (p wechatProvider) call(ctx context.Context, cfg map[string]string, method, path string, payload any) (int, []byte, error) {
	return p.do(ctx, cfg, method, path, payload, true)
}

// do 发签名请求。verify=false 只用于 GET /v3/certificates（那一刻还没有平台证书，
// 完整性由应答自身的 AES-256-GCM tag 保证）；其余调用一律验应答签名。
func (p wechatProvider) do(ctx context.Context, cfg map[string]string, method, path string, payload any, verify bool) (int, []byte, error) {
	body := ""
	if s, ok := payload.(string); ok && s == "" {
		payload = nil // GET 一律无请求体：签名串的 body 必须是空串，不能是 JSON 的 `""`
	}
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
	req, err := http.NewRequestWithContext(ctx, method, wechatGateway(cfg)+path, strings.NewReader(body))
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
	if verify {
		if err := p.verifyResponse(ctx, cfg, resp.Header, raw); err != nil {
			return 0, nil, err
		}
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

// verifyResponse 验应答签名：时间戳双向硬窗（应答签名每次重新生成，铁律②的第一类语义）
// + 按 Wechatpay-Serial 选平台证书。
func (p wechatProvider) verifyResponse(ctx context.Context, cfg map[string]string, h http.Header, body []byte) error {
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
	pub, err := p.platformKey(ctx, cfg, h.Get("Wechatpay-Serial"))
	if err != nil {
		return err
	}
	if !rsaVerifySHA256(pub, ts+"\n"+nonce+"\n"+string(body)+"\n", sig) {
		return channelGatewayError("wechat", "网关应答验签失败")
	}
	return nil
}

// wechatNotifyID 回调取证号：微信事件 id 优先；缺失时用 serial+timestamp+nonce 拼一个
// 可检索指纹。列宽 128（Callback.NotifyID），只做取证，不做唯一约束。
func wechatNotifyID(id, serial, ts, nonce string) string {
	if v := strings.TrimSpace(id); v != "" {
		return truncate(v, 128)
	}
	parts := make([]string, 0, 3)
	if s := strings.TrimSpace(serial); s != "" {
		parts = append(parts, "serial="+s)
	}
	if ts != "" {
		parts = append(parts, "ts="+ts)
	}
	if nonce != "" {
		parts = append(parts, "nonce="+nonce)
	}
	return truncate(strings.Join(parts, ";"), 128)
}

// ---- 渠道账单（对账 source=channel）----
//
// 两步：GET /v3/bill/tradebill 拿 download_url + hash_type/hash_value → 下载文件。
// 微信账单是 gzip 压缩的 CSV，行内字段带 ` 前缀（防 Excel 科学计数法），以 # 开头的
// 行是注释/汇总段；hash_value 是「压缩原文」的摘要，所以先校验摘要再解压。
// 账单本体不进返回值也不落库（可达数 MB），只回 Rows/Digest/RowCount。
const (
	wechatTradeBillPath = "/v3/bill/tradebill"
	wechatBillTypeAll   = "ALL"
	// wechatBillMaxBytes 是账单下载硬上限：超限直接失败，不能让对账任务吃满内存。
	wechatBillMaxBytes = 32 << 20
)

// 账单的三类失败路径，彼此不混淆（各有测试）：
// 下载/解压失败、摘要不符、下载成功但解析出 0 行。
var (
	errWechatBillDownload = errors.New("wechat: bill download failed")
	errWechatBillHash     = errors.New("wechat: bill hash mismatch")
	errWechatBillFormat   = errors.New("wechat: bill format invalid")
	errWechatBillNoRows   = errors.New("wechat: bill parsed zero rows")
)

func wechatBillError(detail string, cause error) error {
	return response.New(http.StatusBadGateway, errChannelError).WithMsg("wechat: " + detail).Wrap(cause)
}

func (p wechatProvider) FetchBill(ctx context.Context, in BillRequest, cfg map[string]string) (*Bill, error) {
	date := strings.TrimSpace(in.BillDate)
	if !billDateRe.MatchString(date) {
		return nil, channelConfigError("wechat", "账单日期需要 YYYY-MM-DD，当前 "+date)
	}
	path := wechatTradeBillPath + "?bill_date=" + url.QueryEscape(date) + "&bill_type=" + wechatBillTypeAll
	status, raw, err := p.call(ctx, cfg, http.MethodGet, path, "")
	if err != nil {
		return nil, err
	}
	res, err := wechatResult("wechat", status, raw)
	if err != nil {
		return nil, err
	}
	downloadURL := strings.TrimSpace(res["download_url"])
	hashType := strings.ToUpper(strings.TrimSpace(res["hash_type"]))
	hashValue := strings.ToLower(strings.TrimSpace(res["hash_value"]))
	if downloadURL == "" {
		return nil, wechatBillError("账单接口未返回 download_url（账单可能尚未生成，稍后重试）", errWechatBillDownload)
	}
	if hashValue == "" {
		// 没有摘要就无法证明下载到的就是这份账单，直接拒绝，不做「无摘要也解析」。
		return nil, wechatBillError("账单接口未返回 hash_value，无法校验账单完整性", errWechatBillHash)
	}
	if err := wechatSameGateway(cfg, downloadURL); err != nil {
		return nil, err
	}
	file, err := p.downloadBill(ctx, cfg, downloadURL)
	if err != nil {
		return nil, err
	}
	if err := wechatCheckBillHash(file, hashType, hashValue); err != nil {
		return nil, err
	}
	rows, text, err := parseWechatTradeBill(file)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		// 与「下载失败」分开：这里网络与摘要都过了，是账单当天没有明细行/表头不认识。
		return nil, wechatBillError("账单解析出 0 行（下载与摘要校验均通过，请核对表头列名或当天是否确有交易）", errWechatBillNoRows)
	}
	return &Bill{Rows: rows, Digest: wechatDigest(text), RowCount: len(rows)}, nil
}

// wechatSameGateway 限制 download_url 必须落在刚才那台网关上：
// 渠道应答不能可信到把中台变成任意 URL 抓取器（SSRF + 换包）。
func wechatSameGateway(cfg map[string]string, downloadURL string) error {
	want, err := url.Parse(wechatGateway(cfg))
	if err != nil {
		return channelConfigError("wechat", "渠道网关地址非法: "+err.Error())
	}
	got, err := url.Parse(downloadURL)
	if err != nil {
		return wechatBillError("账单下载地址非法: "+err.Error(), errWechatBillDownload)
	}
	if got.Scheme != want.Scheme {
		return wechatBillError("账单下载地址协议与渠道网关不一致: "+got.Scheme, errWechatBillDownload)
	}
	if !strings.EqualFold(got.Host, want.Host) {
		return wechatBillError("账单下载地址主机与渠道网关不一致: "+got.Host, errWechatBillDownload)
	}
	return nil
}

func (p wechatProvider) downloadBill(ctx context.Context, cfg map[string]string, downloadURL string) ([]byte, error) {
	u, err := url.Parse(downloadURL)
	if err != nil {
		return nil, wechatBillError("账单下载地址非法: "+err.Error(), errWechatBillDownload)
	}
	path := u.Path
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	auth, err := wechatAuthorization(cfg, http.MethodGet, path, "")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, wechatBillError("账单下载请求构造失败: "+err.Error(), errWechatBillDownload)
	}
	req.Header.Set("Authorization", auth)
	// 不要 Accept-Encoding：让 Go 自己处理，我们按「收到的字节」校验渠道给的摘要。
	resp, err := gatewayClient.Do(req)
	if err != nil {
		return nil, wechatBillError("账单下载失败："+err.Error(), errWechatBillDownload)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, wechatBillError(fmt.Sprintf("账单下载 HTTP %d：%s",
			resp.StatusCode, truncate(strings.TrimSpace(string(preview)), 200)), errWechatBillDownload)
	}
	file, err := io.ReadAll(io.LimitReader(resp.Body, wechatBillMaxBytes+1))
	if err != nil {
		return nil, wechatBillError("读取账单失败："+err.Error(), errWechatBillDownload)
	}
	if len(file) > wechatBillMaxBytes {
		return nil, wechatBillError(fmt.Sprintf("账单超过 %d 字节上限", wechatBillMaxBytes), errWechatBillDownload)
	}
	return file, nil
}

// wechatCheckBillHash 按渠道给的 hash_type/hash_value 校验账单原文（压缩态）。
// 摘要算法不认识也必须报错：账单没被证明完整就不能参与对账。
func wechatCheckBillHash(file []byte, hashType, hashValue string) error {
	var sum string
	switch hashType {
	case "SHA1":
		s := sha1.Sum(file) // 渠道规定的账单摘要，非本地选择
		sum = hex.EncodeToString(s[:])
	case "SHA256":
		s := sha256.Sum256(file)
		sum = hex.EncodeToString(s[:])
	case "":
		return wechatBillError("账单缺少 hash_type，无法校验完整性", errWechatBillHash)
	default:
		return wechatBillError("账单摘要算法不支持: "+hashType+"（SM3 需引入国密依赖，本渠道未实现）", errWechatBillHash)
	}
	if !strings.EqualFold(sum, strings.TrimSpace(hashValue)) {
		return wechatBillError("账单摘要与渠道声明不一致，已丢弃（计算值 "+sum+"）", errWechatBillHash)
	}
	return nil
}

// wechatDigest 是解析用账单文本的 sha256（十六进制）。注意它与渠道的 hash_value
// 不在同一层：hash_value 校验的是压缩原文，Digest 固定取解压后的文本，便于
// 「同一份账单重跑对账」时比对行集合是否一致。
func wechatDigest(text []byte) string {
	sum := sha256.Sum256(text)
	return hex.EncodeToString(sum[:])
}

// 账单列名（含微信历史上用过的别名），按下标取值的写法一律禁止：
// 微信加列是常态，只有按表头名定位才不会把金额读成订单号。
var (
	wechatBillColPayment  = []string{"商户订单号"}
	wechatBillColTxn      = []string{"微信支付订单号", "微信订单号"}
	wechatBillColRefund   = []string{"商户退款单号", "商户请求退款单号"}
	wechatBillColRefTxn   = []string{"微信退款单号", "微信支付退款单号"}
	wechatBillColTradeAMT = []string{"实收金额", "订单金额", "应结算金额", "总金额"}
	wechatBillColRefAMT   = []string{"退款金额", "总金额（退款）"}
	wechatBillColFee      = []string{"手续费金额", "服务费", "应结算手续费"}
	wechatBillColState    = []string{"交易状态", "录制状态"}
	wechatBillColRefState = []string{"退款状态"}
)

// parseWechatTradeBill 解压（微信账单是 gzip）并按表头列名解析交易行与退款行。
// 返回 (行, 账单文本, error)；文本供上层算 Digest。
func parseWechatTradeBill(file []byte) ([]BillRow, []byte, error) {
	text, err := wechatMaybeGunzip(file)
	if err != nil {
		return nil, nil, wechatBillError("账单解压失败: "+err.Error(), errWechatBillDownload)
	}
	text = bytes.TrimPrefix(text, []byte("\ufeff"))
	header := map[string]int{}
	var rows []BillRow
	for _, line := range strings.Split(string(text), "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue // 注释/汇总段（#合计、# 微信支付账单 之类）
		}
		cells := wechatSplitCSVLine(line)
		if len(header) == 0 {
			for i, c := range cells {
				key := strings.TrimSpace(c)
				if _, dup := header[key]; !dup {
					header[key] = i
				}
			}
			if wechatBillLookup(header, wechatBillColPayment) < 0 {
				return nil, nil, wechatBillError("账单表头缺少 商户订单号 列，无法定位交易行", errWechatBillFormat)
			}
			continue
		}
		row, ok := wechatBillRowFromCells(header, cells)
		if ok {
			rows = append(rows, row...)
		}
	}
	return rows, text, nil
}

// wechatBillRowFromCells 从一行账单生成 0..2 条 BillRow：
// 带商户订单号且金额可解析 → 一条 trade 行；同一行还带商户退款单号 → 再加一条 refund 行
// （微信交易账单把退款信息附在原交易行上，资金账单则是独立退款行，两种形状都覆盖）。
func wechatBillRowFromCells(header map[string]int, cells []string) ([]BillRow, bool) {
	out := make([]BillRow, 0, 2)
	state := wechatBillCell(header, cells, wechatBillColState)
	if state != "" && wechatTradeStateUncleared(state) {
		return nil, false // 明确没成功的交易不进对账
	}
	paymentNo := wechatBillCell(header, cells, wechatBillColPayment)
	refundNo := wechatBillCell(header, cells, wechatBillColRefund)
	refTxn := wechatBillCell(header, cells, wechatBillColRefTxn)
	if paymentNo == "" && refundNo == "" && refTxn == "" {
		return nil, false
	}
	if paymentNo != "" {
		if amt, ok := wechatBillAmount(wechatBillCell(header, cells, wechatBillColTradeAMT)); ok {
			fee, _ := wechatBillAmount(wechatBillCell(header, cells, wechatBillColFee))
			out = append(out, BillRow{
				Kind: BillKindTrade, PaymentNo: paymentNo, AmountMinor: amt, FeeMinor: fee,
				ChannelTxnID: wechatBillCell(header, cells, wechatBillColTxn),
			})
		}
	}
	if refundNo != "" || refTxn != "" {
		if amt, ok := wechatBillAmount(wechatBillCell(header, cells, wechatBillColRefAMT)); ok {
			fee, _ := wechatBillAmount(wechatBillCell(header, cells, wechatBillColFee))
			out = append(out, BillRow{
				Kind: BillKindRefund, PaymentNo: paymentNo, RefundNo: refundNo,
				AmountMinor: amt, FeeMinor: fee, ChannelTxnID: refTxn,
			})
		}
	}
	return out, len(out) > 0
}

// wechatTradeStateUncleared 报告该交易状态是否明确表示「这笔没收到钱」。
// REFUND 是「收款后又退款」，钱确实收到过，所以不算未成功。
func wechatTradeStateUncleared(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "CLOSED", "REVOKED", "PAYERROR", "FAIL", "FAILED", "INIT", "NOTPAY", "USERPAYING":
		return true
	default:
		return false
	}
}

// wechatBillLookup 按候选列名（含历史别名）找列下标；找不到返回 -1。
// 一律按表头名定位：微信加列是常态，写死下标迟早把金额读成订单号。
func wechatBillLookup(header map[string]int, names []string) int {
	for _, n := range names {
		if i, ok := header[n]; ok {
			return i
		}
	}
	return -1
}

func wechatBillCell(header map[string]int, cells []string, names []string) string {
	i := wechatBillLookup(header, names)
	if i < 0 || i >= len(cells) {
		return ""
	}
	return cells[i]
}

// wechatMaybeGunzip 只在内容确实是 gzip 时解压：Go 的 transport 可能已经透明解过，
// 也可能没解（微信用 Content-Type: application/gzip 而非 Content-Encoding）。
func wechatMaybeGunzip(b []byte) ([]byte, error) {
	if len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
		return b, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	// 解压后不再受 32MB 约束前，先按同量级设限，防 gzip 炸弹。
	out, err := io.ReadAll(io.LimitReader(zr, wechatBillMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(out) > wechatBillMaxBytes {
		return nil, fmt.Errorf("解压后账单超过 %d 字节上限", wechatBillMaxBytes)
	}
	return out, nil
}

// wechatSplitCSVLine 按双引号语义切列（账单里的金额/商品名可能带引号与逗号）。
func wechatSplitCSVLine(line string) []string {
	line = strings.TrimSuffix(line, "\r")
	out := make([]string, 0, 16)
	var cur strings.Builder
	quoted := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			if quoted && i+1 < len(line) && line[i+1] == '"' {
				cur.WriteByte('"')
				i++
				continue
			}
			quoted = !quoted
		case c == ',' && !quoted:
			out = append(out, normalizeWechatCell(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	out = append(out, normalizeWechatCell(cur.String()))
	return out
}

// normalizeWechatCell 去掉微信为防 Excel 科学计数法加的 ` 前缀与包裹引号；
// 千分位逗号只在金额解析时去，避免改动订单号/商品名等非金额字段。
func normalizeWechatCell(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "`")
	s = strings.Trim(s, "\"")
	return strings.TrimSpace(s)
}

// wechatBillAmount 把账单里的「元」金额读成分（绝不经 float），容忍千分位与空串。
func wechatBillAmount(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	s = strings.NewReplacer(",", "", "￥", "", " ", "").Replace(s)
	return minorFromYuan(s)
}

// ---- 代付 / 商家转账到零钱 ----
//
// POST /v3/transfer/batches 受理批次，GET /v3/transfer/batches/out-batch-no/{no} 查询。
// 前提与约束都由渠道原始错误回传（PAY_CHANNEL_ERROR）：商户必须签约「转账产品」、
// 单笔/单日额度、收款用户 openid 必须属于本 appid——本渠道不猜、不补、不假成功。
const (
	wechatTransferBatchPath    = "/v3/transfer/batches"
	wechatTransferQueryPath    = "/v3/transfer/batches/out-batch-no/"
	wechatTransferDetailMaxLen = 32
)

func (p wechatProvider) Transfer(ctx context.Context, in TransferRequest, cfg map[string]string) (*TransferResult, error) {
	if err := cnyOnly("wechat", in.Currency); err != nil {
		return nil, err
	}
	batchNo := strings.TrimSpace(in.BatchNo)
	itemNo := strings.TrimSpace(in.ItemNo)
	if batchNo == "" || itemNo == "" {
		return nil, channelConfigError("wechat", "转账需要批次号 BatchNo(out_batch_no) 与明细号 ItemNo(out_detail_no)")
	}
	if in.AmountMinor <= 0 {
		return nil, channelConfigError("wechat", "转账金额必须大于 0")
	}
	openid := strings.TrimSpace(in.ReceiverID)
	if openid == "" {
		return nil, channelConfigError("wechat", "转账到零钱需要收款用户 openid（ReceiverID）")
	}
	if strings.TrimSpace(in.ReceiverName) != "" {
		// 微信校验收款人姓名要求用平台证书公钥 RSA 加密 recipient_name 并带
		// Wechatpay-Serial 头，本渠道未实现该加密；与其明文送/悄悄丢掉这个校验信号，
		// 不如明确拒绝并告诉运维只填 openid。
		return nil, channelConfigError("wechat",
			"微信转账的收款人姓名需先用平台证书 RSA 加密（本渠道未实现），请去掉 ReceiverName 只做 openid 转账")
	}
	remark := truncate(strings.TrimSpace(in.Remark), wechatTransferDetailMaxLen)
	if remark == "" {
		remark = "商家转账"
	}
	body := map[string]any{
		"appid":        cfg["appId"],
		"out_batch_no": batchNo,
		"batch_name":   remark,
		"batch_remark": remark,
		"total_amount": in.AmountMinor,
		"total_num":    1,
		"transfer_detail_list": []map[string]any{{
			"out_detail_no":   itemNo,
			"transfer_amount": in.AmountMinor,
			"transfer_remark": remark,
			"openid":          openid,
		}},
	}
	if notify := strings.TrimSpace(in.NotifyURL); notify != "" {
		abs, err := absoluteNotifyURL("wechat", notify)
		if err != nil {
			return nil, err
		}
		body["notify_url"] = abs
	}
	status, raw, err := p.call(ctx, cfg, http.MethodPost, wechatTransferBatchPath, body)
	if err != nil {
		return nil, err
	}
	res, err := wechatResult("wechat", status, raw)
	if err != nil {
		return nil, err
	}
	batchID := res["batch_id"]
	if batchID == "" {
		return nil, channelGatewayError("wechat", "转账受理应答缺少 batch_id")
	}
	// 受理成功只代表微信已收单，钱有没有到账要查（或等转账回调）。
	return &TransferResult{
		Accepted: true, Pending: true, ChannelBatchNo: batchID,
		State: "ACCEPTED", Reason: "微信已受理转账批次，实际到账以查询/回调为准",
	}, nil
}

func (p wechatProvider) QueryTransfer(ctx context.Context, batchNo string, cfg map[string]string) (*TransferResult, error) {
	batchNo = strings.TrimSpace(batchNo)
	if batchNo == "" {
		return nil, channelConfigError("wechat", "转账查询需要批次号 BatchNo(out_batch_no)")
	}
	path := wechatTransferQueryPath + url.PathEscape(batchNo) + "?need_query_detail=true"
	status, raw, err := p.call(ctx, cfg, http.MethodGet, path, "")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		// 查无批次 ≠ 批次失败：不能报失败让上层以为可以重新发起。
		return &TransferResult{State: "NOT_FOUND", Reason: "微信查无此转账批次（未受理，不代表失败）"}, nil
	}
	if _, err := wechatResult("wechat", status, raw); err != nil {
		return nil, err // 非 2xx：原样带出微信的 code/message
	}
	var doc struct {
		BatchID     string `json:"batch_id"`
		OutBatchNo  string `json:"out_batch_no"`
		BatchStatus string `json:"batch_status"`
		FailReason  string `json:"fail_reason"`
		Details     []struct {
			OutDetailNo  string `json:"out_detail_no"`
			DetailID     string `json:"detail_id"`
			DetailStatus string `json:"detail_status"`
			FailReason   string `json:"fail_reason"`
		} `json:"transfer_detail_list"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, channelGatewayError("wechat", "转账批次应答解析失败: "+err.Error())
	}
	out := &TransferResult{ChannelBatchNo: doc.BatchID, State: doc.BatchStatus, Reason: doc.FailReason}
	switch doc.BatchStatus {
	case "":
		// 应答里没有 batch_status：不下结论，也不谎报「已受理」。
		out.Pending = true
		out.Reason = "微信转账批次应答缺少 batch_status，保持待核对"
	case "ACCEPTED", "PROCESSING", "WAIT_PAY", "WAIT_CONFIRM":
		// 批次确实已被微信受理，只是钱还没到账（与 Transfer 的 Accepted+Pending 口径一致）。
		out.Accepted = true
		out.Pending = true
		if out.Reason == "" {
			out.Reason = "微信转账批次状态 " + doc.BatchStatus + "（已受理未到账）"
		}
	case "FAILED", "CANCELED", "CANCELLED":
		out.Accepted = false
		out.Pending = false
		if out.Reason == "" {
			out.Reason = "微信批次状态 " + doc.BatchStatus
		} else {
			out.Reason = "微信批次状态 " + doc.BatchStatus + " " + doc.FailReason
		}
	case "FINISHED":
		// 批次结束不代表每笔明细都成功，按明细下结论（本中台一批一笔）。
		var sawSuccess, sawFail, sawPending bool
		var why string
		for _, d := range doc.Details {
			if d.DetailID != "" {
				out.ChannelDetailNo = d.DetailID
			}
			switch strings.ToUpper(strings.TrimSpace(d.DetailStatus)) {
			case "SUCCESS":
				sawSuccess = true
			case "FAIL", "DEACTIVATED", "CANCELED":
				sawFail = true
				why = d.FailReason
			default:
				sawPending = true
				why = d.DetailStatus
			}
		}
		out.Reason = ""
		switch {
		case sawPending:
			out.Accepted = true
			out.Pending = true
			out.Reason = "批次已结束但明细仍在处理: " + why
		case sawFail:
			out.Reason = "转账明细失败: " + orDefaultWechat(why, "渠道未给原因")
		case sawSuccess:
			out.Accepted = true
			out.Reason = "微信转账到账成功"
		default:
			out.Pending = true
			out.Reason = "批次 FINISHED 但没有明细可判（need_query_detail 未生效或批次里没有明细）"
		}
		return out, nil
	default:
		out.Pending = true
		out.Reason = "微信转账批次状态未知: " + doc.BatchStatus
	}
	return out, nil
}

func orDefaultWechat(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
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
