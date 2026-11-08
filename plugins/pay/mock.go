package pay

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nexadmin/core/response"
)

// mockProvider 是 P0 的模拟渠道：收银台页面模拟用户扫码支付，回调报文与真实网关
// 同构（JSON + HMAC-SHA256 + 时间窗），把验签、幂等、状态机的整条链路真实跑通。
//
// 它同时是「渠道可选能力」的演练场：退款查询、预授权、账单、代付四组能力全部
// 真实实现（不是只把布尔位翻成 true），语义与真渠道一致，主链、契约、前端用例
// 都靠它跑端到端。所以 mock 与真渠道走同一条 Provider 路径：服务层只按
// Capabilities() 判能力，绝不对 provider=="mock" 特判，也不再伪造渠道流水号。
type mockProvider struct{}

func (mockProvider) Name() string { return "mock" }

// MockCallbackHeaders 是 mock 渠道回调使用的签名头（与框架 webhook 的签名方案一致）。
const (
	mockTimestampHeader = "X-Timestamp"
	mockSignatureHeader = "X-Signature"
	// 回调时间窗统一 5 分钟（契约：mall wechat 5min / mock 10min，取严）。
	CallbackClockSkew = 5 * time.Minute
)

// ---- 能力矩阵 ----

// Capabilities 如实声明：mock 是多币种、全能力的演练渠道。
//   - 币种：CNY + 四个外币（换汇下单、原币金额核对、外币账单都要能演练）。
//   - 下单方式：PayRequest.Method 注释里的全部规范取值——mock 的收银台与 method
//     无关（一律返回跳转 URL），声称全支持是事实而不是伪装。别名写法
//     （scan/web/mweb/mini…由真实渠道各自归一化）不在清单里，服务层按本清单精确匹配。
//   - 四个布尔能力全 true，且下面四组接口逐个真实现。
func (mockProvider) Capabilities() Capabilities {
	return Capabilities{
		Currencies:  []string{"CNY", "USD", "HKD", "EUR", "JPY"},
		Methods:     []string{"native", "qr", "page", "wap", "h5", "jsapi", "app", "micropay"},
		RefundQuery: true,
		Auth:        true,
		Bill:        true,
		Payout:      true,
	}
}

// ---- mock 的结论怎么定：显式指令，绝不随机 ----
//
// 真渠道的结论由渠道侧事实决定，mock 没有渠道侧，所以结论一律由**入参**确定性推出，
// 再记进进程内台账；同一笔重复调用永远得到同一个结论与同一个流水号。
//
//  1. 单笔指令（优先级最高）：RefundNo / PaymentNo / IdempotencyKey / AuthNo /
//     BatchNo / ItemNo 任一含以下片段（大小写与 _ - 不敏感）：
//     pending | processing | wait → 已受理未回执（异步态）
//     fail | reject | closed      → 渠道拒绝
//     服务层生成的单号是 "PAY"+数字 / "rf"+hex，撞不到这些片段，不会误触发。
//  2. 渠道级旋钮（cfg = 渠道凭据 JSON，只对 mock 有意义；mock 没有 ConfigSchema，
//     由渠道编辑接口或种子数据直接给）：
//     refundOutcome / payoutOutcome = success | pending | fail —— 该渠道所有退款/代付的受理结果
//     billFeeBp    = 60            账单行手续费（基点，默认 0；退款行按负数冲销）
//     billExtraRow = 1             账单里多出一笔本地没有的渠道流水（演练长款与差异）
//     要端到端演练「退款卡在 pending、补偿查询翻转成成功/失败」就改这里。
//  3. 都没有 → success（保持 mock 既有语义：退款同步成功）。
const (
	mockOutcomeSuccess  = "success"
	mockOutcomePending  = "pending"
	mockOutcomeRejected = "rejected"
)

// mockDirectiveOf 扫描单号里的显式指令；ok=false 表示没有指令。
func mockDirectiveOf(hints ...string) (string, bool) {
	for _, h := range hints {
		s := strings.ToLower(h)
		s = strings.NewReplacer("_", "", "-", "", " ", "").Replace(s)
		switch {
		case strings.Contains(s, "pending"), strings.Contains(s, "processing"), strings.Contains(s, "wait"):
			return mockOutcomePending, true
		case strings.Contains(s, "fail"), strings.Contains(s, "reject"), strings.Contains(s, "closed"):
			return mockOutcomeRejected, true
		}
	}
	return "", false
}

// mockCfg 安全读渠道配置键（nil 配置在单测里很常见）。键名大小写不敏感：
// 凭据 JSON 里的 mock 旋钮既可能写成 billExtraRow 也可能被抄成全小写，
// 先精确匹配再折叠大小写，保证同一次进程内取值稳定（账单摘要要确定性）。
func mockCfg(cfg map[string]string, key string) string {
	if cfg == nil {
		return ""
	}
	if v, ok := cfg[key]; ok {
		return strings.ToLower(strings.TrimSpace(v))
	}
	for k, v := range cfg {
		if strings.EqualFold(k, key) {
			return strings.ToLower(strings.TrimSpace(v))
		}
	}
	return ""
}

