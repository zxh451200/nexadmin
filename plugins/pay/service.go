package pay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Service 是 pay 插件的业务层。宿主模块通过 client.go 的进程内 API 对接；
// 资金结果订阅 pay.* 事件获得，以 QueryByBiz 兜底。
type Service struct {
	db  *gorm.DB
	log logger
	// loc 是应用时区：统计按天分桶要用它把 UTC 时间换算成用户看到的自然日。
	loc *time.Location
}

// WithTimezone 设置应用时区（非法值退回 UTC）。
func (s *Service) WithTimezone(tz string) *Service {
	loc, err := time.LoadLocation(tz)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	s.loc = loc
	return s
}

// logger 是 Service 需要的最小日志面（测试易替换）。
type logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService 创建 pay 服务。
func NewService(db *gorm.DB, log logger) *Service { return &Service{db: db, log: log} }

const defaultExpireSec = 30 * 60

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

// CreateInput 是下单入参。BizType/BizNo 组成业务幂等键（DB 唯一索引兜底）。
// 金额一律来自服务端计算，绝不透传客户端金额。
type CreateInput struct {
	BizType         string
	BizNo           string
	SubjectType     string
	SubjectID       string
	Subject         string
	AmountMinor     int64
	AmountOrigMinor int64
	OrigCurrency    string
	FxRateMicro     int64
	Currency        string
	ChannelCode     string // 空 = 第一个启用渠道
	Method          string
	Scene           string
	PayerID         string
	PayerOpenID     string
	ClientIP        string
	OperatorID      string
	ReturnURL       string
	ExpireSec       int
	Metadata        map[string]string
}

// Create 创建（或幂等取回）一笔 pending 支付单，返回订单与拉起支付的参数。
func (s *Service) Create(ctx context.Context, in CreateInput) (*Order, *PayParams, error) {
	in.BizType, in.BizNo = strings.TrimSpace(in.BizType), strings.TrimSpace(in.BizNo)
	in.Subject, in.ReturnURL = strings.TrimSpace(in.Subject), strings.TrimSpace(in.ReturnURL)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	switch {
	case in.BizType == "" || in.BizNo == "":
		return nil, nil, response.BadRequest(errBizRequired)
	case in.AmountMinor <= 0:
		return nil, nil, response.BadRequest(errInvalidAmount)
	case in.Currency != "" && !currencyRe.MatchString(in.Currency):
		return nil, nil, response.BadRequest(errCurrencyUnsupported)
	}
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if in.ExpireSec <= 0 {
		in.ExpireSec = defaultExpireSec
	}
	if in.ExpireSec < 60 {
		in.ExpireSec = 60
	}
	if in.ExpireSec > 7200 {
		in.ExpireSec = 7200
	}
	fx, amountOrig, origCur, err := normalizeFx(in.FxRateMicro, in.AmountOrigMinor, strings.ToUpper(strings.TrimSpace(in.OrigCurrency)), in.Currency)
	if err != nil {
		return nil, nil, err
	}

	// 幂等：同 (biz_type, biz_no) 命中已有单，pending 则重新取拉起参数。
	var existing Order
	err = s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&existing).Error
	if err == nil {
		params, perr := s.payParams(ctx, &existing, in.ReturnURL, in.ClientIP)
		if perr != nil {
			return nil, nil, perr
		}
		return &existing, params, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}

	ch, err := s.pickChannel(ctx, in.ChannelCode)
	if err != nil {
		return nil, nil, err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, nil, err
	}
	meta, err := marshalMeta(in.Metadata)
	if err != nil {
		return nil, nil, err
	}
	o := &Order{
		PaymentNo: "PAY" + formatID(int64(id)),
		BizType:   in.BizType, BizNo: in.BizNo,
		SubjectType: truncate(in.SubjectType, 32), SubjectID: truncate(in.SubjectID, 64),
		Subject:     subjectOr(in.Subject),
		AmountMinor: in.AmountMinor, AmountOrigMinor: amountOrig,
		OrigCurrency: origCur, Currency: in.Currency, FxRateMicro: fx,
		ChannelCode: ch.Code, Method: truncate(in.Method, 32), Scene: truncate(in.Scene, 32),
		PayerID: truncate(in.PayerID, 64), PayerOpenID: truncate(in.PayerOpenID, 128),
		OperatorID: truncate(in.OperatorID, 64), MetadataJSON: meta,
		Status:   StatusPending,
		ExpireAt: ptr(time.Now().UTC().Add(time.Duration(in.ExpireSec) * time.Second)),
	}
	o.ID = id
	if err := s.db.WithContext(ctx).Create(o).Error; err != nil {
		// 并发同键命中唯一索引：读回胜者。
		var dup Order
		if take := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&dup).Error; take == nil {
			params, perr := s.payParams(ctx, &dup, in.ReturnURL, in.ClientIP)
			if perr != nil {
				return nil, nil, perr
			}
			return &dup, params, nil
		}
		return nil, nil, err
	}
	params, err := s.payParams(ctx, o, in.ReturnURL, in.ClientIP)
	if err != nil {
		return nil, nil, err
	}
	return o, params, nil
}

