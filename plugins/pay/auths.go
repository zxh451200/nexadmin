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
// 真路径：渠道支持 Auth（实现 AuthProvider 且声明 caps.Auth）时先调 Freeze，
// 用渠道返回的 ChannelAuthNo 落库；渠道报错原样上抛，且「不落已冻结单」——
// 渠道没确认冻结就不能在本地凭空造一张 authorized 单。渠道不支持 Auth 时返回
// capabilityUnsupported，绝不伪造渠道号。幂等：同 bizType+bizNo 命中已有单先返回，
// 不因重试重复冻结。
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

	// 幂等预检：同 bizType+bizNo 已有单先返回（不调渠道，不重复冻结）。
	var existing Authorization
	err := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

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

	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	authNo := "AUTH" + formatID(int64(id))
	expireAt := now.AddDate(0, 0, in.ValidDays)
	notify := strings.TrimSpace(ch.NotifyURL)
	if notify == "" {
		notify = channelNotifyURL(ch.Code)
	}

	// 先渠道冻结：渠道报错即原样上抛（渠道拒绝=PAY_CHANNEL_ERROR，缺凭据由渠道层给明确信息），
	// 且此时尚未落库，不会产生「假已冻结」单。
	res, err := ap.Freeze(ctx, AuthFreezeRequest{
		AuthNo: authNo, AmountMinor: in.AmountMinor, Currency: in.Currency,
		Subject: subjectOr("预授权 " + in.BizNo), PayerID: truncate(in.PayerID, 128),
		NotifyURL: notify, ExpireAt: expireAt, Remark: truncate(in.Remark, 500),
	}, cfg)
	if err != nil {
		return nil, err
	}
	if res == nil || authStatusInProgress(res.Status) {
		// 渠道受理但未确认冻结：不落 authorized（避免假成功），调用方可用同一 authNo 重试。
		s.logWarn("pay: auth freeze not confirmed", "authNo", authNo, "status", freezeStatus(res))
		return nil, response.Conflict(errStateConflict)
	}

	a := &Authorization{
		AuthNo: authNo, BizType: in.BizType, BizNo: in.BizNo,
		SubjectID: truncate(in.SubjectID, 64), ChannelCode: ch.Code, Method: truncate(in.Method, 32),
		Status: AuthAuthorized, AmountMinor: in.AmountMinor, Currency: in.Currency,
		FxRateMicro: in.FxRateMicro, ChannelAuthNo: truncate(res.ChannelAuthNo, 128),
		PayerID: truncate(in.PayerID, 128), CardLast4: truncate(in.CardLast4, 8),
		Remark: truncate(in.Remark, 500), ExpiresAt: ptr(expireAt),
	}
	a.ID = id
	if err := s.db.WithContext(ctx).Create(a).Error; err != nil {
		// 并发同键命中唯一索引：读回胜者（本次冻结为渠道侧冗余，由对账兜底，见回票）。
		var dup Authorization
		if take := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&dup).Error; take == nil {
			return &dup, nil
		}
		return nil, err
	}
	return a, nil
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
		like := "%" + kw + "%"
		db = db.Where("auth_no LIKE ? OR biz_no LIKE ?", like, like)
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
