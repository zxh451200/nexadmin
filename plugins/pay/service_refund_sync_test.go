package pay

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
)

// ---- 测试桩：可选能力 Provider ----
//
// 夹具沿用本包既有的 sqlmock（mysql 方言）路子：go.mod 里没有 sqlite 驱动，加驱动要改 go.mod
// （不在本泳道写域），故走 service_test.go 同一套 mockDB/channelRow/orderRow。sqlmock 与
// sqlite 一样不真正执行 SQL、不强制 FOR UPDATE 行锁——涉及锁与并发的断言标注「需真库覆盖」。

// stubQuery 是退款查询桩：实现 RefundQueryProvider + CapabilityProvider，可配 caps/res/err，计调用次数。
type stubQuery struct {
	mockProvider
	name    string
	caps    Capabilities
	res     *RefundStatusResult
	err     error
	calls   int
	lastReq RefundQueryRequest
}

func (p *stubQuery) Name() string               { return p.name }
func (p *stubQuery) Capabilities() Capabilities { return p.caps }
func (p *stubQuery) QueryRefund(_ context.Context, req RefundQueryRequest, _ map[string]string) (*RefundStatusResult, error) {
	p.calls++
	p.lastReq = req
	return p.res, p.err
}

// stubAuth 是预授权桩：实现 AuthProvider + CapabilityProvider。caps.Auth 可关，
// 用来验证「实现了接口但未声明能力」仍被拒（负向控制）。
type stubAuth struct {
	mockProvider
	name        string
	caps        Capabilities
	freezeRes   *AuthResult
	freezeErr   error
	completeRes *AuthResult
	completeErr error
	cancelRes   *AuthResult
	cancelErr   error
	queryRes    *AuthResult
	queryErr    error
	freezeCalls int
	captCalls   int
	cancelCalls int
	queryCalls  int
}

func (p *stubAuth) Name() string               { return p.name }
func (p *stubAuth) Capabilities() Capabilities { return p.caps }
func (p *stubAuth) Freeze(_ context.Context, _ AuthFreezeRequest, _ map[string]string) (*AuthResult, error) {
	p.freezeCalls++
	return p.freezeRes, p.freezeErr
}
func (p *stubAuth) CompleteAuth(_ context.Context, _ AuthCompleteRequest, _ map[string]string) (*AuthResult, error) {
	p.captCalls++
	return p.completeRes, p.completeErr
}
func (p *stubAuth) CancelAuth(_ context.Context, _ AuthCancelRequest, _ map[string]string) (*AuthResult, error) {
	p.cancelCalls++
	return p.cancelRes, p.cancelErr
}
func (p *stubAuth) QueryAuth(_ context.Context, _ string, _ map[string]string) (*AuthResult, error) {
	p.queryCalls++
	if p.queryRes == nil && p.queryErr == nil {
		return &AuthResult{}, nil
	}
	return p.queryRes, p.queryErr
}

// countingLogger 计 Warn 条数，验证 no-op 留痕「查了 N 笔、翻了 M 笔」。
type countingLogger struct{ warns int }

func (l *countingLogger) Info(string, ...any)  {}
func (l *countingLogger) Warn(string, ...any)  { l.warns++ }
func (l *countingLogger) Error(string, ...any) {}

// ---- 行夹具 ----

func refundRow(status string, amount int64, attempts int, lastSync any, idem string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "refund_no", "payment_no", "subject_id", "biz_type", "biz_no",
		"amount_minor", "currency", "fx_rate_micro", "status", "sync_attempts", "last_sync_at", "idempotency_key",
	}).AddRow(5001, 7, "RF5001", "PAY1", "", "trade_refund", "R-1", amount, "CNY", 1000000, status, attempts, lastSync, idem)
}

func refundScanRow(lastSync any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "refund_no", "payment_no", "biz_type", "biz_no",
		"amount_minor", "currency", "fx_rate_micro", "status", "sync_attempts", "last_sync_at",
	}).AddRow(5001, 7, "RF5001", "PAY1", "trade_refund", "R-1", 200, "CNY", 1000000, RefundPending, 0, lastSync)
}

func authRow(status string, amount, captured int64, channelCode, chanAuth string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "auth_no", "biz_type", "biz_no", "subject_id", "channel_code", "method",
		"status", "amount_minor", "captured_minor", "currency", "fx_rate_micro", "channel_auth_no", "payer_id",
	}).AddRow(2001, 7, "AUTH2001", "preauth", "PA-1", "subj", channelCode, "native",
		status, amount, captured, "CNY", 1000000, chanAuth, "buyer")
}

