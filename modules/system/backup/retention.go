package backup

import (
	"sort"
	"time"
)

// selectExpired 按保留策略挑出该清理的记录（纯函数，便于测试）：
//   - keepCount > 0：成功的备份只保留最新的 N 份；
//   - keepDays  > 0：创建时间早于 N 天前的（成功或失败）都清理；
//   - 两个都是 0 = 不清理；运行中的记录永远不动；
//   - 至少保留最新的一份成功备份，避免策略配错后把唯一的备份也清掉。
func selectExpired(recs []Record, now time.Time, keepDays, keepCount int) []Record {
	if keepDays <= 0 && keepCount <= 0 {
		return nil
	}
	var ok, other []Record
	for _, r := range recs {
		switch r.Status {
		case StatusSuccess:
			ok = append(ok, r)
		case StatusRunning:
		default:
			other = append(other, r)
		}
	}
	sort.SliceStable(ok, func(i, j int) bool { return ok[i].CreatedAt.After(ok[j].CreatedAt) })
	cutoff := now.AddDate(0, 0, -keepDays)
	var out []Record
	for i, r := range ok {
		if i == 0 {
			continue // 最新一份成功备份始终保留
		}
		if (keepCount > 0 && i >= keepCount) || (keepDays > 0 && r.CreatedAt.Before(cutoff)) {
			out = append(out, r)
		}
	}
	for _, r := range other {
		if keepDays > 0 && r.CreatedAt.Before(cutoff) {
			out = append(out, r)
		}
	}
	return out
}

// nextRun 计算下一次定时备份时间；schedule 为空返回 nil。今天的时刻已过或今天已经抢占执行过，则是明天。
func nextRun(schedule string, now time.Time, lastRunDate string) *time.Time {
	if schedule == "" {
		return nil
	}
	hm, err := time.Parse("15:04", schedule)
	if err != nil {
		return nil
	}
	t := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, now.Location())
	if !now.Before(t) || lastRunDate == now.Format("2006-01-02") {
		t = t.AddDate(0, 0, 1)
	}
	return &t
}

// dueNow 今天的计划时刻是否已到。
func dueNow(schedule string, now time.Time) bool {
	hm, err := time.Parse("15:04", schedule)
	if err != nil {
		return false
	}
	return !now.Before(time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, now.Location()))
}
