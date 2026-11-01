package pay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
)

// 渠道账单驱动对账与渠道代付打款（b3887ad 渠道能力底座的两个消费方）：
//   - RunChannelReconcileTask：用 BillProvider.FetchBill 下载的渠道账单替代
//     人工录入的渠道金额（source=channel），退款流水一并纳入比对；
//   - PayPayoutViaChannel：用 PayoutProvider.Transfer 走渠道代付，替代
//     人工回执号（MarkPayoutPaid 路径保持可用）。
// 两者都只依赖 Provider 可选能力接口，不依赖任何具体渠道实现。

// RunChannelReconcileTask 下载渠道账单并对账（source=channel）。与人工路径的
// 三点差异：
//  1. 渠道金额来自 BillProvider.FetchBill，不接受调用方传入金额（铁律①）；
//  2. 退款流水进比对：账单 kind=refund 行与本地成功退款单按 RefundNo 对，
//     差异行写 ReconcileDiff.RefundNo；
//  3. 任务落 BillDigest / BillRows 留痕，并严格区分三种情况：
//     账单里没有这笔 → 正常单边差异行；下载/解析失败 → 任务落 FailReason 并
//     返回错误（不回退 manual、不出「对平」结论）；解析出 0 行 → 无法与解析器
//     缺陷区分，同样显式失败留痕，绝不静默当对平。
//
// 失败时返回 (带 FailReason 的任务, 错误)：调用方可展示留痕，也可以只取错误；
// 渠道能力缺口（Capabilities.Bill=false）返回 (nil, PAY_CAPABILITY_UNSUPPORTED)，
// 此时不写任务（属于配置/签约问题，不是对账结论）。
func (s *Service) RunChannelReconcileTask(ctx context.Context, channelCode, billDate, subjectID string) (*ReconcileTask, error) {
	channelCode, billDate = strings.TrimSpace(channelCode), strings.TrimSpace(billDate)
	subjectID = strings.TrimSpace(subjectID)
	if channelCode == "" || !billDateRe.MatchString(billDate) {
		return nil, response.BadRequest(errInvalidAmount)
	}
	startAt, endAt, err := s.dayBounds(billDate)
	if err != nil {
		return nil, response.BadRequest(errInvalidAmount)
	}
	p, cfg, err := s.channelOf(ctx, channelCode)
	if err != nil {
		return nil, err
	}
	bp, isBill := p.(BillProvider)
	if !isBill || !ProviderCaps(p).Bill {
		return nil, capabilityUnsupported(p.Name(), "渠道账单（source=channel 对账）")
	}

	bill, err := bp.FetchBill(ctx, BillRequest{BillDate: billDate}, cfg)
	if err != nil {
		return s.reconcileBillFailure(ctx, p.Name(), channelCode, billDate, subjectID, 0,
			"渠道账单下载失败: "+truncate(err.Error(), 150))
	}
	if bill == nil {
		return s.reconcileBillFailure(ctx, p.Name(), channelCode, billDate, subjectID, 0,
			"渠道账单返回空响应")
	}
	rows := bill.Rows
	if bill.RowCount != 0 && bill.RowCount != len(rows) {
		return s.reconcileBillFailure(ctx, p.Name(), channelCode, billDate, subjectID, len(rows),
			fmt.Sprintf("渠道账单行数不一致（声明 %d、实际 %d），疑似解析缺陷", bill.RowCount, len(rows)))
	}
	if len(rows) == 0 {
		return s.reconcileBillFailure(ctx, p.Name(), channelCode, billDate, subjectID, 0,
			"渠道账单解析出 0 行：空账单日或解析器缺陷无法区分，拒绝按对平处理")
	}

	channelPays := map[string]int64{}
	channelRefunds := map[string]int64{}
	for _, row := range rows {
		switch row.Kind {
		case BillKindTrade, "":
			channelPays[row.PaymentNo] += row.AmountMinor
		case BillKindRefund:
			channelRefunds[row.RefundNo] += row.AmountMinor
		default:
			return s.reconcileBillFailure(ctx, p.Name(), channelCode, billDate, subjectID, len(rows),
				"渠道账单含未知流水种类: "+truncate(row.Kind, 40))
		}
	}

	localPays, err := s.localReconcilePayments(ctx, channelCode, subjectID, startAt, endAt)
	if err != nil {
		return nil, err
	}
	localRefunds, err := s.localReconcileRefunds(ctx, channelCode, subjectID, startAt, endAt)
	if err != nil {
		return nil, err
	}
	digest := truncate(bill.Digest, 64)
	if digest == "" {
		digest = billRowsDigest(rows)
	}
	return s.applyReconcile(ctx, reconcileRun{
		channelCode: channelCode, billDate: billDate, subjectID: subjectID,
		source: ReconcileSourceChannel, billDigest: digest, billRows: len(rows),
		sections: []reconcileSection{
			{local: localPays, channel: channelPays},
			{isRefund: true, local: localRefunds, channel: channelRefunds},
		},
	})
}

