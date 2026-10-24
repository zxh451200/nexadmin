package cron

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// Stats 是工作台卡片 cron-overview 用的概览：任务总数、启用数、近 24 小时失败的执行次数（均限当前租户）。
type Stats struct {
	Total     int64 `json:"total"`
	Enabled   int64 `json:"enabled"`
	Failed24h int64 `json:"failed24h"`
}

// Stats 统计当前租户的任务与近 24 小时失败次数；租户隔离由 TenantModel 的 scope 自动追加。
func (s *Service) Stats(ctx context.Context, now time.Time) (Stats, error) {
	var out Stats
	if err := s.db.WithContext(ctx).Model(&Job{}).Count(&out.Total).Error; err != nil {
		return Stats{}, err
	}
	if err := s.db.WithContext(ctx).Model(&Job{}).Where("enabled = ?", true).Count(&out.Enabled).Error; err != nil {
		return Stats{}, err
	}
	if err := s.db.WithContext(ctx).Model(&Log{}).
		Where("status = ? AND started_at >= ?", StatusFailed, now.Add(-24*time.Hour)).Count(&out.Failed24h).Error; err != nil {
		return Stats{}, err
	}
	return out, nil
}

// GET /plugins/cron/stats
func (h *handler) stats(c *gin.Context) {
	v, err := h.svc.Stats(c.Request.Context(), time.Now())
	respond(c, v, err)
}