// mockOutcome 按「单笔指令 > 渠道旋钮 > 默认 success」定结论；cfgKey 为空表示
// 该动作不支持渠道级旋钮，只看单笔指令。
func mockOutcome(cfg map[string]string, cfgKey string, hints ...string) string {
	if o, ok := mockDirectiveOf(hints...); ok {
		return o
	}
	if cfgKey == "" {
		return mockOutcomeSuccess
	}
	switch mockCfg(cfg, cfgKey) {
	case mockOutcomePending:
		return mockOutcomePending
	case "fail", "failed", "reject", "rejected":
		return mockOutcomeRejected
	}
	return mockOutcomeSuccess
}

// mockHash 由入参确定性推出渠道侧流水号：同一笔重试永远同一个号（16 位 hex，
// 与原 randomToken(8) 同宽，渠道流水号列都是 size:128，绰绰有余）。
func mockHash(seed ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(seed, "\x1f")))
	return hex.EncodeToString(sum[:8])
}

// mockBillLoc 是 mock 账单的账期时区（业务时区 UTC+8，与支付宝口径一致）。
// 用 FixedZone 而不是 LoadLocation：Windows 无 tzdata 时也不会退化成 UTC。
var mockBillLoc = time.FixedZone("UTC+8", 8*3600)

// errStaleWindow 是「验签通过但时间戳超窗」的 error 形态哨兵。
// 底座里的 errNotifyStale 是 i18n key（字符串常量），不能当 errors.Is 的目标，
// 这里补一个哨兵并把它放进 response.Error 的 Key：服务层既能
// errors.Is(err, errStaleWindow)，也能按 re.Key == errNotifyStale 判，
// 从而把回调审计记成 ResultStale 而不是混进 sign_invalid。
var errStaleWindow = errors.New(errNotifyStale)

// staleWindowError 是超窗错误（mock 与易支付共用：两者的时间戳都是「每次下发都刷新」的签名时间戳）。
// 里层仍是一个 *response.Error（键 = errNotifyStale），这样服务层 errors.As 取到的
// 就是带正确错误码的响应错误，而 errors.Is(err, errStaleWindow) 又能单独判「是不是超窗」。
func staleWindowError(provider string, delta time.Duration) error {
	detail := response.BadRequest(errNotifyStale).WithMsg(fmt.Sprintf(
		"%s: 通知时间戳偏差 %s，超出 ±%s 窗口", provider, delta.Round(time.Second), CallbackClockSkew))
	return response.BadRequest(errNotifyStale).Wrap(fmt.Errorf("%w: %w", errStaleWindow, detail))
}

// mockInvalidParam 是 mock 的入参缺陷（缺幂等号 / authNo / 账期格式）。
// 报请求级错误码而不是能力缺口或渠道拒绝：这类问题只有调用方能修。
func mockInvalidParam(detail string) error {
	return response.BadRequest(errBizRequired).WithMsg("mock: " + detail)
}

// mockAuthMissing 是渠道侧查无这笔授权。
func mockAuthMissing(authNo string) error {
	return response.NotFound(errNotFound).WithMsg("mock: 渠道侧没有这笔授权 " + authNo)
}

// ---- 进程内台账 ----
//
// mock 没有渠道侧存储，「渠道侧发生过什么」只能记在进程里。它有三个作用：
// 退款补偿查询与本地受理结论自洽、预授权/代付的多步动作有地方存中间态、
// 账单有内容可下发。台账有界（超出 mockLedgerLimit 丢最旧），单测与端到端演练
// 的体量远够用；长驻进程里它只是一份调试痕迹，绝不承载资金事实。
const mockLedgerLimit = 4096

type (
	mockTrade struct {
		Seq          uint64
		PaymentNo    string
		AmountMinor  int64
		Currency     string
		Paid         bool
		Failed       bool
		ChannelTxnID string
		At           time.Time
	}
	mockRefund struct {
		Seq             uint64
		Key             string // 幂等号：IdempotencyKey，缺省退化到 RefundNo
		RefundNo        string
		PaymentNo       string
		AmountMinor     int64
		Outcome         string
		ChannelRefundID string
		At              time.Time
	}
	mockAuth struct {
		Seq           uint64
		AuthNo        string
		ChannelAuthNo string
		FrozenMinor   int64
		CapturedMinor int64
		// Captured 按中台请款单号记账：重复请款（同一张收款单重试）不再累加额度。
		Captured map[string]int64
		Currency string
		Status   string
		Reason   string
		At       time.Time
	}
	mockPayout struct {
		Seq             uint64
		BatchNo         string
		ItemNo          string
		AmountMinor     int64
		Currency        string
		Outcome         string
		ChannelBatchNo  string
		ChannelDetailNo string
		At              time.Time
	}
)

type mockLedger struct {
	mu      sync.Mutex
	seq     uint64
	trades  map[string]*mockTrade
	refunds map[string]*mockRefund
	auths   map[string]*mockAuth
	payouts map[string]*mockPayout
	batches map[string][]string // BatchNo → ItemNo，保证批次聚合与账单顺序稳定
}

