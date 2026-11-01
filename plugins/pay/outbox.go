package pay

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
)

// 出站箱状态：pending = 待发布；sent = 已成功进入进程内总线（webhook 下游有
// 自己的投递记录与重试，见 sys_openapi_delivery）；dead = 超过重试上限或载荷
// 不可解析的死信，dispatcher 不再取走，等人工重投或丢弃；discarded = 人工确认丢弃。
//
// 死信存在的意义：此前毒丸载荷会被静默标成 sent，「事件丢了」和「事件投出去了」
// 在库里长得一模一样，资金事件不允许这种伪装。
const (
	OutboxPending    = "pending"
	OutboxSent       = "sent"
	OutboxDead       = "dead"
	OutboxDiscarded  = "discarded"
	outboxMaxAttempt = 12 // 退避封顶 10 分钟后，12 次已覆盖约 1.5 小时的总线不可用
)

// publish 把已落出站箱的事件发布到进程内总线。返回 false 表示总线缓冲满被丢弃，
// dispatcher 会按退避重投——这正是 outbox 存在的意义（core/event 非持久、满即丢）。
// 注入了 publisher 时用注入的出口（测试要的就是「连续拒绝」这条路径）。
func (s *Service) publish(ctx context.Context, data EventData) bool {
	if s.publisher != nil {
		return s.publisher(ctx, data.EventName, data)
	}
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

// flushOutbox 取出到期的 pending 事件逐条重投；投不出去累计 attempts 到上限转死信，
// 载荷不可解析的毒丸直接转死信并留 last_error——两者都不许再伪装成已投递。
func (s *Service) flushOutbox(ctx context.Context) error {
	var rows []Outbox
	if err := s.db.WithContext(ctx).
		Where("status = ? AND next_retry_at <= ?", OutboxPending, time.Now().UTC()).
		Order("id").Limit(200).Find(&rows).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	dead := 0
	for i := range rows {
		row := &rows[i]
		// 先抢占：多实例会同时扫到同一条 pending，用 next_retry_at 当租约，只有抢到的实例投递。
		claim := s.db.WithContext(ctx).Model(&Outbox{}).
			Where("id = ? AND status = ? AND next_retry_at <= ?", int64(row.ID), OutboxPending, now).
			Update("next_retry_at", now.Add(outboxLease))
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			continue
		}
		var data EventData
		if err := json.Unmarshal([]byte(row.Payload), &data); err != nil {
			s.markOutboxDead(ctx, row, "载荷不可解析: "+err.Error(), now)
			dead++
			continue
		}
		if s.publish(ctx, data) {
			s.db.WithContext(ctx).Model(&Outbox{}).Where("id = ? AND status = ?", int64(row.ID), OutboxPending).
				Updates(map[string]any{"status": OutboxSent, "sent_at": now, "last_error": ""})
			continue
		}
		attempts := row.Attempts + 1
		if attempts >= outboxMaxAttempt {
			s.markOutboxDead(ctx, row, "进程内总线连续拒绝投递，已达重试上限", now)
			dead++
			continue
		}
		s.db.WithContext(ctx).Model(&Outbox{}).Where("id = ?", int64(row.ID)).
			Updates(map[string]any{
				"attempts": attempts, "next_retry_at": now.Add(outboxBackoff(attempts)),
				"last_error": truncate("总线缓冲满，第 "+strconv.Itoa(attempts)+" 次重投失败", 500),
			})
	}
	// 死信必须无条件留痕：否则「一条都没转死信」与「没人来看」在日志里长得一样。
	if dead > 0 {
		s.logWarn("pay: outbox moved to dead", "count", dead, "hint", "管理端「资金事件出站箱」可查看并重投")
	}
	return nil
}

// markOutboxDead 把出站箱行转入死信并记录原因。
func (s *Service) markOutboxDead(ctx context.Context, row *Outbox, reason string, now time.Time) {
	if err := s.db.WithContext(ctx).Model(&Outbox{}).
		Where("id = ? AND status = ?", int64(row.ID), OutboxPending).
		Updates(map[string]any{"status": OutboxDead, "dead_at": now, "last_error": truncate(reason, 500)}).Error; err != nil {
		s.logWarn("pay: mark outbox dead failed", "eventId", row.EventID, "err", err.Error())
	}
	s.logWarn("pay: outbox event dead", "eventId", row.EventID, "eventName", row.EventName, "reason", reason)
}