// payParams 向渠道请求拉起参数。Provider.Create 必须按 paymentNo 幂等。
func (s *Service) payParams(ctx context.Context, o *Order, returnURL, clientIP string) (*PayParams, error) {
	if o.Status != StatusPending {
		return &PayParams{}, nil
	}
	ch, err := s.loadChannel(ctx, o.ChannelCode)
	if err != nil {
		return nil, err
	}
	p, cfg, err := providerWithConfig(ch)
	if err != nil {
		return nil, err
	}
	// 渠道自定义的回调地址优先（真实网关要求完整 URL）；mock 收银台用站点内相对路径。
	notify := strings.TrimSpace(ch.NotifyURL)
	if notify == "" {
		notify = channelNotifyURL(o.ChannelCode)
	}
	req := PayRequest{
		PaymentNo: o.PaymentNo, Subject: o.Subject, AmountMinor: o.AmountMinor,
		Currency: o.Currency, Method: o.Method, Scene: o.Scene,
		PayerOpenID: o.PayerOpenID, ClientIP: clientIP,
		ReturnURL: returnURL, NotifyURL: notify,
	}
	if o.ExpireAt != nil {
		req.ExpireAt = *o.ExpireAt
	}
	return p.Create(ctx, req, cfg)
}

// HandleCallback 处理一笔渠道回调（或 mock 收银台触发）：
// 定位渠道 → 验签 → 审计 → 幂等翻转 → 事件。重复回调不报错、不重复发事件。
//
// 公开路由没有租户上下文：渠道按全局唯一 code 定位（tenant.Skip），订单先按
// payment_no 全局定位拿到租户，再在订单的租户上下文里翻转。
func (s *Service) HandleCallback(ctx context.Context, in CallbackInput) (*Order, error) {
	ch, err := s.loadChannelGlobal(ctx, in.Channel)
	if err != nil {
		return nil, err
	}
	cfg, err := decodeConfig(ch.ConfigJSON)
	if err != nil {
		return nil, err
	}
	p, err := LookupProvider(ch.Provider)
	if err != nil {
		return nil, err
	}
	res, verr := p.VerifyCallback(in, cfg)
	payload := truncate(string(in.Body), 4000)
	if verr != nil {
		s.recordCallback(ctx, ch.TenantID, ch.Code, "", false, ResultSignInvalid, payload)
		return nil, response.BadRequest(errSignInvalid)
	}
	if res.RefundNo != "" {
		r, changed, rerr := s.transitionRefund(ctx, res)
		result := ResultDuplicate
		if changed {
			result = ResultAccepted
		} else if rerr == nil && r != nil && r.Status != RefundPending {
			result = ResultIgnoredStatePrefix + r.Status
		} else if errors.Is(rerr, errCallbackAmount) {
			result = ResultAmountMismatch
		} else if isNotFound(rerr) {
			result = ResultOrderMissing
		}
		s.recordCallback(ctx, ch.TenantID, ch.Code, res.PaymentNo, true, result, payload)
		if rerr != nil {
			return nil, rerr
		}
		return nil, nil
	}
	o, changed, err := s.transition(ctx, res)
	result := ResultDuplicate
	if changed {
		result = ResultAccepted
	} else if err == nil && o != nil && o.Status != StatusPending {
		result = ResultIgnoredStatePrefix + o.Status
	} else if errors.Is(err, errCallbackAmount) {
		result = ResultAmountMismatch
	} else if isNotFound(err) {
		result = ResultOrderMissing
	}
	s.recordCallback(ctx, ch.TenantID, ch.Code, res.PaymentNo, true, result, payload)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// transition 把 pending 支付单翻转为 succeeded/failed：行锁 + 状态位，任何其他
// 当前状态原样放行（幂等）。事件（出站箱 + 进程内总线）只在翻转发生时发。
// CallbackAck 返回渠道要求的回调应答体（支付宝/易支付要纯文本 success，微信要 JSON）。
// 未声明应答格式的渠道返回 ok=false，走统一 JSON 响应。
func (s *Service) CallbackAck(ctx context.Context, channelCode string) (contentType, body string, ok bool) {
	ch, err := s.loadChannelGlobal(ctx, channelCode)
	if err != nil {
		return "", "", false
	}
	p, err := LookupProvider(ch.Provider)
	if err != nil {
		return "", "", false
	}
	if a, isAck := p.(AckProvider); isAck {
		ct, b := a.CallbackAck()
		return ct, b, true
	}
	return "", "", false
}

func (s *Service) transition(ctx context.Context, res *CallbackResult) (*Order, bool, error) {
	var o Order
	var ev *EventData
	changed := false
	// err 必须在事务闭包之外声明：闭包里要对 ev/err 赋值，而 := 声明的变量
	// 在该语句结束前不进作用域，闭包内引用会编译不过。
	var err error
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("payment_no = ?", res.PaymentNo).Take(&o).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		if o.Status != StatusPending {
			return nil
		}
		if res.AmountMinor > 0 && res.AmountMinor != o.AmountMinor {
			return callbackAmountMismatch()
		}
		now := time.Now().UTC()
		updates := map[string]any{}
		switch {
		case res.Paid:
			rule, rerr := s.matchRule(tx, ctx, o.SubjectID, o.BizType)
			if rerr != nil {
				return rerr
			}
			updates["status"] = StatusSucceeded
			updates["channel_txn_id"] = truncate(res.ChannelTxnID, 128)
			updates["gateway_status"] = truncate(res.GatewayStatus, 64)
			updates["fee_minor"] = feeOf(o.AmountMinor, rule)
			updates["paid_at"] = now
		case res.Failed:
			updates["status"] = StatusFailed
			updates["fail_reason"] = truncate(res.Reason, 200)
			updates["gateway_status"] = truncate(res.GatewayStatus, 64)
		default:
			return nil
		}
		if err := tx.Model(&Order{}).Where("id = ?", int64(o.ID)).Updates(updates).Error; err != nil {
			return err
		}
		o.Status = updates["status"].(string)
		if txn, ok := updates["channel_txn_id"].(string); ok {
			o.ChannelTxnID = &txn
		}
		if fee, ok := updates["fee_minor"].(int64); ok {
			o.FeeMinor = fee
		}
		changed = true
		// 事件出站箱与状态翻转同事务；事件载荷带订单租户（必非 0）。
		ev, err = buildEvent(s.eventNameOf(o.Status), o.TenantID, orderPayload(&o, ""))
		if err != nil {
			return err
		}
		return publishOutbox(ctx, tx, ev)
	})
	if err != nil {
		return nil, false, err
	}
	if changed {
		s.emit(ctx, ev)
	}
	return &o, changed, nil
}

