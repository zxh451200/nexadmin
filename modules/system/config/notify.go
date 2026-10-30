package config

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"nexadmin/core/notify"
)

// notifyDefaults 是「通知渠道」分区的默认值：邮件（SMTP）、机器人列表、账号通知开关。
// 短信沿用已有的 sms 分区 / sms.* 配置，这里不重复。
func notifyDefaults() map[string]any {
	return map[string]any{
		"mail": map[string]any{
			"host": "", "port": 587, "security": "starttls", "username": "", "password": "", "fromName": "", "fromAddr": "",
		},
		"robots":      []any{},
		"accountMail": false,
	}
}

var robotName = regexp.MustCompile(`^[A-Za-z0-9_\-\p{Han}]{1,32}$`)

func asStr(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }

func asInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	}
	return 0
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

// parseNotify 把分区 map（明文）转成 notify.Settings。
func parseNotify(m map[string]any) notify.Settings {
	mm := asMap(m["mail"])
	st := notify.Settings{
		Mail: notify.MailSettings{
			Host: asStr(mm["host"]), Port: asInt(mm["port"]), Security: asStr(mm["security"]),
			Username: asStr(mm["username"]), Password: asStr0(mm["password"]),
			FromName: asStr(mm["fromName"]), FromAddr: asStr(mm["fromAddr"]),
		},
	}
	st.AccountMail, _ = m["accountMail"].(bool)
	list, _ := m["robots"].([]any)
	for _, e := range list {
		r := asMap(e)
		md, _ := r["markdown"].(bool)
		st.Robots = append(st.Robots, notify.RobotSettings{
			Name: asStr(r["name"]), Type: asStr(r["type"]), URL: asStr(r["url"]), Secret: asStr0(r["secretKey"]), Markdown: md,
		})
	}
	return st
}

// asStr0 不裁剪空白（密码 / 密钥原样使用）。
func asStr0(v any) string { s, _ := v.(string); return s }

// normalizeNotify 校验并整理待保存的分区：机器人字段白名单、名称唯一、URL 必须是 https 且不是内网 IP 字面量，
// SMTP 配置能通过 pkg/mail 的校验。
func normalizeNotify(m map[string]any) (map[string]any, error) {
	st := parseNotify(m)
	if st.Mail.Host != "" {
		if _, err := notify.NewMailClient(st.Mail); err != nil {
			return nil, badErr("config.notify.mail_invalid", err)
		}
	}
	list, _ := m["robots"].([]any)
	if len(list) > 20 {
		return nil, bad("config.notify.too_many_robots")
	}
	seen := map[string]bool{}
	robots := make([]any, 0, len(list))
	for _, rs := range st.Robots {
		if !robotName.MatchString(rs.Name) {
			return nil, bad("config.notify.robot_name_invalid")
		}
		if seen[rs.Name] {
			return nil, bad("config.notify.robot_duplicated", rs.Name)
		}
		seen[rs.Name] = true
		if _, err := notify.NewRobotClient(rs); err != nil {
			return nil, badErr("config.notify.robot_invalid", err, rs.Name)
		}
		robots = append(robots, map[string]any{
			"name": rs.Name, "type": strings.ToLower(rs.Type), "url": rs.URL, "secretKey": rs.Secret, "markdown": rs.Markdown,
		})
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	out["robots"] = robots
	return out, nil
}

// notifySettings 实现 notify.SettingsFunc：读取当前租户的「通知渠道」分区并补上站点名称与默认语言。
func (s *Service) notifySettings(ctx context.Context) (notify.Settings, error) {
	sec, err := s.section(ctx, "notify")
	if err != nil {
		return notify.Settings{}, err
	}
	st := parseNotify(sec)
	st.SiteName, st.Lang = "NexAdmin", "zh-CN"
	if site, err := s.section(ctx, "site"); err == nil {
		if v := asStr(site["siteName"]); v != "" {
			st.SiteName = v
		}
		if v := asStr(site["defaultLanguage"]); v != "" {
			st.Lang = v
		}
	}
	return st, nil
}

// testNotify 真实发送一条测试消息。body 除表单值外带 channel（mail | robot:<name>）与 to（测试收件邮箱）。
// 联调说明：只有单元测试里的假服务器验证过；真实 SMTP / 钉钉 / 飞书 / 企业微信未联调。
func (s *Service) testNotify(ctx context.Context, body map[string]any) (*TestResult, error) {
	channel, to := asStr(body["channel"]), asStr(body["to"])
	cur, err := s.section(ctx, "notify")
	if err != nil {
		return nil, err
	}
	merged := merge(cur, keepSecrets(body, cur).(map[string]any))
	st := parseNotify(merged)
	site, _ := s.notifySettings(ctx)
	fail := func(reason string, err error) (*TestResult, error) {
		if err != nil && s.log != nil {
			s.log.Warn("settings: notify test failed", zap.String("channel", channel), zap.String("reason", reason), zap.Error(err))
		}
		return &TestResult{Reason: reason}, nil
	}
	switch {
	case s.notifier == nil:
		return fail("unavailable", nil)
	case channel == "mail":
		if !st.Mail.Configured() {
			return fail("incomplete", nil)
		}
		if to == "" {
			return fail("noRecipient", nil)
		}
		if err := s.notifier.TestMail(ctx, st.Mail, to, site.Lang, site.SiteName); err != nil {
			return fail("sendFailed", err)
		}
	case strings.HasPrefix(channel, notify.ChannelRobotPrefix):
		name := strings.TrimPrefix(channel, notify.ChannelRobotPrefix)
		for _, rs := range st.Robots {
			if rs.Name != name {
				continue
			}
			if rs.URL == "" {
				return fail("incomplete", nil)
			}
			if err := s.notifier.TestRobot(ctx, rs); err != nil {
				return fail("sendFailed", err)
			}
			return &TestResult{OK: true}, nil
		}
		return fail("robotNotFound", nil)
	default:
		return fail("unknown", nil)
	}
	return &TestResult{OK: true}, nil
}