var mockStore = &mockLedger{
	trades:  map[string]*mockTrade{},
	refunds: map[string]*mockRefund{},
	auths:   map[string]*mockAuth{},
	payouts: map[string]*mockPayout{},
	batches: map[string][]string{},
}

// nextLocked 取单调递增的插入序号（淘汰最旧用）。调用方需持锁。
func (l *mockLedger) nextLocked() uint64 { l.seq++; return l.seq }

// ResetMockLedger 清空 mock 的渠道侧台账，供端到端用例从干净状态起算。
// 只影响 mock（模拟渠道）自己的内存，真实渠道各自面对渠道侧事实。
func ResetMockLedger() {
	mockStore.mu.Lock()
	defer mockStore.mu.Unlock()
	mockStore.trades = map[string]*mockTrade{}
	mockStore.refunds = map[string]*mockRefund{}
	mockStore.auths = map[string]*mockAuth{}
	mockStore.payouts = map[string]*mockPayout{}
	mockStore.batches = map[string][]string{}
}

// mockTrimLocked 把 map 限制在 limit 条内，按插入序号丢最旧（序号相同按键，保证确定性）。
func mockTrimLocked[T any](m map[string]T, limit int, seqOf func(T) uint64) {
	if len(m) <= limit {
		return
	}
	type entry struct {
		key string
		seq uint64
	}
	items := make([]entry, 0, len(m))
	for k, v := range m {
		items = append(items, entry{k, seqOf(v)})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].seq != items[j].seq {
			return items[i].seq < items[j].seq
		}
		return items[i].key < items[j].key
	})
	for _, it := range items[:len(items)-limit] {
		delete(m, it.key)
	}
}

// recordTradeCreate 记下预创建的渠道订单（账单行的金额只有这里拿得到）。
func (l *mockLedger) recordTradeCreate(req PayRequest) {
	paymentNo := strings.TrimSpace(req.PaymentNo)
	if paymentNo == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if old, ok := l.trades[paymentNo]; ok {
		// 同一笔重复下单（重试或换下单方式）：金额币种按最新一次，支付事实不动。
		old.AmountMinor = req.AmountMinor
		old.Currency = req.Currency
		old.Seq = l.nextLocked()
		return
	}
	l.trades[paymentNo] = &mockTrade{
		Seq: l.nextLocked(), PaymentNo: paymentNo, AmountMinor: req.AmountMinor,
		Currency: req.Currency, At: time.Now(),
	}
	mockTrimLocked(l.trades, mockLedgerLimit, func(t *mockTrade) uint64 { return t.Seq })
}

// recordTradeCallback 把已验签回调的支付事实落台账（重复回调幂等：结论只会单向变成功）。
func (l *mockLedger) recordTradeCallback(paymentNo, txnID string, paid, failed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.trades[paymentNo]
	if !ok {
		rec = &mockTrade{Seq: l.nextLocked(), PaymentNo: paymentNo, At: time.Now()}
		l.trades[paymentNo] = rec
		mockTrimLocked(l.trades, mockLedgerLimit, func(t *mockTrade) uint64 { return t.Seq })
	}
	switch {
	case paid:
		rec.Paid = true
		rec.Failed = false
		if txnID != "" {
			rec.ChannelTxnID = txnID
		}
	case failed && !rec.Paid:
		// 已成功过的单子不会被后来的「取消」翻回去：渠道侧事实也以成交为准。
		rec.Failed = true
	}
	rec.At = time.Now()
	rec.Seq = l.nextLocked()
}

// tradeSnapshot 读一笔渠道侧订单事实。
func (l *mockLedger) tradeSnapshot(paymentNo string) (mockTrade, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.trades[paymentNo]
	if !ok {
		return mockTrade{}, false
	}
	return *rec, true
}

// recordRefund 记下退款受理结论；同一幂等号重复受理返回首笔（渠道侧只有一笔）。
func (l *mockLedger) recordRefund(rec *mockRefund) *mockRefund {
	l.mu.Lock()
	defer l.mu.Unlock()
	if old, ok := l.refunds[rec.Key]; ok {
		return old
	}
	rec.Seq = l.nextLocked()
	l.refunds[rec.Key] = rec
	mockTrimLocked(l.refunds, mockLedgerLimit, func(r *mockRefund) uint64 { return r.Seq })
	cp := *rec
	return &cp
}

func (l *mockLedger) refund(key string) (mockRefund, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.refunds[key]
	if !ok {
		return mockRefund{}, false
	}
	return *r, true
}

// confirmRefund 按退款回执把受理结论翻成已确认（异步退款的闭环入口）。
func (l *mockLedger) confirmRefund(refundNo, channelRefundID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.refunds {
		if r.RefundNo != refundNo {
			continue
		}
		r.Outcome = mockOutcomeSuccess
		if channelRefundID != "" {
			r.ChannelRefundID = channelRefundID
		}
		r.Seq = l.nextLocked()
		return
	}
}

