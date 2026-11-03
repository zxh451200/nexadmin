package ticket

// 客户邮件通知：坐席公开回复 / 解决 / 关闭工单时按设置给报告人发邮件，客户自助建单时回执。
//
// 与站内信（service.go 的 sendNotify）互补：站内信发给坐席，邮件发给客户。三层闸门：
//   1. 全局开关 plg_ticket_setting.email_notify_enabled（后台「通知设置」控制，默认关）；
//   2. 报告人有邮箱（reporter_email 列；邮件进单天然有，表单可选填，且 reporterId 是邮箱形态时兜底）；
//   3. core/notify 的 SMTP 配置与每收件人限频（email 渠道 10 封/小时）兜底。
// 内部备注绝不触发邮件——那是坐席之间的悄悄话。
//
// SMTP 未配置返回 ErrMailNotConfigured：新部署没配 SMTP 是常态，按 Debug 处理不刷 warn
// （与 website 插件的邮件抄送同一口径）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/ai"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/pkg/mail"
)

// mailSender 是 core/notify 的邮件面（测试注入假实现）。
type mailSender interface {
	SendMail(ctx context.Context, to, tpl, lang string, d mail.Data) error
}

// settingsCacheTTL 是设置读缓存的寿命：开关改动最迟一分钟生效，换来每封邮件不查表。
const settingsCacheTTL = time.Minute

type cachedSetting struct {
	value TicketSetting
	at    time.Time
}

// 客户邮件的类型：决定标题文案。
const (
	EmailCreated  = "created"  // 自助建单回执（坐席代建不发——坐席当面告知即可）
	EmailReplied  = "replied"  // 坐席公开回复
	EmailResolved = "resolved" // 标记解决
	EmailClosed   = "closed"   // 关闭
)

// WithMailer 注入邮件出口；不注入时客户邮件静默跳过（业务照常）。
func (s *Service) WithMailer(m mailSender) *Service {
	s.mailer = m
	return s
}

// reporterEmailOf 返回该工单可用于通知的报告人邮箱：reporter_email 列优先，
// 为空且 reporterId 本身是邮箱形态（邮件进单的存量单）时兜底用 reporterId。
func reporterEmailOf(t *Ticket) string {
	if t == nil {
		return ""
	}
	if e := strings.TrimSpace(t.ReporterEmail); e != "" {
		return e
	}
	if strings.Contains(t.ReporterID, "@") {
		return t.ReporterID
	}
	return ""
}

// Settings 读当前设置（带 60s 进程内缓存）。读失败按默认值兜底：
// 设置读不出来不该挡住建单/回复，只是少发邮件。
func (s *Service) Settings(ctx context.Context) TicketSetting {
	if c := s.settingCache.Load(); c != nil && time.Since(c.at) < settingsCacheTTL {
		return c.value
	}
	var row TicketSetting
	if err := s.db.WithContext(ctx).Where("id = ?", 1).Take(&row).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.logWarn("ticket: load settings failed, using defaults", "err", err.Error())
		return TicketSetting{}
	}
	s.settingCache.Store(&cachedSetting{value: row, at: time.Now()})
	return row
}

// SettingsFresh 与 Settings 相同但绕过缓存：设置编辑页要看到库里现值而不是旧值。
func (s *Service) SettingsFresh(ctx context.Context) TicketSetting {
	s.settingCache.Store(nil)
	return s.Settings(ctx)
}

// SettingsInput 是「通知设置」弹窗的入参。PortalURL 为客服门户绝对地址，
// 拼进邮件的「查看工单」链接；空 = 邮件不带链接。
type SettingsInput struct {
	EmailNotifyEnabled bool   `json:"emailNotifyEnabled"`
	PortalURL          string `json:"portalUrl"`
}