// eventNameOf 支付单状态对应的事件名。
func (s *Service) eventNameOf(status string) string {
	if status == StatusSucceeded {
		return EventSucceeded
	}
	return EventFailed
}

// transitionRefund 处理异步退款回执（Pending 退款单由回调翻转）。
func (s *Service) transitionRefund(ctx context.Context, res *CallbackResult) (*Refund, bool, error) {
	var r Refund
	var o Order
	var ev *EventData
	changed := false
	var err error
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("refund_no = ?", res.RefundNo).Take(&r).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		if r.Status != RefundPending {
			return nil
		}
		if res.AmountMinor > 0 && res.AmountMinor != r.AmountMinor {
			return callbackAmountMismatch()
		}
		now := time.Now().UTC()
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("payment_no = ?", r.PaymentNo).Take(&o).Error; err != nil {
			return err
		}
		if res.RefundOK {
			if err := applyRefundSuccess(tx, &o, &r, res.ChannelTxnID, now); err != nil {
				return err
			}
		} else {
			if err := tx.Model(&Refund{}).Where("id = ?", int64(r.ID)).Updates(map[string]any{
				"status": RefundFailed, "fail_reason": truncate(res.Reason, 200),
			}).Error; err != nil {
				return err
			}
			if o.Status == StatusRefunding {
				if err := tx.Model(&Order{}).Where("id = ?", int64(o.ID)).
					Updates(map[string]any{"status": refundRestoreStatus(o)}).Error; err != nil {
					return err
				}
			}
		}
		changed = true
		name := EventRefundOK
		if !res.RefundOK {
			name = EventRefundFailed
		}
		ev, err = buildEvent(name, r.TenantID, refundPayload(&r, &o))
		if err != nil {
			return err
		}
		return publishOutbox(ctx, tx, ev)
	})
	if err != nil {
		return nil, false, err
	}
	if changed {
		s.emit(ctx, ev)
	}
	return &r, changed, nil
}

// QueryByBiz 按业务幂等键取单（pending 时顺带向渠道补查一次，回调丢失靠它收敛）。
func (s *Service) QueryByBiz(ctx context.Context, bizType, bizNo string) (*Order, error) {
	var o Order
	if err := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", bizType, bizNo).Take(&o).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound(errNotFound)
		}
		return nil, err
	}
	return s.SyncIfPending(ctx, &o)
}

