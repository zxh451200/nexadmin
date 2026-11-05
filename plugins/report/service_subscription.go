package report

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
)

// 报表订阅：到点给收件人列表发一封文本提醒（标题 = 报表名，链接 = 后台报表中心）。
//
//   - 调度自持（每分钟 tick 一次的 RunDue），不依赖 cron 插件——订阅是报表的附属能力，
//     跨插件编排只会让「装了报表没装 cron」的用户多问一句为什么；
//   - 去重靠 last_sent_date：同一个「自然日」只发一次，服务重启不会重发；
//   - 邮件用通用通知模板；未配置 SMTP 时按跳过处理（与留言通知同语义），
//     站内信渠道不带附件价值有限，留待附件能力落地时一起接。

// 订阅周期。
const (
	SchedDaily  = "daily"
	SchedWeekly = "weekly"
)

// maxRecipients 是单个订阅的收件人上限：防手滑把整个通讯录粘进来。
const maxRecipients = 20

// mailer 是订阅投递需要的最小邮件面；nil = 邮件未装配，发送按跳过处理。
// 附件走带附件的模板发送：正文仍是通知模板，报表里的表格图以 xlsx 附件跟在后面。
type mailer interface {
	SendMailWithAttachments(ctx context.Context, to, tpl, lang string, d mail.Data, atts []mail.Attachment) error
}

// WithMailer 注入邮件出口（Register 时装配；测试传 nil 或自定义实现）。
func (s *Service) WithMailer(m mailer) *Service {
	s.mailer = m
	return s
}

// SubscriptionView 是订阅配置视图。
type SubscriptionView struct {
	ReportID   idgen.ID   `json:"reportId"`
	Schedule   string     `json:"schedule"`
	SendHour   int        `json:"sendHour"`
	Weekday    int        `json:"weekday"`
	Recipients string     `json:"recipients"`
	Enabled    bool       `json:"enabled"`
	LastSentAt *time.Time `json:"lastSentAt,omitempty"`
}

// SubscriptionForm 是订阅配置表单。
type SubscriptionForm struct {
	Schedule   string `json:"schedule"`
	SendHour   int    `json:"sendHour"`
	Weekday    int    `json:"weekday"`
	Recipients string `json:"recipients"`
	Enabled    *bool  `json:"enabled"`
}

// GetSubscription 读某报表的订阅配置；未配置返回 nil。
func (s *Service) GetSubscription(ctx context.Context, reportID idgen.ID) (*SubscriptionView, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var row Subscription
	err = s.db.WithContext(ctx).Where("tenant_id = ? AND report_id = ?", tid, int64(reportID)).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return subscriptionView(&row), nil
}

// SaveSubscription 保存（存在则更新）某报表的订阅配置。recipients 逐个过 mail.ParseAddress，
// 拒绝一切不规范地址——这里存的是「将来要发信的对象」，格式错误应该当场报而不是发送时静默丢。
func (s *Service) SaveSubscription(ctx context.Context, reportID idgen.ID, f SubscriptionForm) (*SubscriptionView, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	if f.Schedule != SchedDaily && f.Schedule != SchedWeekly {
		return nil, response.BadRequest("report.subscription_schedule_invalid")
	}
	if f.SendHour < 0 || f.SendHour > 23 {
		return nil, response.BadRequest("report.subscription_hour_invalid")
	}
	if f.Schedule == SchedWeekly && (f.Weekday < 0 || f.Weekday > 6) {
		return nil, response.BadRequest("report.subscription_weekday_invalid")
	}
	recipients, err := normalizeRecipients(f.Recipients)
	if err != nil {
		return nil, err
	}
	// 报表必须存在且属于本租户（unique index 按 report_id，跨租户同名 id 也进不来）。
	sdb, err := s.scoped(ctx)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := sdb.Model(&ReportDef{}).Where("id = ?", int64(reportID)).Count(&count).Error; err != nil {
		return nil, dbErr(err)
	}
	if count == 0 {
		return nil, notFound()
	}
	enabled := 1
	if f.Enabled != nil && !*f.Enabled {
		enabled = 0
	}
	var row Subscription
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND report_id = ?", tid, int64(reportID)).First(&row).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
			row = Subscription{ReportID: reportID}
		}
		row.Schedule, row.SendHour, row.Weekday = f.Schedule, f.SendHour, f.Weekday
		row.Recipients, row.Enabled = recipients, enabled
		return tx.Save(&row).Error
	})
	if err != nil {
		return nil, dbErr(err)
	}
	return subscriptionView(&row), nil
}

