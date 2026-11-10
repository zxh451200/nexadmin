package pay

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 预授权（源自 nexHotel biz_preauth，泛化 hotel_id → subject_id）。
//
// 资金语义（铁律）：冻结/请款/撤销都以渠道结论为准，「先渠道后本地」——
// 渠道没确认就不能落本地终态。渠道不实现 AuthProvider 或未声明 Auth 能力时
// 直接报 capabilityUnsupported，绝不伪造渠道授权号（旧代码写死 "mockauth_"+random
// 就是伪造，plugin.yaml 把「预授权」当对外能力宣传却全靠假号承载，是资金层面的假成功）。

// authStatusInProgress 是渠道明确「仍在处理中」的状态标记：冻结/请款/撤销遇到它
// 都不算确认，本地不能落终态，调用方据错误重试（渠道幂等号不变，重试安全）。
func authStatusInProgress(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending", "processing", "in_process", "inprocess", "init", "created", "wait", "waiting":
		return true
	default:
		return false
	}
}

// authStatusTerminalOK 报告渠道撤销/请款结论是否确认成功。
// 约定：无 error 且渠道未标 in-progress 即视为已确认（同步渠道语义）；
// Status 为空按成功处理（同步实现常不回填状态串）。
func authStatusTerminalOK(status string) bool {
	return !authStatusInProgress(status)
}

// authChannelOf 按渠道实例码解析渠道配置 + 实现 + 解密凭据。
func (s *Service) authChannelOf(ctx context.Context, code string) (Provider, *Channel, map[string]string, error) {
	ch, err := s.loadChannel(ctx, code)
	if err != nil {
		return nil, nil, nil, err
	}
	p, cfg, err := providerWithConfig(ch)
	if err != nil {
		return nil, nil, nil, err
	}
	return p, ch, cfg, nil
}

// CreateAuthInput 是预授权冻结入参。cardLast4 只允许末四位，禁存卡号。
// PayerID 是渠道侧买家标识（支付宝 buyer_id），真实渠道冻结必需；mock/测试可空。
type CreateAuthInput struct {
	BizType     string
	BizNo       string
	SubjectID   string
	ChannelCode string
	Method      string
	AmountMinor int64
	Currency    string
	FxRateMicro int64
	PayerID     string
	CardLast4   string
	ValidDays   int
	Remark      string
}

// CreateAuth 冻结一笔预授权。
//
// 顺序是「先占位、再冻结、再回填」：先按 (biz_type,biz_no) 唯一索引落一张 authorizing
// 行抢到冻结权，才去打渠道；渠道报错原样上抛并把该行落 failed，渠道受理但未确认
// （含没回授权号）时保持 authorizing 交给 ReconcileStuckAuths 补查——
// 任何情况下都不会凭空造一张 authorized 单，也不会伪造渠道授权号。
// 幂等：同 bizType+bizNo 命中已有单（含并发抢输）直接返回该行，不重复冻结。
func (s *Service) CreateAuth(ctx context.Context, in CreateAuthInput) (*Authorization, error) {
	in.BizType, in.BizNo = strings.TrimSpace(in.BizType), strings.TrimSpace(in.BizNo)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.CardLast4 = strings.TrimSpace(in.CardLast4)
	in.PayerID = strings.TrimSpace(in.PayerID)
	switch {
	case in.BizType == "" || in.BizNo == "":
		return nil, response.BadRequest(errBizRequired)
	case in.AmountMinor <= 0:
		return nil, response.BadRequest(errInvalidAmount)
	case in.Currency != "" && !currencyRe.MatchString(in.Currency):
		return nil, response.BadRequest(errCurrencyUnsupported)
	case len(in.CardLast4) > 8:
		return nil, response.BadRequest(errInvalidAmount)
	}
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if in.FxRateMicro <= 0 {
		in.FxRateMicro = FxUnity
	}
	if in.ValidDays <= 0 {
		in.ValidDays = 7
	}
	if in.ValidDays > 30 {
		in.ValidDays = 30
	}

	// 渠道与能力先判：不支持预授权就不建行、不打渠道。
	ch, err := s.pickChannel(ctx, in.ChannelCode)
	if err != nil {
		return nil, err
	}
	p, cfg, err := providerWithConfig(ch)
	if err != nil {
		return nil, err
	}
	ap, canAuth := p.(AuthProvider)
	if !canAuth || !ProviderCaps(p).Auth {
		return nil, capabilityUnsupported(p.Name(), "预授权")
	}

	a, claimed, err := s.claimAuth(ctx, in, ch.Code)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return a, nil
	}
	if err := s.freezeAuth(ctx, a, ch, ap, cfg); err != nil {
		return nil, err
	}
	return a, nil
}