// freezeAuth 记下冻结；重复冻结同一 authNo 幂等返回首笔。
func (l *mockLedger) freezeAuth(in AuthFreezeRequest, reject bool) mockAuth {
	l.mu.Lock()
	defer l.mu.Unlock()
	if old, ok := l.auths[in.AuthNo]; ok {
		return *old
	}
	rec := mockAuth{
		Seq: l.nextLocked(), AuthNo: in.AuthNo, ChannelAuthNo: "mockauth_" + mockHash(in.AuthNo),
		FrozenMinor: in.AmountMinor, Captured: map[string]int64{},
		Currency: strings.ToUpper(strings.TrimSpace(in.Currency)),
		Status:   AuthAuthorized, At: time.Now(),
	}
	if reject {
		rec.Status = "rejected"
		rec.Reason = "mock 渠道拒绝冻结（单号带拒绝指令）"
	}
	l.auths[in.AuthNo] = &rec
	mockTrimLocked(l.auths, mockLedgerLimit, func(a *mockAuth) uint64 { return a.Seq })
	return rec
}

func (l *mockLedger) authSnapshot(authNo string) (mockAuth, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.auths[authNo]
	if !ok {
		return mockAuth{}, false
	}
	return *a, true
}

// captureAuth 记一笔请款（幂等号 = 中台请款单号），返回改后的快照与拒绝原因。
func (l *mockLedger) captureAuth(authNo, captureNo string, amountMinor int64) (mockAuth, string, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.auths[authNo]
	if !ok {
		return mockAuth{}, "", "not_found"
	}
	if prev, done := rec.Captured[captureNo]; done {
		return *rec, "", fmt.Sprintf("duplicate:%d", prev)
	}
	if !Freezable(rec.Status) {
		return *rec, "", "渠道拒绝请款：授权状态 " + rec.Status
	}
	if rec.CapturedMinor+amountMinor > rec.FrozenMinor {
		return *rec, "", "渠道拒绝请款：超出冻结额"
	}
	rec.CapturedMinor += amountMinor
	if rec.Captured == nil {
		rec.Captured = map[string]int64{}
	}
	rec.Captured[captureNo] = amountMinor
	if rec.CapturedMinor >= rec.FrozenMinor {
		rec.Status = AuthCaptured
	} else {
		rec.Status = AuthPartialCaptured
	}
	rec.Reason = ""
	rec.Seq = l.nextLocked()
	return *rec, rec.Status, ""
}

// voidAuth 撤销冻结，释放剩余额度。
func (l *mockLedger) voidAuth(authNo string) (mockAuth, string, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.auths[authNo]
	if !ok {
		return mockAuth{}, "", "not_found"
	}
	switch rec.Status {
	case AuthVoided:
		return *rec, AuthVoided, "" // 重复撤销幂等
	case AuthCaptured:
		return *rec, "", "渠道拒绝撤销：授权已全部请款"
	case "rejected":
		return *rec, "", "渠道拒绝撤销：授权从未冻结成功"
	}
	rec.Status = AuthVoided
	rec.Reason = ""
	rec.Seq = l.nextLocked()
	return *rec, AuthVoided, ""
}

// recordPayout 记下代付单笔；同 ItemNo 重复调用返回首笔结论（绝不重复转账）。
func (l *mockLedger) recordPayout(rec *mockPayout) mockPayout {
	l.mu.Lock()
	defer l.mu.Unlock()
	if old, ok := l.payouts[rec.ItemNo]; ok {
		return *old
	}
	rec.Seq = l.nextLocked()
	l.payouts[rec.ItemNo] = rec
	l.batches[rec.BatchNo] = append(l.batches[rec.BatchNo], rec.ItemNo)
	mockTrimLocked(l.payouts, mockLedgerLimit, func(p *mockPayout) uint64 { return p.Seq })
	for len(l.batches) > mockLedgerLimit {
		// 批次索引同样淘汰最旧：序号取该批第一笔。
		oldest, oldestSeq := "", uint64(0)
		for b, items := range l.batches {
			first := uint64(0)
			if len(items) > 0 {
				if p, ok := l.payouts[items[0]]; ok {
					first = p.Seq
				}
			}
			if oldest == "" || first < oldestSeq {
				oldest, oldestSeq = b, first
			}
		}
		delete(l.batches, oldest)
	}
	cp := *rec
	return cp
}

func (l *mockLedger) batch(batchNo string) []mockPayout {
	l.mu.Lock()
	defer l.mu.Unlock()
	items := l.batches[batchNo]
	out := make([]mockPayout, 0, len(items))
	for _, it := range items {
		if p, ok := l.payouts[it]; ok {
			out = append(out, *p)
		}
	}
	return out
}

