package pay

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"nexadmin/core/response"
)

// 「假渠道必须像真渠道那样可测」泳道的用例：mock 的五组可选能力逐个闭环，
// epay 的能力边界与时间窗防重放。命名统一 ext 前缀，避免与在途用例的助手撞名。

// ---- 助手 ----

// extMockCfg 是 mock 渠道的测试配置（账单旋钮按用例往同一张 map 里加）。
func extMockCfg() map[string]string { return map[string]string{"secret": "ext-secret"} }

// extMockNotify 造一条合法签名的 mock 回调（与收银台处理器同一条签名路径）。
func extMockNotify(t *testing.T, secret string, body mockCallbackBody) CallbackInput {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal mock body: %v", err)
	}
	ts := time.Now().Unix()
	return CallbackInput{
		Channel: "mock",
		Body:    raw,
		Headers: map[string]string{
			"x-timestamp": strconv.FormatInt(ts, 10),
			"x-signature": SignMockCallback(secret, raw, ts),
		},
	}
}

// extEpayNotify 按易支付协议给通知表单签 MD5（sign 参与签名时被排除，与实现无关）。
func extEpayNotify(key string, params map[string]string) []byte {
	p := make(map[string]string, len(params)+2)
	for k, v := range params {
		p[k] = v
	}
	p["sign"] = epaySign(p, key)
	p["sign_type"] = "MD5"
	q := url.Values{}
	for k, v := range p {
		q.Set(k, v)
	}
	return []byte(q.Encode())
}

// extRespKey 取业务错误的 i18n key（错误码），拿不到就返回空串。
func extRespKey(err error) string {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Key
	}
	return ""
}

// ---- mock：能力声明 ----

func TestMockDeclaresFullCapabilities(t *testing.T) {
	ResetMockLedger()
	p, err := LookupProvider("mock")
	if err != nil {
		t.Fatalf("lookup mock: %v", err)
	}
	// 五组接口必须都真实实现：主链按类型断言取能力，缺一个就是一条能力永远测不到。
	if _, ok := p.(CapabilityProvider); !ok {
		t.Error("mock must implement CapabilityProvider")
	}
	if _, ok := p.(RefundQueryProvider); !ok {
		t.Error("mock must implement RefundQueryProvider")
	}
	if _, ok := p.(AuthProvider); !ok {
		t.Error("mock must implement AuthProvider")
	}
	if _, ok := p.(BillProvider); !ok {
		t.Error("mock must implement BillProvider")
	}
	if _, ok := p.(PayoutProvider); !ok {
		t.Error("mock must implement PayoutProvider")
	}

	caps := ProviderCaps(p)
	if !caps.RefundQuery || !caps.Auth || !caps.Bill || !caps.Payout {
		t.Errorf("mock must declare all four optional capabilities, got %+v", caps)
	}
	if !caps.supportsCurrency("CNY") {
		t.Error("mock must support CNY")
	}
	foreign := 0
	for _, c := range caps.Currencies {
		if c != "CNY" {
			foreign++
		}
	}
	if foreign < 2 {
		t.Errorf("mock is the multi-currency playground, want >=2 foreign currencies, got %v", caps.Currencies)
	}
	for _, m := range []string{"native", "page", "wap", "jsapi", "app"} {
		if !caps.supportsMethod(m) {
			t.Errorf("mock should support method %s (declared %v)", m, caps.Methods)
		}
	}
	if caps.supportsCurrency("BRL") {
		t.Error("undeclared currency must not pass the matrix")
	}
	// 既有对外形态不许破坏：mock 不声明凭据字段（收银台用种子密钥），也不改应答格式。
	if _, ok := p.(SchemaProvider); ok {
		t.Error("mock must not declare credential fields (TestProviderInfosExposeSchemas asserts it)")
	}
	if _, ok := p.(AckProvider); ok {
		t.Error("mock keeps the unified JSON ack; implementing AckProvider would change the contract")
	}
}

// ---- mock：退款结果查询三态 + 幂等 ----