// DeleteSubscription 删除订阅（未配置时按成功处理，删除按钮不必二态）。
func (s *Service) DeleteSubscription(ctx context.Context, reportID idgen.ID) error {
	tid, err := tenantOf(ctx)
	if err != nil {
		return err
	}
	return dbErr(s.db.WithContext(ctx).
		Where("tenant_id = ? AND report_id = ?", tid, int64(reportID)).
		Delete(&Subscription{}).Error)
}

// normalizeRecipients 拆分、清洗并校验收件人：支持逗号/分号/空白分隔。
func normalizeRecipients(raw string) (string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		addr, err := mail.ParseAddress(f)
		if err != nil {
			return "", response.BadRequest("report.subscription_recipient_invalid", f)
		}
		out = append(out, addr)
	}
	if len(out) == 0 {
		return "", response.BadRequest("report.subscription_recipient_required")
	}
	if len(out) > maxRecipients {
		return "", response.BadRequest("report.subscription_recipient_too_many", maxRecipients)
	}
	return strings.Join(out, ","), nil
}

func subscriptionView(row *Subscription) *SubscriptionView {
	return &SubscriptionView{
		ReportID: row.ReportID, Schedule: row.Schedule, SendHour: row.SendHour,
		Weekday: row.Weekday, Recipients: row.Recipients, Enabled: row.Enabled != 0,
		LastSentAt: row.LastSentDate,
	}
}

// RunDue 扫描并投递到期的订阅，返回已投递条数。由 plugin.Starter 每分钟调用；
// now 注入方便测试。跨租户读取（tenant.Skip）——邮件投递与租户上下文无关。
// 发送时刻按服务器时区（time.Local）解释：运维改时区会平移发送窗口，语义可预期。
func (s *Service) RunDue(ctx context.Context, now time.Time) (int, error) {
	if s.mailer == nil {
		return 0, nil
	}
	now = now.In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	var rows []Subscription
	// daily/weekly 两个到期分支必须括号成一个整体，再与 enabled、last_sent_date AND；
	// GORM 的 Where/Or 是平铺拼接，散着写 OR 会绕过 last_sent_date 的当天去重。
	db := s.db.WithContext(tenant.Skip(ctx)).Model(&Subscription{}).
		Where("enabled = ?", 1).
		Where("(schedule = ? AND send_hour = ? AND weekday = ?) OR (schedule = ? AND send_hour = ? AND weekday = ?)",
			SchedDaily, now.Hour(), int(now.Weekday()), SchedWeekly, now.Hour(), int(now.Weekday())).
		Where("last_sent_date IS NULL OR last_sent_date < ?", today)
	if err := db.Find(&rows).Error; err != nil {
		return 0, err
	}
	sent := 0
	for _, row := range rows {
		if !s.deliver(ctx, row, now) {
			continue
		}
		sent++
		if err := s.db.WithContext(tenant.Skip(ctx)).Model(&Subscription{}).Where("id = ?", int64(row.ID)).
			Update("last_sent_date", today).Error; err != nil {
			s.log.Warn("report: subscription mark sent failed", zapID(row.ID), zapErr(err))
		}
	}
	return sent, nil
}