func emptyRefundRows() *sqlmock.Rows { return sqlmock.NewRows([]string{"id", "tenant_id"}) }
func emptyAuthRows() *sqlmock.Rows   { return sqlmock.NewRows([]string{"id", "tenant_id"}) }
func emptyOrderRows() *sqlmock.Rows  { return sqlmock.NewRows([]string{"id", "tenant_id"}) }
func emptyRuleRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "tenant_id", "subject_id", "biz_type", "rate_bp", "fixed_minor"})
}

// ---- 缺陷 1：退款黑洞 ----

// 1. 异步退款停在 pending → SyncRefund 翻成 succeeded、支付单推进、出站箱落一行。
func TestSyncRefundFlipsPendingToSucceeded(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	sp := &stubQuery{name: "stubq-ok", caps: Capabilities{RefundQuery: true},
		res: &RefundStatusResult{Found: true, OK: true, ChannelRefundID: "chanrf_9"}}
	RegisterProvider(sp)

	// top: refund + order + channel；transition：lock refund + lock order + 2 UPDATE + outbox；emit：UPDATE outbox；reload：refund。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
		WillReturnRows(refundRow(RefundPending, 200, 0, nil, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no").
		WillReturnRows(orderRow("PAY1", StatusRefunding, 500, 0))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("mock", "stubq-ok", `{"k":"v"}`))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
		WillReturnRows(refundRow(RefundPending, 200, 0, nil, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no").
		WillReturnRows(orderRow("PAY1", StatusRefunding, 500, 0))
	m.ExpectExec("UPDATE `plg_pay_refund`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("UPDATE `plg_pay_order`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("INSERT INTO `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
		WillReturnRows(refundRow(RefundSucceeded, 200, 0, nil, "chanrf_9"))

	r, err := s.SyncRefund(tenantCtx, "RF5001")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != RefundSucceeded {
		t.Fatalf("refund status = %q, want succeeded", r.Status)
	}
	if sp.calls != 1 {
		t.Fatalf("QueryRefund calls = %d, want 1", sp.calls)
	}
	// 补偿查询必须带与发起退款时同一个幂等号。
	wantKey := refundIdempotencyKey("PAY1", "trade_refund", "R-1")
	if sp.lastReq.IdempotencyKey != wantKey {
		t.Fatalf("query idem key = %q, want %q", sp.lastReq.IdempotencyKey, wantKey)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 1b. 进事务时并发回调已经翻好：本轮没写 → changed=false，且交回的是库里真值而不是
// 手里那份 pending 副本。sweeper 靠这两个信息把 flipped 与 skipped 分开，
// 否则补查者与回调各数一次，「救回几笔」会是资金事实的两倍。
func TestSyncRefundAlreadyFlippedByPeer(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	sp := &stubQuery{name: "stubq-peer", caps: Capabilities{RefundQuery: true},
		res: &RefundStatusResult{Found: true, OK: true, ChannelRefundID: "chanrf_peer"}}
	RegisterProvider(sp)

	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
		WillReturnRows(refundRow(RefundPending, 200, 0, nil, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no").
		WillReturnRows(orderRow("PAY1", StatusRefunding, 500, 0))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("mock", "stubq-peer", `{"k":"v"}`))
	// 事务里的 FOR UPDATE 读到的是已经终态的行：直接返回，不写任何表。
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
		WillReturnRows(refundRow(RefundSucceeded, 200, 0, nil, "chanrf_peer"))
	m.ExpectCommit()
	// changed=false 也要回读真值交回去。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
		WillReturnRows(refundRow(RefundSucceeded, 200, 0, nil, "chanrf_peer"))

	upd, changed, err := s.syncRefundOnce(tenantCtx, "RF5001")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("changed = true, want false（本轮没有写入就不算救回一笔）")
	}
	if upd == nil || upd.Status != RefundSucceeded {
		t.Fatalf("返回 %+v, want succeeded —— 把旧的 pending 副本交回去，调用方会以为还没结论", upd)
	}
	if sp.calls != 1 {
		t.Fatalf("QueryRefund calls = %d, want 1", sp.calls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 2. 渠道拒绝 → failed 且 refunding 还原为上一个可退状态（refunded=0→succeeded / >0→partially_refunded）。
func TestSyncRefundChannelReject(t *testing.T) {
	cases := []struct {
		name     string
		refunded int64
	}{
		{"restore-succeeded", 0},
		{"restore-partially-refunded", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, m := mockDB(t)
			s := NewService(db, nopLog())
			sp := &stubQuery{name: "stubq-reject-" + tc.name, caps: Capabilities{RefundQuery: true},
				res: &RefundStatusResult{Found: true, OK: false, Pending: false, Reason: "channel declined"}}
			RegisterProvider(sp)

			m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
				WillReturnRows(refundRow(RefundPending, 200, 0, nil, ""))
			m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no").
				WillReturnRows(orderRow("PAY1", StatusRefunding, 500, tc.refunded))
			m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
				WillReturnRows(channelRow("mock", sp.name, `{"k":"v"}`))
			m.ExpectBegin()
			m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
				WillReturnRows(refundRow(RefundPending, 200, 0, nil, ""))
			m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no").
				WillReturnRows(orderRow("PAY1", StatusRefunding, 500, tc.refunded))
			m.ExpectExec("UPDATE `plg_pay_refund`").WillReturnResult(sqlmock.NewResult(1, 1))
			// 还原 refunding→上一个可退状态：这条 UPDATE 的 status 由 transitionRefund 复用既有 refundRestoreStatus 决定。
			m.ExpectExec("UPDATE `plg_pay_order`").WillReturnResult(sqlmock.NewResult(1, 1))
			m.ExpectExec("INSERT INTO `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
			m.ExpectCommit()
			m.ExpectBegin()
			m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
			m.ExpectCommit()
			m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
				WillReturnRows(refundRow(RefundFailed, 200, 0, nil, ""))

			r, err := s.SyncRefund(tenantCtx, "RF5001")
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != RefundFailed {
				t.Fatalf("refund status = %q, want failed", r.Status)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// 3. 渠道 Found=false → 仍 pending、SyncAttempts 递增、幂等号回填、可数日志计数为 1。
func TestSyncRefundNotFoundKeepsPending(t *testing.T) {
	db, m := mockDB(t)
	lg := &countingLogger{}
	s := NewService(db, lg)
	sp := &stubQuery{name: "stubq-notfound", caps: Capabilities{RefundQuery: true},
		res: &RefundStatusResult{Found: false}}
	RegisterProvider(sp)

	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
		WillReturnRows(refundRow(RefundPending, 200, 0, nil, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no").
		WillReturnRows(orderRow("PAY1", StatusRefunding, 500, 0))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("mock", sp.name, `{"k":"v"}`))
	// 节流更新（standalone 写，gorm 默认隐式事务包裹）：sync_attempts++、last_sync_at、回填 idempotency_key。
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_refund`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	r, err := s.SyncRefund(tenantCtx, "RF5001")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != RefundPending {
		t.Fatalf("status = %q, want still pending", r.Status)
	}
	if r.SyncAttempts != 1 {
		t.Fatalf("SyncAttempts = %d, want 1", r.SyncAttempts)
	}
	if r.IdempotencyKey == "" {
		t.Fatal("idempotency key not backfilled")
	}
	if sp.calls != 1 {
		t.Fatalf("QueryRefund calls = %d, want 1", sp.calls)
	}
	// no-op 必须留痕：恰好一条可数告警（若删掉 Found=false 分支它就不会被计）。
	if lg.warns != 1 {
		t.Fatalf("warn log count = %d, want 1", lg.warns)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 4. 渠道不支持 RefundQuery（实现接口但未声明能力）→ PAY_CAPABILITY_UNSUPPORTED。
// 负向控制：删掉 caps 判定这段代码会直接调 QueryRefund，calls 变 1 且不报错，本测试必红。
func TestSyncRefundUnsupported(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	sp := &stubQuery{name: "stubq-nocaps", caps: Capabilities{RefundQuery: false},
		res: &RefundStatusResult{Found: true, OK: true}}
	RegisterProvider(sp)

	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
		WillReturnRows(refundRow(RefundPending, 200, 0, nil, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no").
		WillReturnRows(orderRow("PAY1", StatusRefunding, 500, 0))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("mock", sp.name, `{"k":"v"}`))

	_, err := s.SyncRefund(tenantCtx, "RF5001")
	var re *response.Error
	if !errors.As(err, &re) || re.Key != errCapabilityUnsupported {
		t.Fatalf("want %s, got %v", errCapabilityUnsupported, err)
	}
	if re.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", re.Status)
	}
	if sp.calls != 0 {
		t.Fatalf("QueryRefund must not be called, calls = %d", sp.calls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 5. 已终态退款重复 Sync → 直接返回、不打渠道。
func TestSyncRefundTerminalNoChannel(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	sp := &stubQuery{name: "stubq-terminal", caps: Capabilities{RefundQuery: true},
		res: &RefundStatusResult{Found: true, OK: true}}
	RegisterProvider(sp)

	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
		WillReturnRows(refundRow(RefundSucceeded, 200, 0, nil, "x"))

	r, err := s.SyncRefund(tenantCtx, "RF5001")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != RefundSucceeded {
		t.Fatalf("status = %q", r.Status)
	}
	if sp.calls != 0 {
		t.Fatalf("QueryRefund must not be called for terminal refund, calls = %d", sp.calls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 6. SyncStuckRefunds 节流：同一笔在一个间隔内只被查询一次。
func TestStuckRefundsThrottle(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	sp := &stubQuery{name: "stubq-stuck", caps: Capabilities{RefundQuery: true},
		res: &RefundStatusResult{Found: false}}
	RegisterProvider(sp)

	// 第一轮：扫到 pending 卡住的退款（last_sync_at 为空）→ SyncRefund 内 top refund/order/channel + 节流 UPDATE。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE status").
		WillReturnRows(refundScanRow(nil))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE refund_no").
		WillReturnRows(refundRow(RefundPending, 200, 0, nil, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE payment_no").
		WillReturnRows(orderRow("PAY1", StatusRefunding, 500, 0))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("mock", sp.name, `{"k":"v"}`))
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_refund`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	// 第二轮：同一笔但 last_sync_at 已是本轮时间 → 节流跳过，不再打渠道。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_refund` WHERE status").
		WillReturnRows(refundScanRow(testNow))

	now := testNow
	sum1, err := s.SyncStuckRefunds(tenant.Skip(context.Background()), now, time.Minute, 50)
	if err != nil {
		t.Fatal(err)
	}
	sum2, err := s.SyncStuckRefunds(tenant.Skip(context.Background()), now, time.Minute, 50)
	if err != nil {
		t.Fatal(err)
	}
	if sum1.Checked != 1 {
		t.Fatalf("round1 checked = %d, want 1", sum1.Checked)
	}
	if sum1.NotFound != 1 {
		t.Fatalf("round1 notFound = %d, want 1", sum1.NotFound)
	}
	if sum2.Checked != 0 {
		t.Fatalf("round2 checked = %d, want 0 (throttled)", sum2.Checked)
	}
	if sp.calls != 1 {
		t.Fatalf("QueryRefund calls across two rounds = %d, want 1 (throttle)", sp.calls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// ---- 缺陷 2：预授权 ----

// 冻结→部分请款→请满：全走渠道，用渠道返回的授权号落库，请款以渠道结论决定入账。
func TestAuthFreezeAndCapture(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	sp := &stubAuth{name: "stubauth-flow", caps: Capabilities{Auth: true},
		freezeRes:   &AuthResult{ChannelAuthNo: "chan_auth_1", Status: "success"},
		completeRes: &AuthResult{Status: "success"}}
	RegisterProvider(sp)

	// CreateAuth 的真实顺序：pickChannel → 幂等预检（空）→ INSERT authorizing 占位
	// → Freeze → UPDATE 回填。占位行排在渠道调用之前，sqlmock 的顺序期望就是它的闸：
	// 谁把顺序改回「先冻结再插入」，这条用例立刻红（并发同键会各冻结一次）。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("auth-flow", sp.name, `{"k":"v"}`))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE biz_type").WillReturnRows(emptyAuthRows())
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_pay_auth`").WillReturnResult(sqlmock.NewResult(2001, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_auth`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	a, err := s.CreateAuth(tenantCtx, CreateAuthInput{
		BizType: "preauth", BizNo: "PA-1", SubjectID: "subj", ChannelCode: "auth-flow",
		AmountMinor: 1000, Currency: "CNY", PayerID: "buyer", ValidDays: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ChannelAuthNo != "chan_auth_1" {
		t.Fatalf("channel auth no = %q, must be the real one returned by channel", a.ChannelAuthNo)
	}
	if strings.HasPrefix(a.ChannelAuthNo, "mockauth_") {
		t.Fatal("still fabricating a mock channel auth number")
	}
	if a.Status != AuthAuthorized || sp.freezeCalls != 1 {
		t.Fatalf("status = %q freezeCalls = %d", a.Status, sp.freezeCalls)
	}

	capture := func(bizNo string, amount int64, capturedBefore int64, statusBefore string) {
		// probe 读授权 → channel → tx(lock auth / 查历史请款单 / 渠道 / 规则 / 落单 / 更新授权 / 出站箱 x2) → emit x2。
		m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE auth_no").
			WillReturnRows(authRow(statusBefore, 1000, capturedBefore, "auth-flow", "chan_auth_1"))
		m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
			WillReturnRows(channelRow("auth-flow", sp.name, `{"k":"v"}`))
		m.ExpectBegin()
		m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE auth_no").
			WillReturnRows(authRow(statusBefore, 1000, capturedBefore, "auth-flow", "chan_auth_1"))
		m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE biz_type").WillReturnRows(emptyOrderRows())
		m.ExpectQuery("SELECT \\* FROM `plg_pay_commission_rule`").WillReturnRows(emptyRuleRows())
		m.ExpectQuery("SELECT \\* FROM `plg_pay_commission_rule`").WillReturnRows(emptyRuleRows())
		m.ExpectExec("INSERT INTO `plg_pay_order`").WillReturnResult(sqlmock.NewResult(3001, 1))
		m.ExpectExec("UPDATE `plg_pay_auth`").WillReturnResult(sqlmock.NewResult(1, 1))
		m.ExpectExec("INSERT INTO `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
		m.ExpectExec("INSERT INTO `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(2, 1))
		m.ExpectCommit()
		m.ExpectBegin()
		m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
		m.ExpectCommit()
		m.ExpectBegin()
		m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(1, 1))
		m.ExpectCommit()

		res, err := s.CaptureAuth(tenantCtx, a.AuthNo, CaptureAuthInput{AmountMinor: amount, BizType: "cap", BizNo: bizNo})
		if err != nil {
			t.Fatalf("capture %s: %v", bizNo, err)
		}
		if res.CapturedMinor != capturedBefore+amount {
			t.Fatalf("captured minor = %d, want %d", res.CapturedMinor, capturedBefore+amount)
		}
	}

	capture("C-1", 400, 0, AuthAuthorized)        // 部分请款
	capture("C-2", 600, 400, AuthPartialCaptured) // 请满
	if sp.captCalls != 2 {
		t.Fatalf("CompleteAuth calls = %d, want 2", sp.captCalls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 撤销：先渠道 CancelAuth 确认，再落本地 voided。
func TestAuthVoidChannelConfirm(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	sp := &stubAuth{name: "stubauth-void", caps: Capabilities{Auth: true},
		cancelRes: &AuthResult{Status: "success"}}
	RegisterProvider(sp)

	m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE auth_no").
		WillReturnRows(authRow(AuthAuthorized, 1000, 0, "auth-void", "chan_auth_9"))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("auth-void", sp.name, `{"k":"v"}`))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE auth_no").
		WillReturnRows(authRow(AuthAuthorized, 1000, 0, "auth-void", "chan_auth_9"))
	m.ExpectExec("UPDATE `plg_pay_auth`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	a, err := s.VoidAuth(tenantCtx, "AUTH2001")
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != AuthVoided {
		t.Fatalf("status = %q, want voided", a.Status)
	}
	if sp.cancelCalls != 1 {
		t.Fatalf("CancelAuth calls = %d, want 1", sp.cancelCalls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 撤销兼容旧单：渠道根本不支持 Auth 时（伪造/历史单）退化为仅本地置 voided 并留痕。
// 用 epay 而不是 mock：mock 现在如实声明支持预授权，走的是「渠道没确认就不算撤销」那条路。
func TestAuthVoidLegacyNoAuthChannel(t *testing.T) {
	db, m := mockDB(t)
	lg := &countingLogger{}
	s := NewService(db, lg)

	m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE auth_no").
		WillReturnRows(authRow(AuthAuthorized, 1000, 0, "legacy", ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("legacy", "epay", `{"k":"v"}`))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE auth_no").
		WillReturnRows(authRow(AuthAuthorized, 1000, 0, "legacy", ""))
	m.ExpectExec("UPDATE `plg_pay_auth`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	a, err := s.VoidAuth(tenantCtx, "AUTH2001")
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != AuthVoided {
		t.Fatalf("status = %q, want voided", a.Status)
	}
	if lg.warns != 1 {
		t.Fatalf("legacy void must leave exactly 1 warn trace, got %d", lg.warns)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 冻结不支持预授权的渠道（实现了接口但未声明 Auth 能力，epay 语义）→ PAY_CAPABILITY_UNSUPPORTED。
// 负向控制：删掉 caps 判定会直接 Freeze 并落单，本测试必红。
func TestAuthUnsupportedChannel(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	sp := &stubAuth{name: "stubauth-nocap", caps: Capabilities{Auth: false},
		freezeRes: &AuthResult{ChannelAuthNo: "chan", Status: "success"}}
	RegisterProvider(sp)

	// 能力门禁排在幂等预检之前：不支持预授权的渠道连占位行都不该建。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("c-noauth", sp.name, `{"k":"v"}`))

	_, err := s.CreateAuth(tenantCtx, CreateAuthInput{
		BizType: "preauth", BizNo: "PA-X", ChannelCode: "c-noauth", AmountMinor: 100, Currency: "CNY",
	})
	var re *response.Error
	if !errors.As(err, &re) || re.Key != errCapabilityUnsupported {
		t.Fatalf("want %s, got %v", errCapabilityUnsupported, err)
	}
	if sp.freezeCalls != 0 {
		t.Fatalf("Freeze must not be called when capability unsupported, calls = %d", sp.freezeCalls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 渠道冻结明确失败：错误原样上抛，占位行落 failed + 原因（钱没冻住，业务可换单重试），
// 绝不留在 authorized 上。
func TestAuthFreezeFailureMarksRowFailed(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	sp := &stubAuth{name: "stubauth-fail", caps: Capabilities{Auth: true},
		freezeErr: response.New(http.StatusBadGateway, errChannelError).WithMsg("stubauth-fail: declined")}
	RegisterProvider(sp)

	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("c-fail", sp.name, `{"k":"v"}`))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE biz_type").WillReturnRows(emptyAuthRows())
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_pay_auth`").WillReturnResult(sqlmock.NewResult(2002, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_auth`").
		WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	a, err := s.CreateAuth(tenantCtx, CreateAuthInput{
		BizType: "preauth", BizNo: "PA-F", ChannelCode: "c-fail", AmountMinor: 100, Currency: "CNY", PayerID: "b",
	})
	if err == nil || a != nil {
		t.Fatalf("freeze failure must return error and no auth: a=%v err=%v", a, err)
	}
	if sp.freezeCalls != 1 {
		t.Fatalf("Freeze calls = %d, want 1", sp.freezeCalls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 预授权残留补查：停在 authorizing 的占位行只能问渠道。渠道确认过 → 回填 authorized；
// 渠道明确查无此单 → 落 failed（钱没冻住，业务可换单重试）；拿不到结论 → 原样留着。
func TestReconcileStuckAuths(t *testing.T) {
	cases := []struct {
		name     string
		queryRes *AuthResult
		queryErr error
		want     string // adopted / failed / unresolved
	}{
		{"渠道确认", &AuthResult{ChannelAuthNo: "chan_real", Status: "AUTHORIZED"}, nil, "adopted"},
		{"渠道查无", nil, response.NotFound(errNotFound), "failed"},
		{"渠道仍在处理", &AuthResult{Status: "processing"}, nil, "unresolved"},
		{"渠道报错", nil, response.New(http.StatusBadGateway, errChannelError), "unresolved"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, m := mockDB(t)
			lg := &countingLogger{}
			s := NewService(db, lg)
			sp := &stubAuth{name: "stubauth-recon-" + c.want, caps: Capabilities{Auth: true},
				queryRes: c.queryRes, queryErr: c.queryErr}
			RegisterProvider(sp)

			rows := authRow(AuthAuthorizing, 1000, 0, sp.name, "")
			m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE status").WillReturnRows(rows)
			m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
				WillReturnRows(channelRow(sp.name, sp.name, `{"k":"v"}`))
			if c.want != "unresolved" {
				m.ExpectBegin()
				m.ExpectExec("UPDATE `plg_pay_auth`").WillReturnResult(sqlmock.NewResult(1, 1))
				m.ExpectCommit()
			}

			got, err := s.ReconcileStuckAuths(tenantCtx, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if got.Checked != 1 {
				t.Fatalf("checked = %d, want 1（残留必须被清点数）", got.Checked)
			}
			switch c.want {
			case "adopted":
				if got.Adopted != 1 {
					t.Fatalf("adopted = %d, want 1", got.Adopted)
				}
			case "failed":
				if got.Failed != 1 {
					t.Fatalf("failed = %d, want 1", got.Failed)
				}
			default:
				if got.Unresolved != 1 {
					t.Fatalf("unresolved = %d, want 1", got.Unresolved)
				}
			}
			if sp.queryCalls != 1 {
				t.Fatalf("QueryAuth calls = %d, want 1", sp.queryCalls)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// 补查撞车：两个实例同时收敛同一张 authorizing。渠道查询多打一次是幂等的，但汇总只许
// 数真的写入者——条件更新影响 0 行时必须落 Skipped，否则一轮扫完 adopted/failed 翻倍，
// 运维据此判断「补偿救回了几笔」就是错的（真库那一侧由 itCrossAuthReconcile 钉住）。
func TestReconcileStuckAuthsCountsOnlyWriters(t *testing.T) {
	cases := []struct {
		tag      string
		queryRes *AuthResult
		queryErr error
		what     string
	}{
		{"adopt", &AuthResult{ChannelAuthNo: "chan_late", Status: "AUTHORIZED"}, nil, "渠道确认"},
		{"miss", nil, response.NotFound(errNotFound), "渠道查无"},
	}
	for _, c := range cases {
		t.Run(c.what+"但行已被别人落定", func(t *testing.T) {
			db, m := mockDB(t)
			s := NewService(db, nopLog())
			sp := &stubAuth{name: "stubauth-skip-" + c.tag, caps: Capabilities{Auth: true},
				queryRes: c.queryRes, queryErr: c.queryErr}
			RegisterProvider(sp)

			m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE status").
				WillReturnRows(authRow(AuthAuthorizing, 1000, 0, sp.name, ""))
			m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
				WillReturnRows(channelRow(sp.name, sp.name, `{"k":"v"}`))
			m.ExpectBegin()
			// affected 0 = WHERE status='authorizing' 一行都不匹配，本轮没有写进去。
			m.ExpectExec("UPDATE `plg_pay_auth`").WillReturnResult(sqlmock.NewResult(1, 0))
			m.ExpectCommit()

			got, err := s.ReconcileStuckAuths(tenantCtx, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if got.Checked != 1 {
				t.Fatalf("checked = %d, want 1", got.Checked)
			}
			if got.Adopted != 0 || got.Failed != 0 {
				t.Fatalf("adopted=%d failed=%d, want 0/0（本轮没写就不算救回）：%+v", got.Adopted, got.Failed, got)
			}
			if got.Skipped != 1 {
				t.Fatalf("skipped = %d, want 1", got.Skipped)
			}
			if got.Adopted+got.Failed+got.Unresolved+got.Skipped != got.Checked {
				t.Fatalf("分桶加起来 %d != checked %d —— 有笔没归属",
					got.Adopted+got.Failed+got.Unresolved+got.Skipped, got.Checked)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// 幂等：同 bizType+bizNo 已有授权单先返回，不重复冻结。
func TestAuthIdempotentExisting(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	sp := &stubAuth{name: "stubauth-idem", caps: Capabilities{Auth: true},
		freezeRes: &AuthResult{ChannelAuthNo: "chan", Status: "success"}}
	RegisterProvider(sp)

	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("auth-idem", sp.name, `{"k":"v"}`))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_auth` WHERE biz_type").
		WillReturnRows(authRow(AuthAuthorized, 1000, 0, "auth-idem", "chan_pre"))

	a, err := s.CreateAuth(tenantCtx, CreateAuthInput{
		BizType: "preauth", BizNo: "PA-1", ChannelCode: "auth-idem", AmountMinor: 1000, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ChannelAuthNo != "chan_pre" {
		t.Fatalf("must return existing auth, got %q", a.ChannelAuthNo)
	}
	if sp.freezeCalls != 0 {
		t.Fatalf("existing auth must not re-freeze, calls = %d", sp.freezeCalls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
