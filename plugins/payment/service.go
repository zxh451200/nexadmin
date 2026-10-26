package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Service is the payment business layer. Host modules use it via the
// package-level functions in client.go.
type Service struct {
	db  *gorm.DB
	log logger
}

// logger is the minimal logging surface used by Service (easy to stub in tests).
type logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NewService creates the payment service.
func NewService(db *gorm.DB, log logger) *Service { return &Service{db: db, log: log} }

// CreateInput is the input for starting a payment. BizType/BizNo form the
// host-side idempotency key (e.g. "trade_group"/"123"). Amounts always come
// from server-side calculation; never pass through client-supplied amounts.
type CreateInput struct {
	BizType     string
	BizNo       string
	Subject     string
	AmountMinor int64
	Currency    string
	Channel     string // empty = first enabled channel by sort
	ReturnURL   string // where the business frontend jumps back after payment
	ExpireSec   int    // 0 = default 30 minutes
}

const defaultExpireSec = 30 * 60

// Create creates (or idempotently fetches) a pending payment order and
// returns the pay URL.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Order, string, error) {
	in.BizType, in.BizNo = strings.TrimSpace(in.BizType), strings.TrimSpace(in.BizNo)
	in.Subject, in.ReturnURL = strings.TrimSpace(in.Subject), strings.TrimSpace(in.ReturnURL)
	switch {
	case in.BizType == "" || in.BizNo == "":
		return nil, "", response.BadRequest("payment.biz_required")
	case in.AmountMinor <= 0:
		return nil, "", response.BadRequest("payment.amount_invalid")
	}
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if in.ExpireSec <= 0 {
		in.ExpireSec = defaultExpireSec
	}

	// Idempotency: same (biz_type, biz_no) returns the existing order
	// (re-fetching pay params while still pending).
	var existing Order
	err := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&existing).Error
	if err == nil {
		payURL := ""
		if existing.Status == StatusPending {
			if payURL, err = s.payURL(ctx, &existing, in.ReturnURL); err != nil {
				return nil, "", err
			}
		}
		return &existing, payURL, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", err
	}

	ch, err := s.pickChannel(ctx, in.Channel)
	if err != nil {
		return nil, "", err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, "", err
	}
	o := &Order{
		PaymentNo: "PAY" + formatID(int64(id)),
		BizType:   in.BizType, BizNo: in.BizNo, Subject: subjectOr(in.Subject),
		AmountMinor: in.AmountMinor, Currency: strings.ToUpper(in.Currency),
		Channel: ch.Code, Status: StatusPending,
		ExpireAt: ptr(time.Now().UTC().Add(time.Duration(in.ExpireSec) * time.Second)),
	}
	o.ID = id
	if err := s.db.WithContext(ctx).Create(o).Error; err != nil {
		// Concurrent create with the same idempotency key hit the unique
		// index: read back and return the winner.
		var dup Order
		if take := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&dup).Error; take == nil {
			payURL := ""
			if dup.Status == StatusPending {
				if payURL, err = s.payURL(ctx, &dup, in.ReturnURL); err != nil {
					return nil, "", err
				}
			}
			return &dup, payURL, nil
		}
		return nil, "", err
	}
	payURL, err := s.payURL(ctx, o, in.ReturnURL)
	if err != nil {
		return nil, "", err
	}
	return o, payURL, nil
}

// payURL asks the channel for pay params. Provider.Create must be idempotent
// by paymentNo.
func (s *Service) payURL(ctx context.Context, o *Order, returnURL string) (string, error) {
	p, cfg, err := s.channelOf(ctx, o.Channel)
	if err != nil {
		return "", err
	}
	params, err := p.Create(ctx, PayRequest{
		PaymentNo: o.PaymentNo, Subject: o.Subject, AmountMinor: o.AmountMinor,
		Currency: o.Currency, ReturnURL: returnURL,
	}, cfg)
	if err != nil {
		return "", err
	}
	return params.PayURL, nil
}