// QueryByNo 按支付单号取单（管理端/退款用）。
func (s *Service) QueryByNo(ctx context.Context, paymentNo string) (*Order, error) {
	var o Order
	if err := s.db.WithContext(ctx).Where("payment_no = ?", paymentNo).Take(&o).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound(errNotFound)
		}
		return nil, err
	}
	return s.SyncIfPending(ctx, &o)
}

// SyncIfPending 向渠道补查并补偿：渠道侧已支付但回调丢失时在此收敛。
// 状态翻转只发事件；本查询路径不做请求级幂等（查询天然幂等）。
func (s *Service) SyncIfPending(ctx context.Context, o *Order) (*Order, error) {
	if o.Status != StatusPending {
		return o, nil
	}
	p, cfg, err := s.channelOf(ctx, o.ChannelCode)
	if err != nil {
		return nil, err
	}
	st, err := p.Query(ctx, o.PaymentNo, cfg)
	if err != nil {
		return nil, err
	}
	if !st.Paid && !st.Failed {
		return o, nil
	}
	upd, _, err := s.transition(ctx, &CallbackResult{
		PaymentNo: o.PaymentNo, Paid: st.Paid, Failed: st.Failed,
		ChannelTxnID: st.ChannelTxnID, GatewayStatus: st.GatewayStatus, Reason: st.Reason,
	})
	return upd, err
}

// RefundInput 是退款入参；BizType/BizNo 是退款侧幂等键。
type RefundInput struct {
	PaymentNo   string
	BizType     string
	BizNo       string
	AmountMinor int64 // > 0 且 ≤ 可退余额
	Reason      string
	OperatorID  string
}

// CreateRefund 发起退款：Σrefund ≤ amount 不变式在行锁事务内校验；P0 mock 渠道
// 同步成功；异步渠道落 pending 并把支付单置 refunding，由回调翻转。
func (s *Service) CreateRefund(ctx context.Context, in RefundInput) (*Refund, error) {
	in.PaymentNo, in.BizType, in.BizNo = strings.TrimSpace(in.PaymentNo), strings.TrimSpace(in.BizType), strings.TrimSpace(in.BizNo)
	in.Reason, in.OperatorID = strings.TrimSpace(in.Reason), strings.TrimSpace(in.OperatorID)
	switch {
	case in.PaymentNo == "" || in.BizType == "" || in.BizNo == "":
		return nil, response.BadRequest(errBizRequired)
	case in.AmountMinor <= 0:
		return nil, response.BadRequest(errInvalidAmount)
	}

	// 幂等预检（事务内还会再查一次）。
	var existing Refund
	err := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var out Refund
	var ev *EventData
	rejected := ""
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var o Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("payment_no = ?", in.PaymentNo).Take(&o).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		if !Settleable(o.Status) {
			return response.Conflict(errNotRefundable)
		}
		if in.AmountMinor > o.AmountMinor-o.RefundedMinor {
			return response.Conflict(errRefundExceeds)
		}
		if err := tx.Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&out).Error; err == nil {
			return nil // 并发幂等：先到的退款已存在
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		rule, err := s.matchRule(tx, ctx, o.SubjectID, o.BizType)
		if err != nil {
			return err
		}
		out = Refund{
			RefundNo: "RF" + formatID(int64(id)), PaymentNo: o.PaymentNo,
			SubjectID: o.SubjectID, BizType: in.BizType, BizNo: in.BizNo,
			AmountMinor: in.AmountMinor, FeeRefundMinor: feeRefundOf(in.AmountMinor, rule, o.FeeMinor),
			Currency: o.Currency, FxRateMicro: o.FxRateMicro,
			Reason: truncate(in.Reason, 200), OperatorID: truncate(in.OperatorID, 64),
			Status: RefundPending,
		}
		out.ID = id
		// 事务内的一切读必须走 tx：s.db 会从池里另取一条连接，并发退款时
		// 持锁事务等第 N+1 条连接、其他事务等行锁，互相卡死到锁超时（实库压测踩中）。
		ch, p, cfg, err := channelOfDB(tx, o.ChannelCode)
		if err != nil {
			return err
		}
		notify := strings.TrimSpace(ch.NotifyURL)
		if notify == "" {
			notify = channelNotifyURL(ch.Code)
		}
		res, err := p.Refund(ctx, RefundRequest{
			RefundNo: out.RefundNo, PaymentNo: o.PaymentNo, ChannelTxnID: txnIDOf(&o),
			AmountMinor: in.AmountMinor, OrderAmountMinor: o.AmountMinor,
			IdempotencyKey: refundIdempotencyKey(o.PaymentNo, in.BizType, in.BizNo),
			NotifyURL:      notify,
		}, cfg)
		if err != nil {
			return err
		}
		switch {
		case res.Pending:
			// 已受理待回调：退款单留 pending，支付单进 refunding。
			if err := tx.Create(&out).Error; err != nil {
				return err
			}
			return tx.Model(&Order{}).Where("id = ?", int64(o.ID)).
				Updates(map[string]any{"status": StatusRefunding}).Error
		case !res.OK:
			rejected = res.Reason
			out.Status = RefundFailed
			out.FailReason = truncate(res.Reason, 200)
			if err := tx.Create(&out).Error; err != nil {
				return err
			}
			ev, err = buildEvent(EventRefundFailed, out.TenantID, refundPayload(&out, &o))
			return err
		}
		out.Status = RefundSucceeded
		now := time.Now().UTC()
		out.ChannelRefundID = truncate(res.ChannelRefundID, 128)
		out.SucceededAt = &now
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		if err := settleRefundOrder(tx, &o, in.AmountMinor, now); err != nil {
			return err
		}
		ev, err = buildEvent(EventRefundOK, out.TenantID, refundPayload(&out, &o))
		if err != nil {
			return err
		}
		return publishOutbox(ctx, tx, ev)
	})
	if err != nil {
		return nil, err
	}
	s.emit(ctx, ev)
	if rejected != "" {
		// 渠道给出的拒绝原因只进日志与退款单的 FailReason，不回给调用方。
		s.logWarn("pay: refund rejected by channel", "reason", rejected)
		return &out, response.Conflict(errRefundRejected)
	}
	return &out, nil
}

