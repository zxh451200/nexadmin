package openapi

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/tenant"
)

type gormStore struct{ db *gorm.DB }

func (s gormStore) ActiveHooks(ctx context.Context, tenantID int64) ([]Webhook, error) {
	var out []Webhook
	err := s.db.WithContext(tenant.Skip(ctx)).Where("tenant_id = ? AND enabled = ?", tenantID, true).Find(&out).Error
	return out, err
}

func (s gormStore) Enqueue(ctx context.Context, ds []Delivery) error {
	if len(ds) == 0 {
		return nil
	}
	return s.db.WithContext(tenant.Skip(ctx)).Create(&ds).Error
}

// ClaimDue 取出到期的 pending 投递，并把它们的 next_attempt_at 推后一个租约时间，
// 多实例同时运行时同一条只会被一个实例认领（UPDATE 的行锁 + 条件重判）。
func (s gormStore) ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Delivery, error) {
	ctx = tenant.Skip(ctx)
	var ids []int64
	err := s.db.WithContext(ctx).Model(&Delivery{}).Where("status = ? AND next_attempt_at <= ?", StatusPending, now).
		Order("next_attempt_at ASC").Limit(limit).Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	var out []Delivery
	for _, id := range ids {
		res := s.db.WithContext(ctx).Model(&Delivery{}).
			Where("id = ? AND status = ? AND next_attempt_at <= ?", id, StatusPending, now).
			UpdateColumn("next_attempt_at", now.Add(lease))
		if res.Error != nil {
			return out, res.Error
		}
		if res.RowsAffected != 1 {
			continue // 被别的实例抢走
		}
		var d Delivery
		if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return out, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (s gormStore) Webhook(ctx context.Context, id int64) (*Webhook, error) {
	var w Webhook
	err := s.db.WithContext(tenant.Skip(ctx)).Where("id = ?", id).Take(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (s gormStore) Finish(ctx context.Context, d *Delivery) error {
	return s.db.WithContext(tenant.Skip(ctx)).Model(&Delivery{}).Where("id = ?", int64(d.ID)).Updates(map[string]any{
		"status": d.Status, "attempts": d.Attempts, "next_attempt_at": d.NextAttemptAt,
		"response_status": d.ResponseStatus, "response_body": d.ResponseBody, "error_msg": d.ErrorMsg,
		"duration_ms": d.DurationMs, "delivered_at": d.DeliveredAt,
	}).Error
}

func (s gormStore) Cleanup(ctx context.Context, before time.Time) (int64, error) {
	res := s.db.WithContext(tenant.Skip(ctx)).Where("created_at < ?", before).Delete(&Delivery{})
	return res.RowsAffected, res.Error
}

// dispatcher 订阅事件总线：把事件写成每个匹配 Webhook 的待投递记录，然后唤醒 worker。
type dispatcher struct {
	st   store
	log  *zap.Logger
	wake func()
	now  func() time.Time
}

func (d *dispatcher) Handle(ev event.Event) {
	if ev.TenantID == 0 {
		return // 无法确定租户的事件不外发，避免跨租户泄露
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hooks, err := d.st.ActiveHooks(ctx, int64(ev.TenantID))
	if err != nil {
		d.log.Warn("openapi: load webhooks failed", zap.Error(err), zap.String("event", ev.Name))
		return
	}
	var match []Webhook
	for _, h := range hooks {
		for _, name := range h.EventList() {
			if name == ev.Name {
				match = append(match, h)
				break
			}
		}
	}
	if len(match) == 0 {
		return
	}
	data, err := event.Scrub(ev.Data) // 最后一道防线：剔除名字像密码/密钥/令牌/哈希的字段
	if err != nil {
		d.log.Warn("openapi: encode event payload failed", zap.Error(err), zap.String("event", ev.Name))
		return
	}
	body, err := json.Marshal(envelope{ID: ev.ID, Event: ev.Name, CreatedAt: ev.CreatedAt, TenantID: ev.TenantID, Data: data})
	if err != nil {
		d.log.Warn("openapi: encode event failed", zap.Error(err), zap.String("event", ev.Name))
		return
	}
	now := d.now().UTC()
	rows := make([]Delivery, 0, len(match))
	for _, h := range match {
		row := Delivery{WebhookID: h.ID, EventID: ev.ID, Event: ev.Name, Payload: string(body), Status: StatusPending, NextAttemptAt: now}
		row.TenantID = ev.TenantID
		rows = append(rows, row)
	}
	if err := d.st.Enqueue(ctx, rows); err != nil {
		d.log.Warn("openapi: enqueue deliveries failed", zap.Error(err), zap.String("event", ev.Name))
		return
	}
	d.wake()
}