// HandleCallback processes one channel callback (or mock cashier trigger):
// verify signature -> audit log -> idempotent transition -> publish event.
// Duplicate callbacks (success again after paid) are not errors and do not
// re-publish events.
func (s *Service) HandleCallback(ctx context.Context, in CallbackInput) (*Order, error) {
	ch, err := s.loadChannel(ctx, in.Channel)
	if err != nil {
		return nil, err
	}
	cfg, err := decodeConfig(ch.ConfigJSON)
	if err != nil {
		return nil, err
	}
	p, err := LookupProvider(ch.Code)
	if err != nil {
		return nil, err
	}
	res, verr := p.VerifyCallback(in, cfg)
	payload := truncate(string(in.Body), 4000)
	if verr != nil {
		s.recordCallback(ctx, ch.Code, "", false, "invalid_signature: "+verr.Error(), payload)
		return nil, response.BadRequest("payment.callback_invalid")
	}
	o, changed, err := s.transition(ctx, res)
	result := "duplicate"
	if changed {
		result = "accepted"
	} else if err == nil && o != nil && o.Status != StatusPending {
		result = "ignored_state_" + o.Status
	}
	s.recordCallback(ctx, ch.Code, res.PaymentNo, true, result, payload)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// transition moves a pending order to paid/failed under a row lock; any other
// current state is left untouched (idempotent). Events fire only on change.
func (s *Service) transition(ctx context.Context, res *CallbackResult) (*Order, bool, error) {
	var o Order
	changed := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("payment_no = ?", res.PaymentNo).Take(&o).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("payment.order_not_found")
			}
			return err
		}
		if o.Status != StatusPending {
			return nil
		}
		now := time.Now().UTC()
		updates := map[string]any{}
		switch {
		case res.Paid:
			updates["status"] = StatusPaid
			updates["channel_txn_id"] = res.ChannelTxnID
			updates["paid_at"] = now
		case res.Failed:
			updates["status"] = StatusFailed
			updates["fail_reason"] = truncate(res.Reason, 200)
		default:
			return nil
		}
		if err := tx.Model(&Order{}).Where("id = ?", int64(o.ID)).Updates(updates).Error; err != nil {
			return err
		}
		o.Status = updates["status"].(string)
		if txn, ok := updates["channel_txn_id"].(string); ok {
			o.ChannelTxnID = txn
		}
		changed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if changed {
		now := time.Now().UTC()
		if o.Status == StatusPaid {
			s.publish(ctx, EventSucceeded, &o, now)
		} else {
			s.publish(ctx, EventFailed, &o, now)
		}
	}
	return &o, changed, nil
}

// QueryByBiz fetches an order by (biz_type, biz_no); while pending it also
// syncs from the channel once (fallback for lost events).
func (s *Service) QueryByBiz(ctx context.Context, bizType, bizNo string) (*Order, error) {
	var o Order
	if err := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", bizType, bizNo).Take(&o).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("payment.order_not_found")
		}
		return nil, err
	}
	return s.SyncIfPending(ctx, &o)
}

// QueryByNo fetches an order by payment number (admin/refund use).
func (s *Service) QueryByNo(ctx context.Context, paymentNo string) (*Order, error) {
	var o Order
	if err := s.db.WithContext(ctx).Where("payment_no = ?", paymentNo).Take(&o).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("payment.order_not_found")
		}
		return nil, err
	}
	return s.SyncIfPending(ctx, &o)
}

// SyncIfPending asks the channel and compensates while the order is still
// pending (channel-side paid but callback lost).
func (s *Service) SyncIfPending(ctx context.Context, o *Order) (*Order, error) {
	if o.Status != StatusPending {
		return o, nil
	}
	p, cfg, err := s.channelOf(ctx, o.Channel)
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
		ChannelTxnID: st.ChannelTxnID, Reason: st.Reason,
	})
	return upd, err
}

// RefundInput is the input for starting a refund; BizType/BizNo are the
// refund-side idempotency key.
type RefundInput struct {
	PaymentNo   string
	BizType     string
	BizNo       string
	AmountMinor int64 // must be > 0 and <= refundable remainder
	Reason      string
	Operator    string
}

