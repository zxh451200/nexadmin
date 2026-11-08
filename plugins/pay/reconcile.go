package pay

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 对账（源自 nexHotel biz_reconcile_task/diff 骨架，hotel_id 泛化为 subject_id）、
// 结算与打款（源自 nexMall biz_settlement/biz_payout，维度泛化为「主体 + 业务类型」）。
// 渠道金额来源两条路：人工录入（source=manual，本文件）与渠道账单驱动
// （source=channel，见 bill.go RunChannelReconcileTask）；打款可走渠道代付
// （bill.go PayPayoutViaChannel）或人工回执（MarkPayoutPaid）。

// 对账任务与差异状态。
const (
	ReconcileOpen   = "open"
	ReconcileClosed = "closed"
	DiffLong        = "long"
	DiffShort       = "short"
	DiffMissing     = "missing"
	DiffOpen        = "open"
	DiffResolved    = "resolved"
	DiffIgnored     = "ignored"

	ReconcileSourceManual  = "manual"
	ReconcileSourceChannel = "channel"
)

// 结算与打款状态。
const (
	SettlementDraft     = "draft"
	SettlementConfirmed = "confirmed"
	SettlementPaid      = "paid"
	PayoutDraft         = "draft"
	PayoutPaid          = "paid"
	// PayoutPartial / PayoutFailed 只在渠道代付（PayPayoutViaChannel）路径出现：
	// 一笔失败不拖垮整批，批次用这两个状态把「部分/全部未成功」摆到台面上供重试。
	PayoutPartial = "partial"
	PayoutFailed  = "failed"
	// PayoutItem 状态闸门：pending 未发起；initiated 已发起待渠道终局（不许重复发起，
	// 由 QueryTransfer 清扫收敛）；success / failed 终局可辨（failed 可用同 ItemNo 重试）。
	PayoutItemPending   = "pending"
	PayoutItemInitiated = "initiated"
	PayoutItemSuccess   = "success"
	PayoutItemFailed    = "failed"
)

// billDateRe 账期/账单日期：YYYY-MM-DD。
var billDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// dayBounds 把业务自然日换算成 UTC 查询边界：应用时区的当天 00:00 至次日 00:00。
// 数据库时间列存 UTC；直接用 "date 00:00:00" 比会漏掉东八区 0–8 点支付的单。
func (s *Service) dayBounds(date string) (time.Time, time.Time, error) {
	loc := s.loc
	if loc == nil {
		loc = time.UTC
	}
	start, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start.UTC(), start.AddDate(0, 0, 1).UTC(), nil
}

// ChannelAmountEntry 是对账入参里的一行渠道金额（临时人工录入通道）。
type ChannelAmountEntry struct {
	PaymentNo   string
	AmountMinor int64
}

// reconcileSection 是对账比对的一个段：键 = 单据号（支付段为 PaymentNo、
// 退款段为 RefundNo），值 = 归并后的金额。isRefund 决定差异行写
// ReconcileDiff.PaymentNo 还是 RefundNo。
type reconcileSection struct {
	isRefund bool
	local    map[string]int64
	channel  map[string]int64
}

// reconcileRun 是一次对账结论的完整输入。failReason 非空表示「这次对账没能
// 得出结论」（账单下载失败、解析 0 行等）：任务照落但只做留痕，不比对、
// 不出「对平」结论——绝不把没拿到的账单当成对平。
type reconcileRun struct {
	channelCode string
	billDate    string
	subjectID   string
	source      string
	billDigest  string
	billRows    int
	failReason  string
	sections    []reconcileSection
}

