package pay

import (
	"context"
	"errors"
	"strings"
	"time"

	"nexadmin/core/tenant"

	"gorm.io/gorm"

	"nexadmin/core/response"
)

// 退款黑洞补偿查询。
//
// Provider 只有 Refund（异步渠道返回 Pending=true 后只能靠回调翻转），回调一旦丢失
// 退款单就永远停在 pending、支付单卡在 refunding——这就是「退款黑洞」。支付单侧早有
// SyncIfPending 主动补查的范式，本文件把同一条路搬到退款侧：定期主动 QueryRefund，
// 结论复用既有的 transitionRefund（金额核对 / 行锁 / 事件 / 出站箱那一条路），
// 绝不另写一套状态翻转。

// refundSyncThrottle 是同一笔退款两次补查渠道的最小间隔：未到间隔的不再打渠道，
// 避免 sweeper 每一轮都对着查不到的退款狂刷渠道接口。
const refundSyncThrottle = 5 * time.Minute

// RefundSyncSummary 是 SyncStuckRefunds 一轮的可数汇总，供 sweeper 观测与告警。
// Checked 实际打渠道的笔数；Flipped 收敛到终态的笔数；NotFound 查无 / 仍未定
// （保持 pending）的笔数；Failed 渠道或能力层面的失败笔数。
type RefundSyncSummary struct {
	Checked  int `json:"checked"`
	Flipped  int `json:"flipped"`
	NotFound int `json:"notFound"`
	Failed   int `json:"failed"`
	// Skipped = 库里这一笔已终态，但不是本轮翻的（并发回调或另一个补查先落定）。
	// 与 Flipped 分开，一轮扫下来「救回几笔」才等于真的写入数。
	Skipped int `json:"skipped"`
}

// SyncRefund 主动查一笔退款在渠道侧的真实结论并收敛。
func (s *Service) SyncRefund(ctx context.Context, refundNo string) (*Refund, error) {
	upd, _, err := s.syncRefundOnce(ctx, refundNo)
	return upd, err
}

// syncRefundOnce 与 SyncRefund 同一条路，多报一个 changed：这一笔到底是不是本轮翻的。
// sweeper 用它把 flipped 和 skipped 分开——两个补查者（或补查与回调）撞在一起时，
// 只有真的写入者算「救回一笔」，否则指标会翻倍，运维据此判断补偿有效性就是错的。
//
// 幂等：本地退款单已终态直接返回，不打渠道。不支持 RefundQuery 的渠道返回
// capabilityUnsupported（明确失败，绝不静默成功）。渠道返回 Found=false（查无此退款）
// 时保持 pending，递增 SyncAttempts、刷新 LastSyncAt 节流，并留一条可数告警——
// 「查了但没动」必须区别于「没查」，否则运维无从判断补偿是否真的跑过。
func (s *Service) syncRefundOnce(ctx context.Context, refundNo string) (*Refund, bool, error) {
	var r Refund
	if err := s.db.WithContext(ctx).Where("refund_no = ?", strings.TrimSpace(refundNo)).Take(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, response.NotFound(errNotFound)
		}
		return nil, false, err
	}
	// 已终态（succeeded/failed）：幂等返回，不打渠道。
	if r.Status != RefundPending {
		return &r, false, nil
	}

	// 定位原支付单（取渠道与渠道流水），退款结论要落回它上面。
	var o Order
	if err := s.db.WithContext(ctx).Where("payment_no = ?", r.PaymentNo).Take(&o).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, response.NotFound(errNotFound)
		}
		return nil, false, err
	}
	p, cfg, err := s.channelOf(ctx, o.ChannelCode)
	if err != nil {
		return nil, false, err
	}
	qp, canQuery := p.(RefundQueryProvider)
	// 双闸：既要实现 QueryRefund，又要渠道声明 RefundQuery 能力——任一不满足都算不支持。
	if !canQuery || !ProviderCaps(p).RefundQuery {
		return nil, false, capabilityUnsupported(p.Name(), "退款查询")
	}

	// 补偿查询必须带与发起退款时同一个幂等号，渠道才认得出是同一笔。
	// 幂等号由 (paymentNo, bizType, bizNo) 确定性派生，落库列为空时按同式现算即可复现。
	idemKey := r.IdempotencyKey
	if idemKey == "" {
		idemKey = refundIdempotencyKey(o.PaymentNo, r.BizType, r.BizNo)
	}

	res, err := qp.QueryRefund(ctx, RefundQueryRequest{
		RefundNo: r.RefundNo, PaymentNo: o.PaymentNo, ChannelTxnID: txnIDOf(&o),
		IdempotencyKey: idemKey, AmountMinor: r.AmountMinor, OrderAmountMinor: o.AmountMinor,
	}, cfg)
	if err != nil {
		return nil, false, err
	}
	if res == nil {
		return nil, false, response.New(502, errChannelError)
	}

	// 查无此退款 / 渠道仍在处理：保持 pending，节流 + 可数留痕，不动状态机。
	if !res.Found || res.Pending {
		s.recordRefundSyncNoop(ctx, &r, idemKey)
		// no-op 必须留痕：单笔语义即「查了 1 笔、翻了 0 笔」，否则「已跳过」与「没执行」长得一样。
		s.logWarn("pay: refund sync no-op",
			"checked", 1, "flipped", 0, "refundNo", r.RefundNo,
			"found", res.Found, "reason", res.Reason)
		return &r, false, nil
	}

	// 渠道给出确定结论：组装成 CallbackResult 复用 transitionRefund 收敛——
	// 金额核对、行锁、退款成功结算、refunding 还原、事件与出站箱全沿用既有那一条路。
	// AmountMinor 用本地额（渠道查询结论不带金额），让 transitionRefund 的核对成为恒等，
	// 不凭空造一个渠道金额把这笔判成金额不符。
	_, changed, terr := s.transitionRefund(ctx, &CallbackResult{
		PaymentNo:     o.PaymentNo,
		RefundNo:      r.RefundNo,
		RefundOK:      res.OK,
		AmountMinor:   r.AmountMinor,
		ChannelTxnID:  res.ChannelRefundID,
		GatewayStatus: res.Reason,
		Reason:        res.Reason,
	})
	if terr != nil {
		return nil, false, terr
	}
	if !changed {
		// 并发回调/另一个补查已经把这笔翻到终态：本轮没写，但也不能把手里那份 pending
		// 的旧副本当结论交回去——回读一次，调用方才知道钱已经动了。
		var settled Refund
		if serr := s.db.WithContext(ctx).Where("refund_no = ?", r.RefundNo).Take(&settled).Error; serr != nil {
			return &r, false, nil
		}
		return &settled, false, nil
	}
	// 回读权威终态返回（transitionRefund 的失败分支只改库不改内存态）。
	var fresh Refund
	if rerr := s.db.WithContext(ctx).Where("refund_no = ?", r.RefundNo).Take(&fresh).Error; rerr != nil {
		return &r, true, nil
	}
	return &fresh, true, nil
}

