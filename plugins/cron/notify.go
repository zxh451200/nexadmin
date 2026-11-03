package cron

// 任务失败告警：定时任务无人盯着后台，连续重试耗尽还失败就该有人收到站内信。
//
//   - 收件人：持有 cron:log 权限的人（能打开执行历史的人），按租户分组发送
//     （站内信 sys_message 是租户表，与官网留言通知同一套做法）。
//   - 只在重试全部耗尽、最终落败时发一次；重试期间的抖动不打扰人。
//   - 始终发站内信，零配置；邮件/机器人由收件人自己的通知设置决定（core/notify 兜底限频）。

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/notify"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// alertPerm 是收件人条件：持有它的人能看到执行历史。
const alertPerm = "cron:log"

// alertCategory 对应字典 message_category 的 cron 项；缺字典项时站内信分类回退默认。
const alertCategory = "cron"

type alertHolder struct {
	UserID   idgen.ID `gorm:"column:user_id"`
	TenantID idgen.ID `gorm:"column:tenant_id"`
}

// alerter 是任务失败告警器；sender 为 nil（core/notify 未装配）时是安全空操作。
type alerter struct {
	ctx    context.Context
	db     *gorm.DB
	sender msgSender
	log    *zap.Logger

	findHolders func(ctx context.Context) ([]alertHolder, error)
}

// msgSender 是测试缝：core/notify 的 *Service 满足它，nil 时安全空操作。
type msgSender interface {
	Send(ctx context.Context, m notify.Msg) error
}

func newAlerter(ctx context.Context, db *gorm.DB, n msgSender, log *zap.Logger) *alerter {
	if db == nil {
		return nil
	}
	a := &alerter{ctx: ctx, db: db, log: log}
	a.findHolders = a.queryHolders
	a.sender = n
	return a
}

// alert 发一次失败告警；收件人为空或发送失败都只记日志，不影响调度主链路。
func (a *alerter) alert(job Job, row *Log, errText string) {
	if a == nil || a.sender == nil || a.ctx.Err() != nil {
		return
	}
	holders, err := a.findHolders(a.ctx)
	if err != nil {
		a.warn("查询告警收件人失败", zap.Error(err))
		return
	}
	if len(holders) == 0 {
		return
	}
	title := fmt.Sprintf("定时任务失败：%s", job.Name)
	content := fmt.Sprintf("任务「%s」（%s）在第 %d 次尝试后仍然失败：\n\n%s\n\n发生时间：%s\n可到「定时任务 → 执行历史」查看完整日志。",
		job.Name, job.Handler, row.Attempts, errText, row.StartedAt.Format("2006-01-02 15:04:05"))
	byTenant := map[idgen.ID][]idgen.ID{}
	for _, h := range holders {
		byTenant[h.TenantID] = append(byTenant[h.TenantID], h.UserID)
	}
	for tid, ids := range byTenant {
		m := notify.Msg{To: ids, Category: alertCategory, Title: title, Content: content,
			Link: "/system/cron", Channels: []string{notify.ChannelInApp}}
		if err := a.sender.Send(tenant.With(a.ctx, tid), m); err != nil {
			a.warn("发送失败告警", zap.Int64("tenant", int64(tid)), zap.Error(err))
		}
	}
}

func (a *alerter) queryHolders(ctx context.Context) ([]alertHolder, error) {
	db := a.db.WithContext(tenant.Skip(ctx))
	holders := db.Table("sys_user_role AS ur").Select("ur.user_id").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ?", "1").
		Joins("LEFT JOIN sys_role_menu rm ON rm.role_id = r.id").
		Joins("LEFT JOIN sys_menu m ON m.id = rm.menu_id AND m.status = ? AND m.perm = ?", "1", alertPerm).
		Where("r.builtin = ? OR m.id IS NOT NULL", true)
	var rows []alertHolder
	err := db.Table("sys_user AS u").
		Select("DISTINCT u.id AS user_id, u.tenant_id AS tenant_id").
		Where("u.deleted_at IS NULL AND u.status = ?", "1").
		Where("u.is_super = ? OR u.id IN (?)", true, holders).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (a *alerter) warn(msg string, fields ...zap.Field) {
	if a.log != nil {
		a.log.Warn("cron: "+msg, fields...)
	}
}
