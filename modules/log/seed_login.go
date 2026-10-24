package log

import (
	"time"

	"gorm.io/gorm"

	"nexadmin/core/tenant"
)

type loginSeed struct {
	username, loginType, ip, location, browser, os, failReason string
	minutesAgo                                                 int
}

// 与前端 login-log.mock.ts 一致的演示登录日志。failReason 为空表示成功。
var loginSeeds = []loginSeed{
	{"admin", "password", "10.20.1.15", "广东省深圳市", "Chrome 126", "Windows 11", "", 3},
	{"zhang.wei", "wecom", "10.20.3.42", "北京市", "WeCom 4.1", "macOS 14", "", 12},
	{"li.na", "password", "192.168.8.21", "上海市", "Safari 17", "iOS 17", "", 25},
	{"unknown_user", "password", "203.0.113.45", "美国加利福尼亚州", "Firefox 127", "Linux", "credentials", 41},
	{"wang.fang", "phone", "172.16.5.9", "浙江省杭州市", "Chrome 126", "Android 14", "", 58},
	{"sun.yue", "feishu", "10.20.7.8", "四川省成都市", "Feishu 7.2", "Windows 10", "", 80},
	{"admin", "password", "198.51.100.23", "俄罗斯莫斯科", "Chrome 120", "Windows 10", "captcha", 105},
	{"admin", "password", "198.51.100.23", "俄罗斯莫斯科", "Chrome 120", "Windows 10", "locked", 106},
	{"zhao.lei", "dingtalk", "192.168.8.37", "江苏省南京市", "DingTalk 7.6", "macOS 14", "", 150},
	{"john.smith", "google", "172.20.4.66", "美国纽约州", "Chrome 126", "macOS 14", "", 210},
	{"dev-bot", "github", "172.20.4.71", "德国法兰克福", "Edge 126", "Ubuntu 22.04", "", 260},
	{"li.na", "password", "192.168.8.21", "上海市", "Safari 17", "iOS 17", "credentials", 330},
	{"liu.yang", "password", "10.20.9.3", "广东省广州市", "Chrome 125", "Windows 11", "disabled", 400},
	{"chen.hao", "oidc", "10.20.2.19", "湖北省武汉市", "Chrome 126", "Windows 11", "", 520},
	{"zhang.wei", "phone", "10.20.3.42", "北京市", "Safari 17", "iOS 17", "", 700},
	{"guest01", "phone", "223.5.5.5", "浙江省杭州市", "Chrome 126", "Android 13", "blocked", 880},
	{"admin", "password", "10.20.1.15", "广东省深圳市", "Chrome 126", "Windows 11", "", 1300},
	{"wang.fang", "password", "172.16.5.9", "浙江省杭州市", "Firefox 127", "Windows 11", "credentials", 1500},
	{"wang.fang", "password", "172.16.5.9", "浙江省杭州市", "Firefox 127", "Windows 11", "", 1502},
	{"sun.yue", "password", "10.20.7.8", "四川省成都市", "Chrome 126", "Windows 10", "", 2000},
	{"zhao.lei", "wecom", "192.168.8.37", "江苏省南京市", "WeCom 4.1", "Windows 11", "", 2700},
	{"test_user", "password", "45.33.32.156", "美国弗吉尼亚州", "curl 8.4", "Linux", "other", 3500},
	{"li.na", "google", "192.168.8.21", "上海市", "Chrome 126", "macOS 14", "", 5200},
	{"chen.hao", "password", "10.20.2.19", "湖北省武汉市", "Edge 126", "Windows 11", "", 8000},
	{"admin", "password", "10.20.1.15", "广东省深圳市", "Chrome 125", "Windows 11", "", 14000},
}

// seedLogin 幂等：默认租户下已有登录日志就跳过。
func seedLogin(db *gorm.DB) error {
	db = db.WithContext(tenant.Skip(db.Statement.Context))
	var c int64
	if err := db.Model(&LoginLog{}).Where("tenant_id = ?", int64(tenant.DefaultID)).Count(&c).Error; err != nil || c > 0 {
		return err
	}
	now := time.Now().UTC()
	for _, r := range loginSeeds {
		t := now.Add(-time.Duration(r.minutesAgo) * time.Minute)
		l := LoginLog{Username: r.username, LoginType: r.loginType, IP: r.ip, Location: r.location, Browser: r.browser,
			OS: r.os, Status: StatusSuccess, FailReason: r.failReason}
		if r.failReason != "" {
			l.Status = StatusFailed
		}
		l.TenantID, l.CreatedAt, l.UpdatedAt = tenant.DefaultID, t, t
		if err := db.Create(&l).Error; err != nil {
			return err
		}
	}
	return nil
}