func TestMockRefundQueryStates(t *testing.T) {
	ResetMockLedger()
	p := mockProvider{}
	ctx := context.Background()
	cfg := extMockCfg()

	type step struct {
		name           string
		refundNo       string
		wantOK         bool
		wantPending    bool
		wantQueryOK    bool
		wantQueryPend  bool
		wantQueryFound bool
	}
	steps := []step{
		{name: "settled", refundNo: "RF-SUCCESS-1", wantOK: true, wantQueryOK: true, wantQueryFound: true},
		// 异步态：受理成功但渠道未回执。把 Refund/QueryRefund 里的 pending 分支
		// 改成直接成功，本行断言立刻红——这条用例就是异步态的回归闸门。
		{name: "pending", refundNo: "RF-PENDING-1", wantPending: true, wantQueryPend: true, wantQueryFound: true},
		{name: "rejected", refundNo: "RF-FAIL-1", wantQueryFound: true},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			key := "rf" + s.refundNo
			res, err := p.Refund(ctx, RefundRequest{
				RefundNo: s.refundNo, PaymentNo: "PAY-EXT-1", AmountMinor: 100, IdempotencyKey: key,
			}, cfg)
			if err != nil {
				t.Fatalf("Refund: %v", err)
			}
			if res.OK != s.wantOK || res.Pending != s.wantPending {
				t.Fatalf("Refund state = ok:%v pending:%v, want ok:%v pending:%v",
					res.OK, res.Pending, s.wantOK, s.wantPending)
			}
			if res.OK && res.Pending {
				t.Fatal("OK 与 Pending 不能同时为真")
			}
			if !res.OK && !res.Pending && !strings.Contains(res.Reason, "拒绝") {
				t.Fatalf("既不是成功也不是受理中，就必须给出拒绝原因，got %+v", res)
			}
			if !strings.HasPrefix(res.ChannelRefundID, "mockrf_") {
				t.Fatalf("channel refund id %q must be recognisably mock", res.ChannelRefundID)
			}

			q := RefundQueryRequest{RefundNo: s.refundNo, PaymentNo: "PAY-EXT-1", AmountMinor: 100, IdempotencyKey: key}
			first, err := p.QueryRefund(ctx, q, cfg)
			if err != nil {
				t.Fatalf("QueryRefund: %v", err)
			}
			if first.Found != s.wantQueryFound || first.OK != s.wantQueryOK || first.Pending != s.wantQueryPend {
				t.Fatalf("QueryRefund = %+v, want found:%v ok:%v pending:%v", first, s.wantQueryFound, s.wantQueryOK, s.wantQueryPend)
			}
			if first.ChannelRefundID != res.ChannelRefundID {
				t.Fatalf("query %q disagrees with the accepted refund %q", first.ChannelRefundID, res.ChannelRefundID)
			}
			// 重复查询幂等：结论与渠道号都不许每次变（否则补偿轮询会把一笔退款看成翻来覆去）。
			for i := 0; i < 3; i++ {
				again, err := p.QueryRefund(ctx, q, cfg)
				if err != nil {
					t.Fatalf("QueryRefund repeat: %v", err)
				}
				if *again != *first {
					t.Fatalf("QueryRefund not idempotent: %+v vs %+v", again, first)
				}
			}
			// 重复受理也不许换号或改结论。
			repeat, err := p.Refund(ctx, RefundRequest{
				RefundNo: s.refundNo, PaymentNo: "PAY-EXT-1", AmountMinor: 100, IdempotencyKey: key,
			}, cfg)
			if err != nil {
				t.Fatalf("Refund repeat: %v", err)
			}
			if repeat.OK != res.OK || repeat.Pending != res.Pending || repeat.ChannelRefundID != res.ChannelRefundID {
				t.Fatalf("repeat Refund = %+v, want %+v", repeat, res)
			}
		})
	}

	t.Run("unknown_refund_is_not_a_fake_success", func(t *testing.T) {
		res, err := p.QueryRefund(ctx, RefundQueryRequest{RefundNo: "RF-NEVER", IdempotencyKey: "rfNEVER"}, cfg)
		if err != nil {
			t.Fatalf("QueryRefund: %v", err)
		}
		if res.Found || res.OK || res.Pending {
			t.Fatalf("查无此退款必须 Found=false 且不报成功，got %+v", res)
		}
	})

	t.Run("channel_knob_drives_pending", func(t *testing.T) {
		res, err := p.Refund(ctx, RefundRequest{RefundNo: "RF-KNOB", IdempotencyKey: "rfKNOB", AmountMinor: 10},
			map[string]string{"secret": "ext-secret", "refundOutcome": "pending"})
		if err != nil {
			t.Fatalf("Refund: %v", err)
		}
		if !res.Pending || res.OK {
			t.Fatalf("refundOutcome=pending must yield Pending, got %+v", res)
		}
	})

	t.Run("zero_amount_is_rejected", func(t *testing.T) {
		res, err := p.Refund(ctx, RefundRequest{RefundNo: "RF-ZERO", IdempotencyKey: "rfZERO"}, cfg)
		if err != nil || res.OK || res.Pending {
			t.Fatalf("零额退款必须被拒，got %+v err %v", res, err)
		}
	})
}

// ---- mock：预授权四件套 ----

