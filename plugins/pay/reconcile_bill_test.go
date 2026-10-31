package pay

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"time"
)

// 账单驱动对账（RunChannelReconcileTask）与渠道代付（PayPayoutViaChannel）的
// 服务层测试。夹具说明：仓库依赖树里没有 sqlite 驱动（go.mod 只有 mysql/
// postgres + go-sqlmock），因此沿用包内既有做法——sqlmock + gorm 回调捕获
// 落库对象，逐条 SQL 设防：多写一条（比如失败路径偷偷比对/落差异行）会直接
// 报「unmatched SQL」让测试变红。

// ---- stub providers ----

type billStub struct {
	name       string
	caps       Capabilities
	bill       *Bill
	err        error
	fetchCalls int
	lastReq    BillRequest
}

func (b *billStub) Name() string { return b.name }

func (b *billStub) Create(context.Context, PayRequest, map[string]string) (*PayParams, error) {
	return nil, errors.New("billStub: Create unused")
}

func (b *billStub) Query(context.Context, string, map[string]string) (*ChannelStatus, error) {
	return &ChannelStatus{}, nil
}

func (b *billStub) Refund(context.Context, RefundRequest, map[string]string) (*RefundResult, error) {
	return nil, errors.New("billStub: Refund unused")
}

func (b *billStub) VerifyCallback(CallbackInput, map[string]string) (*CallbackResult, error) {
	return nil, errors.New("billStub: VerifyCallback unused")
}

func (b *billStub) Capabilities() Capabilities { return b.caps }

func (b *billStub) FetchBill(_ context.Context, in BillRequest, _ map[string]string) (*Bill, error) {
	b.fetchCalls++
	b.lastReq = in
	if b.err != nil {
		return nil, b.err
	}
	return b.bill, nil
}

type payoutStub struct {
	name       string
	caps       Capabilities
	transfer   func(req TransferRequest) (*TransferResult, error)
	queryRes   func(batchNo string) (*TransferResult, error)
	calls      []TransferRequest
	queryCalls int
}

func (p *payoutStub) Name() string { return p.name }

func (p *payoutStub) Create(context.Context, PayRequest, map[string]string) (*PayParams, error) {
	return nil, errors.New("payoutStub: Create unused")
}

func (p *payoutStub) Query(context.Context, string, map[string]string) (*ChannelStatus, error) {
	return &ChannelStatus{}, nil
}

func (p *payoutStub) Refund(context.Context, RefundRequest, map[string]string) (*RefundResult, error) {
	return nil, errors.New("payoutStub: Refund unused")
}

func (p *payoutStub) VerifyCallback(CallbackInput, map[string]string) (*CallbackResult, error) {
	return nil, errors.New("payoutStub: VerifyCallback unused")
}

func (p *payoutStub) Capabilities() Capabilities { return p.caps }

func (p *payoutStub) Transfer(_ context.Context, req TransferRequest, _ map[string]string) (*TransferResult, error) {
	p.calls = append(p.calls, req)
	if p.transfer != nil {
		return p.transfer(req)
	}
	return &TransferResult{Accepted: true, ChannelBatchNo: "CB-1", ChannelDetailNo: "CH-" + req.ItemNo}, nil
}

func (p *payoutStub) QueryTransfer(_ context.Context, batchNo string, _ map[string]string) (*TransferResult, error) {
	p.queryCalls++
	if p.queryRes != nil {
		return p.queryRes(batchNo)
	}
	return &TransferResult{Accepted: true, State: "SUCCESS", ChannelBatchNo: batchNo}, nil
}

func (p *payoutStub) callsFor(itemNo string) int {
	n := 0
	for _, c := range p.calls {
		if c.ItemNo == itemNo {
			n++
		}
	}
	return n
}

// ---- 捕获落库对象 ----

type capturedUpdates struct {
	recs []capturedUpdate
}

type capturedUpdate struct {
	table string
	dest  map[string]any
	vars  []any
}

// captureWrites 记录经 gorm 真正提交的 INSERT（差异行/任务）与 UPDATE
// （批次与明细的状态写入），断言不依赖 SQL 文本细节。
func captureWrites(db *gorm.DB, diffs *[]ReconcileDiff, ups *capturedUpdates) {
	db.Callback().Create().After("gorm:create").Register("test:captureCreate", func(d *gorm.DB) {
		if diff, ok := d.Statement.Dest.(*ReconcileDiff); ok {
			*diffs = append(*diffs, *diff)
		}
	})
	db.Callback().Update().After("gorm:update").Register("test:captureUpdate", func(d *gorm.DB) {
		m, ok := d.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		cp := make(map[string]any, len(m))
		for k, v := range m {
			cp[k] = v
		}
		vars := make([]any, len(d.Statement.Vars))
		copy(vars, d.Statement.Vars)
		ups.recs = append(ups.recs, capturedUpdate{table: d.Statement.Table, dest: cp, vars: vars})
	})
}