// CreateRefund starts a refund. P0 channels answer synchronously: success
// settles in the same transaction and publishes the event; a rejected refund
// is stored as a failed refund row and returned as Conflict.
func (s *Service) CreateRefund(ctx context.Context, in RefundInput) (*Refund, error) {
	in.PaymentNo, in.BizType, in.BizNo = strings.TrimSpace(in.PaymentNo), strings.TrimSpace(in.BizType), strings.TrimSpace(in.BizNo)
	in.Reason, in.Operator = strings.TrimSpace(in.Reason), strings.TrimSpace(in.Operator)
	switch {
	case in.PaymentNo == "" || in.BizType == "" || in.BizNo == "":
		return nil, response.BadRequest("payment.biz_required")
	case in.AmountMinor <= 0:
		return nil, response.BadRequest("payment.amount_invalid")
	}

	// Idempotency pre-check (checked again inside the transaction).
	var existing Refund
	err := s.db.WithContext(ctx).Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var out Refund
	rejected := ""
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var o Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("payment_no = ?", in.PaymentNo).Take(&o).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("payment.order_not_found")
			}
			return err
		}
		if !Settleable(o.Status) {
			return response.Conflict("payment.order_not_refundable")
		}
		if in.AmountMinor > o.AmountMinor-o.RefundedMinor {
			return response.Conflict("payment.refund_exceeded")
		}
		if err := tx.Where("biz_type = ? AND biz_no = ?", in.BizType, in.BizNo).Take(&out).Error; err == nil {
			return nil // concurrent idempotency: the first refund already exists
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		out = Refund{
			RefundNo: "RF" + formatID(int64(id)), PaymentNo: o.PaymentNo,
			BizType: in.BizType, BizNo: in.BizNo, AmountMinor: in.AmountMinor,
			Currency: o.Currency, Reason: truncate(in.Reason, 200), Operator: truncate(in.Operator, 64),
			Status: RefundPending,
		}
		out.ID = id
		p, cfg, err := s.channelOf(ctx, o.Channel)
		if err != nil {
			return err
		}
		res, err := p.Refund(ctx, RefundRequest{
			RefundNo: out.RefundNo, PaymentNo: o.PaymentNo, AmountMinor: in.AmountMinor,
		}, cfg)
		// P0 channels answer synchronously; async channels will store pending
		// and flip via callback (see provider.Refund).
		if err != nil {
			return err
		}
		if !res.OK {
			rejected = res.Reason
			out.Status = RefundFailed
			return tx.Create(&out).Error
		}
		out.Status = RefundSucceeded
		out.ChannelRefundID = res.ChannelRefundID
		now := time.Now().UTC()
		out.SucceededAt = &now
		refunded := o.RefundedMinor + in.AmountMinor
		status := StatusPartiallyRefunded
		if refunded >= o.AmountMinor {
			status = StatusRefunded
		}
		if err := tx.Model(&Order{}).Where("id = ?", int64(o.ID)).
			Updates(map[string]any{"refunded_minor": refunded, "status": status, "updated_at": now}).Error; err != nil {
			return err
		}
		o.RefundedMinor, o.Status = refunded, status
		return tx.Create(&out).Error
	})
	if err != nil {
		return nil, err
	}
	// Publish by refund result; reload the order snapshot for the payload
	// (failure to reload is logged, never blocking).
	var o Order
	if err := s.db.WithContext(ctx).Where("payment_no = ?", in.PaymentNo).Take(&o).Error; err != nil {
		s.logWarn("payment: reload order after refund failed", "paymentNo", in.PaymentNo, "err", err.Error())
	}
	if out.Status == RefundSucceeded {
		s.publish(ctx, EventRefundOK, &o, time.Now().UTC())
	} else {
		s.publish(ctx, EventRefundFailed, &o, time.Now().UTC())
	}
	if rejected != "" {
		return &out, response.Conflict("payment.refund_rejected").WithMsg(rejected)
	}
	return &out, nil
}

// CloseExpired moves expired pending orders to closed and returns the count.
func (s *Service) CloseExpired(ctx context.Context, now time.Time) (int64, error) {
	var expired []Order
	if err := s.db.WithContext(ctx).
		Where("status = ? AND expire_at IS NOT NULL AND expire_at < ?", StatusPending, now).
		Find(&expired).Error; err != nil {
		return 0, err
	}
	for i := range expired {
		o := &expired[i]
		if err := s.db.WithContext(ctx).Model(&Order{}).Where("id = ? AND status = ?", int64(o.ID), StatusPending).
			Updates(map[string]any{"status": StatusClosed, "updated_at": now}).Error; err != nil {
			return int64(i), err
		}
		s.publish(ctx, EventClosed, o, now)
	}
	return int64(len(expired)), nil
}

// ---- channel management ----

// SaveChannel creates or updates a channel config; Config is encrypted as a
// whole. code must be a registered Provider.
func (s *Service) SaveChannel(ctx context.Context, id idgen.ID, code, name string, enabled bool, sort int, cfg map[string]string) (*Channel, error) {
	code, name = strings.TrimSpace(code), strings.TrimSpace(name)
	if code == "" || name == "" {
		return nil, response.BadRequest("payment.channel_invalid")
	}
	if _, err := LookupProvider(code); err != nil {
		return nil, response.BadRequest("payment.channel_unknown_provider")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	enc, err := secretEncrypt(string(raw))
	if err != nil {
		return nil, err
	}
	var ch Channel
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if id != 0 {
			if err := tx.Take(&ch, id).Error; err != nil {
				return err
			}
			ch.Name, ch.Enabled, ch.Sort, ch.ConfigJSON = name, enabled, sort, enc
			return tx.Model(&Channel{}).Where("id = ?", int64(id)).
				Updates(map[string]any{"name": name, "enabled": enabled, "sort": sort, "config_json": enc}).Error
		}
		ch = Channel{Code: code, Name: name, Enabled: enabled, Sort: sort, ConfigJSON: enc}
		return tx.Create(&ch).Error
	})
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// DeleteChannel deletes a channel config.
func (s *Service) DeleteChannel(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Delete(&Channel{}, id).Error
}