// claimAuth 抢这张预授权的「冻结权」：先用唯一索引 (biz_type,biz_no) 落一行
// authorizing，抢到的人才去打渠道。
//
// 顺序不能反：先查一下没有 → 调渠道冻结 → 再插入，两个并发请求会双双通过预检，
// 各自向渠道冻结一次，客户的钱被冻两遍，而本地两条记录看起来都完全正常。
// 抢不到的人读回胜者原样返回（幂等语义：同业务键同一张单），绝不重复冻结。
func (s *Service) claimAuth(ctx context.Context, in CreateAuthInput, channelCode string) (*Authorization, bool, error) {
	var existing Authorization
	err := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&existing).Error
	switch {
	case err == nil:
		return &existing, false, nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, false, err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	a := &Authorization{
		AuthNo:  "AUTH" + formatID(int64(id)),
		BizType: in.BizType, BizNo: in.BizNo,
		SubjectID: truncate(in.SubjectID, 64), ChannelCode: channelCode, Method: truncate(in.Method, 32),
		Status: AuthAuthorizing, AmountMinor: in.AmountMinor, Currency: in.Currency,
		FxRateMicro: in.FxRateMicro, PayerID: truncate(in.PayerID, 128),
		CardLast4: truncate(in.CardLast4, 8), Remark: truncate(in.Remark, 500),
		ExpiresAt: ptr(now.AddDate(0, 0, in.ValidDays)),
	}
	a.ID = id
	if err := s.db.WithContext(ctx).Create(a).Error; err != nil {
		// 并发同键命中唯一索引：读回胜者；读不到才把原始错误抛出去。
		var dup Authorization
		if take := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&dup).Error; take == nil {
			return &dup, false, nil
		}
		return nil, false, err
	}
	return a, true, nil
}

// freezeAuth 用占位行去渠道冻结，并按渠道结论回写：
// 成功 → authorized + 渠道真实授权号；明确报错 → failed + 原因（钱没冻住，业务可换单重试）；
// 受理但未确认（含渠道没回授权号）→ 保持 authorizing，等 ReconcileStuckAuths 补查，
// 绝不把「渠道没答完」写成本地终态。
func (s *Service) freezeAuth(ctx context.Context, a *Authorization, ch *Channel, ap AuthProvider, cfg map[string]string) error {
	notify := strings.TrimSpace(ch.NotifyURL)
	if notify == "" {
		notify = channelNotifyURL(ch.Code)
	}
	res, err := ap.Freeze(ctx, AuthFreezeRequest{
		AuthNo: a.AuthNo, AmountMinor: a.AmountMinor, Currency: a.Currency,
		Subject: subjectOr("预授权 " + a.BizNo), PayerID: a.PayerID,
		NotifyURL: notify, ExpireAt: timeOf(a.ExpiresAt), Remark: a.Remark,
	}, cfg)
	if err != nil {
		s.markAuthFailed(ctx, a, err)
		return err
	}
	if res == nil || strings.TrimSpace(res.ChannelAuthNo) == "" || authStatusInProgress(res.Status) {
		s.logWarn("pay: auth freeze not confirmed by channel", "authNo", a.AuthNo,
			"provider", ch.Provider, "status", freezeStatus(res))
		return response.Conflict(errStateConflict)
	}
	authNo := truncate(res.ChannelAuthNo, 128)
	if err := s.db.WithContext(ctx).Model(&Authorization{}).
		Where("id = ? AND status = ?", int64(a.ID), AuthAuthorizing).
		Updates(map[string]any{"status": AuthAuthorized, "channel_auth_no": authNo}).Error; err != nil {
		return err
	}
	a.Status, a.ChannelAuthNo = AuthAuthorized, authNo
	return nil
}

