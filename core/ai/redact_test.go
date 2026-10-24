package ai

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 测试用的号码都是公开示例：身份证校验位、银行卡 Luhn 经过计算。
const (
	idOK   = "11010519491231002X" // 校验位合法的示例身份证
	idBad  = "110105194912310021" // 校验位不对：不当作身份证
	cardOK = "6222021234567890128"
)

func TestRedactEachKind(t *testing.T) {
	all := DefaultRedact()
	cases := []struct{ name, in, want string }{
		{"phone plain", "联系 13800138000 谢谢", "联系 [PHONE] 谢谢"},
		{"phone spaced", "call 138 0013 8000 now", "call [PHONE] now"},
		{"phone dashed with +86", "+86-138-0013-8000", "[PHONE]"},
		{"email", "mail me: Zhang.San+x@Example-Corp.com.cn, ok", "mail me: [EMAIL], ok"},
		{"id card", "身份证 " + idOK + " 已核验", "身份证 [ID_CARD] 已核验"},
		{"bank card plain", "卡号" + cardOK + "。", "卡号[BANK_CARD]。"},
		{"bank card grouped", "6222 0212 3456 7890 128", "[BANK_CARD]"},
		{"all in one", "13800138000 / a@b.io / " + idOK + " / " + cardOK, "[PHONE] / [EMAIL] / [ID_CARD] / [BANK_CARD]"},
	}
	for _, c := range cases {
		if got := Redact(c.in, all); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestRedactLeavesOrdinaryText(t *testing.T) {
	all := DefaultRedact()
	keep := []string{
		"2026年10月17日 09:30 召开会议",
		"订单号 20261017123456789012 共 3 件", // 20 位：既不是身份证也不是银行卡
		"金额 12345.67 元，编号 A-1001",
		"版本 v1.2.3，端口 8080",
		"用户@公司 或 @mention",
		"110105194912310021 校验位错误的 18 位数字：" + idBad,
		"1234567890123 不通过 Luhn 的 13 位数字",
		"",
	}
	for _, s := range keep {
		if got := Redact(s, all); got != s {
			t.Errorf("must stay unchanged: %q -> %q", s, got)
		}
	}
	// 不从更长的数字串里切出手机号
	if got := Redact("1380013800099", all); strings.Contains(got, MaskPhone) {
		t.Errorf("phone cut out of a longer number: %q", got)
	}
}

func TestRedactOptionsAreIndependent(t *testing.T) {
	in := "13800138000 a@b.io " + idOK + " " + cardOK
	if got := Redact(in, RedactOptions{}); got != in {
		t.Errorf("all off must be identity: %q", got)
	}
	got := Redact(in, RedactOptions{Email: true})
	if strings.Contains(got, "a@b.io") || !strings.Contains(got, "13800138000") || !strings.Contains(got, idOK) {
		t.Errorf("email only: %q", got)
	}
	got = Redact(in, RedactOptions{Phone: true})
	if strings.Contains(got, "13800138000") || !strings.Contains(got, "a@b.io") || !strings.Contains(got, cardOK) {
		t.Errorf("phone only: %q", got)
	}
}

func TestFromSettingsRedactDefaultsOnAndCanBeDisabled(t *testing.T) {
	if !FromSettings(nil, Config{BaseURL: "http://x"}).Redact.Any() {
		t.Fatal("redact must default to on when settings are not saved")
	}
	if got := FromSettings(map[string]any{"enabled": true}, Config{}).Redact; got != DefaultRedact() {
		t.Fatalf("missing redact key must keep default: %+v", got)
	}
	if FromSettings(map[string]any{"redact": false}, Config{}).Redact.Any() {
		t.Fatal("redact=false must turn it off")
	}
	if !FromSettings(map[string]any{"redact": true}, Config{}).Redact.Any() {
		t.Fatal("redact=true must stay on")
	}
}

const sensitive = "手机 13800138000，邮箱 zhang.san@corp.example.com，身份证 " + idOK + "，银行卡 " + cardOK

// 三个入口（筛选 / 翻译 / 写作）发给上游的内容里都不能出现原始敏感值。
func TestGatewaySendsRedactedTextUpstream(t *testing.T) {
	srv, cap := upstream(t, func(req chatRequest) string {
		switch {
		case strings.Contains(req.Messages[0].Content, "translation engine"):
			return `{"items":["` + "ok" + `"]}`
		case strings.Contains(req.Messages[0].Content, "writing assistant"):
			return `{"text":"` + "done" + `"}`
		}
		return `{"filters":{},"explanation":"x"}`
	})
	g, _ := gateway(srv.URL)
	cfgOn := func(context.Context) (Config, error) {
		return Config{Enabled: true, AllowPrivate: true, BaseURL: srv.URL + "/v1", APIKey: testKey, Model: "m", Timeout: 2 * time.Second, Redact: DefaultRedact()}, nil
	}
	g.Load = cfgOn
	raw := []string{"13800138000", "zhang.san@corp.example.com", idOK, cardOK}
	sent := func() string {
		var sb strings.Builder
		for _, m := range cap.req.Messages {
			sb.WriteString(m.Content)
		}
		return sb.String()
	}
	check := func(label string) {
		s := sent()
		for _, r := range raw {
			if strings.Contains(s, r) {
				t.Errorf("%s: upstream saw %q in %q", label, r, s)
			}
		}
		for _, m := range []string{MaskPhone, MaskEmail, MaskIDCard, MaskBankCard} {
			if !strings.Contains(s, m) {
				t.Errorf("%s: upstream request is missing %s: %q", label, m, s)
			}
		}
	}
	if _, err := g.Filter(context.Background(), tester, "user", sensitive); err != nil {
		t.Fatal(err)
	}
	check("filter")
	if _, err := g.Translate(context.Background(), tester, TranslateRequest{Texts: []string{sensitive}, From: "zh", To: "en", Format: "text"}); err != nil {
		t.Fatal(err)
	}
	check("translate")
	for _, mode := range []string{"write", "polish"} {
		if _, err := g.Write(context.Background(), tester, WriteRequest{Mode: mode, Text: sensitive}); err != nil {
			t.Fatal(err)
		}
		check("write/" + mode)
	}

	// 关闭脱敏后原样发送
	g.Load = func(ctx context.Context) (Config, error) {
		c, _ := cfgOn(ctx)
		c.Redact = RedactOptions{}
		return c, nil
	}
	if _, err := g.Filter(context.Background(), tester, "user", sensitive); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent(), "13800138000") {
		t.Errorf("redact off must send as is: %q", sent())
	}
}

// HTML 标签在发给上游前已被令牌替换，标签里的邮箱既不会外泄，也不会被改写。
func TestTranslateKeepsTagAttributesIntact(t *testing.T) {
	srv, cap := upstream(t, func(chatRequest) string { return `{"items":["⟦0⟧Contact us⟦1⟧"]}` })
	g, _ := gateway(srv.URL)
	g.Load = func(context.Context) (Config, error) {
		return Config{Enabled: true, AllowPrivate: true, BaseURL: srv.URL + "/v1", APIKey: testKey, Model: "m", Timeout: 2 * time.Second, Redact: DefaultRedact()}, nil
	}
	const tag = `<a href="mailto:boss@corp.example.com">`
	out, err := g.Translate(context.Background(), tester, TranslateRequest{
		Texts: []string{tag + `联系我们</a>`}, From: "zh", To: "en", Format: "html"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0] != tag+`Contact us</a>` {
		t.Fatalf("out = %q", out)
	}
	for _, m := range cap.req.Messages {
		if strings.Contains(m.Content, "boss@corp.example.com") {
			t.Fatalf("tag attribute leaked upstream: %q", m.Content)
		}
	}
}

// 模型把脱敏占位符当成检索关键字时丢弃。
func TestFilterDropsMaskedKeyword(t *testing.T) {
	srv, _ := upstream(t, func(chatRequest) string {
		return `{"filters":{"keyword":"[PHONE]","status":"1"},"explanation":"x"}`
	})
	g, _ := gateway(srv.URL)
	g.Load = func(context.Context) (Config, error) {
		return Config{Enabled: true, AllowPrivate: true, BaseURL: srv.URL + "/v1", APIKey: testKey, Model: "m", Timeout: 2 * time.Second, Redact: DefaultRedact()}, nil
	}
	res, err := g.Filter(context.Background(), tester, "user", "手机号 13800138000 且启用")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Filters["keyword"]; ok || res.Filters["status"] != "1" {
		t.Fatalf("filters = %v", res.Filters)
	}
}