func TestMockAuthLifecycle(t *testing.T) {
	ResetMockLedger()
	p := mockProvider{}
	ctx, cfg := context.Background(), extMockCfg()

	freeze, err := p.Freeze(ctx, AuthFreezeRequest{AuthNo: "AUTH-EXT-1", AmountMinor: 300, Currency: "CNY", Subject: "押金"}, cfg)
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if !strings.HasPrefix(freeze.ChannelAuthNo, "mockauth_") {
		t.Fatalf("mock 授权号必须一眼看出是 mock，got %q", freeze.ChannelAuthNo)
	}
	if freeze.Status != AuthAuthorized || freeze.AmountMinor != 300 {
		t.Fatalf("Freeze = %+v", freeze)
	}
	// 重复冻结（重试）必须给同一个渠道授权号。
	again, err := p.Freeze(ctx, AuthFreezeRequest{AuthNo: "AUTH-EXT-1", AmountMinor: 300, Currency: "CNY"}, cfg)
	if err != nil || again.ChannelAuthNo != freeze.ChannelAuthNo {
		t.Fatalf("repeat Freeze = %+v err %v, want same channel auth no %q", again, err, freeze.ChannelAuthNo)
	}

	if got, err := p.QueryAuth(ctx, "AUTH-EXT-1", cfg); err != nil || got.Status != AuthAuthorized {
		t.Fatalf("QueryAuth after freeze = %+v err %v", got, err)
	}

	part, err := p.CompleteAuth(ctx, AuthCompleteRequest{AuthNo: "AUTH-EXT-1", ChannelAuthNo: freeze.ChannelAuthNo, AmountMinor: 100, PaymentNo: "PAY-CAP-1"}, cfg)
	if err != nil {
		t.Fatalf("CompleteAuth partial: %v", err)
	}
	if part.Status != AuthPartialCaptured || part.AmountMinor != 100 {
		t.Fatalf("partial capture = %+v", part)
	}
	// 同一张请款单重试：额度不累加（真渠道按 out_trade_no 幂等）。
	dup, err := p.CompleteAuth(ctx, AuthCompleteRequest{AuthNo: "AUTH-EXT-1", AmountMinor: 100, PaymentNo: "PAY-CAP-1"}, cfg)
	if err != nil {
		t.Fatalf("CompleteAuth repeat: %v", err)
	}
	if q, _ := p.QueryAuth(ctx, "AUTH-EXT-1", cfg); q.AmountMinor != 200 {
		t.Fatalf("重复请款把冻结额刷爆了：剩余 %d，want 200（dup=%+v）", q.AmountMinor, dup)
	}
	if over, err := p.CompleteAuth(ctx, AuthCompleteRequest{AuthNo: "AUTH-EXT-1", AmountMinor: 5000, PaymentNo: "PAY-CAP-OVER"}, cfg); err != nil || over.Reason == "" {
		t.Fatalf("超出冻结额的请款必须被渠道拒绝，got %+v err %v", over, err)
	}

	rest, err := p.CompleteAuth(ctx, AuthCompleteRequest{AuthNo: "AUTH-EXT-1", AmountMinor: 200, PaymentNo: "PAY-CAP-2"}, cfg)
	if err != nil || rest.Status != AuthCaptured {
		t.Fatalf("请款收尾 = %+v err %v", rest, err)
	}
	if after, err := p.CancelAuth(ctx, AuthCancelRequest{AuthNo: "AUTH-EXT-1"}, cfg); err != nil || after.Reason == "" {
		t.Fatalf("已全额请款的授权不能被撤销，got %+v err %v", after, err)
	}

	// 另一笔：撤销释放剩余额度，重复撤销幂等。
	if _, err := p.Freeze(ctx, AuthFreezeRequest{AuthNo: "AUTH-EXT-2", AmountMinor: 500, Currency: "USD"}, cfg); err != nil {
		t.Fatalf("Freeze 2: %v", err)
	}
	voided, err := p.CancelAuth(ctx, AuthCancelRequest{AuthNo: "AUTH-EXT-2", Reason: "客人离店"}, cfg)
	if err != nil || voided.Status != AuthVoided || voided.AmountMinor != 500 {
		t.Fatalf("CancelAuth = %+v err %v", voided, err)
	}
	again2, err := p.CancelAuth(ctx, AuthCancelRequest{AuthNo: "AUTH-EXT-2"}, cfg)
	if err != nil || again2.Status != AuthVoided || again2.ChannelAuthNo != voided.ChannelAuthNo {
		t.Fatalf("repeat CancelAuth = %+v err %v", again2, err)
	}

	if nf, err := p.QueryAuth(ctx, "AUTH-NOPE", cfg); err != nil || nf.Status != "not_found" {
		t.Fatalf("查无此授权应回 not_found 而不是报错，got %+v err %v", nf, err)
	}
	if _, err := p.CompleteAuth(ctx, AuthCompleteRequest{AuthNo: "AUTH-NOPE", AmountMinor: 1}, cfg); err == nil {
		t.Fatal("对不存在的授权请款必须报错")
	}
	if _, err := p.Freeze(ctx, AuthFreezeRequest{AuthNo: "AUTH-BADCUR", AmountMinor: 100, Currency: "BRL"}, cfg); err == nil {
		t.Fatal("未声明币种不能被冻结")
	} else if k := extRespKey(err); k != errCapabilityUnsupported {
		t.Errorf("币种缺口应报 %s，实报 %s", errCapabilityUnsupported, k)
	}
}