// markAuthFailed 把渠道明确拒绝的占位行落成 failed 并留原因。只从 authorizing 迁移，
// 避免覆盖并发补查已经回填的结论；返回是否真的写了这一行（false = 败给并发，或写失败）。
func (s *Service) markAuthFailed(ctx context.Context, a *Authorization, cause error) bool {
	reason := "预授权冻结失败"
	if cause != nil {
		reason = truncate(cause.Error(), 200)
	}
	res := s.db.WithContext(ctx).Model(&Authorization{}).
		Where("id = ? AND status = ?", int64(a.ID), AuthAuthorizing).
		Updates(map[string]any{"status": AuthFailed, "fail_reason": reason})
	if res.Error != nil {
		s.logWarn("pay: mark auth failed error", "authNo", a.AuthNo, "err", res.Error.Error())
		return false
	}
	if res.RowsAffected == 0 {
		// 行已被并发补查落定：不覆盖，也不算本轮救回。
		s.logWarn("pay: auth already settled by another writer", "authNo", a.AuthNo, "reason", reason)
		return false
	}
	s.logWarn("pay: auth freeze rejected", "authNo", a.AuthNo, "reason", reason)
	return true
}

// AuthReconcileSummary 是 authorizing 残留补查一轮的可数结果。
type AuthReconcileSummary struct {
	Checked    int `json:"checked"`
	Adopted    int `json:"adopted"`
	Failed     int `json:"failed"`
	Unresolved int `json:"unresolved"`
	// Skipped 是「渠道结论拿到了，但这一行不是本轮落定的」：条件更新影响 0 行。
	// 单独一档，因为两个实例补查同一张滞留单时渠道查询本就该幂等重复，若把败者也
	// 记进 Adopted/Failed，指标会比真实资金事实多出一倍，运维据此判断「补查救回了几笔」
	// 就是错的。恒等式：Checked = Adopted + Failed + Unresolved + Skipped。
	Skipped int `json:"skipped"`
}

// authStaleAfter 是「占位行多久没结论才算残留」：小于这个窗口留给在途请求自己收尾。
const authStaleAfter = 2 * time.Minute

// ReconcileStuckAuths 补查停在 authorizing 的预授权（进程崩溃或渠道受理未回执）：
// 渠道确认过 → 回填 authorized + 真实授权号；渠道明确查无此单（404 语义）→ 落 failed，
// 让业务知道「钱没冻住」可以换单重试；两边都拿不到结论 → 原样留着，下轮再查。
func (s *Service) ReconcileStuckAuths(ctx context.Context, now time.Time) (AuthReconcileSummary, error) {
	var out AuthReconcileSummary
	var rows []Authorization
	err := s.db.WithContext(tenant.Skip(ctx)).
		Where("status = ? AND updated_at <= ?", AuthAuthorizing, now.Add(-authStaleAfter)).
		Order("id").Limit(50).Find(&rows).Error
	if err != nil {
		return out, err
	}
	for i := range rows {
		a := &rows[i]
		out.Checked++
		p, ch, cfg, perr := s.authChannelOf(tenant.Skip(ctx), a.ChannelCode)
		if perr != nil {
			out.Unresolved++
			s.logWarn("pay: auth reconcile channel unresolved", "authNo", a.AuthNo, "err", perr.Error())
			continue
		}
		ap, canAuth := p.(AuthProvider)
		if !canAuth || !ProviderCaps(p).Auth {
			// 渠道配置被改成不支持预授权：本地这张 authorizing 无法再确认，留人工核对。
			out.Unresolved++
			s.logWarn("pay: auth reconcile channel no longer supports auth", "authNo", a.AuthNo, "provider", ch.Provider)
			continue
		}
		res, qerr := ap.QueryAuth(ctx, a.AuthNo, cfg)
		switch {
		case qerr == nil && res != nil && strings.TrimSpace(res.ChannelAuthNo) != "" && !authStatusInProgress(res.Status):
			wrote, err := s.applyAuthConfirmed(ctx, a, truncate(res.ChannelAuthNo, 128))
			if err != nil {
				out.Unresolved++
				s.logWarn("pay: auth adopt failed", "authNo", a.AuthNo, "err", err.Error())
				continue
			}
			// 只数真的写入者：败者的渠道结论没有落库，记 Skipped。
			if wrote {
				out.Adopted++
			} else {
				out.Skipped++
			}
		case isNotFound(qerr):
			// 渠道查无此单：冻结请求根本没落到渠道，落 failed 让业务重试。
			if s.markAuthFailed(ctx, a, qerr) {
				a.Status = AuthFailed
				out.Failed++
			} else {
				out.Skipped++
			}
		default:
			out.Unresolved++
			s.logWarn("pay: auth reconcile unresolved", "authNo", a.AuthNo,
				"status", freezeStatus(res), "err", errText(qerr))
		}
	}
	if out.Checked > 0 {
		s.logInfo("pay: stuck auths reconciled", "checked", out.Checked, "adopted", out.Adopted,
			"failed", out.Failed, "unresolved", out.Unresolved, "skipped", out.Skipped)
	}
	return out, nil
}

