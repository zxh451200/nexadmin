package mail

import (
	"bytes"
	"errors"
	"fmt"
	htmltpl "html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	texttpl "text/template"
)

// 内置模板名。
const (
	TplForgotCode     = "forgot_code"     // 忘记密码验证码
	TplAccountCreated = "account_created" // 账号创建 / 管理员重置密码通知
	TplSecurityAlert  = "security_alert"  // 安全告警
	TplNotice         = "notice"          // 通用通知（notify 的 email 渠道使用）
)

// 语言。
const (
	LangZH = "zh-CN"
	LangEN = "en-US"
)

// Data 是模板变量。所有字符串在 HTML 里经 html/template 转义，用户可控文本放心填。
type Data struct {
	SiteName string
	Name     string // 收件人称呼
	Username string
	Code     string
	Minutes  int
	Title    string
	Content  string
	Link     string
	IP       string
	Time     string
	Reason   string // account_created：created | reset
}

// NormalizeLang 把任意语言标记收敛到内置支持的 zh-CN / en-US。
func NormalizeLang(l string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "zh") {
		return LangZH
	}
	return LangEN
}

type tpl struct{ subject, text, html string }

// 内置模板：subject 与 text 用 text/template，html 用 html/template。
var builtin = map[string]map[string]tpl{
	TplForgotCode: {
		LangZH: {
			subject: "{{.SiteName}} 密码重置验证码",
			text:    "您正在重置 {{.SiteName}} 的登录密码。\n验证码：{{.Code}}（{{.Minutes}} 分钟内有效，仅可使用一次）。\n如非本人操作，请忽略本邮件。",
			html:    `<p>您正在重置 <b>{{.SiteName}}</b> 的登录密码。</p><p>验证码：<b style="font-size:20px;letter-spacing:2px">{{.Code}}</b></p><p>{{.Minutes}} 分钟内有效，仅可使用一次。如非本人操作，请忽略本邮件。</p>`,
		},
		LangEN: {
			subject: "{{.SiteName}} password reset code",
			text:    "You are resetting your {{.SiteName}} password.\nVerification code: {{.Code}} (valid for {{.Minutes}} minutes, single use).\nIf this was not you, ignore this email.",
			html:    `<p>You are resetting your <b>{{.SiteName}}</b> password.</p><p>Verification code: <b style="font-size:20px;letter-spacing:2px">{{.Code}}</b></p><p>Valid for {{.Minutes}} minutes, single use. If this was not you, ignore this email.</p>`,
		},
	},
	TplAccountCreated: {
		LangZH: {
			subject: "{{.SiteName}} 账号通知",
			text:    "{{.Name}}，您好：\n{{if eq .Reason \"reset\"}}管理员已重置您在 {{.SiteName}} 的登录密码。{{else}}管理员已为您创建 {{.SiteName}} 账号。{{end}}\n登录账号：{{.Username}}\n出于安全考虑，邮件不包含密码，请向管理员获取。{{if .Link}}\n登录地址：{{.Link}}{{end}}",
			html:    `<p>{{.Name}}，您好：</p><p>{{if eq .Reason "reset"}}管理员已重置您在 <b>{{.SiteName}}</b> 的登录密码。{{else}}管理员已为您创建 <b>{{.SiteName}}</b> 账号。{{end}}</p><p>登录账号：<b>{{.Username}}</b></p><p>出于安全考虑，邮件不包含密码，请向管理员获取。</p>{{if .Link}}<p><a href="{{.Link}}">登录</a></p>{{end}}`,
		},
		LangEN: {
			subject: "{{.SiteName}} account notice",
			text:    "Hello {{.Name}},\n{{if eq .Reason \"reset\"}}An administrator reset your {{.SiteName}} password.{{else}}An administrator created your {{.SiteName}} account.{{end}}\nUsername: {{.Username}}\nFor security, this email does not contain the password; ask your administrator.{{if .Link}}\nSign in: {{.Link}}{{end}}",
			html:    `<p>Hello {{.Name}},</p><p>{{if eq .Reason "reset"}}An administrator reset your <b>{{.SiteName}}</b> password.{{else}}An administrator created your <b>{{.SiteName}}</b> account.{{end}}</p><p>Username: <b>{{.Username}}</b></p><p>For security, this email does not contain the password; ask your administrator.</p>{{if .Link}}<p><a href="{{.Link}}">Sign in</a></p>{{end}}`,
		},
	},
	TplSecurityAlert: {
		LangZH: {
			subject: "{{.SiteName}} 安全提醒：{{.Title}}",
			text:    "{{.Name}}，您好：\n{{.Content}}\n时间：{{.Time}}{{if .IP}}\nIP：{{.IP}}{{end}}\n如非本人操作，请尽快修改密码并联系管理员。",
			html:    `<p>{{.Name}}，您好：</p><p>{{.Content}}</p><p>时间：{{.Time}}{{if .IP}}　IP：{{.IP}}{{end}}</p><p>如非本人操作，请尽快修改密码并联系管理员。</p>`,
		},
		LangEN: {
			subject: "{{.SiteName}} security alert: {{.Title}}",
			text:    "Hello {{.Name}},\n{{.Content}}\nTime: {{.Time}}{{if .IP}}\nIP: {{.IP}}{{end}}\nIf this was not you, change your password and contact an administrator.",
			html:    `<p>Hello {{.Name}},</p><p>{{.Content}}</p><p>Time: {{.Time}}{{if .IP}} &nbsp; IP: {{.IP}}{{end}}</p><p>If this was not you, change your password and contact an administrator.</p>`,
		},
	},
	TplNotice: {
		LangZH: {
			subject: "{{.SiteName}} 通知：{{.Title}}",
			text:    "{{.Title}}\n\n{{.Content}}{{if .Link}}\n\n{{.Link}}{{end}}",
			html:    `<h3>{{.Title}}</h3><p>{{.Content}}</p>{{if .Link}}<p><a href="{{.Link}}">查看详情</a></p>{{end}}`,
		},
		LangEN: {
			subject: "{{.SiteName}} notice: {{.Title}}",
			text:    "{{.Title}}\n\n{{.Content}}{{if .Link}}\n\n{{.Link}}{{end}}",
			html:    `<h3>{{.Title}}</h3><p>{{.Content}}</p>{{if .Link}}<p><a href="{{.Link}}">View details</a></p>{{end}}`,
		},
	},
}

