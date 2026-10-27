package pay

import (
	"context"
	"encoding/json"
	"time"

	"nexadmin/core/event"
	"nexadmin/core/tenant"
)

// 出站箱状态：pending = 待发布；sent = 已成功进入进程内总线（webhook 下游有
// 自己的投递记录与重试，见 sys_openapi_delivery）。
const (
	OutboxPending = "pending"
	OutboxSent    = "sent"
)

// publish 把已落出站箱的事件发布到进程内总线。返回 false 表示总线缓冲满被丢弃，
// dispatcher 会按退避重投——这正是 outbox 存在的意义（core/event 非持久、满即丢）。
func (s *Service) publish(ctx context.Context, data EventData) bool {
	return event.Default().Publish(ctx, data.EventName, data)
}

// emit 在业务事务提交后调用：入队成功则把出站箱标记为已投，失败留给 dispatcher。
func (s *Service) emit(ctx context.Context, data *EventData) {
	if data == nil {
		return
	}
	if s.publish(ctx, *data) {
		s.markOutboxSent(ctx, data.EventID)
	}
}

// markOutboxSent 按事件幂等键把出站箱行标记为已投。
func (s *Service) markOutboxSent(ctx context.Context, eventID string) {
	now := time.Now().UTC()
	if err := s.db.WithContext(tenant.Skip(ctx)).Model(&Outbox{}).
		Where("event_id = ? AND status = ?", eventID, OutboxPending).
		Updates(map[string]any{"status": OutboxSent, "sent_at": now}).Error; err != nil {
		s.logWarn("pay: mark outbox sent failed", "eventId", eventID, "err", err.Error())
	}
}

// runOutboxDispatcher 周期性重投未送达的资金事件。ctx 取消时退出。
func (s *Service) runOutboxDispatcher(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// 后台 goroutine 无请求上下文：显式跳过租户隔离，跨租户重投。
			if err := s.flushOutbox(tenant.Skip(context.Background())); err != nil {
				s.logWarn("pay: flush outbox failed", "err", err.Error())
			}
		}
	}
}

// flushOutbox 取出到期的 pending 事件逐条重投；损坏载荷视为毒丸，标记 sent 并告警，
// 避免无限重试堵住队列。
func (s *Service) flushOutbox(ctx context.Context) error {
	var rows []Outbox
	if err := s.db.WithContext(ctx).
		Where("status = ? AND next_retry_at <= ?", OutboxPending, time.Now().UTC()).
		Order("id").Limit(200).Find(&rows).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	for i := range rows {
		row := &rows[i]
		var data EventData
		if err := json.Unmarshal([]byte(row.Payload), &data); err != nil {
			s.logWarn("pay: poison outbox payload", "eventId", row.EventID, "err", err.Error())
			s.db.WithContext(ctx).Model(&Outbox{}).Where("id = ? AND status = ?", int64(row.ID), OutboxPending).
				Updates(map[string]any{"status": OutboxSent, "sent_at": now})
			continue
		}
		if s.publish(ctx, data) {
			s.db.WithContext(ctx).Model(&Outbox{}).Where("id = ? AND status = ?", int64(row.ID), OutboxPending).
				Updates(map[string]any{"status": OutboxSent, "sent_at": now})
			continue
		}
		attempts := row.Attempts + 1
		delay := outboxBackoff(attempts)
		s.db.WithContext(ctx).Model(&Outbox{}).Where("id = ?", int64(row.ID)).
			Updates(map[string]any{"attempts": attempts, "next_retry_at": now.Add(delay)})
	}
	return nil
}

// outboxBackoff 指数退避：1s, 2s, 4s … 封顶 10 分钟。
func outboxBackoff(attempts int) time.Duration {
	if attempts > 10 {
		attempts = 10
	}
	d := time.Duration(1<<uint(attempts)) * time.Second
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}