// applyAuthConfirmed 把占位行回填为已冻结（只从 authorizing 迁移）。
// 返回是否真的写了这一行：影响 0 行 = 并发补查已经落定，调用方记 Skipped 而不是 Adopted。
func (s *Service) applyAuthConfirmed(ctx context.Context, a *Authorization, channelAuthNo string) (bool, error) {
	res := s.db.WithContext(tenant.Skip(ctx)).Model(&Authorization{}).
		Where("id = ? AND status = ?", int64(a.ID), AuthAuthorizing).
		Updates(map[string]any{"status": AuthAuthorized, "channel_auth_no": channelAuthNo})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	a.Status, a.ChannelAuthNo = AuthAuthorized, channelAuthNo
	return true, nil
}

// errText 把 error 转成日志用的字符串（nil 安全）。
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// timeOf 安全解 *time.Time（nil 按零值，渠道会用自己的默认有效期）。
func timeOf(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// freezeStatus 安全取冻结结论状态串（res 可能为 nil）。
func freezeStatus(res *AuthResult) string {
	if res == nil {
		return "<nil>"
	}
	return res.Status
}

// CaptureAuthInput 是请款入参；bizType/bizNo 同时作为请款产生的收款单幂等键。
type CaptureAuthInput struct {
	AmountMinor int64
	BizType     string
	BizNo       string
	OperatorID  string
}

// CaptureAuth 请款（支持部分请款）：以请款生成的 PaymentNo 作渠道侧幂等号调 CompleteAuth，
// 只有渠道确认同步成功才把收款单落 succeeded 并推进已请款额；渠道报错或返回处理中时
// 不落本地终态。剩余额度可继续请款，全部请完转 captured。
func (s *Service) CaptureAuth(ctx context.Context, authNo string, in CaptureAuthInput) (*Authorization, error) {
	in.BizType, in.BizNo = strings.TrimSpace(in.BizType), strings.TrimSpace(in.BizNo)
	switch {
	case in.BizType == "" || in.BizNo == "":
		return nil, response.BadRequest(errBizRequired)
	case in.AmountMinor <= 0:
		return nil, response.BadRequest(errInvalidAmount)
	}

	// 先按渠道码定位并判能力（不支持 Auth 就不动库、不打渠道）。请款必须走真渠道，
	// 历史伪造单（无渠道授权）请款也在此被拒，避免在假授权上继续动钱。
	var probe Authorization
	if err := s.db.WithContext(ctx).Where("auth_no = ?", authNo).Take(&probe).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound(errNotFound)
		}
		return nil, err
	}
	p, ch, cfg, err := s.authChannelOf(ctx, probe.ChannelCode)
	if err != nil {
		return nil, err
	}
	ap, canAuth := p.(AuthProvider)
	if !canAuth || !ProviderCaps(p).Auth {
		return nil, capabilityUnsupported(p.Name(), "预授权")
	}

	var a Authorization
	var orderEv *EventData
	var authEv *EventData
	var booked bool
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("auth_no = ?", authNo).Take(&a).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		if !Freezable(a.Status) {
			return response.Conflict(errStateConflict)
		}
		if in.AmountMinor > a.AmountMinor-a.CapturedMinor {
			return response.Conflict(errRefundExceeds)
		}

		// 幂等：同 bizType+bizNo 已有请款收款单——
		//   succeeded 说明上次已渠道确认并入账，直接返回不重复请款；
		//   pending 说明上次渠道已受理但本地未终态，绝不能再打渠道（会重复扣款），
		//   留给人工/对账收敛（缺渠道幂等号回填能力，见回票「需底座补」）。
		var prior Order
		perr := tx.Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&prior).Error
		if perr == nil {
			if prior.Status == StatusSucceeded {
				booked = false
				return nil
			}
			return response.Conflict(errStateConflict)
		}
		if !errors.Is(perr, gorm.ErrRecordNotFound) {
			return perr
		}

		id, err := idgen.Next()
		if err != nil {
			return err
		}
		paymentNo := "PAY" + formatID(int64(id))
		notify := strings.TrimSpace(ch.NotifyURL)
		if notify == "" {
			notify = channelNotifyURL(a.ChannelCode)
		}
		// 先渠道请款，用本次 PaymentNo 作渠道侧幂等号；渠道没确认就不动本地。
		res, cerr := ap.CompleteAuth(ctx, AuthCompleteRequest{
			AuthNo: a.AuthNo, ChannelAuthNo: a.ChannelAuthNo, AmountMinor: in.AmountMinor,
			PaymentNo: paymentNo, NotifyURL: notify, Remark: truncate(a.Remark, 500),
		}, cfg)
		if cerr != nil {
			return cerr
		}
		if res == nil || !authStatusTerminalOK(res.Status) {
			s.logWarn("pay: auth capture not confirmed by channel", "authNo", a.AuthNo, "status", freezeStatus(res))
			return response.Conflict(errStateConflict)
		}

		o := &Order{
			PaymentNo: paymentNo,
			BizType:   in.BizType, BizNo: in.BizNo,
			SubjectType: "auth", SubjectID: a.SubjectID, Subject: subjectOr("预授权请款 " + a.AuthNo),
			AmountMinor: in.AmountMinor, Currency: a.Currency, FxRateMicro: a.FxRateMicro,
			ChannelCode: a.ChannelCode, Method: a.Method,
			OperatorID: truncate(in.OperatorID, 64),
			Status:     StatusSucceeded,
		}
		o.ID = id
		now := time.Now().UTC()
		o.PaidAt = &now
		o.TenantID = a.TenantID
		rule, err := s.matchRule(tx, ctx, o.SubjectID, o.BizType)
		if err != nil {
			return err
		}
		o.FeeMinor = feeOf(o.AmountMinor, rule)
		if err := tx.Create(o).Error; err != nil {
			return err
		}
		a.CapturedMinor += in.AmountMinor
		a.PaymentNo = o.PaymentNo
		if a.CapturedMinor >= a.AmountMinor {
			a.Status = AuthCaptured
		} else {
			a.Status = AuthPartialCaptured
		}
		if err := tx.Model(&Authorization{}).Where("id = ?", int64(a.ID)).Updates(map[string]any{
			"captured_minor": a.CapturedMinor, "status": a.Status, "payment_no": a.PaymentNo,
		}).Error; err != nil {
			return err
		}
		orderEv, err = buildEvent(EventSucceeded, o.TenantID, orderPayload(o, ""))
		if err != nil {
			return err
		}
		if err := publishOutbox(ctx, tx, orderEv); err != nil {
			return err
		}
		authEv, err = buildEvent(EventAuthCaptured, a.TenantID, authPayload(&a, o))
		if err != nil {
			return err
		}
		if err := publishOutbox(ctx, tx, authEv); err != nil {
			return err
		}
		booked = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if booked {
		s.emit(ctx, orderEv)
		s.emit(ctx, authEv)
	}
	return &a, nil
}