// applyRefundSuccess 在事务内结算一笔已存在的退款单（回调翻转路径）：
// 退款单终态 + 支付单累计与状态机推进。调用方保证已持有 o 与 r 的行锁。
func applyRefundSuccess(tx *gorm.DB, o *Order, r *Refund, channelRefundID string, now time.Time) error {
	r.Status = RefundSucceeded
	r.ChannelRefundID = truncate(channelRefundID, 128)
	r.SucceededAt = &now
	if err := tx.Model(&Refund{}).Where("id = ?", int64(r.ID)).Updates(map[string]any{
		"status": r.Status, "channel_refund_id": r.ChannelRefundID, "succeeded_at": now,
	}).Error; err != nil {
		return err
	}
	return settleRefundOrder(tx, o, r.AmountMinor, now)
}

// settleRefundOrder 推进支付单的累计退款与状态机（Σrefund ≤ amount 在调用方校验）。
func settleRefundOrder(tx *gorm.DB, o *Order, amountMinor int64, now time.Time) error {
	refunded := o.RefundedMinor + amountMinor
	status := StatusPartiallyRefunded
	if refunded >= o.AmountMinor {
		status = StatusRefunded
	}
	if err := tx.Model(&Order{}).Where("id = ?", int64(o.ID)).
		Updates(map[string]any{"refunded_minor": refunded, "status": status, "updated_at": now}).Error; err != nil {
		return err
	}
	o.RefundedMinor, o.Status = refunded, status
	return nil
}

// refundRestoreStatus 退款失败回执把 refunding 还原为上一个可退状态。
func refundRestoreStatus(o Order) string {
	if o.RefundedMinor > 0 {
		return StatusPartiallyRefunded
	}
	return StatusSucceeded
}

// Close 手动关闭一笔 pending 支付单（管理端动作）。
func (s *Service) Close(ctx context.Context, paymentNo string) (*Order, error) {
	var o Order
	var ev *EventData
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("payment_no = ?", paymentNo).Take(&o).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		if o.Status != StatusPending {
			return response.Conflict(errNotClosable)
		}
		var err error
		ev, err = closeOrderRow(tx, &o, time.Now().UTC())
		if err == nil && ev == nil {
			return response.Conflict(errNotClosable)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	s.emit(ctx, ev)
	return &o, nil
}

// closeOrderRow 关单并落事件出站箱（同一事务），返回提交后要发布的事件。
func closeOrderRow(tx *gorm.DB, o *Order, now time.Time) (*EventData, error) {
	res := tx.Model(&Order{}).Where("id = ? AND status = ?", int64(o.ID), StatusPending).
		Updates(map[string]any{"status": StatusClosed, "closed_at": now, "updated_at": now})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		// 条件更新落空：并发回调已把单子改为 paid/closed 等状态，不能再发 closed 事件。
		return nil, nil
	}
	o.Status = StatusClosed
	o.ClosedAt = &now
	ev, err := buildEvent(EventClosed, o.TenantID, orderPayload(o, ""))
	if err != nil {
		return nil, err
	}
	return ev, publishOutbox(tx.Statement.Context, tx, ev)
}

