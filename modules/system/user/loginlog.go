package user

import (
	"context"

	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/database"
	"nexadmin/modules/internal/session"
)

// loginLogRow 对应 sys_login_log（与 log 模块的 LoginLog 同表；这里只写不读，避免模块间互相依赖）。
type loginLogRow struct {
	database.TenantModel
	Username   string
	LoginType  string
	IP         string `gorm:"column:ip"`
	Location   string
	Browser    string
	OS         string `gorm:"column:os"`
	Status     string
	FailReason string
}

func (loginLogRow) TableName() string { return "sys_login_log" }

// 登录方式（login_type）取值，与登录日志页的筛选一致。
const (
	loginPassword = "password"
	loginPhone    = "phone"
)

// 失败原因分类：只给分类，不暴露细节（真实原因进服务端日志）。
const (
	reasonCredentials = "credentials"
	reasonLocked      = "locked"
	reasonDisabled    = "disabled"
	reasonBlocked     = "blocked"
	reasonOther       = "other"
)

// writeLoginLog 记一条登录日志（尽力而为，写失败只告警，不影响登录结果）。failReason 为空表示成功。
func writeLoginLog(ctx context.Context, d *core.Deps, username, loginType, ip, ua, failReason string) {
	_, browser, os := session.ParseUA(ua)
	row := &loginLogRow{Username: clip(username, 64), LoginType: loginType, IP: clip(ip, 64), Browser: clip(browser, 64), OS: clip(os, 64), Status: "success"}
	if failReason != "" {
		row.Status, row.FailReason = "failed", failReason
	}
	if err := d.DB.WithContext(ctx).Create(row).Error; err != nil {
		d.Log.Warn("write login log failed", zap.Error(err))
	}
}