// VoidAuth 撤销预授权的剩余额度：先渠道 CancelAuth，渠道确认后才落本地 voided。
//
// 历史数据兼容：本改动之前创建的授权单是「伪造渠道号 + 从未真冻结」的旧单，撤销时
// 若当前渠道支持 Auth 就按约定真撤销（旧单渠道从未真冻结 → 渠道可能报「查无」，让错误
// 原样上抛、本地不改，交由人工核对，绝不在渠道未确认时假撤销）；若当前渠道根本不支持
// Auth（伪造单本就无处可撤），退化为仅本地置 voided 并留痕。取舍写进回票。
func (s *Service) VoidAuth(ctx context.Context, authNo string) (*Authorization, error) {
	var probe Authorization
	if err := s.db.WithContext(ctx).Where("auth_no = ?", authNo).Take(&probe).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound(errNotFound)
		}
		return nil, err
	}
	p, _, cfg, chErr := s.authChannelOf(ctx, probe.ChannelCode)
	ap, canAuth := p.(AuthProvider)
	supportsAuth := chErr == nil && canAuth && ProviderCaps(p).Auth

	var a Authorization
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("auth_no = ?", authNo).Take(&a).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		if !Freezable(a.Status) {
			return response.Conflict(errStateConflict)
		}
		now := time.Now().UTC()
		if supportsAuth {
			// 先渠道撤销：渠道未确认（报错 / 处理中）就不落本地 voided。
			res, cerr := ap.CancelAuth(ctx, AuthCancelRequest{
				AuthNo: a.AuthNo, ChannelAuthNo: a.ChannelAuthNo, Reason: "merchant void",
			}, cfg)
			if cerr != nil {
				return cerr
			}
			if res == nil || !authStatusTerminalOK(res.Status) {
				s.logWarn("pay: auth void not confirmed by channel", "authNo", a.AuthNo, "status", freezeStatus(res))
				return response.Conflict(errStateConflict)
			}
		} else {
			// 渠道不支持 Auth：这笔只可能是本改动前的伪造/历史单，无处真撤销，本地置 voided 并留痕。
			s.logWarn("pay: void auth without channel confirm (legacy/no-auth channel)",
				"authNo", a.AuthNo, "provider", providerNameOr(p))
		}
		a.Status, a.VoidedAt = AuthVoided, &now
		return tx.Model(&Authorization{}).Where("id = ?", int64(a.ID)).
			Updates(map[string]any{"status": a.Status, "voided_at": now}).Error
	})
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// providerNameOr 安全取渠道实现名（p 可能为 nil，如渠道解析失败时）。
func providerNameOr(p Provider) string {
	if p == nil {
		return "<none>"
	}
	return p.Name()
}