// recordRefundSyncNoop 记录一次「查了但没翻」：递增 SyncAttempts、刷新 LastSyncAt 节流，
// 并回填渠道幂等号（历史 pending 单没有这列时补上，后续对账与重查都用它）。
func (s *Service) recordRefundSyncNoop(ctx context.Context, r *Refund, idemKey string) {
	now := time.Now().UTC()
	updates := map[string]any{
		"sync_attempts": r.SyncAttempts + 1,
		"last_sync_at":  now,
	}
	if r.IdempotencyKey == "" && idemKey != "" {
		updates["idempotency_key"] = idemKey
	}
	if err := s.db.WithContext(ctx).Model(&Refund{}).Where("id = ?", int64(r.ID)).Updates(updates).Error; err != nil {
		s.logWarn("pay: refund sync throttle update failed", "refundNo", r.RefundNo, "err", err.Error())
		return
	}
	r.SyncAttempts++
	r.LastSyncAt = &now
	if r.IdempotencyKey == "" {
		r.IdempotencyKey = idemKey
	}
}

// SyncStuckRefunds 扫长期停在 pending 的退款单逐笔向渠道补查收敛（由 sweeper 周期调用）。
// 跨租户扫描走 tenant.Skip；尊重 LastSyncAt 节流，未到间隔的不再打渠道。返回本轮可数汇总。
func (s *Service) SyncStuckRefunds(ctx context.Context, now time.Time, olderThan time.Duration, limit int) (RefundSyncSummary, error) {
	if limit <= 0 {
		limit = 200
	}
	cutoff := now.Add(-olderThan)
	var rows []Refund
	if err := s.db.WithContext(tenant.Skip(ctx)).
		Where("status = ? AND updated_at < ?", RefundPending, cutoff).
		Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return RefundSyncSummary{}, err
	}

	var summary RefundSyncSummary
	for i := range rows {
		row := rows[i]
		// 节流：距上次补查未到间隔的跳过（不打渠道、不计入 Checked）。
		if row.LastSyncAt != nil && now.Sub(row.LastSyncAt.UTC()) < refundSyncThrottle {
			continue
		}
		summary.Checked++
		upd, changed, err := s.syncRefundOnce(ctx, row.RefundNo)
		if err != nil {
			summary.Failed++
			s.logWarn("pay: stuck refund sync failed", "refundNo", row.RefundNo, "err", err.Error())
			continue
		}
		switch {
		case changed:
			summary.Flipped++
		case upd != nil && upd.Status != RefundPending:
			// 本轮没写、库里已终态 = 并发回调或另一个补查先落定，不能再算一次救回。
			summary.Skipped++
		default:
			summary.NotFound++
		}
	}
	// 每轮一条可数日志：没有这行，就分不清「扫了但没有卡住的退款」与「sweeper 没跑」。
	s.logWarn("pay: stuck refund sync pass",
		"checked", summary.Checked, "flipped", summary.Flipped,
		"notFound", summary.NotFound, "failed", summary.Failed, "skipped", summary.Skipped)
	return summary, nil
}