// ---- mock：账单确定性 ----

func TestMockBillIsDeterministic(t *testing.T) {
	ResetMockLedger()
	p := mockProvider{}
	ctx, cfg := context.Background(), extMockCfg()

	for _, no := range []string{"PAY-BILL-1", "PAY-BILL-2"} {
		if _, err := p.Create(ctx, PayRequest{PaymentNo: no, AmountMinor: 1234, Currency: "CNY", Method: "page"}, cfg); err != nil {
			t.Fatalf("Create %s: %v", no, err)
		}
		if _, err := p.VerifyCallback(extMockNotify(t, cfg["secret"],
			mockCallbackBody{PaymentNo: no, Result: "success", ChannelTxnID: "mock_txn_" + no}), cfg); err != nil {
			t.Fatalf("VerifyCallback %s: %v", no, err)
		}
	}
	if _, err := p.Refund(ctx, RefundRequest{RefundNo: "RF-BILL-1", PaymentNo: "PAY-BILL-1", AmountMinor: 100, IdempotencyKey: "rfBILL1"}, cfg); err != nil {
		t.Fatalf("Refund: %v", err)
	}
	// 未回执的退款不进账单（渠道账单只列已成立的事实）：pending 分支不许出现在行里。
	if _, err := p.Refund(ctx, RefundRequest{RefundNo: "RF-BILL-PENDING", PaymentNo: "PAY-BILL-2", AmountMinor: 50, IdempotencyKey: "rfBILLP"}, cfg); err != nil {
		t.Fatalf("Refund pending: %v", err)
	}

	today := time.Now().In(mockBillLoc).Format("2006-01-02")
	first, err := p.FetchBill(ctx, BillRequest{BillDate: today}, cfg)
	if err != nil {
		t.Fatalf("FetchBill: %v", err)
	}
	second, err := p.FetchBill(ctx, BillRequest{BillDate: today}, cfg)
	if err != nil {
		t.Fatalf("FetchBill repeat: %v", err)
	}
	if first.Digest != second.Digest || first.RowCount != second.RowCount {
		t.Fatalf("同一批单据的账单必须逐次一致：%s/%d vs %s/%d", first.Digest, first.RowCount, second.Digest, second.RowCount)
	}
	if len(first.Rows) != first.RowCount {
		t.Fatalf("RowCount %d != len(Rows) %d", first.RowCount, len(first.Rows))
	}
	var trades, refunds int
	for _, r := range first.Rows {
		switch r.Kind {
		case BillKindTrade:
			if r.PaymentNo == "" || r.AmountMinor <= 0 {
				t.Fatalf("trade 行必须带本地单号与金额：%+v", r)
			}
			trades++
		case BillKindRefund:
			if r.RefundNo == "" || r.AmountMinor <= 0 {
				t.Fatalf("refund 行必须带退款单号与金额：%+v", r)
			}
			refunds++
		default:
			t.Fatalf("unknown bill row kind %q", r.Kind)
		}
	}
	if trades == 0 || refunds == 0 {
		t.Fatalf("账单要同时覆盖 trade 与 refund 两种行，got trades=%d refunds=%d rows=%+v", trades, refunds, first.Rows)
	}
	for _, r := range first.Rows {
		if r.RefundNo == "RF-BILL-PENDING" {
			t.Fatalf("pending 退款不该出现在渠道账单里：%+v", r)
		}
	}

	t.Run("empty_period_is_not_a_failure", func(t *testing.T) {
		// 「解析 0 行」与「下载失败」必须是两件事：对账任务据此区分差异与故障。
		empty, err := p.FetchBill(ctx, BillRequest{BillDate: "2000-01-01"}, cfg)
		if err != nil {
			t.Fatalf("空账期不能报错：%v", err)
		}
		if empty.RowCount != 0 || len(empty.Rows) != 0 || empty.Digest == "" {
			t.Fatalf("空账期账单 = %+v", empty)
		}
	})

	t.Run("fee_and_long_row_knobs", func(t *testing.T) {
		knob := map[string]string{"secret": cfg["secret"], "billFeeBp": "100", "billExtraRow": "1"}
		withFee, err := p.FetchBill(ctx, BillRequest{BillDate: today}, knob)
		if err != nil {
			t.Fatalf("FetchBill with knobs: %v", err)
		}
		if withFee.Digest == first.Digest {
			t.Fatal("手续费/长款旋钮必须改变账单内容与摘要")
		}
		if withFee.RowCount != first.RowCount+1 {
			t.Fatalf("长款行缺失：%d vs %d", withFee.RowCount, first.RowCount)
		}
		var long *BillRow
		for i := range withFee.Rows {
			r := withFee.Rows[i]
			switch r.Kind {
			case BillKindTrade:
				if r.PaymentNo == "" {
					long = &withFee.Rows[i]
					continue
				}
				if r.FeeMinor != r.AmountMinor*100/10000 {
					t.Fatalf("手续费按 100bp 计算不符：%+v", r)
				}
			case BillKindRefund:
				if r.FeeMinor > 0 {
					t.Fatalf("退款行的手续费应是冲销（<=0）：%+v", r)
				}
			}
		}
		if long == nil || long.ChannelTxnID == "" {
			t.Fatalf("账单里要有那笔渠道多出来的长款行，got %+v", withFee.Rows)
		}
	})

	t.Run("digest_tracks_row_content", func(t *testing.T) {
		// 台账多一笔流水 → 摘要必须变（对账复现靠它定位是哪一批账单）。
		if _, err := p.Create(ctx, PayRequest{PaymentNo: "PAY-BILL-3", AmountMinor: 700, Currency: "CNY"}, cfg); err != nil {
			t.Fatalf("Create 3: %v", err)
		}
		if _, err := p.VerifyCallback(extMockNotify(t, cfg["secret"],
			mockCallbackBody{PaymentNo: "PAY-BILL-3", Result: "success", ChannelTxnID: "mock_txn_3"}), cfg); err != nil {
			t.Fatalf("VerifyCallback 3: %v", err)
		}
		third, err := p.FetchBill(ctx, BillRequest{BillDate: today}, cfg)
		if err != nil {
			t.Fatalf("FetchBill third: %v", err)
		}
		if third.Digest == first.Digest || third.RowCount != first.RowCount+1 {
			t.Fatalf("新增流水后账单没变：%s/%d vs %s/%d", third.Digest, third.RowCount, first.Digest, first.RowCount)
		}
	})
}