// Templates 渲染邮件模板。Dir 非空时，config/mail/<name>.<lang>.html 存在则覆盖内置 HTML 正文
// （主题与纯文本仍用内置的；覆盖文件里的 {{.X}} 同样经 html/template 转义）。
type Templates struct{ Dir string }

var tagRe = regexp.MustCompile(`(?s)<[^>]*>`)

// Render 渲染模板，返回主题、纯文本和 HTML。name 必须是内置模板名（同时杜绝路径穿越）。
func (t Templates) Render(name, lang string, d Data) (subject, text, html string, err error) {
	lang = NormalizeLang(lang)
	def, ok := builtin[name][lang]
	if !ok {
		return "", "", "", fmt.Errorf("mail: unknown template %q", name)
	}
	if d.Minutes == 0 {
		d.Minutes = 10
	}
	subject, err = runText(def.subject, d)
	if err != nil {
		return "", "", "", err
	}
	subject = strings.NewReplacer("\r", " ", "\n", " ").Replace(subject) // 站点名等可控文本不得破坏头部
	htmlSrc := def.html
	override := false
	if t.Dir != "" {
		if b, rerr := os.ReadFile(filepath.Join(t.Dir, name+"."+lang+".html")); rerr == nil {
			htmlSrc, override = string(b), true
		} else if !errors.Is(rerr, os.ErrNotExist) {
			return "", "", "", rerr
		}
	}
	h, err := htmltpl.New(name).Parse(htmlSrc)
	if err != nil {
		return "", "", "", err
	}
	var hb bytes.Buffer
	if err := h.Execute(&hb, d); err != nil {
		return "", "", "", err
	}
	html = hb.String()
	if override { // 覆盖了 HTML：纯文本由 HTML 去标签得到，保证两部分内容一致
		text = strings.TrimSpace(unescape(tagRe.ReplaceAllString(html, " ")))
	} else if text, err = runText(def.text, d); err != nil {
		return "", "", "", err
	}
	return subject, text, html, nil
}

func unescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&#34;", `"`, "&#39;", "'", "&nbsp;", " ").Replace(s)
}

func runText(src string, d Data) (string, error) {
	t, err := texttpl.New("t").Parse(src)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}
