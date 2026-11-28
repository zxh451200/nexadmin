package recycle

import (
	"context"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/tenant"
	"nexadmin/modules/internal/recbin"
)

const retentionBatch = 200

// sweepLoop 是孤儿记录与保留期清理循环：每天清一次源行已失效的回收记录，
// 并按 recycle.retention_days 彻底删除过期数据；未配置保留期（<=0）时只清孤儿记录。
func (s *Service) sweepLoop(stop <-chan struct{}) {
	if s.db == nil {
		return
	}
	ctx := tenant.Skip(context.Background())
	t := time.NewTimer(time.Minute) // 启动后缓一分钟再扫第一遍，避开启动高峰
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		s.sweepOnce(ctx)
		t.Reset(24 * time.Hour)
	}
}

// sweepOnce 扫一遍孤儿记录：逐类型取出回收记录，源行不在了（或已不在软删除状态）就清掉记录。
// 查询全部走 GORM API——表名经 recbin.Tables 白名单传入 Table()，条件值参数绑定，不拼 SQL 文本。
// 逐条校验是刻意的：回收站体量有限，每天一次的 N+1 换来三库零方言差异。
func (s *Service) sweepOnce(ctx context.Context) {
	for typ, table := range recbin.Tables {
		var entries []recbin.Entry
		if err := s.db.WithContext(tenant.Skip(ctx)).Where("type = ?", typ).Find(&entries).Error; err != nil {
			s.warn("recycle: 读取回收记录失败", zap.String("type", typ), zap.Error(err))
			continue
		}
		orphanIDs := make([]int64, 0, len(entries))
		broken := false
		for _, e := range entries {
			var n int64
			if err := s.db.WithContext(tenant.Skip(ctx)).Table(table).
				Where("id = ? AND deleted_at IS NOT NULL", int64(e.RefID)).Count(&n).Error; err != nil {
				s.warn("recycle: 校验源行失败", zap.String("type", typ), zap.Error(err))
				broken = true
				break // 源表读不出来时不删任何记录：宁可多留，不可误删
			}
			if n == 0 {
				orphanIDs = append(orphanIDs, int64(e.ID))
			}
		}
		if broken || len(orphanIDs) == 0 {
			continue
		}
		if err := s.db.WithContext(tenant.Skip(ctx)).Where("id IN ?", orphanIDs).Delete(&recbin.Entry{}).Error; err != nil {
			s.warn("recycle: 清理孤儿记录失败", zap.String("type", typ), zap.Error(err))
			continue
		}
		if s.log != nil {
			s.log.Info("recycle: 已清理孤儿回收记录", zap.String("type", typ), zap.Int64("count", int64(len(orphanIDs))))
		}
	}
	s.purgeExpired(ctx)
}

func (s *Service) purgeExpired(ctx context.Context) {
	if s.retentionDays <= 0 {
		return
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -s.retentionDays)
	total := int64(0)
	for {
		var entries []recbin.Entry
		if err := s.db.WithContext(ctx).Where("deleted_at < ?", cutoff).
			Order("id").Limit(retentionBatch).Find(&entries).Error; err != nil {
			s.warn("recycle: 读取过期回收记录失败", zap.Error(err))
			return
		}
		if len(entries) == 0 {
			break
		}
		resolved := int64(0)
		for _, e := range entries {
			if err := s.Purge(ctx, e.ID, true); err != nil && !isNotFoundErr(err) {
				s.warn("recycle: 彻底删除过期数据失败", zap.String("type", e.Type), zap.Int64("refId", int64(e.RefID)), zap.Error(err))
				continue
			}
			resolved++
			total++
		}
		if resolved == 0 || len(entries) < retentionBatch {
			break
		}
	}
	if total > 0 && s.log != nil {
		s.log.Info("recycle: 已按保留期彻底删除数据", zap.Int("days", s.retentionDays), zap.Int64("count", total))
	}
}

func (s *Service) warn(msg string, fields ...zap.Field) {
	if s.log != nil {
		s.log.Warn(msg, fields...)
	}
}
