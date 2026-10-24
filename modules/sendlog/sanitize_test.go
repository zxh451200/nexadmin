package sendlog

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMaskPhone(t *testing.T) {
	cases := map[string]string{
		"13812345678":       "138****5678",
		" 138 1234 5678 ":   "138****5678",
		"+8613812345678":    "138****5678",
		"+86 138-1234-5678": "138****5678",
		"8613812345678":     "138****5678",
		"138****5678":       "138****5678", // 幂等
		"4155550123":        "41****23",    // 短号码保留首尾各 2 位
		"123":               "****",
		"":                  "",
	}
	for in, want := range cases {
		if got := MaskPhone(in); got != want {
			t.Errorf("MaskPhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"alice@example.com":  "a***@example.com",
		"  Bob@Example.org ": "B***@Example.org",
		"张三@example.cn":      "张***@example.cn",
		"a***@example.com":   "a***@example.com", // 幂等
		"x@y.io":             "x***@y.io",
		"not-an-email":       "n***",
		"@example.com":       "@***", // 没有本地部分：不原样返回
		"trailing@":          "t***",
		"":                   "",
	}
	for in, want := range cases {
		if got := MaskEmail(in); got != want {
			t.Errorf("MaskEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskRecipientByChannel(t *testing.T) {
	cases := []struct{ ch, in, want string }{
		{"sms", "13812345678", "138****5678"},
		{"email", "alice@example.com", "a***@example.com"},
		{"robot", "robot:ops", "robot:ops"},                         // 机器人存渠道名
		{"robot", "https://hooks.example.com/T0/B0/TOKEN", "robot"}, // 即使误传 URL 也不落库
		{"", "alice@example.com", "a***@example.com"},               // 渠道未知：按形态判断
		{"", "13812345678", "138****5678"},
		{"sms", "", ""},
	}
	for _, c := range cases {
		if got := MaskRecipient(c.ch, c.in); got != c.want {
			t.Errorf("MaskRecipient(%q,%q) = %q, want %q", c.ch, c.in, got, c.want)
		}
	}
	long := MaskRecipient("robot", "robot:"+strings.Repeat("机", 300))
	if utf8.RuneCountInString(long) > MaxRecipient || !utf8.ValidString(long) {
		t.Errorf("recipient must be truncated to %d characters, got %d", MaxRecipient, utf8.RuneCountInString(long))
	}
}

func TestSanitizeErrorTruncatesToFiveHundredCharacters(t *testing.T) {
	long := strings.Repeat("发送失败，", 400) // 2000 个字符
	got := SanitizeError(long)
	if n := utf8.RuneCountInString(got); n > MaxError || n < MaxError-2 {
		t.Fatalf("length = %d characters, want <= %d", n, MaxError)
	}
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncation must stay on a character boundary and end with an ellipsis: %q", got[len(got)-12:])
	}
	if short := SanitizeError("connection refused"); short != "connection refused" {
		t.Fatalf("short messages must pass through unchanged, got %q", short)
	}
	// 非法 UTF-8 / 控制字符 / 换行不会进库。
	if got := SanitizeError("bad\xff\x00bytes\r\nsecond line"); !utf8.ValidString(got) || strings.ContainsAny(got, "\x00\r\n") {
		t.Fatalf("control characters leaked: %q", got)
	}
}

func TestSanitizeErrorRemovesSecrets(t *testing.T) {
	cases := []struct {
		name, in string
		banned   []string
	}{
		{"webhook url path and token", `Post "https://oapi.dingtalk.com/robot/send?access_token=abcd1234efgh5678ijkl": EOF`,
			[]string{"abcd1234efgh5678ijkl", "/robot/send"}},
		{"url userinfo", "dial https://admin:hunter2@smtp.example.com:465/x failed", []string{"hunter2", "admin:"}},
		{"bearer token", "401 Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl", []string{"eyJhbGciOiJIUzI1NiJ9"}},
		{"password kv", "login failed password=hunter2 for user", []string{"hunter2"}},
		{"json secret", `{"AccessKeySecret":"wJalrXUtnFEMI/K7MDENG","msg":"denied"}`, []string{"wJalrXUtnFEMI"}},
		{"api key colon", "api_key: sk_live_abc123", []string{"sk_live_abc123"}},
		{"cn phone", "invalid mobile 13812345678 for template", []string{"13812345678"}},
		{"cn phone with prefix", "send to +86 13812345678 rejected", []string{"13812345678"}},
		{"email", "mailbox alice@example.com unavailable", []string{"alice@example.com"}},
		{"verification code cn", "短信内容 验证码：123456 发送失败", []string{"123456"}},
		{"verification code en", "body: Your code is 654321, valid 5 minutes", []string{"654321"}},
		{"long opaque token", "invalid key LTAI5tAbCdEfGhIjKlMnOpQrStUv given", []string{"LTAI5tAbCdEfGhIjKlMnOpQrStUv"}},
		{"long digits", "order 20240101123456789 failed", []string{"20240101123456789"}},
	}
	for _, c := range cases {
		got := SanitizeError(c.in)
		for _, b := range c.banned {
			if strings.Contains(got, b) {
				t.Errorf("%s: %q still contains %q (from %q)", c.name, got, b, c.in)
			}
		}
	}
	// 排障信息保留：主机名、错误类别。
	got := SanitizeError(`Post "https://hooks.example.com/services/T0/B0/TOKEN": dial tcp: connection refused`)
	if !strings.Contains(got, "hooks.example.com") || !strings.Contains(got, "connection refused") {
		t.Errorf("useful diagnostics were lost: %q", got)
	}
}

func TestSanitizeTextCoversSubject(t *testing.T) {
	if got := SanitizeText("Reset your password", MaxSubject); got != "Reset your password" {
		t.Fatalf("plain titles must be unchanged, got %q", got)
	}
	if got := SanitizeText("您的验证码 123456，请勿泄露", MaxSubject); strings.Contains(got, "123456") {
		t.Fatalf("code in title leaked: %q", got)
	}
	if got := SanitizeText(strings.Repeat("a b ", 200), MaxSubject); utf8.RuneCountInString(got) > MaxSubject {
		t.Fatalf("subject too long: %d", utf8.RuneCountInString(got))
	}
}
