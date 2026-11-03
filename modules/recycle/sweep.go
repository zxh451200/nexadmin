package recycle

import (
	"context"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/tenant"
	"nexadmin/modules/internal/recbin"
)

// sweepLoop 是孤儿记录清理循环：回收记录指向的源行可能已经被物理清掉（管理员在数据库里
// 手工清理、或源模块做了硬删除），这些记录永远无法还原，只会一直躺在列表里。
//
// 每天清一次，只删「源行已不存在」的回收记录本身——绝不自动彻底删除源数据，
// 那是 recycle:delete 权限下的人工决定。stop 关闭（进程优雅关闭）时立即退出。
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
// 逐条校验是刻意的：回收站体量有限（只登记 user/notice 的软删除），每天一次的 N+1 换来三库零方言差异。
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
}

func (s *Service) warn(msg string, fields ...zap.Field) {
	if s.log != nil {
		s.log.Warn(msg, fields...)
	}
}
