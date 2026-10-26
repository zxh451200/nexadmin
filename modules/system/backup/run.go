package backup

import (
	"context"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/ctl"
	"nexadmin/pkg/idgen"
)

// Start 立即开始一次备份（异步执行），返回 status=running 的记录；页面轮询列表看结果。
// 工具不可用返回 *UnavailableError（定时来源会额外留一条失败记录，页面上能看到原因）；
// 本实例或其他实例已有备份在跑返回 ErrBusy。
func (s *Service) Start(ctx context.Context, source string, by idgen.ID) (*Record, error) {
	now := s.now().UTC()
	rec := &Record{
		FileName: ctl.BackupFileName(s.dumper.Engine(), s.version, now), Engine: s.dumper.Engine(),
		Version: s.version, Source: source, CreatedBy: by,
	}
	rec.CreatedAt = now
	if _, err := s.dumper.Check(); err != nil {
		if source == SourceSchedule {
			rec.Status, rec.Error = StatusFailed, clipErr(err.Error())
			_ = s.db.WithContext(ctx).Create(rec).Error
		}
		return nil, &UnavailableError{Reason: err.Error()}
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	s.running = true
	s.mu.Unlock()
	release := func() { s.mu.Lock(); s.running = false; s.mu.Unlock() }

	// 其他实例正在备份（记录里有未超时的 running）也算忙
	var n int64
	if err := s.db.WithContext(ctx).Model(&Record{}).Where("status = ? AND created_at > ?", StatusRunning, now.Add(-staleAfter)).Count(&n).Error; err != nil {
		release()
		return nil, err
	}
	if n > 0 {
		release()
		return nil, ErrBusy
	}
	rec.Status = StatusRunning
	if err := s.db.WithContext(ctx).Create(rec).Error; err != nil {
		release()
		return nil, err
	}
	go func() {
		defer release()
		s.execute(rec)
	}()
	return rec, nil
}

// execute 真正做备份并回写记录；之后按保留策略清理。
func (s *Service) execute(rec *Record) {
	ctx, cancel := context.WithTimeout(context.Background(), dumpTimeout)
	defer cancel()
	start := time.Now()
	upd := map[string]any{}
	path, err := ctl.BackupPath(s.dir, rec.FileName)
	var res ctl.DumpResult
	if err == nil {
		res, err = s.dumper.Dump(ctx, path)
	}
	upd["duration_ms"] = time.Since(start).Milliseconds()
	if err != nil {
		upd["status"], upd["error"] = StatusFailed, clipErr(err.Error())
		s.log.Warn("backup: failed", zap.String("file", rec.FileName), zap.Error(err))
	} else {
		upd["status"], upd["size"], upd["sha256"] = StatusSuccess, res.Size, res.SHA256
	}
	if err := s.db.WithContext(context.Background()).Model(&Record{}).Where("id = ?", rec.ID).Updates(upd).Error; err != nil {
		s.log.Error("backup: update record failed", zap.Error(err))
	}
	if n, err := s.ApplyRetention(context.Background()); err != nil {
		s.log.Warn("backup: retention failed", zap.Error(err))
	} else if n > 0 {
		s.log.Info("backup: retention removed old backups", zap.Int("count", n))
	}
}

// ---- 定时 ----

// Loop 每 30 秒检查一次计划；ctx 结束时退出。启动时先补齐设置行、把中断的 running 记录标成失败。
func (s *Service) Loop(ctx context.Context) {
	if err := s.EnsureSetting(ctx); err != nil {
		s.log.Warn("backup: ensure setting failed", zap.Error(err))
	}
	s.db.WithContext(ctx).Model(&Record{}).
		Where("status = ? AND created_at < ?", StatusRunning, s.now().UTC().Add(-staleAfter)).
		Updates(map[string]any{"status": StatusFailed, "error": "进程中断，备份未完成"})
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick 做一次计划检查：到点且今天还没人执行过，就用数据库里的一次条件更新抢占（rows=1 才执行），
// 多实例同时到点时只有一个会成功。
func (s *Service) Tick(ctx context.Context) {
	set, err := s.Setting(ctx)
	if err != nil {
		s.log.Warn("backup: load setting failed", zap.Error(err))
		return
	}
	now := s.now().In(s.loc)
	today := now.Format("2006-01-02")
	if set.Schedule == "" || !dueNow(set.Schedule, now) || set.LastRunDate == today {
		return
	}
	res := s.db.WithContext(ctx).Model(&Setting{}).
		Where("id = ? AND (last_run_date IS NULL OR last_run_date <> ?)", settingID, today).
		Update("last_run_date", today)
	if res.Error != nil || res.RowsAffected != 1 {
		return // 出错或已被其他实例抢占
	}
	if _, err := s.Start(ctx, SourceSchedule, 0); err != nil {
		s.log.Warn("backup: scheduled backup not started", zap.Error(err))
	}
}