// RunReconcileTask 生成（或复用）一张对账任务：本地按 billDate + 渠道聚合
// 已成功支付单，与渠道金额（人工录入，source=manual）逐笔比对，产出
// long/short/missing 差异行。口径不变：DiffAmountMinor = local - channel。
func (s *Service) RunReconcileTask(ctx context.Context, channelCode, billDate, subjectID string, entries []ChannelAmountEntry) (*ReconcileTask, error) {
	channelCode, billDate = strings.TrimSpace(channelCode), strings.TrimSpace(billDate)
	subjectID = strings.TrimSpace(subjectID)
	if channelCode == "" || !billDateRe.MatchString(billDate) {
		return nil, response.BadRequest(errInvalidAmount)
	}
	startAt, endAt, err := s.dayBounds(billDate)
	if err != nil {
		return nil, response.BadRequest(errInvalidAmount)
	}
	local, err := s.localReconcilePayments(ctx, channelCode, subjectID, startAt, endAt)
	if err != nil {
		return nil, err
	}
	channel := map[string]int64{}
	for _, e := range entries {
		no := strings.TrimSpace(e.PaymentNo)
		if no != "" {
			channel[no] += e.AmountMinor
		}
	}
	return s.applyReconcile(ctx, reconcileRun{
		channelCode: channelCode, billDate: billDate, subjectID: subjectID,
		source:   ReconcileSourceManual,
		sections: []reconcileSection{{local: local, channel: channel}},
	})
}

// localReconcilePayments 聚合账期内该渠道（可限定主体）已成功支付单的渠道金额输入。
func (s *Service) localReconcilePayments(ctx context.Context, channelCode, subjectID string, startAt, endAt time.Time) (map[string]int64, error) {
	db := s.db.WithContext(ctx).Model(&Order{}).
		Where("channel_code = ? AND status IN ?", channelCode,
			[]string{StatusSucceeded, StatusPartiallyRefunded, StatusRefunded}).
		Where("paid_at >= ? AND paid_at < ?", startAt, endAt)
	if subjectID != "" {
		db = db.Where("subject_id = ?", subjectID)
	}
	var rows []Order
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	local := map[string]int64{}
	for i := range rows {
		local[rows[i].PaymentNo] += rows[i].AmountMinor
	}
	return local, nil
}

