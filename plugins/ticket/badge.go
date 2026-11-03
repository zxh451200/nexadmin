package ticket

// 侧栏面板数据：菜单徽标计数与报告人历史工单。都是窄投影只读查询，不发事件、不动状态。

import (
	"context"
	"strings"
	"time"
)

// BadgeCount 是侧边栏菜单徽标：需要坐席注意的未完结数 =
// 无人认领的 open 单（等谁认领）+ 认领给我的未完结单（该我做点什么）。
// pending（等客户回复）也在内——SLA 停表但仍是「我的活」；resolved/closed 不算。
func (s *Service) BadgeCount(ctx context.Context, viewerID int64) (int64, error) {
	var n int64
	// 括号语义靠 AND 优先级保证：未完结 AND ((未认领 AND open) OR 认领给我)。
	err := s.db.WithContext(ctx).Model(&Ticket{}).
		Where("status IN ?", []string{StatusOpen, StatusProcessing, StatusPending}).
		Where("assignee_id = 0 AND status = ? OR assignee_id = ?", StatusOpen, viewerID).
		Count(&n).Error
	return n, err
}

// ReporterHistoryItem 是报告人历史工单的窄投影（详情侧栏用）。
type ReporterHistoryItem struct {
	TicketNo     string    `json:"ticketNo"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	Priority     string    `json:"priority"`
	ReporterName string    `json:"reporterName"`
	CreatedAt    time.Time `json:"createdAt"`
}

// ReporterHistory 返回该工单报告人（按 reporter_id 匹配，邮件进单即邮箱）最近 10 张
// 其他工单：坐席在详情页一眼看出「这人之前提过什么」，避免重复建单与重复背景询问。
// reporter_id 为空（未留联系方式的访客单）没有匹配口径，返回空列表。
func (s *Service) ReporterHistory(ctx context.Context, ticketNo string) ([]ReporterHistoryItem, error) {
	var cur Ticket
	if err := s.db.WithContext(ctx).Select("id", "reporter_id").
		Where("ticket_no = ?", strings.TrimSpace(ticketNo)).Take(&cur).Error; err != nil {
		return nil, err
	}
	if cur.ReporterID == "" {
		return []ReporterHistoryItem{}, nil
	}
	rows := make([]ReporterHistoryItem, 0, 8)
	err := scopeCond(ctx, s.db.WithContext(ctx).Model(&Ticket{})).
		Select("ticket_no", "title", "status", "priority", "reporter_name", "created_at").
		Where("reporter_id = ? AND id <> ?", cur.ReporterID, int64(cur.ID)).
		Order("id DESC").Limit(10).Find(&rows).Error
	return rows, err
}