// ---- mock：代付幂等 ----

func TestMockTransferIsIdempotent(t *testing.T) {
	ResetMockLedger()
	p := mockProvider{}
	ctx, cfg := context.Background(), extMockCfg()

	in := TransferRequest{BatchNo: "PB-EXT-1", ItemNo: "PB-EXT-1-01", AmountMinor: 8800, Currency: "CNY", ReceiverType: "personal", ReceiverID: "mock-openid"}
	first, err := p.Transfer(ctx, in, cfg)
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if !first.Accepted || first.Pending || first.State != mockOutcomeSuccess {
		t.Fatalf("Transfer = %+v", first)
	}
	if !strings.HasPrefix(first.ChannelBatchNo, "mockbt_") || !strings.HasPrefix(first.ChannelDetailNo, "mockdt_") {
		t.Fatalf("代付渠道号必须一眼看出是 mock，got %+v", first)
	}
	// 同一 ItemNo 重复调用：不重复转账、不换号。
	for i := 0; i < 3; i++ {
		repeat, err := p.Transfer(ctx, in, cfg)
		if err != nil {
			t.Fatalf("Transfer repeat: %v", err)
		}
		if *repeat != *first {
			t.Fatalf("同幂等号重复代付结果漂移：%+v vs %+v", repeat, first)
		}
	}
	batch, err := p.QueryTransfer(ctx, "PB-EXT-1", cfg)
	if err != nil || batch.State != mockOutcomeSuccess || !batch.Accepted {
		t.Fatalf("QueryTransfer = %+v err %v", batch, err)
	}
	if batch.ChannelBatchNo != first.ChannelBatchNo {
		t.Fatalf("批次号与受理时不一致：%q vs %q", batch.ChannelBatchNo, first.ChannelBatchNo)
	}

	// 批次内第二笔：异步态（未回执）必须把整批拖在 pending 上。
	second, err := p.Transfer(ctx, TransferRequest{BatchNo: "PB-EXT-1", ItemNo: "PB-EXT-1-PENDING", AmountMinor: 100}, cfg)
	if err != nil {
		t.Fatalf("Transfer pending item: %v", err)
	}
	if !second.Pending || second.Accepted == false || second.State != mockOutcomePending {
		t.Fatalf("代付异步态没演出来：%+v", second)
	}
	if after, err := p.QueryTransfer(ctx, "PB-EXT-1", cfg); err != nil || after.State != mockOutcomePending {
		t.Fatalf("批次聚合状态 = %+v err %v", after, err)
	}

	if rejected, err := p.Transfer(ctx, TransferRequest{BatchNo: "PB-EXT-2", ItemNo: "PB-EXT-2-FAIL", AmountMinor: 10}, cfg); err != nil || rejected.Accepted || rejected.Pending {
		t.Fatalf("被拒的代付不能报受理，got %+v err %v", rejected, err)
	}
	if nf, err := p.QueryTransfer(ctx, "PB-NOPE", cfg); err != nil || nf.State != "not_found" {
		t.Fatalf("查无此批次应回 not_found，got %+v err %v", nf, err)
	}
	if _, err := p.Transfer(ctx, TransferRequest{BatchNo: "PB-EXT-3", AmountMinor: 10}, cfg); err == nil {
		t.Fatal("没有 itemNo 的代付必须报错（否则无法幂等）")
	}
}

