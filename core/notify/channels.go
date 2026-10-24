package notify

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/config"
	"nexadmin/pkg/mail"
	"nexadmin/pkg/robot"
	"nexadmin/pkg/sms"
)

// MailSettings 是设置页「通知渠道」里的邮件（SMTP）配置，密码已解密。
type MailSettings struct {
	Host, Security, Username, Password, FromName, FromAddr string
	Port                                                   int
}

// Configured 报告是否填了能发信的最小配置。
func (m MailSettings) Configured() bool {
	return strings.TrimSpace(m.Host) != "" && m.Port > 0 && strings.TrimSpace(m.FromAddr) != ""
}

// RobotSettings 是一个机器人的配置。
type RobotSettings struct {
	Name, Type, URL, Secret string
	Markdown                bool
}

// Settings 是通知渠道的运行时配置。
type Settings struct {
	SiteName    string
	Lang        string // 站点默认语言，用户没有语言偏好时使用
	Mail        MailSettings
	Robots      []RobotSettings
	AccountMail bool // 账号创建 / 管理员重置密码后给用户发通知邮件（默认关）
}

// SettingsFunc 读取当前租户的通知渠道配置（由设置模块注册，notify 不 import 它）。
type SettingsFunc func(ctx context.Context) (Settings, error)

// SetMailer 替换邮件发送实现（默认是 pkg/mail 的 SMTP 客户端）；测试或自定义投递通道用。
func (s *Service) SetMailer(f func(MailSettings) (MailSender, error)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.mailer = f
	s.mu.Unlock()
}

// SetSettings 注入配置读取函数。
func (s *Service) SetSettings(f SettingsFunc) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.settings = f
	s.mu.Unlock()
}

// Settings 返回当前配置；未注入或读取失败时返回零值（等于全部未配置）。
func (s *Service) Settings(ctx context.Context) Settings {
	if s == nil {
		return Settings{}
	}
	s.mu.RLock()
	f := s.settings
	s.mu.RUnlock()
	if f == nil {
		return Settings{}
	}
	st, err := f(ctx)
	if err != nil {
		s.log.Warn("notify: cannot read settings", zap.Error(err))
	}
	return st
}

// MailConfigured 邮件（SMTP）是否已配置；忘记密码入口据此显示 / 隐藏。
func (s *Service) MailConfigured(ctx context.Context) bool {
	return s != nil && s.Settings(ctx).Mail.Configured()
}

func expand(v string) string { return config.ExpandEnv(v) }

func (s *Service) newMailer(ms MailSettings) (MailSender, error) {
	s.mu.RLock()
	f := s.mailer
	s.mu.RUnlock()
	if f != nil {
		return f(ms)
	}
	return NewMailClient(ms)
}

// NewMailClient 按设置创建 SMTP 客户端。
func NewMailClient(ms MailSettings) (*mail.Client, error) {
	return mail.New(mail.Config{
		Host: ms.Host, Port: ms.Port, Security: ms.Security, Username: ms.Username, Password: ms.Password,
		FromName: ms.FromName, FromAddr: ms.FromAddr, Timeout: 15 * time.Second,
	})
}

func (s *Service) templates() mail.Templates {
	dir := "config/mail"
	if s.cfg != nil && s.cfg.UserConfigPath != "" {
		dir = filepath.Join(filepath.Dir(s.cfg.UserConfigPath), "mail")
	}
	return mail.Templates{Dir: dir}
}

// SendMail 异步按模板给一个邮箱发信（忘记密码验证码、账号通知、安全告警用）。
// 邮件未配置时同步返回 ErrMailNotConfigured；其余失败只记日志（重试 2 次）。
func (s *Service) SendMail(ctx context.Context, to, tpl, lang string, d mail.Data) error {
	if s == nil {
		return ErrMailNotConfigured
	}
	st := s.Settings(ctx)
	if !st.Mail.Configured() {
		if addr, perr := mail.ParseAddress(to); perr == nil {
			s.emit(ctx, Record{Channel: RecordEmail, Scene: mailScene(tpl), Recipient: addr, Subject: tpl,
				Status: StatusSkipped, Err: ErrMailNotConfigured.Error()})
		}
		return ErrMailNotConfigured
	}
	if lang == "" {
		lang = st.Lang
	}
	if d.SiteName == "" {
		d.SiteName = st.SiteName
	}
	addr, err := mail.ParseAddress(to)
	if err != nil {
		return err
	}
	s.enqueue(ctx, "mail:"+tpl, func(ctx context.Context) error { return s.sendTemplate(ctx, st.Mail, addr, tpl, lang, d) },
		func(ctx context.Context, err error, spent time.Duration) {
			s.emit(ctx, Record{Channel: RecordEmail, Scene: mailScene(tpl), Recipient: addr, Subject: tpl,
				Status: statusOf(err), Err: s.errText(ctx, err), Duration: spent})
		})
	return nil
}

func (s *Service) sendTemplate(ctx context.Context, ms MailSettings, to, tpl, lang string, d mail.Data) error {
	subject, text, html, err := s.templates().Render(tpl, lang, d)
	if err != nil {
		return errors.Join(ErrSkip, err) // 模板本身有问题，重试没有意义
	}
	c, err := s.newMailer(ms)
	if err != nil {
		return errors.Join(ErrSkip, err)
	}
	return c.Send(ctx, mail.Message{To: []string{to}, Subject: subject, Text: text, HTML: html})
}

