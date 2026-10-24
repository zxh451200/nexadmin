package notice

import (
	"time"

	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type seedRow struct {
	n        int
	title    string
	typ, lvl string
	status   string
	content  string
	scope    string
	targets  []idgen.ID
	pinned   bool
	popup    bool
	platform bool
	pubAgoH  int // 发布时间：N 小时前（0 = 未发布）
	expireH  int // 过期：正数 = N 小时后，负数 = N 小时前
	schedH   int // 定时发布：N 小时后
	total    int
}

// 与前端 notice.mock.ts 一致的出厂公告。已读人数来自真实的 sys_notice_read，所以全新安装时都是 0。
var seedRows = []seedRow{
	{1, "系统升级维护通知 / Scheduled maintenance", "notice", "important", StatusPublished,
		"<p>本周六 <strong>22:00 - 24:00</strong> 将进行系统升级，期间服务可能短暂不可用，请提前保存工作。</p><ul><li>影响范围：全部模块</li><li>预计时长：2 小时</li></ul>",
		ScopeAll, nil, true, true, true, 30, 0, 0, 128},
	{2, "新版本发布 / New release available", "announcement", "important", StatusPublished,
		"<p>NexAdmin 新版本已发布,新增回收站、接口监控和登录锁定,欢迎体验。</p>", ScopeAll, nil, true, false, false, 52, 0, 0, 128},
	{3, "2026 年度团建活动报名 / Team building sign-up", "activity", "normal", StatusPublished,
		"<p>年度团建将于 11 月举行，欢迎在 OA 中报名。<em>Sign up before Nov 1.</em></p>", ScopeAll, nil, false, false, false, 100, 24 * 14, 0, 128},
	{4, "研发部技术分享周", "activity", "normal", StatusPublished,
		"<p>研发部本周五下午举办技术分享，主题：前端性能优化与可观测性。</p>", ScopeDept, []idgen.ID{7300000000000000101}, false, false, false, 140, 0, 0, 36},
	{5, "管理员须知：权限审计", "notice", "normal", StatusPublished,
		"<p>请各租户管理员在本月底前完成角色权限审计，并在系统中确认。</p>", ScopeRole, []idgen.ID{7300000000000000011}, false, false, false, 200, 0, 0, 9},
	{6, "十一假期安排", "announcement", "normal", StatusPublished,
		"<p>国庆假期 10 月 1 日至 10 月 7 日，10 月 8 日正常上班。值班安排详见附件。</p>", ScopeAll, nil, false, false, false, 24 * 20, -24 * 5, 0, 128},
	{7, "新功能预告：消息中心上线", "notice", "normal", StatusDraft,
		"<p>消息中心将支持分类、已读未读与全部已读。</p>", ScopeAll, nil, false, false, false, 0, 0, 0, 0},
	{8, "客户回访计划（草稿）", "notice", "normal", StatusDraft,
		"<p>Q4 重点客户回访名单待确认。</p>", ScopeUser, []idgen.ID{7300000000000000201, 7300000000000000202}, false, false, false, 0, 0, 0, 0},
	{9, "定时发布：年终盘点通知", "announcement", "important", StatusDraft,
		"<p>年终盘点将于 12 月开始，请各部门提前准备。</p>", ScopeAll, nil, false, true, false, 0, 0, 72, 0},
	{10, "旧版 API 下线公告", "notice", "normal", StatusRevoked,
		"<p>v1 接口计划下线，现已撤回，延期至下季度。</p>", ScopeAll, nil, false, false, false, 24 * 9, 0, 0, 128},
	{11, "平台公告：多租户计费规则调整", "announcement", "important", StatusPublished,
		"<p>自下月起，套餐计费将按活跃用户数结算，详情请联系平台运营。</p>", ScopeAll, nil, false, false, true, 60, 0, 0, 560},
	{12, "Welcome to NexAdmin", "notice", "normal", StatusPublished,
		"<p>Welcome aboard! Check the <a href=\"/profile\">profile page</a> to finish your setup.</p>", ScopeAll, nil, false, false, false, 24 * 12, 0, 0, 128},
}

// seed 幂等：按 (租户, 标题) 判断，已存在的一律不动。出厂公告放在默认租户下。
func seed(db *gorm.DB) error {
	ctx := tenant.Skip(db.Statement.Context)
	db = db.WithContext(ctx)
	now := time.Now().UTC()
	hours := func(h int) time.Time { return now.Add(time.Duration(h) * time.Hour) }
	for _, r := range seedRows {
		var c int64
		if err := db.Model(&Notice{}).Where("tenant_id = ? AND title = ?", int64(tenant.DefaultID), r.title).Count(&c).Error; err != nil {
			return err
		}
		if c > 0 {
			continue
		}
		created := hours(-24 * (30 - r.n))
		n := Notice{Title: r.title, Type: r.typ, Level: r.lvl, Status: r.status, Content: r.content, Scope: r.scope,
			Pinned: r.pinned, Popup: r.popup, Platform: r.platform, Attachments: "[]", TotalCount: r.total}
		n.ID, n.TenantID = idgen.ID(7412000000000000000+int64(r.n)), tenant.DefaultID
		n.CreatedAt, n.UpdatedAt = created, created
		if r.pubAgoH > 0 {
			t := hours(-r.pubAgoH)
			n.PublishedAt = &t
		}
		if r.expireH != 0 {
			t := hours(r.expireH)
			n.ExpireAt = &t
		}
		if r.schedH > 0 {
			t := hours(r.schedH)
			n.PublishAt = &t
		}
		if err := db.Create(&n).Error; err != nil {
			return err
		}
		for _, t := range r.targets {
			tg := Target{NoticeID: n.ID, TargetID: t}
			tg.TenantID = tenant.DefaultID // Skip 上下文下不会自动填租户，这里显式指定
			if err := db.Create(&tg).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