// ---- mock：回调时间窗（既有硬窗不许退化）----

func TestMockNotifyWindowAndNotifyID(t *testing.T) {
	ResetMockLedger()
	p, cfg := mockProvider{}, extMockCfg()
	if _, err := p.Create(context.Background(), PayRequest{PaymentNo: "PAY-WIN", AmountMinor: 100}, cfg); err != nil {
		t.Fatalf("Create: %v", err)
	}
	notify := func(ts int64) CallbackInput {
		raw, err := json.Marshal(mockCallbackBody{PaymentNo: "PAY-WIN", Result: "success", ChannelTxnID: "mock_txn_win"})
		if err != nil {
			t.Fatal(err)
		}
		return CallbackInput{Channel: "mock", Body: raw, Headers: map[string]string{
			"x-timestamp": strconv.FormatInt(ts, 10),
			"x-signature": SignMockCallback(cfg["secret"], raw, ts),
		}}
	}
	// mock 的时间戳每次下发都刷新且参与签名 → 双向硬窗（超前、滞后都要挡）。
	for name, ts := range map[string]int64{
		"far_future": time.Now().Add(time.Hour).Unix(),
		"far_past":   time.Now().Add(-time.Hour).Unix(),
	} {
		if _, err := p.VerifyCallback(notify(ts), cfg); !errors.Is(err, errStaleWindow) {
			t.Errorf("%s 时间戳应被时间窗挡下并报 errStaleWindow，got %v", name, err)
		}
	}
	res, err := p.VerifyCallback(notify(time.Now().Unix()), cfg)
	if err != nil {
		t.Fatalf("合法回调被拒: %v", err)
	}
	if !strings.HasPrefix(res.NotifyID, "mocknt_") {
		t.Fatalf("mock 回调必须回填 NotifyID，got %q", res.NotifyID)
	}
	// 同一条通知的重试：NotifyID 必须相同（渠道就是这个行为，取证与排障靠它）。
	res2, err := p.VerifyCallback(notify(time.Now().Unix()), cfg)
	if err != nil || res2.NotifyID != res.NotifyID {
		t.Fatalf("重复回调的 NotifyID 漂移：%q vs %q err %v", res2.NotifyID, res.NotifyID, err)
	}
	// 超窗之外，验签失败仍然是普通错误（不许被误报成 stale）。
	bad := notify(time.Now().Unix())
	bad.Headers["x-signature"] = strings.Repeat("0", 64)
	if _, err := p.VerifyCallback(bad, cfg); err == nil || errors.Is(err, errStaleWindow) {
		t.Fatalf("坏签名应报验签失败，got %v", err)
	}
}

// ---- epay：能力边界 ----

func TestEpayCapabilitiesAreAllOff(t *testing.T) {
	p, err := LookupProvider("epay")
	if err != nil {
		t.Fatalf("lookup epay: %v", err)
	}
	caps := ProviderCaps(p)
	if caps.RefundQuery || caps.Auth || caps.Bill || caps.Payout {
		t.Fatalf("易支付协议没有退款查询/预授权/账单/代付，got %+v", caps)
	}
	if len(caps.Currencies) != 1 || caps.Currencies[0] != "CNY" {
		t.Fatalf("易支付只收 CNY，got %v", caps.Currencies)
	}
	if !caps.supportsCurrency("CNY") || caps.supportsCurrency("USD") {
		t.Fatalf("币种矩阵不符：%+v", caps)
	}
	for _, m := range caps.Methods {
		if m == "jsapi" || m == "app" || m == "native" || m == "micropay" {
			t.Fatalf("易支付只有跳转类下单方式，got %v", caps.Methods)
		}
	}
	for _, m := range []string{"page", "wap"} {
		if !caps.supportsMethod(m) {
			t.Errorf("易支付应支持跳转下单 %s（清单 %v）", m, caps.Methods)
		}
	}
}