// reconcileBillFailure 把一次「没得出结论」的对账显式落盘：任务复用/新建，
// Source=channel + FailReason 留痕，不产差异行、不刷新成「对平」。
// 返回值同时带任务与错误，便于管理端展示失败留痕。
func (s *Service) reconcileBillFailure(ctx context.Context, provider, channelCode, billDate, subjectID string, rows int, reason string) (*ReconcileTask, error) {
	task, terr := s.applyReconcile(ctx, reconcileRun{
		channelCode: channelCode, billDate: billDate, subjectID: subjectID,
		source: ReconcileSourceChannel, billRows: rows, failReason: reason,
	})
	if terr != nil {
		// 连留痕都没落下去（如撞 closed 任务）：以渠道错误为准上抛。
		return nil, channelGatewayError(provider, reason)
	}
	return task, channelGatewayError(provider, reason)
}

// localRefundAgg 是退款比对行的投影（只取比对需要的列）。
type localRefundAgg struct {
	RefundNo    string
	AmountMinor int64
}

// localReconcileRefunds 聚合账期内该渠道的成功退款单：退款按原支付单归属渠道，
// 时间口径用退款成功时间（与账单日的自然日窗口一致）。
func (s *Service) localReconcileRefunds(ctx context.Context, channelCode, subjectID string, startAt, endAt time.Time) (map[string]int64, error) {
	var rows []localRefundAgg
	db := s.db.WithContext(ctx).Model(&Refund{}).
		Select("plg_pay_refund.refund_no, plg_pay_refund.amount_minor").
		Joins("JOIN plg_pay_order o ON o.payment_no = plg_pay_refund.payment_no").
		Where("o.channel_code = ? AND plg_pay_refund.status = ?", channelCode, RefundSucceeded).
		Where("plg_pay_refund.succeeded_at >= ? AND plg_pay_refund.succeeded_at < ?", startAt, endAt)
	if subjectID != "" {
		db = db.Where("plg_pay_refund.subject_id = ?", subjectID)
	}
	if err := db.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[r.RefundNo] += r.AmountMinor
	}
	return out, nil
}