// billRows 取某账期（UTC+8 自然日；空账期 = 全部）渠道侧真实发生的流水。
// 只下发已成立的事实：支付成功的 trade 行 + 已确认的 refund 行；
// pending 的单子渠道账单里本来就没有，账单驱动对账因此天然只核已结算部分。
func (l *mockLedger) billRows(billDate string, feeBp int64, extraRow bool) []BillRow {
	// 跨零点的端到端演练很容易一半 UTC 一半 UTC+8：两个口径任一命中都算，
	// 免得用例随时钟漂移偶发失败（真渠道严格按账期日，这里是有意的宽松）。
	onDate := func(t time.Time) bool {
		if billDate == "" {
			return true
		}
		return t.In(mockBillLoc).Format("2006-01-02") == billDate ||
			t.UTC().Format("2006-01-02") == billDate
	}
	fee := func(minor int64) int64 {
		if feeBp <= 0 {
			return 0
		}
		return minor * feeBp / 10000
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	rows := make([]BillRow, 0, len(l.trades)+len(l.refunds))
	for _, t := range l.trades {
		if !t.Paid || !onDate(t.At) {
			continue
		}
		rows = append(rows, BillRow{
			Kind: BillKindTrade, PaymentNo: t.PaymentNo, AmountMinor: t.AmountMinor,
			FeeMinor: fee(t.AmountMinor), ChannelTxnID: t.ChannelTxnID,
		})
	}
	for _, r := range l.refunds {
		if r.Outcome != mockOutcomeSuccess || !onDate(r.At) {
			continue
		}
		// 退款行的手续费是冲销，取负数（与渠道账单里「退款退手续费」的口径一致）。
		rows = append(rows, BillRow{
			Kind: BillKindRefund, RefundNo: r.RefundNo, PaymentNo: r.PaymentNo,
			AmountMinor: r.AmountMinor, FeeMinor: -fee(r.AmountMinor), ChannelTxnID: r.ChannelRefundID,
		})
	}
	if extraRow {
		// 渠道侧多出来的一笔（本地没有对应单据）：长款用例的抓手。
		rows = append(rows, BillRow{
			Kind: BillKindTrade, AmountMinor: 1,
			ChannelTxnID: "mocklong_" + mockHash(billDate, "extra"),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		if rows[i].PaymentNo != rows[j].PaymentNo {
			return rows[i].PaymentNo < rows[j].PaymentNo
		}
		if rows[i].RefundNo != rows[j].RefundNo {
			return rows[i].RefundNo < rows[j].RefundNo
		}
		return rows[i].ChannelTxnID < rows[j].ChannelTxnID
	})
	return rows
}

// ---- 基础三件事 ----

// Create 返回插件自带的模拟收银台页面（/api/v1 下的相对路径，与宿主前端同源）。
// 币种与下单方式按 Capabilities 精确把关：声明了能力矩阵却不受它约束，矩阵就是装饰。
func (mockProvider) Create(_ context.Context, req PayRequest, _ map[string]string) (*PayParams, error) {
	caps := mockProvider{}.Capabilities()
	if !caps.supportsCurrency(req.Currency) {
		return nil, mockCurrencyGapError(req.Currency, caps.Currencies)
	}
	if !caps.supportsMethod(req.Method) {
		return nil, mockMethodGapError(req.Method, caps.Methods)
	}
	mockStore.recordTradeCreate(req)
	u := "/api/v1/plugins/pay/mock/cashier?no=" + url.QueryEscape(req.PaymentNo)
	if req.ReturnURL != "" {
		u += "&return=" + url.QueryEscape(req.ReturnURL)
	}
	return &PayParams{PayURL: u}, nil
}

// Query 主动查询渠道侧支付结果：mock 的「渠道侧」就是台账——收到过成功回调的单子
// 这里如实报已支付，让主链能演练「回调丢失 → 补偿查询翻转」；没收到回调的单子
// 渠道侧本来没有事实，维持未定（与历史行为一致）。
func (mockProvider) Query(_ context.Context, paymentNo string, _ map[string]string) (*ChannelStatus, error) {
	rec, ok := mockStore.tradeSnapshot(strings.TrimSpace(paymentNo))
	if !ok {
		return &ChannelStatus{}, nil
	}
	st := &ChannelStatus{ChannelTxnID: rec.ChannelTxnID, GatewayStatus: "WAIT_BUYER_PAY"}
	switch {
	case rec.Paid:
		st.Paid = true
		st.GatewayStatus = "TRADE_SUCCESS"
	case rec.Failed:
		st.Failed = true
		st.GatewayStatus = "TRADE_CLOSED"
		st.Reason = "user closed cashier"
	}
	return st, nil
}

// Refund 模拟渠道退款。默认同步成功（保持既有语义）；单号带 pending 指令或渠道
// 旋钮设成 pending 时返回 Pending=true，由回执或 QueryRefund 翻转——异步态必须可演练。
// 渠道退款号由幂等号哈希得到：同一笔重试永远同一个号。
func (mockProvider) Refund(_ context.Context, req RefundRequest, cfg map[string]string) (*RefundResult, error) {
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(req.RefundNo)
	}
	if key == "" {
		return nil, mockInvalidParam("退款需要 refundNo 或幂等号")
	}
	if req.AmountMinor <= 0 {
		return &RefundResult{OK: false, Reason: "amount must be positive"}, nil
	}
	rec := mockStore.recordRefund(&mockRefund{
		Key: key, RefundNo: req.RefundNo, PaymentNo: req.PaymentNo, AmountMinor: req.AmountMinor,
		Outcome:         mockOutcome(cfg, "refundOutcome", req.RefundNo, req.PaymentNo, req.IdempotencyKey),
		ChannelRefundID: "mockrf_" + mockHash(key), At: time.Now(),
	})
	switch rec.Outcome {
	case mockOutcomePending:
		return &RefundResult{Pending: true, ChannelRefundID: rec.ChannelRefundID}, nil
	case mockOutcomeRejected:
		return &RefundResult{OK: false, ChannelRefundID: rec.ChannelRefundID, Reason: "mock 渠道拒绝这笔退款"}, nil
	}
	return &RefundResult{OK: true, ChannelRefundID: rec.ChannelRefundID}, nil
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
// mock 的时间戳每次下发都重新生成且参与签名，按铁律②用双向硬窗。
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
		return nil, staleWindowError("mock", d)
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
	// NotifyID：mock 没有渠道下发的通知号，用报文摘要代替——同一条通知的重试摘要
	// 相同（与支付宝 notify_id 的行为一致），不同通知必然不同，够排障取证用。
	res := &CallbackResult{
		PaymentNo:    body.PaymentNo,
		ChannelTxnID: body.ChannelTxnID,
		NotifyID:     "mocknt_" + mockHash(string(in.Body)),
	}
	switch body.Result {
	case "success":
		if body.RefundNo != "" {
			res.RefundNo = body.RefundNo
			res.RefundOK = true
			if body.ChannelRefundID != "" {
				res.ChannelTxnID = body.ChannelRefundID
			}
			mockStore.confirmRefund(body.RefundNo, body.ChannelRefundID)
			return res, nil
		}
		res.Paid = true
		if res.ChannelTxnID == "" {
			res.ChannelTxnID = "mock_" + mockHash(string(in.Body))
		}
	case "fail":
		res.Failed = true
		res.Reason = "user closed cashier"
	default:
		return nil, fmt.Errorf("pay: unknown mock result %q", body.Result)
	}
	mockStore.recordTradeCallback(body.PaymentNo, res.ChannelTxnID, res.Paid, res.Failed)
	return res, nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- 退款结果查询（RefundQueryProvider）----

// QueryRefund 渠道侧退款结论，三态都可达，且与 Refund 当时的受理结论自洽：
// 优先读台账；台账没有（进程重启、被淘汰）时按同一套显式指令推导，
// 「指令说成功但台账没有」按查无此退款处理（Found=false），绝不猜成功。
func (mockProvider) QueryRefund(_ context.Context, req RefundQueryRequest, cfg map[string]string) (*RefundStatusResult, error) {
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(req.RefundNo)
	}
	if key == "" {
		return nil, mockInvalidParam("退款查询需要 idempotencyKey 或 refundNo")
	}
	if rec, ok := mockStore.refund(key); ok {
		res := &RefundStatusResult{
			Found: true, ChannelRefundID: rec.ChannelRefundID,
			OK: rec.Outcome == mockOutcomeSuccess, Pending: rec.Outcome == mockOutcomePending,
		}
		if rec.Outcome == mockOutcomeRejected {
			res.Reason = "mock 渠道拒绝这笔退款"
		}
		return res, nil
	}
	// 台账里没有：只有显式指令才算数（渠道级旋钮不足以证明这笔退款存在过）。
	directive, explicit := mockDirectiveOf(req.RefundNo, req.PaymentNo, req.IdempotencyKey)
	if !explicit || directive == mockOutcomeSuccess {
		return &RefundStatusResult{Found: false, Reason: "mock 渠道查无这笔退款"}, nil
	}
	res := &RefundStatusResult{Found: true, ChannelRefundID: "mockrf_" + mockHash(key)}
	if directive == mockOutcomePending {
		res.Pending = true
		return res, nil
	}
	res.Reason = "mock 渠道拒绝这笔退款"
	return res, nil
}

// ---- 资金预授权（AuthProvider）----
//
// 渠道授权号一律 mockauth_ 前缀 + authNo 的确定性哈希：一眼看出是模拟渠道的号，
// 同一笔授权重复冻结得到同一个号。服务层不再伪造这个号（原来 auths.go 里
// 本地拼 mockauth_ 的做法就是把「渠道没做」写成「渠道做了」）。
//
// 冻结/请款/撤销都是同步动作（真渠道也是），异步态由退款与代付演练；
// 撤销/查询用模型里的 AuthStatus 词表，服务层可以直接落库不用再映射。

func (mockProvider) Freeze(_ context.Context, in AuthFreezeRequest, cfg map[string]string) (*AuthResult, error) {
	if strings.TrimSpace(in.AuthNo) == "" {
		return nil, mockInvalidParam("冻结需要 authNo")
	}
	if in.AmountMinor <= 0 {
		return nil, mockInvalidParam("冻结金额必须为正")
	}
	caps := mockProvider{}.Capabilities()
	if !caps.supportsCurrency(in.Currency) {
		return nil, mockCurrencyGapError(in.Currency, caps.Currencies)
	}
	rec := mockStore.freezeAuth(in, mockOutcome(cfg, "", in.AuthNo) == mockOutcomeRejected)
	return &AuthResult{
		ChannelAuthNo: rec.ChannelAuthNo, Status: rec.Status,
		AmountMinor: rec.FrozenMinor, Reason: rec.Reason,
	}, nil
}

func (mockProvider) CompleteAuth(_ context.Context, in AuthCompleteRequest, _ map[string]string) (*AuthResult, error) {
	authNo := strings.TrimSpace(in.AuthNo)
	if authNo == "" {
		return nil, mockInvalidParam("请款需要 authNo")
	}
	if in.AmountMinor <= 0 {
		return nil, mockInvalidParam("请款金额必须为正")
	}
	captureNo := strings.TrimSpace(in.PaymentNo)
	if captureNo == "" {
		captureNo = strings.TrimSpace(in.AuthNo) // 没有请款单号时按整笔授权幂等
	}
	rec, status, note := mockStore.captureAuth(authNo, captureNo, in.AmountMinor)
	switch {
	case note == "not_found":
		return nil, mockAuthMissing(authNo)
	case strings.HasPrefix(note, "duplicate:"):
		// 同一张请款单重试：额度只加一次，回首次结论（金额取本次请求，状态是现态）。
		return &AuthResult{ChannelAuthNo: rec.ChannelAuthNo, Status: rec.Status, AmountMinor: in.AmountMinor}, nil
	case note != "":
		return &AuthResult{
			ChannelAuthNo: rec.ChannelAuthNo, Status: rec.Status,
			AmountMinor: rec.FrozenMinor - rec.CapturedMinor, Reason: note,
		}, nil
	}
	return &AuthResult{
		ChannelAuthNo: rec.ChannelAuthNo, Status: status,
		AmountMinor: in.AmountMinor, Reason: rec.Reason,
	}, nil
}

func (mockProvider) CancelAuth(_ context.Context, in AuthCancelRequest, _ map[string]string) (*AuthResult, error) {
	authNo := strings.TrimSpace(in.AuthNo)
	if authNo == "" {
		return nil, mockInvalidParam("撤销需要 authNo")
	}
	rec, status, note := mockStore.voidAuth(authNo)
	if note == "not_found" {
		return nil, mockAuthMissing(authNo)
	}
	if note != "" {
		return &AuthResult{
			ChannelAuthNo: rec.ChannelAuthNo, Status: rec.Status,
			AmountMinor: rec.FrozenMinor - rec.CapturedMinor, Reason: note,
		}, nil
	}
	return &AuthResult{
		ChannelAuthNo: rec.ChannelAuthNo, Status: status,
		AmountMinor: rec.FrozenMinor - rec.CapturedMinor, // 释放的剩余额度
	}, nil
}

func (mockProvider) QueryAuth(_ context.Context, authNo string, _ map[string]string) (*AuthResult, error) {
	rec, ok := mockStore.authSnapshot(strings.TrimSpace(authNo))
	if !ok {
		// 查询不报错：查无此授权是渠道的正常应答，交服务层决定告警还是补单。
		return &AuthResult{Status: "not_found", Reason: "mock 渠道查无这笔授权"}, nil
	}
	return &AuthResult{
		ChannelAuthNo: rec.ChannelAuthNo, Status: rec.Status,
		AmountMinor: rec.FrozenMinor - rec.CapturedMinor, Reason: rec.Reason,
	}, nil
}

// ---- 渠道账单（BillProvider）----

// FetchBill 下发确定性账单：同一批台账 → 同样的行、同样的 Digest（行内容哈希）。
// 账单本体（Raw）按接口约定不返回也不落库，只留 Digest 与行数供审计与复现。
//
// 账期里没有流水时返回 0 行且不报错——「账单为空」与「下载失败」是两件事，
// 对账任务必须能区分：0 行是差异，报错才是失败。
func (mockProvider) FetchBill(_ context.Context, in BillRequest, cfg map[string]string) (*Bill, error) {
	billDate := strings.TrimSpace(in.BillDate)
	if billDate != "" {
		if _, err := time.Parse("2006-01-02", billDate); err != nil {
			return nil, mockInvalidParam("billDate 必须是 YYYY-MM-DD")
		}
	}
	feeBp, _ := strconv.ParseInt(mockCfg(cfg, "billFeeBp"), 10, 64)
	extra := mockCfg(cfg, "billextrarow")
	rows := mockStore.billRows(billDate, feeBp, extra == "1" || extra == "true")
	var b strings.Builder
	fmt.Fprintf(&b, "mock\x1f%s\n", billDate)
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\x1f%s\x1f%s\x1f%d\x1f%d\x1f%s\n",
			r.Kind, r.PaymentNo, r.RefundNo, r.AmountMinor, r.FeeMinor, r.ChannelTxnID)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return &Bill{Rows: rows, Digest: hex.EncodeToString(sum[:]), RowCount: len(rows)}, nil
}