func (s *Service) deliverEmail(ctx context.Context, _ string, m Msg, to []Recipient) error {
	st := s.Settings(ctx)
	if !st.Mail.Configured() {
		return skip("mail is not configured")
	}
	var failed []Recipient
	var last error
	for _, r := range to {
		lang := r.Lang
		if lang == "" {
			lang = st.Lang
		}
		// 标题、正文、称呼都是用户可控文本：只作为模板数据传入，由 html/template 转义。
		d := mail.Data{SiteName: st.SiteName, Name: r.Name, Title: m.Title, Content: m.Content, Link: m.Link}
		if err := s.sendTemplate(ctx, st.Mail, r.Email, mail.TplNotice, lang, d); err != nil {
			if errors.Is(err, ErrSkip) {
				return err
			}
			failed, last = append(failed, r), err
		}
	}
	if len(failed) > 0 {
		return &PartialError{Failed: failed, Err: last}
	}
	return nil
}

func (s *Service) deliverSMS(ctx context.Context, _ string, m Msg, to []Recipient) error {
	if s.cfg == nil || s.cfg.SMS.Provider == "" {
		return skip("sms is not configured")
	}
	c := s.cfg.SMS
	prov, err := sms.New(sms.Config{
		Provider: c.Provider, Log: s.log, Proxy: s.cfg.Auth.Proxy,
		WebhookURL: expand(c.Webhook.URL), WebhookSecret: expand(c.Webhook.Secret),
		AliyunKeyID: expand(c.Aliyun.AccessKeyID), AliyunKeySecret: expand(c.Aliyun.AccessKeySecret),
		AliyunSignName: c.Aliyun.SignName, AliyunEndpoint: c.Aliyun.Endpoint,
		TencentSecretID: expand(c.Tencent.SecretID), TencentSecretKey: expand(c.Tencent.SecretKey),
		TencentSDKAppID: expand(c.Tencent.SDKAppID), TencentSignName: expand(c.Tencent.SignName),
		TencentRegion: expand(c.Tencent.Region), TencentEndpoint: expand(c.Tencent.Endpoint),
		TencentParamOrder: c.Tencent.ParamOrder,
	})
	if err != nil || prov == nil {
		return skip("sms provider unusable: %v", err)
	}
	content := []rune(m.Content)
	if len(content) > 60 {
		content = content[:60]
	}
	params := map[string]string{"title": m.Title, "content": string(content)}
	template := "notify"
	if strings.EqualFold(strings.TrimSpace(c.Provider), "tencent") {
		template = expand(c.Tencent.NotifyTemplate)
		if strings.TrimSpace(template) == "" {
			return skip("sms.tencent.notify_template is not configured")
		}
	}
	var failed []Recipient
	var last error
	for _, r := range to {
		if err := prov.Send(ctx, strings.TrimSpace(r.Phone), template, params); err != nil {
			failed, last = append(failed, r), err
		}
	}
	if len(failed) > 0 {
		return &PartialError{Failed: failed, Err: last}
	}
	return nil
}

func (s *Service) newRobot(rs RobotSettings) (RobotSender, error) {
	if s.robots != nil {
		return s.robots(rs)
	}
	return NewRobotClient(rs)
}

// NewRobotClient 按设置创建机器人客户端（URL 校验与 SSRF 防护在 pkg/robot）。
func NewRobotClient(rs RobotSettings) (*robot.Client, error) {
	return robot.New(robot.Config{Type: rs.Type, URL: rs.URL, Secret: rs.Secret, Markdown: rs.Markdown})
}

func (s *Service) deliverRobot(ctx context.Context, ch string, m Msg, _ []Recipient) error {
	name := strings.TrimPrefix(ch, ChannelRobotPrefix)
	for _, rs := range s.Settings(ctx).Robots {
		if rs.Name != name {
			continue
		}
		c, err := s.newRobot(rs)
		if err != nil {
			return errors.Join(ErrSkip, err)
		}
		content := m.Content
		if m.Link != "" {
			content += "\n" + m.Link
		}
		return c.Send(ctx, m.Title, content)
	}
	return skip("robot %q is not configured", name)
}

// TestMail 同步发一封测试邮件（设置页「发送测试」）。
func (s *Service) TestMail(ctx context.Context, ms MailSettings, to, lang, site string) error {
	if !ms.Configured() {
		return ErrMailNotConfigured
	}
	addr, err := mail.ParseAddress(to)
	if err != nil {
		return err
	}
	d := mail.Data{SiteName: site, Title: "SMTP test", Content: "This is a test message from the notification settings page."}
	t0 := time.Now()
	err = s.sendTemplate(ctx, ms, addr, mail.TplNotice, lang, d)
	s.emit(ctx, Record{Channel: RecordEmail, Scene: SceneTest, Recipient: addr, Subject: d.Title,
		Status: statusOf(err), Err: s.errText(ctx, err, ms.Password), Duration: time.Since(t0)})
	return err
}

// TestRobot 同步发一条测试消息。
func (s *Service) TestRobot(ctx context.Context, rs RobotSettings) error {
	t0 := time.Now()
	err := s.testRobot(ctx, rs)
	s.emit(ctx, Record{Channel: RecordRobot, Scene: SceneTest, Recipient: ChannelRobotPrefix + rs.Name, Subject: "Test",
		Status: statusOf(err), Err: s.errText(ctx, err, rs.URL, rs.Secret), Duration: time.Since(t0)})
	return err
}

func (s *Service) testRobot(ctx context.Context, rs RobotSettings) error {
	c, err := s.newRobot(rs)
	if err != nil {
		return err
	}
	return c.Send(ctx, "Test", "This is a test message from the notification settings page.")
}