// TestEpayCapabilityCallsFailLoudly 检查「被误调用时绝不静默成功」。
// 主闸是 Capabilities 的布尔位（服务层按它禁入口并直接报 PAY_CAPABILITY_UNSUPPORTED），
// 这组方法是第二道闸：哪怕路由写错了也必须拿到错误。
func TestEpayCapabilityCallsFailLoudly(t *testing.T) {
	p := epayProvider{}
	ctx, cfg := context.Background(), map[string]string{"gateway": "https://pay.example.com", "pid": "1", "key": "k"}
	cases := []struct {
		name string
		call func() error
	}{
		{"refund_query", func() error { _, err := p.QueryRefund(ctx, RefundQueryRequest{RefundNo: "RF-1"}, cfg); return err }},
		{"freeze", func() error {
			_, err := p.Freeze(ctx, AuthFreezeRequest{AuthNo: "AUTH-1", AmountMinor: 1}, cfg)
			return err
		}},
		{"complete_auth", func() error {
			_, err := p.CompleteAuth(ctx, AuthCompleteRequest{AuthNo: "AUTH-1", AmountMinor: 1}, cfg)
			return err
		}},
		{"cancel_auth", func() error { _, err := p.CancelAuth(ctx, AuthCancelRequest{AuthNo: "AUTH-1"}, cfg); return err }},
		{"query_auth", func() error { _, err := p.QueryAuth(ctx, "AUTH-1", cfg); return err }},
		{"fetch_bill", func() error { _, err := p.FetchBill(ctx, BillRequest{BillDate: "2026-01-01"}, cfg); return err }},
		{"transfer", func() error {
			_, err := p.Transfer(ctx, TransferRequest{BatchNo: "B", ItemNo: "I", AmountMinor: 1}, cfg)
			return err
		}},
		{"query_transfer", func() error { _, err := p.QueryTransfer(ctx, "B", cfg); return err }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatal("不支持的能力必须报错，绝不能静默成功")
			}
			var re *response.Error
			if !errors.As(err, &re) {
				t.Fatalf("能力缺口必须是业务错误（带错误码），got %T %v", err, err)
			}
			if re.Status != 400 {
				t.Errorf("能力缺口应报 400，实报 %d", re.Status)
			}
			if !strings.Contains(re.Msg, "epay") || !strings.Contains(re.Msg, "不支持") {
				t.Errorf("错误文案要指名渠道与缺口，got %q", re.Msg)
			}
			// 契约要求的错误码。底座 provider.go 的 capabilityUnsupported() 经
			// channelConfigError 落到 PAY_CHANNEL_UNAVAILABLE，与 02 号契约里的
			// PAY_CAPABILITY_UNSUPPORTED 不符；渠道层用 capabilityGapError 校正成
			// 契约值（主链把底座那个 helper 改对后，校正分支可删，本断言不变绿）。
			if re.Key != errCapabilityUnsupported {
				t.Errorf("能力缺口必须报 %s，实报 %s", errCapabilityUnsupported, re.Key)
			}
			// 底座那条路径的原始错误仍在 Wrap 链里，排障时能看到它是渠道层校正过的。
			if re.Err == nil || !strings.Contains(re.Err.Error(), "不支持") {
				t.Errorf("错误链里要留着底座 capabilityUnsupported 的原文，got %v", re.Err)
			}
		})
	}
}

// ---- epay：通知时间窗 ----