// updatesFor 返回对某一行（按主键）的全部 UPDATE 写入。
func (c *capturedUpdates) updatesFor(table string, id int64) []capturedUpdate {
	var out []capturedUpdate
	for _, r := range c.recs {
		if r.table != table {
			continue
		}
		for _, v := range r.vars {
			if iv, ok := v.(int64); ok && iv == id {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// finalStatus 取某明细最后一次状态写入。
func (c *capturedUpdates) finalStatus(table string, id int64, key string) (string, bool) {
	var last string
	found := false
	for _, r := range c.updatesFor(table, id) {
		if v, ok := r.dest[key]; ok {
			if sv, is := v.(string); is {
				last, found = sv, true
			}
		}
	}
	return last, found
}

// ---- sqlmock 行夹具 ----

func reconOrderRows(specs ...[2]any) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "payment_no", "amount_minor"})
	for i, sp := range specs {
		rows.AddRow(int64(100+i), sp[0], sp[1])
	}
	return rows
}

func reconRefundRows(specs ...[2]any) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"refund_no", "amount_minor"})
	for _, sp := range specs {
		rows.AddRow(sp[0], sp[1])
	}
	return rows
}

var reconTaskCols = []string{
	"id", "tenant_id", "subject_id", "channel_code", "bill_date", "status",
	"total_local_minor", "total_channel_minor", "diff_count", "source",
	"bill_digest", "bill_rows", "fail_reason",
}

func reconTaskEmptyRows() *sqlmock.Rows { return sqlmock.NewRows(reconTaskCols) }

// expectDiffInserts 逐条声明差异行 INSERT（sqlmock 1.5 无 Times，重复注册即可）。
func expectDiffInserts(m sqlmock.Sqlmock, n int) {
	for i := 0; i < n; i++ {
		m.ExpectExec("INSERT INTO `plg_pay_reconcile_diff`").WillReturnResult(sqlmock.NewResult(1, 1))
	}
}

// expectItemWrite / expectBatchWrite：gorm 对顶层写操作默认包一层隐式事务。
func expectItemWrite(m sqlmock.Sqlmock) {
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_payout_item` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
}

func expectBatchWrite(m sqlmock.Sqlmock) {
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_payout_batch` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
}

func reconTaskRow(id int64, status, source string) *sqlmock.Rows {
	return sqlmock.NewRows(reconTaskCols).AddRow(
		id, 7, "subj", "billchan", "2026-10-30", status, 0, 0, 0, source, "", 0, "")
}

func payoutBatchRow(id int64, status, channelCode string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "batch_no", "status", "total_minor", "currency",
		"settlement_count", "method", "note", "receipt_no",
		"channel_code", "channel_batch_no", "fail_reason", "paid_at",
	}).AddRow(id, 7, "PO900", status, 700, "CNY", 2, "", "", "",
		channelCode, "", "", nil)
}

func payoutItemRow(id, batchID, settlementID, net int64, receiver, status, channelNo, failReason string) []any {
	return []any{id, 7, batchID, settlementID, net, receiver, status, channelNo, failReason}
}

func payoutItemRows(rows ...[]any) *sqlmock.Rows {
	r := sqlmock.NewRows([]string{
		"id", "tenant_id", "batch_id", "settlement_id", "net_minor",
		"receiver_id", "status", "channel_no", "fail_reason",
	})
	for _, row := range rows {
		cols := make([]driver.Value, len(row))
		for i, v := range row {
			cols[i] = v
		}
		r.AddRow(cols...)
	}
	return r
}

func settlementSubjectRow(id int64, subjectID string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "tenant_id", "subject_id", "net_minor", "currency", "status"}).
		AddRow(id, 7, subjectID, 500, "CNY", SettlementConfirmed)
}

const reconBillDate = "2026-10-30"

// ---- A. 账单驱动对账 ----