// CloseExpired 把过期的 pending 支付单批量置为 closed（跨租户清扫，返回张数）。
func (s *Service) CloseExpired(ctx context.Context, now time.Time) (int64, error) {
	var expired []Order
	if err := s.db.WithContext(ctx).
		Where("status = ? AND expire_at IS NOT NULL AND expire_at < ?", StatusPending, now).
		Find(&expired).Error; err != nil {
		return 0, err
	}
	n := int64(0)
	for i := range expired {
		o := &expired[i]
		var ev *EventData
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var err error
			ev, err = closeOrderRow(tx, o, now)
			return err
		})
		if err != nil {
			return n, err
		}
		if ev == nil {
			continue // 已被并发的支付回调改为非 pending，跳过
		}
		s.emit(ctx, ev)
		n++
	}
	return n, nil
}

// ---- channel management ----

// SaveChannelInput 是渠道配置入参；Config 为空表示保留既有凭据。
type SaveChannelInput struct {
	ID        idgen.ID
	Code      string
	Name      string
	Provider  string
	Method    string
	Scene     string
	SubjectID string
	FeeRateBp int64
	NotifyURL string
	Enabled   bool
	Sort      int
	Config    map[string]string
}

// UpsertChannel 新建或按 code 更新渠道商户配置；凭据整体加密入库，响应永不回显。
func (s *Service) UpsertChannel(ctx context.Context, in SaveChannelInput) (*Channel, error) {
	in.Code, in.Name = strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
	in.Provider = strings.TrimSpace(in.Provider)
	in.NotifyURL = strings.TrimSpace(in.NotifyURL)
	switch {
	case in.Code == "" || in.Name == "":
		return nil, response.BadRequest(errChannelInvalid)
	case !channelCodeRe.MatchString(in.Code):
		return nil, response.BadRequest(errChannelInvalid)
	}
	p, err := LookupProvider(in.Provider)
	if err != nil {
		return nil, response.BadRequest(errChannelUnknownProvider)
	}
	// 必填凭据在保存时就拦住，而不是等商户第一次下单才发现缺 appId/私钥。
	// 编辑且未提交新凭据时沿用既有密文（不回显、无法在此校验）。
	if len(in.Config) > 0 || in.ID == 0 {
		if err := requireProviderConfig(p, in.Config); err != nil {
			return nil, err
		}
	}
	if in.FeeRateBp < 0 || in.FeeRateBp > 9999 {
		return nil, response.BadRequest(errChannelInvalid)
	}

	enc := ""
	if len(in.Config) > 0 {
		raw, err := json.Marshal(in.Config)
		if err != nil {
			return nil, err
		}
		e, err := secretEncrypt(string(raw))
		if err != nil {
			return nil, err
		}
		enc = e
	}

	var ch Channel
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// code 全局唯一：先跨租户查重，给出明确冲突而不是等唯一索引报错。
		var clash Channel
		err := tx.WithContext(tenant.Skip(ctx)).Where("code = ?", in.Code).Take(&clash).Error
		if err == nil {
			if in.ID != 0 && clash.ID == in.ID {
				ch = clash
			} else if in.ID == 0 {
				return response.Conflict(errChannelCodeTaken)
			} else {
				return response.Conflict(errChannelCodeTaken)
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if in.ID != 0 {
			if ch.ID == 0 {
				if err := tx.Take(&ch, in.ID).Error; err != nil {
					return err
				}
			}
			updates := map[string]any{
				"name": in.Name, "provider": in.Provider, "method": truncate(in.Method, 32),
				"scene": truncate(in.Scene, 32), "subject_id": truncate(in.SubjectID, 64),
				"fee_rate_bp": in.FeeRateBp, "notify_url": truncate(in.NotifyURL, 255),
				"enabled": in.Enabled, "sort": in.Sort,
			}
			if enc != "" {
				updates["config_json"] = enc
			}
			if err := tx.Model(&Channel{}).Where("id = ?", int64(ch.ID)).Updates(updates).Error; err != nil {
				return err
			}
			ch.Name, ch.Provider, ch.Method, ch.Scene = in.Name, in.Provider, truncate(in.Method, 32), truncate(in.Scene, 32)
			ch.SubjectID, ch.FeeRateBp, ch.NotifyURL = truncate(in.SubjectID, 64), in.FeeRateBp, truncate(in.NotifyURL, 255)
			ch.Enabled, ch.Sort = in.Enabled, in.Sort
			if enc != "" {
				ch.ConfigJSON = enc
			}
			return nil
		}
		ch = Channel{
			Code: in.Code, Name: in.Name, Provider: in.Provider,
			Method: truncate(in.Method, 32), Scene: truncate(in.Scene, 32),
			SubjectID: truncate(in.SubjectID, 64), FeeRateBp: in.FeeRateBp,
			NotifyURL: truncate(in.NotifyURL, 255), Enabled: in.Enabled, Sort: in.Sort,
			ConfigJSON: enc,
		}
		return tx.Create(&ch).Error
	})
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// DeleteChannel 删除渠道配置。
func (s *Service) DeleteChannel(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Delete(&Channel{}, id).Error
}

// ListChannels 按租户列出渠道（sort, id 排序）。
func (s *Service) ListChannels(ctx context.Context, subjectID string, enabledOnly bool) ([]Channel, error) {
	db := s.db.WithContext(ctx).Model(&Channel{})
	if subjectID != "" {
		db = db.Where("subject_id = ?", subjectID)
	}
	if enabledOnly {
		db = db.Where("enabled = ?", true)
	}
	var rows []Channel
	err := db.Order("sort, id").Find(&rows).Error
	return rows, err
}

// ---- admin lists ----

// OrderQuery 过滤支付单列表。
type OrderQuery struct {
	Keyword    string // 支付单号 / 业务单号模糊
	Status     string
	Channel    string
	SubjectID  string
	BizType    string
	Scene      string
	FailReason string
	Page       int
	PageSize   int
}

// ListOrders 分页列出支付单。
func (s *Service) ListOrders(ctx context.Context, q OrderQuery) ([]Order, int64, error) {
	db := s.db.WithContext(ctx).Model(&Order{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("payment_no LIKE ? OR biz_no LIKE ?", like, like)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Channel != "" {
		db = db.Where("channel_code = ?", q.Channel)
	}
	if q.SubjectID != "" {
		db = db.Where("subject_id = ?", q.SubjectID)
	}
	if q.BizType != "" {
		db = db.Where("biz_type = ?", q.BizType)
	}
	if q.Scene != "" {
		db = db.Where("scene = ?", q.Scene)
	}
	if q.FailReason != "" {
		db = db.Where("fail_reason = ?", q.FailReason)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Order
	if err := db.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// CallbackQuery 过滤回调审计列表。
type CallbackQuery struct {
	Channel  string
	Keyword  string
	OK       *bool
	Page     int
	PageSize int
}

// ListCallbacks 分页列出回调审计。
func (s *Service) ListCallbacks(ctx context.Context, q CallbackQuery) ([]Callback, int64, error) {
	db := s.db.WithContext(ctx).Model(&Callback{})
	if q.Channel != "" {
		db = db.Where("channel_code = ?", q.Channel)
	}
	if q.OK != nil {
		db = db.Where("ok = ?", *q.OK)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		db = db.Where("payment_no LIKE ?", "%"+kw+"%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Callback
	if err := db.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// RefundQuery 过滤退款列表。
type RefundQuery struct {
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

// ListRefunds 分页列出退款单。
func (s *Service) ListRefunds(ctx context.Context, q RefundQuery) ([]Refund, int64, error) {
	db := s.db.WithContext(ctx).Model(&Refund{})
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("refund_no LIKE ? OR payment_no LIKE ? OR biz_no LIKE ?", like, like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Refund
	if err := db.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ---- internals ----

func (s *Service) pickChannel(ctx context.Context, code string) (*Channel, error) {
	if code != "" {
		return s.loadChannel(ctx, code)
	}
	var ch Channel
	err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("sort, id").Take(&ch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.Conflict(errNoChannel)
	}
	return &ch, err
}

func (s *Service) loadChannel(ctx context.Context, code string) (*Channel, error) {
	var ch Channel
	if err := s.db.WithContext(ctx).Where("code = ?", code).Take(&ch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound(errChannelNotFound)
		}
		return nil, err
	}
	return &ch, nil
}

// loadChannelGlobal 公开回调用：无租户上下文，按全局唯一 code 定位渠道。
func (s *Service) loadChannelGlobal(ctx context.Context, code string) (*Channel, error) {
	var ch Channel
	if err := s.db.WithContext(tenant.Skip(ctx)).Where("code = ?", code).Take(&ch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound(errChannelNotFound)
		}
		return nil, err
	}
	return &ch, nil
}

// queryByNoGlobal 公开收银台用：无租户上下文按单号定位订单（只读）。
func (s *Service) queryByNoGlobal(ctx context.Context, paymentNo string) (*Order, error) {
	var o Order
	if err := s.db.WithContext(tenant.Skip(ctx)).Where("payment_no = ?", paymentNo).Take(&o).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound(errNotFound)
		}
		return nil, err
	}
	return &o, nil
}

// channelOf 解析渠道 + 解密凭据 + 渠道实现。
func (s *Service) channelOf(ctx context.Context, code string) (Provider, map[string]string, error) {
	ch, err := s.loadChannel(ctx, code)
	if err != nil {
		return nil, nil, err
	}
	return providerWithConfig(ch)
}

// channelOfDB 同 channelOf，但走传入的 db（事务内必须用它，禁止另取池连接）。
func channelOfDB(db *gorm.DB, code string) (*Channel, Provider, map[string]string, error) {
	var ch Channel
	if err := db.Where("code = ?", code).Take(&ch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, response.NotFound(errChannelNotFound)
		}
		return nil, nil, nil, err
	}
	p, cfg, err := providerWithConfig(&ch)
	if err != nil {
		return nil, nil, nil, err
	}
	return &ch, p, cfg, nil
}

func providerWithConfig(ch *Channel) (Provider, map[string]string, error) {
	cfg, err := decodeConfig(ch.ConfigJSON)
	if err != nil {
		return nil, nil, err
	}
	p, err := LookupProvider(ch.Provider)
	if err != nil {
		return nil, nil, err
	}
	return p, cfg, nil
}

// matchRule 命中抽佣规则：主体精确优先，退回租户默认（subject_id = ”）。无规则返回 nil。
func (s *Service) matchRule(db *gorm.DB, ctx context.Context, subjectID, bizType string) (*CommissionRule, error) {
	var rule CommissionRule
	err := db.WithContext(ctx).Where(
		"status = ? AND biz_type = ? AND subject_id = ?", RuleActive, bizType, subjectID).
		Take(&rule).Error
	if err == nil {
		return &rule, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	err = db.WithContext(ctx).Where(
		"status = ? AND biz_type = ? AND subject_id = ?", RuleActive, bizType, "").
		Take(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// recordCallback 落回调审计；失败只告警不阻断（审计不应反过来挡资金链路）。
// 审计行落在渠道所属租户下；tenantID 为 0（渠道都没找到）时不落库。
func (s *Service) recordCallback(ctx context.Context, tenantID idgen.ID, channel, paymentNo string, ok bool, result, payload string) {
	if tenantID == 0 {
		return
	}
	row := Callback{
		ChannelCode: channel, PaymentNo: truncate(paymentNo, 40),
		OK: ok, Result: truncate(result, 32), Payload: payload,
	}
	row.TenantID = tenantID
	if err := s.db.WithContext(tenant.Skip(ctx)).Create(&row).Error; err != nil {
		s.logWarn("pay: save callback log failed", "err", err.Error())
	}
}

func (s *Service) logWarn(msg string, keysAndValues ...any) {
	if s.log != nil {
		s.log.Warn(msg, keysAndValues...)
	}
}

// isNotFound 判定框架 404 语义错误（回调审计区分 order_not_found 与 duplicate 用）。
func isNotFound(err error) bool {
	var re *response.Error
	return errors.As(err, &re) && re.Status == 404
}

func decodeConfig(enc string) (map[string]string, error) {
	if enc == "" {
		return map[string]string{}, nil
	}
	plain, err := secretDecrypt(enc)
	if err != nil {
		return nil, fmt.Errorf("pay: decrypt channel config: %w", err)
	}
	cfg := map[string]string{}
	if plain == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, fmt.Errorf("pay: bad channel config: %w", err)
	}
	return cfg, nil
}

func subjectOr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "pay"
	}
	return truncate(s, 200)
}

func marshalMeta(meta map[string]string) (string, error) {
	if len(meta) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func metaOf(o *Order) map[string]string {
	if o.MetadataJSON == "" {
		return nil
	}
	m := map[string]string{}
	if err := json.Unmarshal([]byte(o.MetadataJSON), &m); err != nil {
		return nil
	}
	return m
}

func orderPayload(o *Order, refundNo string) EventPayload {
	return EventPayload{
		PaymentNo: o.PaymentNo, RefundNo: refundNo, BizType: o.BizType, BizNo: o.BizNo,
		SubjectID: o.SubjectID, ChannelCode: o.ChannelCode,
		AmountMinor: o.AmountMinor, RefundedMinor: o.RefundedMinor, FeeMinor: o.FeeMinor,
		Currency: o.Currency, FxRateMicro: o.FxRateMicro,
		ChannelTxnID: txnIDOf(o), Status: o.Status, PaidAt: o.PaidAt, Metadata: metaOf(o),
	}
}

func refundPayload(r *Refund, o *Order) EventPayload {
	p := EventPayload{
		PaymentNo: r.PaymentNo, RefundNo: r.RefundNo, BizType: r.BizType, BizNo: r.BizNo,
		SubjectID: r.SubjectID, AmountMinor: r.AmountMinor, FeeMinor: r.FeeRefundMinor,
		Currency: r.Currency, FxRateMicro: r.FxRateMicro, Status: r.Status,
	}
	if o != nil {
		p.ChannelCode = o.ChannelCode
		p.RefundedMinor = o.RefundedMinor
		p.ChannelTxnID = txnIDOf(o)
		p.Metadata = metaOf(o)
	}
	return p
}

func txnIDOf(o *Order) string {
	if o.ChannelTxnID == nil {
		return ""
	}
	return *o.ChannelTxnID
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func formatID(v int64) string { return strconv.FormatInt(v, 10) }

func ptr[T any](v T) *T { return &v }