func TestEpayNotifyTimestampWindow(t *testing.T) {
	const key = "ext-epay-key"
	p := epayProvider{}
	cfg := map[string]string{"key": key}
	base := func() map[string]string {
		return map[string]string{
			"pid": "1001", "type": "alipay", "out_trade_no": "EPAY-1", "trade_no": "2026010100000001",
			"trade_status": "TRADE_SUCCESS", "money": "12.34", "name": "测试商品",
		}
	}

	t.Run("no_timestamp_still_passes", func(t *testing.T) {
		// 兼容旧上游：多数易支付网关不下发时间参数，绝不能因为加了窗口就把它们全拒掉。
		res, err := p.VerifyCallback(CallbackInput{Channel: "epay", Body: extEpayNotify(key, base())}, cfg)
		if err != nil {
			t.Fatalf("不带时间戳的历史通知被拒了：%v", err)
		}
		if !res.Paid || res.PaymentNo != "EPAY-1" || res.AmountMinor != 1234 {
			t.Fatalf("解析结果不符：%+v", res)
		}
		if res.NotifyID != "2026010100000001" {
			t.Fatalf("NotifyID 必须是渠道交易号 trade_no，got %q", res.NotifyID)
		}
	})

	t.Run("fresh_timestamp_passes", func(t *testing.T) {
		p2 := base()
		p2["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
		res, err := p.VerifyCallback(CallbackInput{Body: extEpayNotify(key, p2)}, cfg)
		if err != nil || !res.Paid {
			t.Fatalf("窗口内的通知应通过，res=%+v err=%v", res, err)
		}
	})

	t.Run("future_timestamp_is_stale", func(t *testing.T) {
		for _, c := range []struct {
			param, stamp string
		}{
			{"timestamp", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)},
			{"timestamp", strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)}, // 毫秒口径也要认
		} {
			p2 := base()
			p2[c.param] = c.stamp
			_, err := p.VerifyCallback(CallbackInput{Body: extEpayNotify(key, p2)}, cfg)
			if !errors.Is(err, errStaleWindow) {
				t.Fatalf("%s=%s 超前时间戳应报 errStaleWindow，got %v", c.param, c.stamp, err)
			}
			if k := extRespKey(err); k != errNotifyStale {
				t.Errorf("错误码应是 %s，实报 %s", errNotifyStale, k)
			}
		}
	})

	t.Run("expired_timestamp_is_stale", func(t *testing.T) {
		p2 := base()
		p2["timestamp"] = strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
		if _, err := p.VerifyCallback(CallbackInput{Body: extEpayNotify(key, p2)}, cfg); !errors.Is(err, errStaleWindow) {
			t.Fatalf("超窗（过去方向）的时间戳应被挡下，got %v", err)
		}
	})

	t.Run("business_time_only_checks_clock_ahead", func(t *testing.T) {
		// time 在上游多半是业务时间（成交时间），重试不刷新：晚到要放行，超前才拒。
		p2 := base()
		p2["time"] = time.Now().Add(-6 * time.Hour).In(epayBizLoc).Format("2006-01-02 15:04:05")
		if res, err := p.VerifyCallback(CallbackInput{Body: extEpayNotify(key, p2)}, cfg); err != nil || !res.Paid {
			t.Fatalf("晚到的业务时间不能被当成过期通知丢掉：res=%+v err=%v", res, err)
		}
		p3 := base()
		p3["time"] = strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
		if _, err := p.VerifyCallback(CallbackInput{Body: extEpayNotify(key, p3)}, cfg); !errors.Is(err, errStaleWindow) {
			t.Fatalf("超前的 time 参数同样要拒，got %v", err)
		}
	})

	t.Run("unparseable_timestamp_is_rejected", func(t *testing.T) {
		p2 := base()
		p2["timestamp"] = "刚刚"
		if _, err := p.VerifyCallback(CallbackInput{Body: extEpayNotify(key, p2)}, cfg); err == nil {
			t.Fatal("签名过但说不清格式的时间参数要当异常报文处理")
		}
	})

	t.Run("tampered_timestamp_inside_window_fails_signature", func(t *testing.T) {
		// 时间参数参与签名：改它必须验签失败（否则时间窗只是摆设）。
		p2 := base()
		p2["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
		body := string(extEpayNotify(key, p2))
		tampered := strings.Replace(body, "timestamp="+p2["timestamp"], "timestamp="+strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10), 1)
		if tampered == body {
			t.Fatal("用例没改成时间戳")
		}
		if _, err := p.VerifyCallback(CallbackInput{Body: []byte(tampered)}, cfg); err == nil {
			t.Fatal("篡改时间戳必须验签失败")
		}
	})
}

// ---- epay：既有对外形态不许破坏 ----

func TestEpayProviderSurfaceUnchanged(t *testing.T) {
	p := epayProvider{}
	schema := p.ConfigSchema()
	if len(schema) != 4 {
		t.Fatalf("易支付凭据字段变了：%+v", schema)
	}
	got := map[string]ConfigField{}
	for _, f := range schema {
		got[f.Key] = f
	}
	for _, k := range []string{"gateway", "pid", "key", "payType"} {
		f, ok := got[k]
		if !ok || !f.Required {
			t.Fatalf("字段 %s 缺失或不再必填：%+v", k, schema)
		}
	}
	if !got["key"].Secret || got["key"].Type != "password" {
		t.Errorf("商户密钥必须是保密字段：%+v", got["key"])
	}
	ct, body := p.CallbackAck()
	if ct != "text/plain; charset=utf-8" || body != "success" {
		t.Fatalf("易支付应答格式变了：%q %q", ct, body)
	}

	params, err := p.Create(context.Background(), PayRequest{
		PaymentNo: "EPAY-CREATE", Subject: "s", AmountMinor: 100, Currency: "CNY", Method: "wap",
		NotifyURL: "https://shop.example.com/api/v1/plugins/pay/notify/epay1",
	}, map[string]string{"gateway": "https://pay.example.com/", "pid": "1001", "key": "abc", "payType": "alipay"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	u, err := url.Parse(params.PayURL)
	if err != nil {
		t.Fatalf("pay url %q: %v", params.PayURL, err)
	}
	if u.Path != "/submit.php" || u.Query().Get("sign") == "" {
		t.Fatalf("Create 仍要返回签名过的 submit.php 跳转串：%q", params.PayURL)
	}
	if err := cnyOnly("epay", "USD"); err == nil {
		t.Fatal("能力矩阵说只收 CNY，Create 也必须挡外币")
	}
	// 跳转串的下单方式不参与协议（submit.php 与设备无关），矩阵只用于前端与门禁口径。
	if len(p.Capabilities().Methods) == 0 {
		t.Fatal("epay 必须声明跳转类下单方式")
	}
}