// ListChannels returns all channels ordered by sort, id.
func (s *Service) ListChannels(ctx context.Context) ([]Channel, error) {
	var rows []Channel
	err := s.db.WithContext(ctx).Order("sort, id").Find(&rows).Error
	return rows, err
}

// ---- admin lists ----

// OrderQuery filters the order list.
type OrderQuery struct {
	Keyword  string // fuzzy match on payment no / biz no
	Status   string
	Channel  string
	Page     int
	PageSize int
}

// ListOrders lists payment orders with pagination.
func (s *Service) ListOrders(ctx context.Context, q OrderQuery) ([]OrderView, int64, error) {
	db := s.db.WithContext(ctx).Model(&Order{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("payment_no LIKE ? OR biz_no LIKE ?", like, like)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Channel != "" {
		db = db.Where("channel = ?", q.Channel)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Order
	if err := db.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]OrderView, len(rows))
	for i := range rows {
		out[i] = orderView(&rows[i], "")
	}
	return out, total, nil
}

// CallbackQuery filters the callback audit list.
type CallbackQuery struct {
	Channel  string
	Keyword  string // payment no
	Page     int
	PageSize int
}

// ListCallbacks lists callback audit rows with pagination.
func (s *Service) ListCallbacks(ctx context.Context, q CallbackQuery) ([]CallbackView, int64, error) {
	db := s.db.WithContext(ctx).Model(&Callback{})
	if q.Channel != "" {
		db = db.Where("channel = ?", q.Channel)
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
	out := make([]CallbackView, len(rows))
	for i := range rows {
		out[i] = callbackView(&rows[i])
	}
	return out, total, nil
}

// RefundQuery filters the refund list.
type RefundQuery struct {
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

// ListRefunds lists refunds with pagination.
func (s *Service) ListRefunds(ctx context.Context, q RefundQuery) ([]RefundView, int64, error) {
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
	out := make([]RefundView, len(rows))
	for i := range rows {
		out[i] = refundView(&rows[i])
	}
	return out, total, nil
}

// Close manually closes a pending order (admin action).
func (s *Service) Close(ctx context.Context, paymentNo string) (*Order, error) {
	var o Order
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("payment_no = ?", paymentNo).Take(&o).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("payment.order_not_found")
			}
			return err
		}
		if o.Status != StatusPending {
			return response.Conflict("payment.order_not_closable")
		}
		return tx.Model(&Order{}).Where("id = ?", int64(o.ID)).
			Updates(map[string]any{"status": StatusClosed, "updated_at": time.Now().UTC()}).Error
	})
	if err != nil {
		return nil, err
	}
	o.Status = StatusClosed
	s.publish(ctx, EventClosed, &o, time.Now().UTC())
	return &o, nil
}

// ---- internals ----

func (s *Service) pickChannel(ctx context.Context, code string) (*Channel, error) {
	if code != "" {
		return s.loadChannel(ctx, code)
	}
	var ch Channel
	err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("sort, id").Take(&ch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.Conflict("payment.no_channel")
	}
	return &ch, err
}

func (s *Service) loadChannel(ctx context.Context, code string) (*Channel, error) {
	var ch Channel
	if err := s.db.WithContext(ctx).Where("code = ?", code).Take(&ch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("payment.channel_not_found")
		}
		return nil, err
	}
	return &ch, nil
}

// channelOf resolves channel + decrypted config + provider.
func (s *Service) channelOf(ctx context.Context, code string) (Provider, map[string]string, error) {
	ch, err := s.loadChannel(ctx, code)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := decodeConfig(ch.ConfigJSON)
	if err != nil {
		return nil, nil, err
	}
	p, err := LookupProvider(ch.Code)
	if err != nil {
		return nil, nil, err
	}
	return p, cfg, nil
}

func (s *Service) recordCallback(ctx context.Context, channel, paymentNo string, ok bool, result, payload string) {
	row := Callback{Channel: channel, PaymentNo: paymentNo, OK: ok, Result: truncate(result, 200), Payload: payload}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		s.logWarn("payment: save callback log failed", "err", err.Error())
	}
}

func (s *Service) logWarn(msg string, keysAndValues ...any) {
	if s.log != nil {
		s.log.Warn(msg, keysAndValues...)
	}
}

func decodeConfig(enc string) (map[string]string, error) {
	if enc == "" {
		return map[string]string{}, nil
	}
	plain, err := secretDecrypt(enc)
	if err != nil {
		return nil, fmt.Errorf("payment: decrypt channel config: %w", err)
	}
	cfg := map[string]string{}
	if plain == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, fmt.Errorf("payment: bad channel config: %w", err)
	}
	return cfg, nil
}

func subjectOr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "payment"
	}
	return truncate(s, 200)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func formatID(v int64) string { return strconv.FormatInt(v, 10) }

func ptr[T any](v T) *T { return &v }
