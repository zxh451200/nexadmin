package sendlog

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const redacted = "[REDACTED]"

var (
	maskedPhoneRe = regexp.MustCompile(`^\+?\d{1,3}\*{4}\d{2,4}$`)
	maskedMailRe  = regexp.MustCompile(`^[^@\s*]\*{3}@[^@\s]+$`)
)

// MaskPhone 脱敏手机号：138****1234。接受 +86 / 86 前缀和空格、横线；已脱敏的值原样返回；
// 位数太少无法保留首尾时只返回 ****。
func MaskPhone(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if maskedPhoneRe.MatchString(p) {
		return p
	}
	var b strings.Builder
	for _, r := range p {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) == 13 && strings.HasPrefix(d, "86") { // +86 13812345678
		d = d[2:]
	}
	switch n := len(d); {
	case n >= 11:
		return d[:3] + "****" + d[n-4:]
	case n >= 7:
		return d[:2] + "****" + d[n-2:]
	}
	return "****"
}

// MaskEmail 脱敏邮箱：a***@x.com。只保留本地部分首字符和域名；已脱敏的值原样返回；
// 不是邮箱形态的输入按“首字符 + ***”处理，保证不会原样落库。
func MaskEmail(e string) string {
	e = strings.TrimSpace(e)
	if e == "" {
		return ""
	}
	if maskedMailRe.MatchString(e) {
		return e
	}
	at := strings.LastIndex(e, "@")
	if at <= 0 || at == len(e)-1 {
		return maskHead(e)
	}
	return maskHead(e[:at]) + "@" + e[at+1:]
}

// maskHead 返回 首个字符 + "***"。
func maskHead(s string) string {
	r, _ := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return "***"
	}
	return string(r) + "***"
}

// MaskRecipient 按渠道脱敏收件人并限长。机器人存渠道名（robot:<名称>），不会是 URL；
// 渠道未知时按形态判断（含 @ 当邮箱，否则当手机号），宁可多脱敏。
func MaskRecipient(channel, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var out string
	switch {
	case channel == "robot":
		out = oneLine(raw)
		if strings.Contains(out, "://") {
			out = "robot"
		}
	case channel == "email" || (channel != "sms" && strings.Contains(raw, "@")):
		out = MaskEmail(raw)
	default:
		out = MaskPhone(raw)
	}
	return truncate(out, MaxRecipient)
}

var (
	urlRe     = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://[^\s"'<>]+`)
	bearerRe  = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=\-]+`)
	kvRe      = regexp.MustCompile(`(?i)\b([A-Za-z0-9_\-]*(?:secret|token|password|passwd|pwd|authorization|api[_\-]?key|access[_\-]?key|signature|sign|credential|otp)[A-Za-z0-9_\-]*)(["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;&"'}]+)`)
	codeRe    = regexp.MustCompile(`(?i)(验证码|校验码|动态码|\bverification code|\bpasscode|\bcode is|\bcode=|\botp|\bpin)([^0-9\n]{0,8})\d{4,8}`)
	emailRe   = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	cnPhoneRe = regexp.MustCompile(`(?:\+?86[\- ]?)?1[3-9]\d{9}`)
	intlRe    = regexp.MustCompile(`\+\d[\d\- ]{6,}\d`)
	digitsRe  = regexp.MustCompile(`\d{7,}`)
	tokenRe   = regexp.MustCompile(`[A-Za-z0-9_\-+=]{24,}`)
	b64Re     = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
)

// redactURL 去掉 URL 里的用户信息、路径和查询（webhook 的令牌通常就在这里），只留 scheme://host。
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<url>"
	}
	out := u.Scheme + "://" + u.Host
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		out += "/<redacted>"
	}
	return out
}

// SanitizeText 清理要落库的自由文本（错误信息、标题）：折叠空白与控制字符，抹掉 URL 路径 / 查询、
// Bearer 令牌、key=value 形式的密钥、验证码、邮箱、手机号、长数字串和长随机串，最后按字符数截断到 max。
// 这是最后一道防线：调用方仍应先把已知的密钥自行去掉。
func SanitizeText(s string, max int) string {
	s = oneLine(s)
	if s == "" {
		return ""
	}
	s = urlRe.ReplaceAllStringFunc(s, redactURL)
	s = bearerRe.ReplaceAllString(s, "$1 "+redacted)
	s = kvRe.ReplaceAllString(s, "$1$2"+redacted)
	s = codeRe.ReplaceAllString(s, "$1$2"+redacted)
	s = emailRe.ReplaceAllStringFunc(s, MaskEmail)
	s = cnPhoneRe.ReplaceAllStringFunc(s, MaskPhone)
	s = intlRe.ReplaceAllStringFunc(s, MaskPhone)
	s = digitsRe.ReplaceAllStringFunc(s, MaskPhone)
	s = tokenRe.ReplaceAllString(s, redacted)
	s = b64Re.ReplaceAllString(s, redacted)
	return truncate(s, max)
}

// SanitizeError 清理错误信息并截断到 MaxError 个字符。
func SanitizeError(msg string) string { return SanitizeText(msg, MaxError) }

// oneLine 把任意空白和控制字符折叠成单个空格，并保证是合法 UTF-8。
func oneLine(s string) string {
	s = strings.ToValidUTF8(s, "")
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
}

// truncate 按字符（不是字节）截断；超长时以 … 结尾，总长不超过 max。
func truncate(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max-1])) + "…"
}
