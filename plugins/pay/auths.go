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
// Phase 0 由 mock 渠道承载：冻结/请款/撤销只走本地状态机；真实渠道接入时在
// Provider 增加冻结/请款实现后再挂渠道侧动作。

// CreateAuthInput 是预授权冻结入参。cardLast4 只允许末四位，禁存卡号。
type CreateAuthInput struct {
	BizType     string
	BizNo       string
	SubjectID   string
	ChannelCode string
	Method      string
	AmountMinor int64
	Currency    string
	FxRateMicro int64
	CardLast4   string
	ValidDays   int
	Remark      string
}

// CreateAuth 冻结一笔预授权（幂等：同 bizType+bizNo 命中已有单）。
func (s *Service) CreateAuth(ctx context.Context, in CreateAuthInput) (*Authorization, error) {
	in.BizType, in.BizNo = strings.TrimSpace(in.BizType), strings.TrimSpace(in.BizNo)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.CardLast4 = strings.TrimSpace(in.CardLast4)
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
	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	a := &Authorization{
		AuthNo:  "AUTH" + formatID(int64(id)),
		BizType: in.BizType, BizNo: in.BizNo,
		SubjectID: truncate(in.SubjectID, 64), ChannelCode: ch.Code, Method: truncate(in.Method, 32),
		Status: AuthAuthorized, AmountMinor: in.AmountMinor, Currency: in.Currency,
		FxRateMicro: in.FxRateMicro, ChannelAuthNo: "mockauth_" + randomToken(8),
		CardLast4: truncate(in.CardLast4, 8), Remark: truncate(in.Remark, 500),
		ExpiresAt: ptr(now.AddDate(0, 0, in.ValidDays)),
	}
	a.ID = id
	if err := s.db.WithContext(ctx).Create(a).Error; err != nil {
		// 并发同键命中唯一索引：读回胜者。
		var dup Authorization
		if take := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&dup).Error; take == nil {
			return &dup, nil
		}
		return nil, err
	}
	return a, nil
}

// CaptureAuthInput 是请款入参；bizType/bizNo 同时作为请款产生的收款单幂等键。
type CaptureAuthInput struct {
	AmountMinor int64
	BizType     string
	BizNo       string
	OperatorID  string
}

// CaptureAuth 请款（支持部分请款）：产生一张 pay_order 并立即入账（mock 渠道同步），
// 触发 pay.auth.captured。剩余额度可继续请款，全部请完转 captured。
func (s *Service) CaptureAuth(ctx context.Context, authNo string, in CaptureAuthInput) (*Authorization, error) {
	in.BizType, in.BizNo = strings.TrimSpace(in.BizType), strings.TrimSpace(in.BizNo)
	switch {
	case in.BizType == "" || in.BizNo == "":
		return nil, response.BadRequest(errBizRequired)
	case in.AmountMinor <= 0:
		return nil, response.BadRequest(errInvalidAmount)
	}

	var a Authorization
	var ev *EventData
	var evOrder *EventData
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
		if in.AmountMinor > a.AmountMinor-a.CapturedMinor {
			return response.Conflict(errRefundExceeds)
		}
		// 请款产生的收款单：直接生成 succeeded 状态（mock 请款同步入账），
		// 走 orderPayload 的事件管线，业务侧用同一套 pay.succeeded 落账。
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		o := &Order{
			PaymentNo: "PAY" + formatID(int64(id)),
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
		// 请款收款单同样走 pay.succeeded 落账（订阅方不因预授权路径漏账），
		// 再补 pay.auth.captured 供授权台账推进。
		evOrder, err = buildEvent(EventSucceeded, o.TenantID, orderPayload(o, ""))
		if err != nil {
			return err
		}
		if err := publishOutbox(ctx, tx, evOrder); err != nil {
			return err
		}
		ev, err = buildEvent(EventAuthCaptured, a.TenantID, authPayload(&a, o))
		if err != nil {
			return err
		}
		return publishOutbox(ctx, tx, ev)
	})
	if err != nil {
		return nil, err
	}
	s.emit(ctx, evOrder)
	s.emit(ctx, ev)
	return &a, nil
}

// VoidAuth 撤销预授权的剩余额度（真实渠道接撤销接口；mock 直接落状态）。
func (s *Service) VoidAuth(ctx context.Context, authNo string) (*Authorization, error) {
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
		a.Status, a.VoidedAt = AuthVoided, &now
		return tx.Model(&Authorization{}).Where("id = ?", int64(a.ID)).
			Updates(map[string]any{"status": a.Status, "voided_at": now}).Error
	})
	if err != nil {
		return nil, err
	}
	return &a, nil
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