// TestBillChannelReconcileDiffs 长款/短款/单边缺失分类与 digest/rows 落库；
// DiffAmountMinor = local - channel 的符号口径必须与人工路径一致。
func TestBillChannelReconcileDiffs(t *testing.T) {
	stub := &billStub{name: "billstubdiff", caps: Capabilities{Bill: true}, bill: &Bill{
		Digest:   "digest-abc",
		RowCount: 4,
		Rows: []BillRow{
			{Kind: BillKindTrade, PaymentNo: "PAY1", AmountMinor: 600},
			{Kind: BillKindTrade, PaymentNo: "PAY2", AmountMinor: 300}, // 对平
			{Kind: BillKindTrade, PaymentNo: "PAY4", AmountMinor: 350},
			{Kind: BillKindTrade, PaymentNo: "PAY9", AmountMinor: 90}, // 渠道单边
		},
	}}
	RegisterProvider(stub)

	db, m := mockDB(t)
	var diffs []ReconcileDiff
	var ups capturedUpdates
	captureWrites(db, &diffs, &ups)
	s := NewService(db, nopLog()).WithTimezone("UTC")

	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("billchan", stub.name, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE \\(channel_code").
		WillReturnRows(reconOrderRows([2]any{"PAY1", 500}, [2]any{"PAY2", 300}, [2]any{"PAY4", 400}, [2]any{"PAY5", 100}))
	m.ExpectQuery("FROM `plg_pay_refund` JOIN").WillReturnRows(reconRefundRows())
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_reconcile_task` WHERE channel_code").
		WillReturnRows(reconTaskEmptyRows())
	m.ExpectExec("INSERT INTO `plg_pay_reconcile_task`").WillReturnResult(sqlmock.NewResult(1, 1))
	expectDiffInserts(m, 4)
	m.ExpectExec("UPDATE `plg_pay_reconcile_task` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	task, err := s.RunChannelReconcileTask(tenantCtx, "billchan", reconBillDate, "")
	if err != nil {
		t.Fatalf("channel reconcile failed: %v", err)
	}
	if stub.fetchCalls != 1 || stub.lastReq.BillDate != reconBillDate {
		t.Fatalf("FetchBill calls=%d req=%+v", stub.fetchCalls, stub.lastReq)
	}
	if task.Source != ReconcileSourceChannel || task.BillDigest != "digest-abc" || task.BillRows != 4 {
		t.Fatalf("task provenance wrong: %+v", task)
	}
	if task.FailReason != "" || task.DiffCount != 4 {
		t.Fatalf("task conclusion wrong: %+v", task)
	}
	if task.TotalLocalMinor != 1300 || task.TotalChannelMinor != 1340 {
		t.Fatalf("totals wrong: %+v", task)
	}

	byNo := map[string]ReconcileDiff{}
	for _, d := range diffs {
		byNo[d.PaymentNo] = d
	}
	if len(byNo) != 4 {
		t.Fatalf("diff rows %d: %+v", len(diffs), diffs)
	}
	// 符号口径钉死：DiffAmountMinor = local - channel。
	checks := []struct {
		no, kind string
		diff     int64
	}{
		{"PAY1", DiffLong, -100},   // 渠道 600 > 本地 500
		{"PAY4", DiffShort, 50},    // 渠道 350 < 本地 400
		{"PAY5", DiffMissing, 100}, // 渠道无流水（账单真的没有这笔）
		{"PAY9", DiffMissing, -90}, // 本地无单的渠道流水
	}
	for _, c := range checks {
		d, ok := byNo[c.no]
		if !ok {
			t.Fatalf("missing diff row for %s", c.no)
		}
		if d.Kind != c.kind || d.DiffAmountMinor != c.diff {
			t.Fatalf("%s: kind=%s diff=%d, want kind=%s diff=%d", d.PaymentNo, d.Kind, d.DiffAmountMinor, c.kind, c.diff)
		}
	}
	// 对平行不产差异。
	if _, ok := byNo["PAY2"]; ok {
		t.Fatal("PAY2 equal amounts must not produce a diff")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestBillChannelReconcileDownloadError 账单下载失败：必须报错并留 FailReason，
// 不得回退成 manual、不得落「全对得上」的假任务（没有比对、没有差异行）。
func TestBillChannelReconcileDownloadError(t *testing.T) {
	stub := &billStub{name: "billstuberr", caps: Capabilities{Bill: true}, err: errors.New("gateway timeout")}
	RegisterProvider(stub)

	db, m := mockDB(t)
	var diffs []ReconcileDiff
	var ups capturedUpdates
	captureWrites(db, &diffs, &ups)
	s := NewService(db, nopLog()).WithTimezone("UTC")

	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("billchan", stub.name, ""))
	// 失败留痕任务：只有 task 相关写入，绝不允许出现 order/refund 比对查询。
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_reconcile_task` WHERE channel_code").
		WillReturnRows(reconTaskEmptyRows())
	m.ExpectExec("INSERT INTO `plg_pay_reconcile_task`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("UPDATE `plg_pay_reconcile_task` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	task, err := s.RunChannelReconcileTask(tenantCtx, "billchan", reconBillDate, "")
	if err == nil {
		t.Fatal("download failure must return error")
	}
	var re *response.Error
	if !errors.As(err, &re) || re.Key != errChannelError {
		t.Fatalf("failure must surface as PAY_CHANNEL_ERROR, got %v", err)
	}
	if stub.fetchCalls != 1 {
		t.Fatalf("expected exactly one FetchBill call, got %d", stub.fetchCalls)
	}
	// 「不落假对平任务」：留痕任务必须带 FailReason，且 source=channel、无差异行。
	if task == nil {
		t.Fatal("failure should still be recorded on the task for audit")
	}
	if task.Source != ReconcileSourceChannel || task.FailReason == "" || task.DiffCount != 0 {
		t.Fatalf("failure audit wrong: %+v", task)
	}
	if len(diffs) != 0 {
		t.Fatalf("no diffs may be written on download failure, got %d", len(diffs))
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestBillChannelReconcileEmptyBill 账单解析出 0 行：与「对平」可区分的显式失败
// （任务落 FailReason、返回错误），绝不静默当成对平。
func TestBillChannelReconcileEmptyBill(t *testing.T) {
	stub := &billStub{name: "billstubempty", caps: Capabilities{Bill: true},
		bill: &Bill{Digest: "digest-0", Rows: nil, RowCount: 0}}
	RegisterProvider(stub)

	db, m := mockDB(t)
	var diffs []ReconcileDiff
	var ups capturedUpdates
	captureWrites(db, &diffs, &ups)
	s := NewService(db, nopLog()).WithTimezone("UTC")

	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("billchan", stub.name, ""))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_reconcile_task` WHERE channel_code").
		WillReturnRows(reconTaskEmptyRows())
	m.ExpectExec("INSERT INTO `plg_pay_reconcile_task`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("UPDATE `plg_pay_reconcile_task` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	task, err := s.RunChannelReconcileTask(tenantCtx, "billchan", reconBillDate, "")
	if err == nil {
		t.Fatal("0-row bill must fail explicitly, not be treated as matched")
	}
	if task == nil || task.FailReason == "" {
		t.Fatalf("0-row bill must be recorded with FailReason: %+v", task)
	}
	if task.Source != ReconcileSourceChannel || task.BillRows != 0 || task.DiffCount != 0 {
		t.Fatalf("0-row audit wrong: %+v", task)
	}
	if !strings.Contains(task.FailReason, "0 行") {
		t.Fatalf("FailReason should explain zero rows, got %q", task.FailReason)
	}
	if len(diffs) != 0 {
		t.Fatalf("no diffs expected, got %d", len(diffs))
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestBillChannelReconcileRefundRows 退款流水进对账：本地已退款但渠道无退款
// 流水（或金额不符）→ 差异行写 RefundNo；渠道单边退款同样可见。
func TestBillChannelReconcileRefundRows(t *testing.T) {
	stub := &billStub{name: "billstubrefund", caps: Capabilities{Bill: true}, bill: &Bill{
		Digest:   "digest-r",
		RowCount: 4,
		Rows: []BillRow{
			{Kind: BillKindTrade, PaymentNo: "PAY0", AmountMinor: 500}, // 对平支付
			{Kind: BillKindRefund, RefundNo: "RF1", AmountMinor: 200},  // 对平退款
			{Kind: BillKindRefund, RefundNo: "RF2", AmountMinor: 250},  // 渠道少退 50
			{Kind: BillKindRefund, RefundNo: "RF9", AmountMinor: 100},  // 渠道单边退款
		},
	}}
	RegisterProvider(stub)

	db, m := mockDB(t)
	var diffs []ReconcileDiff
	var ups capturedUpdates
	captureWrites(db, &diffs, &ups)
	s := NewService(db, nopLog()).WithTimezone("UTC")

	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("billchan", stub.name, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE \\(channel_code").
		WillReturnRows(reconOrderRows([2]any{"PAY0", 500}))
	m.ExpectQuery("FROM `plg_pay_refund` JOIN").
		WillReturnRows(reconRefundRows([2]any{"RF1", 200}, [2]any{"RF2", 300}, [2]any{"RF3", 100}))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_reconcile_task` WHERE channel_code").
		WillReturnRows(reconTaskEmptyRows())
	m.ExpectExec("INSERT INTO `plg_pay_reconcile_task`").WillReturnResult(sqlmock.NewResult(1, 1))
	expectDiffInserts(m, 3)
	m.ExpectExec("UPDATE `plg_pay_reconcile_task` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	task, err := s.RunChannelReconcileTask(tenantCtx, "billchan", reconBillDate, "")
	if err != nil {
		t.Fatalf("channel reconcile failed: %v", err)
	}
	if task.DiffCount != 3 {
		t.Fatalf("want 3 diffs, task=%+v diffs=%+v", task, diffs)
	}
	byRefund := map[string]ReconcileDiff{}
	for _, d := range diffs {
		if d.RefundNo != "" {
			byRefund[d.RefundNo] = d
		} else if d.PaymentNo != "" || d.RefundNo == "" {
			t.Fatalf("refund-section diff must carry RefundNo: %+v", d)
		}
	}
	// 本地已退款但渠道无退款流水 → 差异行带 RefundNo。
	rf3, ok := byRefund["RF3"]
	if !ok || rf3.Kind != DiffMissing || rf3.DiffAmountMinor != 100 {
		t.Fatalf("RF3 (local refund missing in bill) wrong: %+v", rf3)
	}
	if rf2 := byRefund["RF2"]; rf2.Kind != DiffShort || rf2.DiffAmountMinor != 50 {
		t.Fatalf("RF2 refund short wrong: %+v", rf2)
	}
	if rf9 := byRefund["RF9"]; rf9.Kind != DiffMissing || rf9.DiffAmountMinor != -100 {
		t.Fatalf("RF9 channel-only refund wrong: %+v", rf9)
	}
	if _, ok := byRefund["RF1"]; ok {
		t.Fatal("matched refund must not produce a diff")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestBillChannelReconcileCapabilityUnsupported 渠道未声明 Bill 能力：报
// PAY_CAPABILITY_UNSUPPORTED 且不下载账单（删掉 caps 判定本条必须红）。
func TestBillChannelReconcileCapabilityUnsupported(t *testing.T) {
	stub := &billStub{name: "billstubcaps", caps: Capabilities{Bill: false},
		bill: &Bill{Rows: []BillRow{{Kind: BillKindTrade, PaymentNo: "PAY1", AmountMinor: 1}}}}
	RegisterProvider(stub)

	db, m := mockDB(t)
	s := NewService(db, nopLog()).WithTimezone("UTC")
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("billchan", stub.name, ""))

	task, err := s.RunChannelReconcileTask(tenantCtx, "billchan", reconBillDate, "")
	if err == nil {
		t.Fatal("bill-less channel must fail")
	}
	var re *response.Error
	if !errors.As(err, &re) || re.Key != errCapabilityUnsupported {
		t.Fatalf("want PAY_CAPABILITY_UNSUPPORTED, got %v", err)
	}
	if task != nil {
		t.Fatalf("no task may be written for a capability gap: %+v", task)
	}
	if stub.fetchCalls != 0 {
		t.Fatalf("capability gate must run before FetchBill, calls=%d", stub.fetchCalls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestReconcileManualUnchanged 回归：人工录入路径（source=manual）的 SQL 形态、
// 分类口径与落库列不因本次改造改变（不查退款、不落账单留痕列）。
func TestReconcileManualUnchanged(t *testing.T) {
	db, m := mockDB(t)
	var diffs []ReconcileDiff
	var ups capturedUpdates
	captureWrites(db, &diffs, &ups)
	s := NewService(db, nopLog()).WithTimezone("UTC")

	m.ExpectQuery("SELECT \\* FROM `plg_pay_order` WHERE \\(channel_code").
		WillReturnRows(reconOrderRows([2]any{"PAY1", 500}, [2]any{"PAY2", 300}))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_reconcile_task` WHERE channel_code").
		WillReturnRows(reconTaskEmptyRows())
	m.ExpectExec("INSERT INTO `plg_pay_reconcile_task`").WillReturnResult(sqlmock.NewResult(1, 1))
	expectDiffInserts(m, 3)
	m.ExpectExec("UPDATE `plg_pay_reconcile_task` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	task, err := s.RunReconcileTask(tenantCtx, "mock", reconBillDate, "", []ChannelAmountEntry{
		{PaymentNo: "PAY1", AmountMinor: 600},
		{PaymentNo: "PAY3", AmountMinor: 100},
	})
	if err != nil {
		t.Fatalf("manual reconcile failed: %v", err)
	}
	if task.Source != ReconcileSourceManual || task.BillDigest != "" || task.BillRows != 0 || task.FailReason != "" {
		t.Fatalf("manual task shape changed: %+v", task)
	}
	if task.DiffCount != 3 || task.TotalLocalMinor != 800 || task.TotalChannelMinor != 700 {
		t.Fatalf("manual totals wrong: %+v", task)
	}
	// 没有退款查询期望：若手动路径多查了退款表，sqlmock 会直接报错。
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// ---- B. 渠道代付 ----

func TestPayoutViaChannelAllSuccess(t *testing.T) {
	stub := &payoutStub{name: "payoutstuball", caps: Capabilities{Payout: true}}
	RegisterProvider(stub)

	db, m := mockDB(t)
	var diffs []ReconcileDiff
	var ups capturedUpdates
	captureWrites(db, &diffs, &ups)
	s := NewService(db, nopLog())

	// 阶段一：锁批次 → 取明细 → 从结算单回填收款方标识 → 绑定代付渠道。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("paychan", stub.name, ""))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_batch`").WillReturnRows(payoutBatchRow(900, PayoutDraft, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_item` WHERE batch_id").
		WillReturnRows(payoutItemRows(
			payoutItemRow(9001, 900, 8001, 500, "", PayoutItemPending, "", ""),
			payoutItemRow(9002, 900, 8002, 200, "subjB", PayoutItemPending, "", ""),
		))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_settlement` WHERE id IN").
		WillReturnRows(settlementSubjectRow(8001, "subjA"))
	m.ExpectExec("UPDATE `plg_pay_payout_item` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE `plg_pay_payout_batch` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	// 阶段二：逐笔认领 → 渠道转账 → 逐笔落终局（每步都是独立语句）。
	expectItemWrite(m) // claim 9001
	expectItemWrite(m) // 9001 success
	expectItemWrite(m) // claim 9002
	expectItemWrite(m) // 9002 success
	// 阶段三：按笔聚合批次终局。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_item` WHERE batch_id").
		WillReturnRows(payoutItemRows(
			payoutItemRow(9001, 900, 8001, 500, "subjA", PayoutItemSuccess, "CH-PI9001", ""),
			payoutItemRow(9002, 900, 8002, 200, "subjB", PayoutItemSuccess, "CH-PI9002", ""),
		))
	expectBatchWrite(m)

	batch, err := s.PayPayoutViaChannel(tenantCtx, 900, "paychan")
	if err != nil {
		t.Fatalf("payout via channel failed: %v", err)
	}
	if batch.Status != PayoutPaid || batch.PaidAt == nil {
		t.Fatalf("all-success batch must be paid: %+v", batch)
	}
	if batch.ChannelCode != "paychan" || batch.ChannelBatchNo != "CB-1" {
		t.Fatalf("batch channel provenance wrong: %+v", batch)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("want 2 transfers, got %d", len(stub.calls))
	}
	// 金额/币种/收款方一律来自批次与结算单（铁律①）：9001 的 ReceiverID
	// 是从结算单 8001 回填的 subjA，不是请求参数。
	if stub.calls[0].ItemNo != "PI9001" || stub.calls[0].AmountMinor != 500 ||
		stub.calls[0].Currency != "CNY" || stub.calls[0].ReceiverID != "subjA" {
		t.Fatalf("transfer[0] wrong: %+v", stub.calls[0])
	}
	if stub.calls[1].ItemNo != "PI9002" || stub.calls[1].AmountMinor != 200 || stub.calls[1].ReceiverID != "subjB" {
		t.Fatalf("transfer[1] wrong: %+v", stub.calls[1])
	}
	if got, ok := ups.finalStatus("plg_pay_payout_item", 9001, "status"); !ok || got != PayoutItemSuccess {
		t.Fatalf("item 9001 final status %q ok=%v", got, ok)
	}
	if got, ok := ups.finalStatus("plg_pay_payout_item", 9002, "status"); !ok || got != PayoutItemSuccess {
		t.Fatalf("item 9002 final status %q ok=%v", got, ok)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestPayoutViaChannelOneFailureKeepsOthers 资金纪律：一笔失败不影响其余笔，
// 不许整批回滚成一笔没打，也不许全部标成功。
func TestPayoutViaChannelOneFailureKeepsOthers(t *testing.T) {
	stub := &payoutStub{name: "payoutstubfail", caps: Capabilities{Payout: true},
		transfer: func(req TransferRequest) (*TransferResult, error) {
			if req.ItemNo == "PI9002" {
				return &TransferResult{Accepted: false, Reason: "渠道余额不足"}, nil
			}
			return &TransferResult{Accepted: true, ChannelBatchNo: "CB-1", ChannelDetailNo: "CH-" + req.ItemNo}, nil
		}}
	RegisterProvider(stub)

	db, m := mockDB(t)
	var diffs []ReconcileDiff
	var ups capturedUpdates
	captureWrites(db, &diffs, &ups)
	s := NewService(db, nopLog())

	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("paychan", stub.name, ""))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_batch`").WillReturnRows(payoutBatchRow(900, PayoutDraft, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_item` WHERE batch_id").
		WillReturnRows(payoutItemRows(
			payoutItemRow(9001, 900, 8001, 500, "subjA", PayoutItemPending, "", ""),
			payoutItemRow(9002, 900, 8002, 200, "subjB", PayoutItemPending, "", ""),
		))
	m.ExpectExec("UPDATE `plg_pay_payout_batch` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	expectItemWrite(m) // claim 9001
	expectItemWrite(m) // 9001 success
	expectItemWrite(m) // claim 9002
	expectItemWrite(m) // 9002 failed（渠道拒绝）
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_item` WHERE batch_id").
		WillReturnRows(payoutItemRows(
			payoutItemRow(9001, 900, 8001, 500, "subjA", PayoutItemSuccess, "CH-PI9001", ""),
			payoutItemRow(9002, 900, 8002, 200, "subjB", PayoutItemFailed, "", "渠道余额不足"),
		))
	expectBatchWrite(m)

	batch, err := s.PayPayoutViaChannel(tenantCtx, 900, "paychan")
	if err != nil {
		t.Fatalf("partial failure must not error out the whole call: %v", err)
	}
	if batch.Status != PayoutPartial {
		t.Fatalf("mixed batch must be partial: %+v", batch)
	}
	if batch.PaidAt != nil || !strings.Contains(batch.FailReason, "渠道余额不足") {
		t.Fatalf("batch failure audit wrong: %+v", batch)
	}
	if got, _ := ups.finalStatus("plg_pay_payout_item", 9001, "status"); got != PayoutItemSuccess {
		t.Fatalf("succeeded item must stay success, got %q", got)
	}
	if got, _ := ups.finalStatus("plg_pay_payout_item", 9002, "status"); got != PayoutItemFailed {
		t.Fatalf("rejected item must be failed, got %q", got)
	}
	if got, _ := ups.finalStatus("plg_pay_payout_item", 9002, "fail_reason"); got != "渠道余额不足" {
		t.Fatalf("rejected item fail reason lost: %q", got)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("both items must be attempted, got %d calls", len(stub.calls))
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestPayoutViaChannelIdempotentRetry 重复调用同一批次：已成功/进行中的笔不再
// 二次发起；失败笔用同一 ItemNo 重试；批次全成功后重复调用是纯读操作。
func TestPayoutViaChannelIdempotentRetry(t *testing.T) {
	rejectOnce := true
	stub := &payoutStub{name: "payoutstubidem", caps: Capabilities{Payout: true},
		transfer: func(req TransferRequest) (*TransferResult, error) {
			if req.ItemNo == "PI9002" && rejectOnce {
				rejectOnce = false
				return &TransferResult{Accepted: false, Reason: "临时拒绝"}, nil
			}
			return &TransferResult{Accepted: true, ChannelBatchNo: "CB-1", ChannelDetailNo: "CH-" + req.ItemNo}, nil
		}}
	RegisterProvider(stub)

	db, m := mockDB(t)
	var diffs []ReconcileDiff
	var ups capturedUpdates
	captureWrites(db, &diffs, &ups)
	s := NewService(db, nopLog())

	itemsPending := func() *sqlmock.Rows {
		return payoutItemRows(
			payoutItemRow(9001, 900, 8001, 500, "subjA", PayoutItemPending, "", ""),
			payoutItemRow(9002, 900, 8002, 200, "subjB", PayoutItemPending, "", ""),
		)
	}
	itemsMixed := func() *sqlmock.Rows {
		return payoutItemRows(
			payoutItemRow(9001, 900, 8001, 500, "subjA", PayoutItemSuccess, "CH-PI9001", ""),
			payoutItemRow(9002, 900, 8002, 200, "subjB", PayoutItemFailed, "", "临时拒绝"),
		)
	}
	itemsAllSuccess := func() *sqlmock.Rows {
		return payoutItemRows(
			payoutItemRow(9001, 900, 8001, 500, "subjA", PayoutItemSuccess, "CH-PI9001", ""),
			payoutItemRow(9002, 900, 8002, 200, "subjB", PayoutItemSuccess, "CH-PI9002", ""),
		)
	}

	// 第一次调用：9001 成功、9002 被拒 → partial。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("paychan", stub.name, ""))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_batch`").WillReturnRows(payoutBatchRow(900, PayoutDraft, ""))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_item` WHERE batch_id").WillReturnRows(itemsPending())
	m.ExpectExec("UPDATE `plg_pay_payout_batch` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	expectItemWrite(m) // claim 9001
	expectItemWrite(m) // 9001 success
	expectItemWrite(m) // claim 9002
	expectItemWrite(m) // 9002 failed
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_item` WHERE batch_id").WillReturnRows(itemsMixed())
	expectBatchWrite(m)

	if _, err := s.PayPayoutViaChannel(tenantCtx, 900, "paychan"); err != nil {
		t.Fatalf("first payout call failed: %v", err)
	}

	// 第二次调用（重试）：只有 failed 的 9002 再发起，成功的 9001 被闸门跳过。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("paychan", stub.name, ""))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_batch`").WillReturnRows(payoutBatchRow(900, PayoutPartial, "paychan"))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_item` WHERE batch_id").WillReturnRows(itemsMixed())
	m.ExpectCommit()
	expectItemWrite(m) // 认领 9002
	expectItemWrite(m) // 9002 success
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_item` WHERE batch_id").WillReturnRows(itemsAllSuccess())
	expectBatchWrite(m)

	batch, err := s.PayPayoutViaChannel(tenantCtx, 900, "paychan")
	if err != nil {
		t.Fatalf("retry payout failed: %v", err)
	}
	if batch.Status != PayoutPaid || batch.PaidAt == nil {
		t.Fatalf("batch should be paid after retry: %+v", batch)
	}

	// 第三次调用：批次已 paid，纯读返回，不再有任何转账。
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("paychan", stub.name, ""))
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_pay_payout_batch`").WillReturnRows(payoutBatchRow(900, PayoutPaid, "paychan"))
	m.ExpectCommit()
	again, err := s.PayPayoutViaChannel(tenantCtx, 900, "paychan")
	if err != nil {
		t.Fatalf("call on paid batch must be an idempotent no-op, got %v", err)
	}
	if again.Status != PayoutPaid {
		t.Fatalf("no-op returned wrong batch: %+v", again)
	}

	if stub.callsFor("PI9001") != 1 {
		t.Fatalf("successful item must never be re-transferred, calls=%d", stub.callsFor("PI9001"))
	}
	if stub.callsFor("PI9002") != 2 {
		t.Fatalf("failed item should be retried once with same ItemNo, calls=%d", stub.callsFor("PI9002"))
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestPayoutViaChannelCapabilityUnsupported 渠道未声明 Payout 能力：报
// PAY_CAPABILITY_UNSUPPORTED，不动批次与明细（既有人工回执路径 MarkPayoutPaid
// 仍可走）。删掉 caps 判定本条必须红：断言 Transfer 一次都没被调用。
func TestPayoutViaChannelCapabilityUnsupported(t *testing.T) {
	stub := &payoutStub{name: "payoutstubcaps", caps: Capabilities{Payout: false}}
	RegisterProvider(stub)

	db, m := mockDB(t)
	s := NewService(db, nopLog())
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code = \\?").
		WillReturnRows(channelRow("paychan", stub.name, ""))

	batch, err := s.PayPayoutViaChannel(tenantCtx, 900, "paychan")
	if err == nil {
		t.Fatal("payout-less channel must fail")
	}
	var re *response.Error
	if !errors.As(err, &re) || re.Key != errCapabilityUnsupported {
		t.Fatalf("want PAY_CAPABILITY_UNSUPPORTED, got %v", err)
	}
	if batch != nil {
		t.Fatalf("no batch state may change on capability gap: %+v", batch)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("capability gate must run before Transfer, calls=%d", len(stub.calls))
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// ---- 代付终局补查（SweepStuckPayouts）----

// 渠道给成功终局：滞留在 initiated 的笔全部落 success，批次随之重算为 paid。
func TestPayoutSweepResolvesSuccess(t *testing.T) {
	stub := &payoutStub{name: "payoutsweepok", caps: Capabilities{Payout: true}}
	RegisterProvider(stub)
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	m.ExpectQuery("plg_pay_payout_batch").
		WillReturnRows(payoutSweepBatchRow(910, PayoutPartial, "sweepchan", "CB-910"))
	m.ExpectQuery("plg_pay_payout_item").
		WillReturnRows(payoutItemRows(
			payoutItemRow(9101, 910, 8101, 500, "subjA", PayoutItemInitiated, "CH-1", ""),
			payoutItemRow(9102, 910, 8102, 200, "subjB", PayoutItemInitiated, "CH-2", ""),
		))
	m.ExpectQuery("plg_pay_channel").
		WillReturnRows(channelRow("sweepchan", stub.name, ""))
	expectItemWrite(m) // 9101 success
	expectItemWrite(m) // 9102 success
	// finalizePayoutBatch 重算全部明细 → 批次 paid
	m.ExpectQuery("plg_pay_payout_item").
		WillReturnRows(payoutItemRows(
			payoutItemRow(9101, 910, 8101, 500, "subjA", PayoutItemSuccess, "CH-1", ""),
			payoutItemRow(9102, 910, 8102, 200, "subjB", PayoutItemSuccess, "CH-2", ""),
		))
	expectBatchWrite(m)

	got, err := s.SweepStuckPayouts(tenantCtx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got.Batches != 1 || got.Resolved != 2 || got.Unresolved != 0 {
		t.Fatalf("summary = %+v, want batches=1 resolved=2 unresolved=0", got)
	}
	if stub.queryCalls != 1 {
		t.Fatalf("QueryTransfer calls = %d, want 1（按批次查一次，不是按笔查）", stub.queryCalls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 渠道仍在处理：一笔都不许动——把「不知道」写成「失败」或「成功」都是伪造资金事实。
func TestPayoutSweepKeepsPendingItems(t *testing.T) {
	stub := &payoutStub{name: "payoutsweeppend", caps: Capabilities{Payout: true},
		queryRes: func(b string) (*TransferResult, error) {
			return &TransferResult{Pending: true, State: "PROCESSING", ChannelBatchNo: b}, nil
		}}
	RegisterProvider(stub)
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	m.ExpectQuery("plg_pay_payout_batch").
		WillReturnRows(payoutSweepBatchRow(911, PayoutPartial, "sweepchan", "CB-911"))
	m.ExpectQuery("plg_pay_payout_item").
		WillReturnRows(payoutItemRows(payoutItemRow(9111, 911, 8111, 500, "subjA", PayoutItemInitiated, "CH-1", "")))
	m.ExpectQuery("plg_pay_channel").
		WillReturnRows(channelRow("sweepchan", stub.name, ""))

	got, err := s.SweepStuckPayouts(tenantCtx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got.Resolved != 0 || got.Unresolved != 1 {
		t.Fatalf("summary = %+v, want resolved=0 unresolved=1", got)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 渠道给失败终局：滞留笔落 failed 并留原因，批次重算为 failed（可重试）。
func TestPayoutSweepResolvesFailure(t *testing.T) {
	stub := &payoutStub{name: "payoutsweepfail", caps: Capabilities{Payout: true},
		queryRes: func(b string) (*TransferResult, error) {
			return &TransferResult{Accepted: false, State: "FAIL", Reason: "收款账户已注销", ChannelBatchNo: b}, nil
		}}
	RegisterProvider(stub)
	db, m := mockDB(t)
	s := NewService(db, nopLog())

	m.ExpectQuery("plg_pay_payout_batch").
		WillReturnRows(payoutSweepBatchRow(912, PayoutPartial, "sweepchan", "CB-912"))
	m.ExpectQuery("plg_pay_payout_item").
		WillReturnRows(payoutItemRows(payoutItemRow(9121, 912, 8121, 500, "subjA", PayoutItemInitiated, "CH-1", "")))
	m.ExpectQuery("plg_pay_channel").
		WillReturnRows(channelRow("sweepchan", stub.name, ""))
	expectItemWrite(m)
	m.ExpectQuery("plg_pay_payout_item").
		WillReturnRows(payoutItemRows(payoutItemRow(9121, 912, 8121, 500, "subjA", PayoutItemFailed, "CH-1", "收款账户已注销")))
	expectBatchWrite(m)

	got, err := s.SweepStuckPayouts(tenantCtx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got.Resolved != 1 {
		t.Fatalf("resolved = %d, want 1", got.Resolved)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 渠道不支持代付（能力被关掉或配置改了）：补查不许猜，原样留着并留痕。
func TestPayoutSweepChannelWithoutCapability(t *testing.T) {
	stub := &payoutStub{name: "payoutsweepnocap", caps: Capabilities{Payout: false}}
	RegisterProvider(stub)
	db, m := mockDB(t)
	lg := &countingLogger{}
	s := NewService(db, lg)

	m.ExpectQuery("plg_pay_payout_batch").
		WillReturnRows(payoutSweepBatchRow(913, PayoutPartial, "sweepchan", "CB-913"))
	m.ExpectQuery("plg_pay_payout_item").
		WillReturnRows(payoutItemRows(payoutItemRow(9131, 913, 8131, 500, "subjA", PayoutItemInitiated, "CH-1", "")))
	m.ExpectQuery("plg_pay_channel").
		WillReturnRows(channelRow("sweepchan", stub.name, ""))

	got, err := s.SweepStuckPayouts(tenantCtx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got.Unresolved != 1 || got.Resolved != 0 {
		t.Fatalf("summary = %+v, want unresolved=1", got)
	}
	if stub.queryCalls != 0 {
		t.Fatalf("能力缺口时不该打渠道，queryCalls=%d", stub.queryCalls)
	}
	if lg.warns == 0 {
		t.Fatal("能力缺口必须留一条可数告警")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// payoutSweepRow 是带渠道批次号的批次行（补查只处理已经拿到渠道批次号的笔）。
func payoutSweepBatchRow(id int64, status, channelCode, channelBatchNo string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "tenant_id", "batch_no", "status", "total_minor", "currency",
		"settlement_count", "method", "note", "receipt_no",
		"channel_code", "channel_batch_no", "fail_reason", "paid_at",
	}).AddRow(id, 7, "PO"+formatID(id), status, 700, "CNY", 1, "", "", "",
		channelCode, channelBatchNo, "", nil)
}