// ---- 代付（PayoutProvider）----

// Transfer 发起代付。BatchNo + ItemNo 是幂等键：同一笔重复调用不再转第二次，
// 直接回首次受理结论与同一个渠道号（渠道侧真实就是这个语义，否则重试就是重复打款）。
func (mockProvider) Transfer(_ context.Context, in TransferRequest, cfg map[string]string) (*TransferResult, error) {
	itemNo := strings.TrimSpace(in.ItemNo)
	if itemNo == "" {
		return nil, mockInvalidParam("代付需要 itemNo（单笔幂等号）")
	}
	if in.AmountMinor <= 0 {
		return &TransferResult{State: mockOutcomeRejected, Reason: "mock: 代付金额必须为正"}, nil
	}
	caps := mockProvider{}.Capabilities()
	if !caps.supportsCurrency(in.Currency) {
		return nil, mockCurrencyGapError(in.Currency, caps.Currencies)
	}
	batchNo := strings.TrimSpace(in.BatchNo)
	if batchNo == "" {
		batchNo = itemNo // 没有批次号时按单笔成批，渠道号仍然稳定
	}
	outcome := mockOutcome(cfg, "payoutOutcome", itemNo, in.BatchNo)
	if outcome == mockOutcomeRejected {
		// 渠道拒绝：不建批次、不给渠道号，重复调用得到同一个拒绝结论。
		return &TransferResult{State: mockOutcomeRejected, Reason: "mock 渠道拒绝这笔代付"}, nil
	}
	rec := mockStore.recordPayout(&mockPayout{
		BatchNo: batchNo, ItemNo: itemNo, AmountMinor: in.AmountMinor,
		Currency: strings.ToUpper(strings.TrimSpace(in.Currency)), Outcome: outcome,
		ChannelBatchNo: "mockbt_" + mockHash(batchNo), ChannelDetailNo: "mockdt_" + mockHash(itemNo),
		At: time.Now(),
	})
	// 结论与渠道号一律以台账里的首笔为准：同幂等号重复调用不会换号、也不会二次打款。
	res := &TransferResult{ChannelBatchNo: rec.ChannelBatchNo, ChannelDetailNo: rec.ChannelDetailNo}
	switch rec.Outcome {
	case mockOutcomePending:
		res.State, res.Accepted, res.Pending = mockOutcomePending, true, true
	case mockOutcomeRejected:
		res.State, res.Reason = mockOutcomeRejected, "mock 渠道拒绝这笔代付"
	default:
		res.State, res.Accepted = mockOutcomeSuccess, true
	}
	return res, nil
}