// billRowsDigest 在渠道没给账单摘要时，按行内容算一份稳定 SHA-256，供审计与复现。
func billRowsDigest(rows []BillRow) string {
	h := sha256.New()
	for _, r := range rows {
		fmt.Fprintf(h, "%s|%s|%s|%d|%d|%s\n", r.Kind, r.PaymentNo, r.RefundNo, r.AmountMinor, r.FeeMinor, r.ChannelTxnID)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ---- 渠道代付打款 ----

// ---- 代付终局补查 ----

// PayoutSweepSummary 是 initiated 笔补查一轮的可数结果。
type PayoutSweepSummary struct {
	Batches    int `json:"batches"`
	Resolved   int `json:"resolved"`
	Unresolved int `json:"unresolved"`
	// Skipped = 渠道终局拿到了，但这一笔已由别的写入者落定（条件更新影响 0 行）。
	// 不单独记就会把「两个 sweeper 补查同一批」数成两倍救回，且败者的相反结论
	// 曾能直接覆写已落定的那一行（见 settleStuckPayoutItem）。
	Skipped int `json:"skipped"`
}

// payoutStaleAfter 是「发起后多久没终局才算滞留」：给渠道正常的异步处理留窗口，
// 避免每分钟把同一批次再查一遍。
const payoutStaleAfter = 10 * time.Minute

// SweepStuckPayouts 补查滞留在 initiated 的代付笔（转账受理了但终局没回来）。
//
// 渠道的查询接口只给批次级结论（TransferResult 是单个 State），所以这里只在
// 批次拿到**明确终局**时才动笔：成功 → 全部置 success；失败/关闭 → 置 failed 并留原因。
// 结论含糊（处理中、未知状态）就原样留着并记一条可数日志，交人工核对——
// 猜一个终局等于把「不知道」写成「有结论」，在出款上不可接受。
func (s *Service) SweepStuckPayouts(ctx context.Context, now time.Time) (PayoutSweepSummary, error) {
	var out PayoutSweepSummary
	var batches []PayoutBatch
	err := s.db.WithContext(tenant.Skip(ctx)).
		Where("status IN ? AND channel_code <> '' AND channel_batch_no <> ?",
			[]string{PayoutDraft, PayoutPartial, PayoutFailed}, "").
		Order("id").Limit(20).Find(&batches).Error
	if err != nil {
		return out, err
	}
	staleBefore := now.Add(-payoutStaleAfter)
	for i := range batches {
		b := &batches[i]
		var stuck []PayoutItem
		if err := s.db.WithContext(tenant.Skip(ctx)).
			Where("batch_id = ? AND status = ? AND updated_at <= ?", int64(b.ID), PayoutItemInitiated, staleBefore).
			Order("id").Find(&stuck).Error; err != nil {
			return out, err
		}
		if len(stuck) == 0 {
			continue
		}
		out.Batches++
		p, cfg, err := s.channelOf(tenant.Skip(ctx), b.ChannelCode)
		if err != nil {
			out.Unresolved += len(stuck)
			s.logWarn("pay: payout sweep channel unresolved", "batch", b.BatchNo, "err", err.Error())
			continue
		}
		pp, isPayout := p.(PayoutProvider)
		if !isPayout || !ProviderCaps(p).Payout {
			out.Unresolved += len(stuck)
			s.logWarn("pay: payout sweep channel no longer supports payout", "batch", b.BatchNo, "provider", p.Name())
			continue
		}
		res, qerr := pp.QueryTransfer(ctx, b.ChannelBatchNo, cfg)
		if qerr != nil || res == nil {
			out.Unresolved += len(stuck)
			s.logWarn("pay: payout sweep query failed", "batch", b.BatchNo, "err", errText(qerr))
			continue
		}
		resolvedHere := 0
		// 两个 sweeper 会各自打一次渠道查询（查询幂等，多打不算错），但终局只许第一个写入者落：
		// 写入带 `status = initiated` 守卫，按真实影响行数分档，败者记 Skipped 不算救回。
		switch {
		case res.Accepted:
			for j := range stuck {
				it := &stuck[j]
				wrote, werr := s.settleStuckPayoutItem(tenant.Skip(ctx), it, PayoutItemSuccess,
					firstNonEmpty(res.ChannelDetailNo, it.ChannelNo), "")
				switch {
				case werr != nil:
					out.Unresolved++
				case wrote:
					out.Resolved++
					resolvedHere++
				default:
					out.Skipped++
				}
			}
		case !res.Pending:
			reason := truncate(firstNonEmpty(res.Reason, res.State, "渠道代付终局为失败"), 200)
			for j := range stuck {
				it := &stuck[j]
				wrote, werr := s.settleStuckPayoutItem(tenant.Skip(ctx), it, PayoutItemFailed, it.ChannelNo, reason)
				switch {
				case werr != nil:
					out.Unresolved++
				case wrote:
					out.Resolved++
					resolvedHere++
				default:
					out.Skipped++
				}
			}
		default:
			out.Unresolved += len(stuck)
		}
		s.logWarn("pay: payout sweep batch conclusion", "batch", b.BatchNo,
			"state", res.State, "accepted", res.Accepted, "pending", res.Pending, "items", len(stuck))
		// 批次结论按全部明细重算：补查把最后的 initiated 笔收敛掉之后，
		// 批次才从 partial/failed 走到 paid，不会永远停在「还有一笔没终局」。
		if resolvedHere > 0 {
			if _, ferr := s.finalizePayoutBatch(tenant.Skip(ctx), b, b.ChannelBatchNo); ferr != nil {
				s.logWarn("pay: payout sweep finalize batch failed", "batch", b.BatchNo, "err", ferr.Error())
			}
		}
	}
	if out.Batches > 0 {
		s.logInfo("pay: stuck payouts swept", "batches", out.Batches,
			"resolved", out.Resolved, "unresolved", out.Unresolved, "skipped", out.Skipped)
	}
	return out, nil
}

// PayPayoutViaChannel 用渠道代付（PayoutProvider.Transfer）支付一个打款批次。
// 与人工回执（MarkPayoutPaid）并存：渠道声明 Payout 能力才可用，否则报
// PAY_CAPABILITY_UNSUPPORTED。资金纪律：
//   - 金额、币种来自批次与结算单（PayoutItem.NetMinor / PayoutBatch.Currency），
//     收款方标识来自结算单主体（Settlement.SubjectID），一律不从请求接受（铁律①）；
//   - 一笔失败不影响其余笔：逐笔独立发起、独立落状态，批次以
//     partial/failed 显式标注，绝不整批回滚也绝不整批标成功；
//   - 重复调用幂等：已成功/进行中的笔由 PayoutItem 状态闸门跳过（条件更新
//     认领），失败笔用确定性 ItemNo（"PI"+明细ID）重试，渠道侧按幂等号去重。
//
// 批次状态机：draft/partial/failed → （重试）→ paid（全部成功）。
// paid 批次重复调用直接返回现状，不再发起任何转账。
func (s *Service) PayPayoutViaChannel(ctx context.Context, batchID int64, channelCode string) (*PayoutBatch, error) {
	channelCode = strings.TrimSpace(channelCode)
	if channelCode == "" {
		return nil, response.BadRequest(errChannelInvalid)
	}
	p, cfg, err := s.channelOf(ctx, channelCode)
	if err != nil {
		return nil, err
	}
	pp, isPayout := p.(PayoutProvider)
	if !isPayout || !ProviderCaps(p).Payout {
		return nil, capabilityUnsupported(p.Name(), "渠道代付（打款转账）")
	}

	var batch PayoutBatch
	var items []PayoutItem
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&batch, batchID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		switch batch.Status {
		case PayoutPaid:
			return nil // 幂等：批次已全量打款，重试原样返回，不再发起任何转账
		case PayoutDraft, PayoutPartial, PayoutFailed:
			// 可发起/可重试状态。
		default:
			return response.Conflict(errStateConflict)
		}
		if batch.ChannelCode != "" && batch.ChannelCode != channelCode {
			// 批次一旦绑定代付渠道就不允许中途换渠道（ItemNo 幂等号只在原渠道有意义）。
			return response.Conflict(errStateConflict)
		}
		if err := tx.Where("batch_id = ?", batchID).Order("id").Find(&items).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return response.NotFound(errNotFound)
		}
		if err := backfillPayoutReceivers(tx, items); err != nil {
			return err
		}
		if batch.ChannelCode == "" {
			if err := tx.Model(&PayoutBatch{}).Where("id = ?", int64(batch.ID)).
				Update("channel_code", channelCode).Error; err != nil {
				return err
			}
			batch.ChannelCode = channelCode
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if batch.Status == PayoutPaid {
		return &batch, nil
	}

	now := time.Now().UTC()
	channelBatchNo := batch.ChannelBatchNo
	for i := range items {
		it := &items[i]
		switch it.Status {
		case PayoutItemSuccess, PayoutItemInitiated:
			continue // 幂等闸门：已成功/已发起待终局的笔绝不二次发起
		case PayoutItemPending, PayoutItemFailed, "":
			// 可发起/可重试。
		default:
			continue
		}
		// 条件更新认领：并发重复调用只有一方能把该行从 pending/failed 推到
		// initiated，认领失败即跳过（sqlite 无 FOR UPDATE，这条闸门需真库 IT 覆盖）。
		claim := s.db.WithContext(ctx).Model(&PayoutItem{}).
			Where("id = ? AND status IN ?", int64(it.ID), []string{PayoutItemPending, PayoutItemFailed}).
			Updates(map[string]any{"status": PayoutItemInitiated, "fail_reason": "", "updated_at": now})
		if claim.Error != nil {
			s.logWarn("pay: payout item claim failed", "item", int64(it.ID), "err", claim.Error.Error())
			continue
		}
		if claim.RowsAffected == 0 {
			continue
		}
		// 前置校验不过的笔直接落 failed，不摸渠道：宁缺毋假。
		if it.NetMinor <= 0 {
			s.finishPayoutItem(ctx, it, PayoutItemFailed, "", "结算净额非正，不能走渠道代付")
			continue
		}
		if it.ReceiverID == "" {
			s.finishPayoutItem(ctx, it, PayoutItemFailed, "", "结算单缺少收款方标识，无法代付")
			continue
		}
		res, terr := pp.Transfer(ctx, TransferRequest{
			BatchNo:      batch.BatchNo,
			ItemNo:       "PI" + formatID(int64(it.ID)), // 确定性幂等号：重试同批同笔，渠道可去重
			AmountMinor:  it.NetMinor,
			Currency:     batch.Currency,
			ReceiverType: "merchant",
			ReceiverID:   it.ReceiverID,
			Remark:       truncate("nexadmin payout "+batch.BatchNo, 100),
		}, cfg)
		switch {
		case terr != nil:
			// 渠道调用失败只标记这一笔，其余笔照常推进。
			s.finishPayoutItem(ctx, it, PayoutItemFailed, "", truncate(terr.Error(), 200))
		case res == nil:
			s.finishPayoutItem(ctx, it, PayoutItemFailed, "", "渠道代付返回空结果")
		case !res.Accepted && !res.Pending:
			s.finishPayoutItem(ctx, it, PayoutItemFailed, "",
				truncate(firstNonEmpty(res.Reason, res.State, "渠道拒绝代付"), 200))
		case res.Pending:
			// 已受理待终局：留在 initiated，由 QueryTransfer 清扫收敛，本次不再触碰。
			s.finishPayoutItem(ctx, it, PayoutItemInitiated,
				firstNonEmpty(res.ChannelBatchNo, res.ChannelDetailNo), "")
			if res.ChannelBatchNo != "" && channelBatchNo == "" {
				channelBatchNo = res.ChannelBatchNo
			}
		default:
			s.finishPayoutItem(ctx, it, PayoutItemSuccess,
				firstNonEmpty(res.ChannelDetailNo, res.ChannelBatchNo), "")
			if res.ChannelBatchNo != "" && channelBatchNo == "" {
				channelBatchNo = res.ChannelBatchNo
			}
		}
	}

	return s.finalizePayoutBatch(ctx, &batch, channelBatchNo)
}

// finishPayoutItem 落一笔代付的终局/进行状态；写库失败只告警——单笔的落库
// 失败不允许挡住其余笔的资金推进，留痕缺漏由清扫与人工核对兜底。
func (s *Service) finishPayoutItem(ctx context.Context, it *PayoutItem, status, channelNo, failReason string) {
	updates := map[string]any{
		"status":      status,
		"channel_no":  truncate(channelNo, 128),
		"fail_reason": truncate(failReason, 200),
	}
	// success 才清失败原因之外的留痕；initiated/failed 保留渠道号可为空。
	if err := s.db.WithContext(ctx).Model(&PayoutItem{}).
		Where("id = ?", int64(it.ID)).Updates(updates).Error; err != nil {
		s.logWarn("pay: payout item update failed", "item", int64(it.ID), "status", status, "err", err.Error())
	}
	it.Status = status
	it.ChannelNo = truncate(channelNo, 128)
	it.FailReason = truncate(failReason, 200)
}

// settleStuckPayoutItem 是代付终局补查专用的落终局写入：只把仍停在 initiated 的那一笔收敛，
// 并报告是否真的写了。与 finishPayoutItem 的区别就是这道 `status = initiated` 守卫——
// 两个 sweeper 可能拿到相反的渠道结论，无守卫写入会让后到的结论覆写已经落定的资金事实。
func (s *Service) settleStuckPayoutItem(ctx context.Context, it *PayoutItem, status, channelNo, failReason string) (bool, error) {
	res := s.db.WithContext(ctx).Model(&PayoutItem{}).
		Where("id = ? AND status = ?", int64(it.ID), PayoutItemInitiated).
		Updates(map[string]any{
			"status":      status,
			"channel_no":  truncate(channelNo, 128),
			"fail_reason": truncate(failReason, 200),
		})
	if res.Error != nil {
		s.logWarn("pay: payout settle update failed", "item", int64(it.ID), "status", status, "err", res.Error.Error())
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	it.Status, it.ChannelNo, it.FailReason = status, truncate(channelNo, 128), truncate(failReason, 200)
	return true, nil
}

// finalizePayoutBatch 按笔的终局聚合批次状态：全部成功才 paid；全失败 failed；
// 其余（部分成功/仍进行中/部分失败）都是 partial，可再次调用本批次重试。
func (s *Service) finalizePayoutBatch(ctx context.Context, batch *PayoutBatch, channelBatchNo string) (*PayoutBatch, error) {
	var final []PayoutItem
	if err := s.db.WithContext(ctx).Where("batch_id = ?", int64(batch.ID)).Order("id").
		Find(&final).Error; err != nil {
		return nil, err
	}
	var total, okN, failN, pendingN int
	firstFail := ""
	for i := range final {
		total++
		switch final[i].Status {
		case PayoutItemSuccess:
			okN++
		case PayoutItemFailed:
			failN++
			if firstFail == "" {
				firstFail = final[i].FailReason
			}
		default:
			pendingN++
		}
	}
	status := PayoutPartial
	switch {
	case total > 0 && okN == total:
		status = PayoutPaid
	case total > 0 && failN == total:
		status = PayoutFailed
	}
	failReason := ""
	switch {
	case failN > 0:
		failReason = fmt.Sprintf("%d 笔失败：%s", failN, firstFail)
	case pendingN > 0 && okN == 0 && failN == 0 && status != PayoutPaid:
		failReason = fmt.Sprintf("%d 笔渠道处理中，待代付结果查询收敛", pendingN)
	}
	failReason = truncate(failReason, 200)

	updates := map[string]any{"status": status, "fail_reason": failReason}
	if channelBatchNo != "" && channelBatchNo != batch.ChannelBatchNo {
		updates["channel_batch_no"] = truncate(channelBatchNo, 128)
	}
	if status == PayoutPaid {
		paidAt := time.Now().UTC()
		updates["paid_at"] = paidAt
		batch.PaidAt = &paidAt
	}
	if err := s.db.WithContext(ctx).Model(&PayoutBatch{}).
		Where("id = ?", int64(batch.ID)).Updates(updates).Error; err != nil {
		return nil, err
	}
	batch.Status, batch.FailReason = status, failReason
	if channelBatchNo != "" {
		batch.ChannelBatchNo = truncate(channelBatchNo, 128)
	}
	return batch, nil
}

// backfillPayoutReceivers 给缺收款方标识的批次明细回填结算单主体
// （ReceiverID 的唯一合法来源是结算单，绝不经请求）。仍补不齐的笔留空，
// 由发起阶段落 failed。必须在持 tx 的事务内调用。
func backfillPayoutReceivers(tx *gorm.DB, items []PayoutItem) error {
	var missing []int64
	for i := range items {
		if items[i].ReceiverID == "" {
			missing = append(missing, int64(items[i].SettlementID))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	var sts []Settlement
	if err := tx.Where("id IN ?", missing).Find(&sts).Error; err != nil {
		return err
	}
	byID := make(map[int64]string, len(sts))
	for i := range sts {
		byID[int64(sts[i].ID)] = sts[i].SubjectID
	}
	for i := range items {
		it := &items[i]
		if it.ReceiverID != "" {
			continue
		}
		subject := byID[int64(it.SettlementID)]
		if subject == "" {
			continue
		}
		it.ReceiverID = subject
		if err := tx.Model(&PayoutItem{}).Where("id = ?", int64(it.ID)).
			Update("receiver_id", subject).Error; err != nil {
			return err
		}
	}
	return nil
}

// ListPayoutItems 列出批次明细（管理端展示代付逐笔状态用）。
func (s *Service) ListPayoutItems(ctx context.Context, batchID int64) ([]PayoutItem, error) {
	var rows []PayoutItem
	err := s.db.WithContext(ctx).Where("batch_id = ?", batchID).Order("id").Find(&rows).Error
	return rows, err
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