// deliver 给一条订阅投递提醒；false = 本条跳过（收件人解析失败或 SMTP 未配置）。
// 单条失败只记日志：一封提醒邮件不值得让扫描器中断。
// 报表里的表格图会导出成 xlsx 附件随邮件发送（附件生成失败降级为纯提醒，不阻塞投递）。
func (s *Service) deliver(ctx context.Context, row Subscription, now time.Time) bool {
	var report ReportDef
	if err := s.db.WithContext(tenant.Skip(ctx)).First(&report, "id = ?", int64(row.ReportID)).Error; err != nil {
		s.log.Warn("report: subscription target missing", zapID(row.ID), zapErr(err))
		return false
	}
	tid := row.TenantID
	if tid == 0 {
		s.log.Warn("report: subscription tenant missing", zapID(row.ID))
		return false
	}
	// 导出与租户相关查询在租户上下文里执行（订阅行本身带 tenant_id）。
	tctx := tenant.With(ctx, tid)
	to := strings.Split(row.Recipients, ",")
	// 语言跟平台默认语言（与 ticket 的 composeReporterMail 同一口径），文案不写死中文；
	// SiteName 留空由 notify 用配置里的站点名，品牌不钉死在代码里。
	lang := core.DefaultLanguage().Code
	title, content := composeSubscriptionMail(lang, report.Name)
	atts := s.subscriptionAttachments(tctx, row.ReportID)
	for _, addr := range to {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		err := s.mailer.SendMailWithAttachments(ctx, addr, mail.TplNotice, lang, mail.Data{
			Title:   title,
			Content: content,
			Link:    "/monitor/report",
			Time:    now.Format("2006-01-02 15:04"),
		}, atts)
		if err != nil {
			s.log.Warn("report: subscription mail skipped/failed", zapID(row.ID), zapErr(err))
		}
	}
	return true
}

// subscriptionAttachments 把报表里的表格图导出为 xlsx 附件（无 table 图返回 nil，纯提醒）。
// 用全量数据范围：订阅由持有 report:report 的管理员配置，邮件代表「报表的完整口径」；
// 生成失败的单张图表跳过并记日志，不让一图表挂掉拖垮整封邮件。
func (s *Service) subscriptionAttachments(ctx context.Context, reportID idgen.ID) []mail.Attachment {
	rep, err := s.loadReport(ctx, reportID)
	if err != nil {
		return nil
	}
	rv := reportView(rep)
	v := Viewer{Scope: &datascope.Scope{All: true}}
	var atts []mail.Attachment
	for _, it := range rv.Config.Items {
		cid, err := idgen.Parse(it.ChartID)
		if err != nil || cid == 0 {
			continue
		}
		ch, err := s.loadChart(ctx, cid)
		if err != nil || ch.Type != ChartTable {
			continue // 非表格图没有可导出的行集；柱/折/饼的底层导出另走查看端
		}
		var buf bytes.Buffer
		err = s.ExportReportChart(ctx, func(ExportFile) io.Writer { return &buf }, reportID, cid, nil, v, ExportLabels{Name: ch.Name})
		if err != nil {
			s.log.Warn("report: subscription chart export failed", zap.String("chart", ch.Name), zapErr(err))
			continue
		}
		name := cleanSheetName(ch.Name)
		if name == "" {
			name = fmt.Sprintf("chart-%s", it.ChartID)
		}
		atts = append(atts, mail.Attachment{Name: name + ".xlsx", ContentType: xlsxContentType, Data: buf.Bytes()})
	}
	return atts
}

// composeSubscriptionMail 拼订阅提醒的标题与正文，双语分支照 ticket 的 composeReporterMail。
// 内置报表的 name 存的是前端 i18n 键（plugin_report.builtin.*，见 seed.go 的 keyPrefix）：
// 后端词典里没有这个键，直接拼进标题会把 "plugin_report.builtin.sales_daily" 发给用户，
// 所以带键名的报表改用不含名字的写法——宁可少个名字，也不发一串键名。
func composeSubscriptionMail(lang, name string) (string, string) {
	named := name != "" && !strings.HasPrefix(name, keyPrefix)
	en := strings.HasPrefix(strings.ToLower(lang), "en")
	switch {
	case en && named:
		return "Your subscribed report: " + name,
			"Your subscribed report “" + name + "” is due. Sign in to the console to see the latest data."
	case en:
		return "Your subscribed report is due",
			"One of your subscribed reports is due. Sign in to the console to see the latest data."
	case named:
		return "订阅报表：" + name,
			"您订阅的报表「" + name + "」已到达计划的发送时刻，请登录后台查看最新数据。"
	default:
		return "订阅报表已送达",
			"您订阅的报表已到达计划的发送时刻，请登录后台查看最新数据。"
	}
}

func zapID(id idgen.ID) zap.Field { return zap.Int64("subscriptionId", int64(id)) }
func zapErr(err error) zap.Field  { return zap.Error(err) }