// QueryTransfer 查批次结论：全部成功=success，仍有受理未回执=pending，全被拒=failed；
// 查无此批次不报错（State=not_found），告警口径留给服务层。
func (mockProvider) QueryTransfer(_ context.Context, batchNo string, _ map[string]string) (*TransferResult, error) {
	items := mockStore.batch(strings.TrimSpace(batchNo))
	if len(items) == 0 {
		return &TransferResult{State: "not_found", Reason: "mock 渠道查无这个代付批次"}, nil
	}
	var pending, success int
	for _, it := range items {
		switch it.Outcome {
		case mockOutcomePending:
			pending++
		case mockOutcomeRejected:
			// 拒绝的笔次既不算成功也不算待回执，整批结论由其余笔决定。
		default:
			success++
		}
	}
	res := &TransferResult{
		ChannelBatchNo: items[0].ChannelBatchNo, ChannelDetailNo: items[0].ChannelDetailNo,
		State: mockOutcomeSuccess, Accepted: success > 0,
	}
	switch {
	case pending > 0:
		res.State, res.Pending, res.Accepted = mockOutcomePending, true, true
		res.Reason = fmt.Sprintf("%d/%d 笔待回执", pending, len(items))
	case success == 0:
		res.State, res.Accepted = mockOutcomeRejected, false
		res.Reason = "mock 渠道拒绝了整批代付"
	}
	return res, nil
}

