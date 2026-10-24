package message

import (
	"time"

	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type seedMsg struct {
	category, title, content, link string
	minutesAgo                     int
	read                           bool
}

// 与前端 message.mock.ts 一致的演示消息。
var seedMsgs = []seedMsg{
	{"security", "检测到异地登录", "您的账号于 10 分钟前在 上海 (IP 203.0.113.24) 登录。如非本人操作，请立即修改密码。", "/profile", 10, false},
	{"todo", "待审批：张伟的请假申请", "张伟提交了 3 天年假申请（10/20 - 10/22），等待您审批。", "/dashboard", 35, false},
	{"business", "订单 #20261017-0098 已发货", "Order #20261017-0098 has been shipped via SF Express. Tracking no. SF1029384756.", "", 80, false},
	{"system", "系统维护通知", "系统将于本周六 22:00 - 24:00 进行升级维护，期间部分功能不可用。", "/content/notice", 150, false},
	{"todo", "待办提醒：季度报表待提交", "财务季度报表将于明日 18:00 截止提交，请尽快完成。", "", 260, false},
	{"business", "客户 Acme Corp 已续费", "Acme Corp renewed the Enterprise plan for 12 months. Amount: ¥128,000.", "", 480, false},
	{"security", "密码即将过期", "您的密码将在 5 天后过期，请前往个人中心修改。", "/profile", 700, false},
	{"system", "导出任务已完成", "您发起的用户列表导出任务已完成，文件将保留 7 天。", "", 1000, true},
	{"business", "新工单分配给您", "工单 #T-5521「接口 502 报警」已分配给您，优先级：高。", "", 1500, false},
	{"todo", "会议提醒：产品评审 15:00", "Product review starts at 15:00 in Room 3B. Please prepare your slides.", "", 1800, true},
	{"system", "角色权限已更新", "管理员调整了您的角色权限，重新登录后生效。", "", 2400, true},
	{"security", "账号已绑定新设备", "您的账号已在 iPhone 15 (Safari) 上登录并绑定。", "/profile", 3000, true},
	{"business", "合同 CT-2026-044 待签署", "合同 CT-2026-044 已完成法务审核，等待您签署。", "", 4000, false},
	{"system", "Welcome to NexAdmin", "Your account has been activated. Explore the dashboard to get started.", "/dashboard", 5000, true},
	{"todo", "待办提醒：本周周报", "请于本周五前提交周报。", "", 6200, true},
	{"business", "库存预警：SKU-8841", "SKU-8841 库存低于安全线（剩余 12 件），建议补货。", "", 7400, true},
	{"system", "数据备份成功", "每日 03:00 自动备份已完成，大小 2.4 GB。", "", 8600, true},
	{"security", "登录失败次数过多", "您的账号在 5 分钟内连续登录失败 4 次，已临时提醒。", "/profile", 10000, true},
	{"business", "发票已开具", "发票 INV-20261009-17 已开具并发送至您的邮箱。", "", 12000, true},
	{"system", "版本更新：v1.3.0", "新增消息中心与公告已读统计，详情见更新日志。", "/content/notice", 14400, true},
}

// seed 给默认租户里第一个用户种入演示消息；该用户已有消息就跳过。
// 向导的执行顺序是“建表 → 种子 → 创建管理员”，全新安装时这里还没有用户，所以会直接跳过，
// 演示消息只在“已有用户的库”上生效（例如升级后补种）。
func seed(db *gorm.DB) error {
	ctx := tenant.Skip(db.Statement.Context)
	db = db.WithContext(ctx)
	var uid int64
	if err := db.Table("sys_user").Select("id").Where("tenant_id = ? AND deleted_at IS NULL", int64(tenant.DefaultID)).
		Order("id").Limit(1).Scan(&uid).Error; err != nil || uid == 0 {
		return nil
	}
	var c int64
	if err := db.Model(&Message{}).Where("user_id = ?", uid).Count(&c).Error; err != nil || c > 0 {
		return err
	}
	now := time.Now().UTC()
	for _, s := range seedMsgs {
		created := now.Add(-time.Duration(s.minutesAgo) * time.Minute)
		m := Message{UserID: idgen.ID(uid), Category: s.category, Title: s.title, Content: s.content, Link: s.link}
		m.TenantID = tenant.DefaultID
		m.CreatedAt, m.UpdatedAt = created, created
		if s.read {
			t := created.Add(5 * time.Minute)
			m.ReadAt = &t
		}
		if err := db.Create(&m).Error; err != nil {
			return err
		}
	}
	return nil
}