// OutboxCounts 是各状态出站箱的行数，用于管理端页签上的告警徽标。
type OutboxCounts struct {
	Pending   int64 `json:"pending"`
	Dead      int64 `json:"dead"`
	Sent      int64 `json:"sent"`
	Discarded int64 `json:"discarded"`
}

// CountOutbox 按状态统计出站箱（管理端页签上的告警徽标；请求上下文自带租户过滤）。
func (s *Service) CountOutbox(ctx context.Context) (OutboxCounts, error) {
	var rows []struct {
		Status string
		Cnt    int64
	}
	db := s.db.WithContext(ctx).Model(&Outbox{}).Select("status", "COUNT(*) AS cnt").Group("status")
	if err := db.Scan(&rows).Error; err != nil {
		return OutboxCounts{}, err
	}
	var c OutboxCounts
	for _, r := range rows {
		switch r.Status {
		case OutboxPending:
			c.Pending = r.Cnt
		case OutboxDead:
			c.Dead = r.Cnt
		case OutboxSent:
			c.Sent = r.Cnt
		case OutboxDiscarded:
			c.Discarded = r.Cnt
		}
	}
	return c, nil
}

// ListOutbox 分页列出出站箱；status 为空列全部（载荷只在详情里回显）。
// 租户过滤依赖请求上下文：出站箱是资金事件，跨租户可见等于泄漏别的租户的流水。
func (s *Service) ListOutbox(ctx context.Context, status, keyword string, page, pageSize int) ([]Outbox, int64, error) {
	db := s.db.WithContext(ctx).Model(&Outbox{})
	if status != "" {
		db = db.Where("status = ?", status)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("event_id LIKE ? OR event_name LIKE ?", like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Outbox
	if err := db.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// GetOutbox 按事件幂等键取一条出站箱（含载荷，供排障详情用）。
func (s *Service) GetOutbox(ctx context.Context, eventID string) (*Outbox, error) {
	var row Outbox
	if err := s.db.WithContext(ctx).Where("event_id = ?", eventID).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound(errNotFound)
		}
		return nil, err
	}
	return &row, nil
}

// RetryOutbox 人工重投一条死信：清零计数并立刻入队。已是 pending/sent 的行不再改动。
func (s *Service) RetryOutbox(ctx context.Context, eventID string) (*Outbox, error) {
	return s.transitionOutbox(ctx, eventID, []string{OutboxDead}, map[string]any{
		"status": OutboxPending, "attempts": 0, "next_retry_at": time.Now().UTC(),
		"last_error": "", "dead_at": nil,
	})
}

// DiscardOutbox 人工确认丢弃一条死信：置为 discarded，dispatcher 不再取走。
func (s *Service) DiscardOutbox(ctx context.Context, eventID, note string) (*Outbox, error) {
	return s.transitionOutbox(ctx, eventID, []string{OutboxDead}, map[string]any{
		"status": OutboxDiscarded, "last_error": truncate("人工丢弃: "+note, 500),
	})
}

// transitionOutbox 在「当前状态属于 from」的前提下改写出站箱，返回改后的行。
// 按 event_id 定位而不是按主键：行主键是 BIGINT 分布式 ID，管理端只可能拿到事件号。
func (s *Service) transitionOutbox(ctx context.Context, eventID string, from []string, upd map[string]any) (*Outbox, error) {
	var row Outbox
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("event_id = ?", eventID).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound(errNotFound)
			}
			return err
		}
		changed := tx.Model(&Outbox{}).Where("id = ? AND status IN ?", int64(row.ID), from).Updates(upd)
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return response.Conflict(errStateConflict)
		}
		return tx.Where("id = ?", int64(row.ID)).Take(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// outboxLease 是投递期间的占位时长；进程崩溃时租约到期后其它实例自动接手。
const outboxLease = time.Minute

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