// applyReconcile 落任务并对账：同键已有 open 任务复用（重跑刷新差异与来源），
// closed 任务撞同键报冲突。差异分类口径唯一：
// 双边金额不等 → 渠道多为 long、渠道少为 short；单边缺失 → missing；
// DiffAmountMinor = local - channel。
func (s *Service) applyReconcile(ctx context.Context, in reconcileRun) (*ReconcileTask, error) {
	var task ReconcileTask
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 同键已有 open 任务则复用（重跑刷新差异）；closed 任务撞同键报冲突。
		err := tx.Where("channel_code = ? AND bill_date = ? AND subject_id = ?", in.channelCode, in.billDate, in.subjectID).
			Take(&task).Error
		if err == nil {
			if task.Status != ReconcileOpen {
				return response.Conflict(errStateConflict)
			}
			if err := tx.Delete(&ReconcileDiff{}, "task_id = ? AND status = ?", int64(task.ID), DiffOpen).Error; err != nil {
				return err
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			id, ierr := idgen.Next()
			if ierr != nil {
				return ierr
			}
			task = ReconcileTask{
				ChannelCode: in.channelCode, BillDate: in.billDate, SubjectID: in.subjectID,
				Status: ReconcileOpen, Source: in.source,
			}
			task.ID = id
			if err := tx.Create(&task).Error; err != nil {
				return err
			}
		} else {
			return err
		}

		totalLocal, totalChannel, diffCount := int64(0), int64(0), 0
		if in.failReason == "" {
			for _, sec := range in.sections {
				l, c, n, err := writeReconcileDiffs(tx, int64(task.ID), task.TenantID, sec)
				if err != nil {
					return err
				}
				totalLocal += l
				totalChannel += c
				diffCount += n
			}
		}
		task.TotalLocalMinor, task.TotalChannelMinor, task.DiffCount = totalLocal, totalChannel, diffCount
		task.Source, task.BillDigest, task.BillRows = in.source, truncate(in.billDigest, 64), in.billRows
		task.FailReason = truncate(in.failReason, 200)
		return tx.Model(&ReconcileTask{}).Where("id = ?", int64(task.ID)).Updates(map[string]any{
			"total_local_minor": totalLocal, "total_channel_minor": totalChannel, "diff_count": diffCount,
			"source": task.Source, "bill_digest": task.BillDigest, "bill_rows": task.BillRows,
			"fail_reason": task.FailReason,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// writeReconcileDiffs 比对一段（支付或退款）双边金额并落差异行，返回双边合计与差异数。
func writeReconcileDiffs(tx *gorm.DB, taskID int64, tenantID idgen.ID, sec reconcileSection) (int64, int64, int, error) {
	keys := make([]string, 0, len(sec.local)+len(sec.channel))
	for no := range sec.local {
		keys = append(keys, no)
	}
	for no := range sec.channel {
		if _, ok := sec.local[no]; !ok {
			keys = append(keys, no)
		}
	}
	sort.Strings(keys)
	totalLocal, totalChannel, diffCount := int64(0), int64(0), 0
	for _, no := range keys {
		l, hasLocal := sec.local[no]
		c, hasChannel := sec.channel[no]
		totalLocal += l
		totalChannel += c
		if hasLocal && hasChannel && l == c {
			continue
		}
		kind := DiffMissing
		if hasLocal && hasChannel {
			if c > l {
				kind = DiffLong
			} else {
				kind = DiffShort
			}
		}
		id, ierr := idgen.Next()
		if ierr != nil {
			return totalLocal, totalChannel, diffCount, ierr
		}
		diff := ReconcileDiff{
			TaskID: taskID, Kind: kind,
			LocalAmountMinor: l, ChannelAmountMinor: c, DiffAmountMinor: l - c,
			Status: DiffOpen,
		}
		if sec.isRefund {
			diff.RefundNo = truncate(no, 40)
		} else {
			diff.PaymentNo = truncate(no, 40)
		}
		diff.ID = id
		diff.TenantID = tenantID
		if err := tx.Create(&diff).Error; err != nil {
			return totalLocal, totalChannel, diffCount, err
		}
		diffCount++
	}
	return totalLocal, totalChannel, diffCount, nil
}

// ResolveDiffInput 是差异处置入参。
type ResolveDiffInput struct {
	Action string // resolved | ignored
	Note   string
	By     string
}

// ResolveDiff 处置一条差异；任务下差异全部处置完时任务自动 closed。
func (s *Service) ResolveDiff(ctx context.Context, diffID int64, in ResolveDiffInput) (*ReconcileDiff, error) {
	if in.Action != DiffResolved && in.Action != DiffIgnored {
		return nil, response.BadRequest(errInvalidAmount)
	}
	var diff ReconcileDiff
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&diff, diffID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		if diff.Status != DiffOpen {
			return response.Conflict(errStateConflict)
		}
		now := time.Now().UTC()
		diff.Status, diff.ResolveNote, diff.ResolvedBy, diff.ResolvedAt =
			in.Action, truncate(in.Note, 500), truncate(in.By, 64), &now
		if err := tx.Model(&ReconcileDiff{}).Where("id = ?", int64(diff.ID)).Updates(map[string]any{
			"status": diff.Status, "resolve_note": diff.ResolveNote,
			"resolved_by": diff.ResolvedBy, "resolved_at": now,
		}).Error; err != nil {
			return err
		}
		// 任务下还有 open 差异则保持 open，否则自动 closed。
		var open int64
		if err := tx.Model(&ReconcileDiff{}).Where("task_id = ? AND status = ?", diff.TaskID, DiffOpen).Count(&open).Error; err != nil {
			return err
		}
		status := ReconcileClosed
		if open > 0 {
			status = ReconcileOpen
		}
		return tx.Model(&ReconcileTask{}).Where("id = ?", diff.TaskID).Update("status", status).Error
	})
	if err != nil {
		return nil, err
	}
	return &diff, nil
}

// ListReconcileTasks 列出对账任务。
func (s *Service) ListReconcileTasks(ctx context.Context, billDate, channelCode, status string) ([]ReconcileTask, error) {
	db := s.db.WithContext(ctx).Model(&ReconcileTask{})
	if billDate != "" {
		db = db.Where("bill_date = ?", billDate)
	}
	if channelCode != "" {
		db = db.Where("channel_code = ?", channelCode)
	}
	if status != "" {
		db = db.Where("status = ?", status)
	}
	var rows []ReconcileTask
	err := db.Order("id DESC").Limit(200).Find(&rows).Error
	return rows, err
}

// ListReconcileDiffs 列出某任务下的差异行。
func (s *Service) ListReconcileDiffs(ctx context.Context, taskID int64) ([]ReconcileDiff, error) {
	var rows []ReconcileDiff
	err := s.db.WithContext(ctx).Where("task_id = ?", taskID).Order("id").Find(&rows).Error
	return rows, err
}

// UpsertCommissionRule 按「主体 + 业务类型」维护抽佣费率（缺省规则 subjectId 为空）。
func (s *Service) UpsertCommissionRule(ctx context.Context, in CommissionRule) (*CommissionRule, error) {
	in.BizType = strings.TrimSpace(in.BizType)
	in.SubjectID = strings.TrimSpace(in.SubjectID)
	if in.BizType == "" || in.RateBp < 0 || in.RateBp > 9999 || in.FixedMinor < 0 {
		return nil, response.BadRequest(errInvalidAmount)
	}
	if in.Currency != "" {
		in.Currency = strings.ToUpper(in.Currency)
	} else {
		in.Currency = "CNY"
	}
	if in.Status == "" {
		in.Status = RuleActive
	}
	if in.Status != RuleActive && in.Status != RuleDisabled {
		return nil, response.BadRequest(errInvalidAmount)
	}
	var out CommissionRule
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Where("subject_id = ? AND biz_type = ?", in.SubjectID, in.BizType).Take(&out).Error
		if err == nil {
			out.RateBp, out.FixedMinor, out.Currency, out.Status = in.RateBp, in.FixedMinor, in.Currency, in.Status
			return tx.Model(&CommissionRule{}).Where("id = ?", int64(out.ID)).Updates(map[string]any{
				"rate_bp": in.RateBp, "fixed_minor": in.FixedMinor, "currency": in.Currency, "status": in.Status,
			}).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		id, ierr := idgen.Next()
		if ierr != nil {
			return ierr
		}
		out = in
		out.ID = id
		return tx.Create(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCommissionRules 列出抽佣规则。
func (s *Service) ListCommissionRules(ctx context.Context) ([]CommissionRule, error) {
	var rows []CommissionRule
	err := s.db.WithContext(ctx).Order("id").Find(&rows).Error
	return rows, err
}

// settlementAgg 是一个 (主体, 币种) 的账期汇总。
type settlementAgg struct {
	gross      int64
	refund     int64
	fee        int64
	commission int64
	count      int64
}

// GenerateSettlement 汇总一段账期内某主体（空 = 全部主体）的已成功收款与退款，
// 按币种出结算草稿。佣金按单累加：MulBp(amount, rateBp) + fixedMinor（half-even），
// net = gross - commission - refund - fee，可为负（后发退款进下期由下期汇总扣除）。
func (s *Service) GenerateSettlement(ctx context.Context, subjectID, periodStart, periodEnd string) ([]Settlement, error) {
	subjectID = strings.TrimSpace(subjectID)
	if !billDateRe.MatchString(periodStart) || !billDateRe.MatchString(periodEnd) || periodEnd < periodStart {
		return nil, response.BadRequest(errInvalidAmount)
	}
	orders, err := s.settlementOrders(ctx, subjectID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, response.NotFound(errNotFound)
	}

	byKey := map[string]*settlementAgg{}
	var keys []string
	orderByNo := map[string]*Order{}
	ruleCache := map[string]*CommissionRule{}
	for i := range orders {
		o := &orders[i]
		k := o.SubjectID + "|" + o.Currency
		a, ok := byKey[k]
		if !ok {
			a = &settlementAgg{}
			byKey[k] = a
			keys = append(keys, k)
		}
		a.gross += o.AmountMinor
		a.fee += o.FeeMinor
		a.count++
		ck := o.SubjectID + "|" + o.BizType
		rule, cached := ruleCache[ck]
		if !cached {
			rule, err = s.matchRule(s.db, ctx, o.SubjectID, o.BizType)
			if err != nil {
				return nil, err
			}
			ruleCache[ck] = rule
		}
		a.commission += feeOf(o.AmountMinor, rule)
		orderByNo[o.PaymentNo] = o
	}
	refunds, err := s.settlementRefunds(ctx, subjectID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	for i := range refunds {
		r := &refunds[i]
		// 退款归属其原单的主体与币种；原单不在本期时按退款自身口径单列（net 可为负）。
		subj, currency := r.SubjectID, r.Currency
		if o, ok := orderByNo[r.PaymentNo]; ok {
			subj, currency = o.SubjectID, o.Currency
		}
		k := subj + "|" + currency
		a, ok := byKey[k]
		if !ok {
			a = &settlementAgg{}
			byKey[k] = a
			keys = append(keys, k)
		}
		a.refund += r.AmountMinor
	}
	sort.Strings(keys)

	out := make([]Settlement, 0, len(keys))
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, k := range keys {
			a := byKey[k]
			parts := strings.SplitN(k, "|", 2)
			subj, currency := parts[0], parts[1]
			var dup Settlement
			err := tx.Where("subject_id = ? AND period_start = ? AND period_end = ? AND currency = ?",
				subj, periodStart, periodEnd, currency).Take(&dup).Error
			if err == nil {
				return response.Conflict(errStateConflict)
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			id, ierr := idgen.Next()
			if ierr != nil {
				return ierr
			}
			st := Settlement{
				SubjectID: subj, PeriodStart: periodStart, PeriodEnd: periodEnd,
				GrossMinor: a.gross, CommissionMinor: a.commission, RefundMinor: a.refund,
				FeeMinor: a.fee, NetMinor: a.gross - a.commission - a.refund - a.fee,
				OrderCount: a.count, Currency: currency, Status: SettlementDraft,
			}
			st.ID = id
			if err := tx.Create(&st).Error; err != nil {
				return err
			}
			out = append(out, st)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ConfirmSettlement 把草稿结算单置为 confirmed（打款前置状态）。
func (s *Service) ConfirmSettlement(ctx context.Context, id int64) (*Settlement, error) {
	var st Settlement
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&st, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		if st.Status != SettlementDraft {
			return response.Conflict(errStateConflict)
		}
		st.Status = SettlementConfirmed
		return tx.Model(&Settlement{}).Where("id = ?", id).Update("status", st.Status).Error
	})
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// ListSettlements 列出结算单。
func (s *Service) ListSettlements(ctx context.Context, subjectID, status string) ([]Settlement, error) {
	db := s.db.WithContext(ctx).Model(&Settlement{})
	if subjectID != "" {
		db = db.Where("subject_id = ?", subjectID)
	}
	if status != "" {
		db = db.Where("status = ?", status)
	}
	var rows []Settlement
	err := db.Order("id DESC").Limit(200).Find(&rows).Error
	return rows, err
}

// CreatePayoutInput 是打款批次入参。
type CreatePayoutInput struct {
	SettlementIDs []int64
	Method        string
	Note          string
}

// CreatePayout 把已确认的结算单打成一批；唯一索引保证一张结算单只进一个批次。
func (s *Service) CreatePayout(ctx context.Context, in CreatePayoutInput) (*PayoutBatch, error) {
	if len(in.SettlementIDs) == 0 {
		return nil, response.BadRequest(errInvalidAmount)
	}
	var batch PayoutBatch
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settlements []Settlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ?", in.SettlementIDs).Find(&settlements).Error; err != nil {
			return err
		}
		if len(settlements) != len(in.SettlementIDs) {
			return response.NotFound(errNotFound)
		}
		currency := settlements[0].Currency
		total := int64(0)
		for i := range settlements {
			if settlements[i].Status != SettlementConfirmed {
				return response.Conflict(errStateConflict)
			}
			if settlements[i].Currency != currency {
				return response.BadRequest(errCurrencyUnsupported)
			}
			total += settlements[i].NetMinor
		}
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		batch = PayoutBatch{
			BatchNo: "PO" + formatID(int64(id)), Status: PayoutDraft,
			TotalMinor: total, Currency: currency,
			SettlementCount: len(settlements), Method: truncate(in.Method, 32), Note: truncate(in.Note, 500),
		}
		batch.ID = id
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		for i := range settlements {
			item := PayoutItem{
				BatchID: int64(batch.ID), SettlementID: int64(settlements[i].ID),
				NetMinor: settlements[i].NetMinor,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &batch, nil
}

// MarkPayoutPaid 录入人工回执号，把批次置为 paid（渠道自动分账为 Phase 4）。
func (s *Service) MarkPayoutPaid(ctx context.Context, id int64, receiptNo string) (*PayoutBatch, error) {
	var batch PayoutBatch
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&batch, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		if batch.Status != PayoutDraft {
			return response.Conflict(errStateConflict)
		}
		now := time.Now().UTC()
		batch.Status, batch.ReceiptNo, batch.PaidAt = PayoutPaid, truncate(receiptNo, 64), &now
		return tx.Model(&PayoutBatch{}).Where("id = ?", id).Updates(map[string]any{
			"status": batch.Status, "receipt_no": batch.ReceiptNo, "paid_at": now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &batch, nil
}

// ListPayouts 列出打款批次。
func (s *Service) ListPayouts(ctx context.Context, status string) ([]PayoutBatch, error) {
	db := s.db.WithContext(ctx).Model(&PayoutBatch{})
	if status != "" {
		db = db.Where("status = ?", status)
	}
	var rows []PayoutBatch
	err := db.Order("id DESC").Limit(200).Find(&rows).Error
	return rows, err
}

// settlementOrders 取账期内已成功收款单（供结算汇总）。
func (s *Service) settlementOrders(ctx context.Context, subjectID, periodStart, periodEnd string) ([]Order, error) {
	startAt, _, err := s.dayBounds(periodStart)
	if err != nil {
		return nil, err
	}
	_, endAt, err := s.dayBounds(periodEnd)
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx).Model(&Order{}).
		Where("status IN ?", []string{StatusSucceeded, StatusPartiallyRefunded, StatusRefunded}).
		Where("paid_at >= ? AND paid_at < ?", startAt, endAt)
	if subjectID != "" {
		db = db.Where("subject_id = ?", subjectID)
	}
	var rows []Order
	err = db.Order("id").Limit(10000).Find(&rows).Error
	return rows, err
}

// settlementRefunds 取账期内成功的退款单（供结算汇总）。
func (s *Service) settlementRefunds(ctx context.Context, subjectID, periodStart, periodEnd string) ([]Refund, error) {
	startAt, _, err := s.dayBounds(periodStart)
	if err != nil {
		return nil, err
	}
	_, endAt, err := s.dayBounds(periodEnd)
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx).Model(&Refund{}).
		Where("status = ?", RefundSucceeded).
		Where("succeeded_at >= ? AND succeeded_at < ?", startAt, endAt)
	if subjectID != "" {
		db = db.Where("subject_id = ?", subjectID)
	}
	var rows []Refund
	err = db.Order("id").Limit(10000).Find(&rows).Error
	return rows, err
}