// ---- 能力缺口错误（两个渠道共用）----
//
// 能力缺口 = 产品签约/配置层面不支持，不是渠道拒了这笔业务：必须报契约里的
// PAY_CAPABILITY_UNSUPPORTED（前端据此禁用入口、用户看的是「换渠道」而不是重试），
// 绝不静默成功、绝不伪造渠道流水号。
//
// 文案统一取自底座 provider.go 的 capabilityUnsupported()，但那个 helper 目前经
// channelConfigError 把错误码落成了 PAY_CHANNEL_UNAVAILABLE（provider.go 属主会话写域，
// 一行就能改对：response.BadRequest(errCapabilityUnsupported).WithMsg(...)）。
// 这里做一次自愈式校正——底座改好后本函数就只是转发的空壳，可以直接删掉。
func capabilityGapError(provider, what string) error {
	gap := capabilityUnsupported(provider, what)
	var re *response.Error
	if errors.As(gap, &re) && re.Key != errCapabilityUnsupported {
		return response.New(400, errCapabilityUnsupported).Wrap(gap).WithMsg(re.Msg)
	}
	return gap
}

func mockCurrencyGapError(currency string, support []string) error {
	return capabilityGapError("mock", fmt.Sprintf("币种 %s（可选 %s）", currency, strings.Join(support, "/")))
}

func mockMethodGapError(method string, support []string) error {
	return capabilityGapError("mock", fmt.Sprintf("下单方式 %s（可选 %s）", method, strings.Join(support, "/")))
}