// ExpireAuths 把过期的预授权批量置为 expired（跨租户清扫）。
func (s *Service) ExpireAuths(ctx context.Context, now time.Time) (int64, error) {
	res := s.db.WithContext(tenant.Skip(ctx)).Model(&Authorization{}).
		Where("status IN ? AND expires_at IS NOT NULL AND expires_at < ?",
			[]string{AuthAuthorized, AuthPartialCaptured}, now).
		Updates(map[string]any{"status": AuthExpired})
	return res.RowsAffected, res.Error
}

// QueryAuth 按授权单号取预授权。
func (s *Service) QueryAuth(ctx context.Context, authNo string) (*Authorization, error) {
	var a Authorization
	if err := s.db.WithContext(ctx).Where("auth_no = ?", authNo).Take(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound(errNotFound)
		}
		return nil, err
	}
	return &a, nil
}

// ListAuths 分页列出预授权。
func (s *Service) ListAuths(ctx context.Context, status, keyword string, page, pageSize int) ([]Authorization, int64, error) {
	db := s.db.WithContext(ctx).Model(&Authorization{})
	if status != "" {
		db = db.Where("status = ?", status)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		db = db.Where("LOWER(auth_no) LIKE LOWER(?) OR LOWER(biz_no) LIKE LOWER(?)", like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Authorization
	if err := db.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// authPayload 组装 pay.auth.captured 的载荷：定位字段 + 金额，不含凭据。
func authPayload(a *Authorization, o *Order) EventPayload {
	p := EventPayload{
		BizType: a.BizType, BizNo: a.BizNo, SubjectID: a.SubjectID,
		ChannelCode: a.ChannelCode, AmountMinor: a.AmountMinor,
		Currency: a.Currency, FxRateMicro: a.FxRateMicro, Status: a.Status,
	}
	if o != nil {
		p.PaymentNo = o.PaymentNo
		p.AmountMinor = o.AmountMinor
		p.FeeMinor = o.FeeMinor
		p.ChannelTxnID = txnIDOf(o)
		p.PaidAt = o.PaidAt
		p.Status = a.Status
	}
	return p
}