// SaveSettings 保存设置（单行 upsert，id 固定 1）并失效缓存。
func (s *Service) SaveSettings(ctx context.Context, in SettingsInput) (*TicketSetting, error) {
	in.PortalURL = strings.TrimSpace(in.PortalURL)
	// 门户地址会原样进邮件正文变成可点链接：只收 http/https，host 拒绝本机/私网/保留段，
	// 防止把内网地址或怪 scheme 发给客户（与 Mimosa 出站 URL 约束同一条线）。
	if in.PortalURL != "" {
		if err := validatePortalURL(ctx, in.PortalURL); err != nil {
			return nil, err
		}
	}
	var row TicketSetting
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", 1).Take(&row).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			row = TicketSetting{}
			row.ID = 1
		}
		row.EmailNotifyEnabled, row.PortalURL = in.EmailNotifyEnabled, in.PortalURL
		// 显式 upsert：Save 在主键非零时只发 UPDATE，首次保存（表为空）会静默丢数据。
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).
			Create(&row).Error
	})
	if err != nil {
		return nil, err
	}
	s.settingCache.Store(&cachedSetting{value: row, at: time.Now()})
	return &row, nil
}

// portalResolver 是门户地址校验的 DNS 缝：生产为 nil（net.DefaultResolver），
// 测试注入假解析避免依赖真实网络。
var portalResolver ai.Resolver

// validatePortalURL 校验客服门户地址：仅 http/https，host（含 DNS 解析结果）
// 拒绝回环 / 私网 / 云元数据段——这串地址会进邮件正文变成客户可点的链接。
func validatePortalURL(ctx context.Context, raw string) error {
	if err := ai.ValidateBaseURL(ctx, raw, false, portalResolver); err != nil {
		return response.BadRequest("ticket.portal_url_invalid")
	}
	return nil
}

// sendReporterEmail 是客户邮件的唯一出口。kind 决定标题；previewText 是回复摘要（可为空）。
// 所有失败只记日志：邮件是增强通知，永远不影响业务主流程。
func (s *Service) sendReporterEmail(ctx context.Context, t *Ticket, kind, previewText string) {
	if s == nil || s.mailer == nil || t == nil {
		return
	}
	set := s.Settings(ctx)
	if !set.EmailNotifyEnabled {
		return
	}
	to := reporterEmailOf(t)
	if to == "" {
		return
	}
	title, content := composeReporterMail(s.lang, kind, t.TicketNo, previewText)
	link := ""
	if set.PortalURL != "" {
		link = strings.TrimRight(set.PortalURL, "/") + "/ticket/view?no=" + t.TicketNo
	}
	err := s.mailer.SendMail(ctx, to, mail.TplNotice, s.lang, mail.Data{
		Title: title, Content: content, Link: link,
	})
	switch {
	case err == nil:
	case errors.Is(err, notify.ErrMailNotConfigured), errors.Is(err, notify.ErrSkip):
		// 未配 SMTP / 限频 / 模板问题都是部署态事实，静默跳过：notify 层已有投递记录可查。
	default:
		s.logWarn("ticket: reporter email failed", "ticketNo", t.TicketNo, "err", err.Error())
	}
}

// composeReporterMail 生成邮件标题与正文。收件人是客户，语言跟应用默认语言
// （自助页有双语入口，但这里拿不到请求上下文，与 website 抄送同一取舍）。
func composeReporterMail(lang, kind, ticketNo, previewText string) (string, string) {
	en := strings.HasPrefix(strings.ToLower(lang), "en")
	content := strings.TrimSpace(previewText)
	if content == "" {
		if en {
			content = "Ticket " + ticketNo
		} else {
			content = "工单 " + ticketNo
		}
	}
	if en {
		switch kind {
		case EmailCreated:
			return "Ticket received: " + ticketNo, content + "\n\nWe have received your ticket and will get back to you soon."
		case EmailReplied:
			return "New reply on ticket " + ticketNo, content
		case EmailResolved:
			return "Ticket resolved: " + ticketNo, content + "\n\nIf this does not fully solve the issue, just reply to reopen it."
		default: // EmailClosed
			return "Ticket closed: " + ticketNo, content + "\n\nYou can submit a new ticket any time."
		}
	}
	switch kind {
	case EmailCreated:
		return "工单已受理：" + ticketNo, content + "\n\n我们已收到你的工单，会尽快处理。"
	case EmailReplied:
		return "工单有新回复：" + ticketNo, content
	case EmailResolved:
		return "工单已解决：" + ticketNo, content + "\n\n如问题未完全解决，直接回复即可重新打开。"
	default: // EmailClosed
		return "工单已关闭：" + ticketNo, content + "\n\n如仍有问题，欢迎随时重新提交。"
	}
}
