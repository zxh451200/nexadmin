package ticket

// 邮件进单的解析与入口闸：真实投递链路（IMAP/网关）在它之前，本包只负责把一封
// 标准 MIME 邮件变成「建单或补信」，并把自动回复挡在门外。

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func buildMultipartMail(t *testing.T, headers map[string]string, plain, html string) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("From: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("张三")) + "?= <Zhang.San@Example.COM>\r\n")
	buf.WriteString("To: support@example.com\r\n")
	buf.WriteString("Subject: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("订单破损")) + "?=\r\n")
	for k, v := range headers {
		buf.WriteString(k + ": " + v + "\r\n")
	}
	mw := multipart.NewWriter(&buf)
	buf.WriteString("MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=" + mw.Boundary() + "\r\n\r\n")
	if plain != "" {
		fw, err := mw.CreatePart(map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(plain))
	}
	if html != "" {
		fw, err := mw.CreatePart(map[string][]string{"Content-Type": {"text/html; charset=utf-8"}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(html))
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseInboundEmailPicksPlainText(t *testing.T) {
	raw := buildMultipartMail(t, map[string]string{
		"Message-ID": "<abc@mail.example.com>",
	}, "我的订单破了\n谢谢", "<p>我的订单破了</p><p>谢谢</p>")
	msg, err := parseInboundEmail(raw)
	if err != nil {
		t.Fatal(err)
	}
	if msg.From != "zhang.san@example.com" {
		t.Errorf("From = %q（应小写纯地址）", msg.From)
	}
	if msg.FromName != "张三" {
		t.Errorf("FromName = %q（编码字应解码）", msg.FromName)
	}
	if msg.Subject != "订单破损" {
		t.Errorf("Subject = %q", msg.Subject)
	}
	if !strings.Contains(msg.Body, "订单破了") || strings.Contains(msg.Body, "<p>") {
		t.Errorf("Body 应取 text/plain 而不是 HTML：%q", msg.Body)
	}
	if msg.MessageID != "<abc@mail.example.com>" {
		t.Errorf("MessageID = %q", msg.MessageID)
	}
	if msg.AutoReply {
		t.Error("正常邮件不该判成自动回复")
	}
}

func TestParseInboundEmailHTMLOnly(t *testing.T) {
	raw := []byte("From: a@b.com\r\nSubject: hi\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<html><body><script>alert(1)</script><p>第一段</p><p>第二段</p></body></html>")
	msg, err := parseInboundEmail(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.Body, "alert") {
		t.Errorf("script 内容应被丢掉：%q", msg.Body)
	}
	if !strings.Contains(msg.Body, "第一段") || !strings.Contains(msg.Body, "第二段") {
		t.Errorf("段落应保留：%q", msg.Body)
	}
}

// 中文邮件常见 GBK：解不出来就会在工单里显示乱码，这条钉住转码。
func TestParseInboundEmailDecodesGBK(t *testing.T) {
	body, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte("退款一直没到账"))
	if err != nil {
		t.Fatal(err)
	}
	raw := append([]byte("From: a@b.com\r\nSubject: gbk\r\nContent-Type: text/plain; charset=gbk\r\n\r\n"), body...)
	msg, err := parseInboundEmail(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Body, "退款一直没到账") {
		t.Errorf("GBK 没转成 UTF-8：%q", msg.Body)
	}
}

func TestTrimQuotedDropsHistory(t *testing.T) {
	body := "我先补充一句\n> 旧的引用\n\n在 2026年1月1日 写道：\n上一封邮件全文\n"
	if got := trimQuoted(body); got != "我先补充一句" {
		t.Errorf("trimQuoted = %q", got)
	}
}

func TestLooksAutoReply(t *testing.T) {
	auto := buildMultipartMail(t, map[string]string{"Auto-Submitted": "auto-replied"}, "x", "")
	msg, err := parseInboundEmail(auto)
	if err != nil {
		t.Fatal(err)
	}
	if !msg.AutoReply {
		t.Error("Auto-Submitted: auto-replied 应判为自动回复")
	}
	prefixed := buildMultipartMail(t, nil, "x", "")
	prefixed = bytes.Replace(prefixed, []byte("=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte("订单破损"))+"?="),
		[]byte("自动回复：我们已收到"), 1)
	if m2, err := parseInboundEmail(prefixed); err != nil || !m2.AutoReply {
		t.Errorf("主题里的「自动回复」前缀应判为自动回复：err=%v auto=%v", err, m2 != nil && m2.AutoReply)
	}
}

// 入口闸：没配密钥必须整体不可用（503），配了但密钥不对必须 403 —— 这两种都不该碰到解析或库。
func TestInboundEmailSecretGate(t *testing.T) {
	t.Cleanup(func() { SetInboundSecret("") })
	gin.SetMode(gin.TestMode)
	h := &handler{}
	r := gin.New()
	r.POST("/inbound", h.inboundEmail)

	post := func(secret, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/inbound", strings.NewReader(body))
		if secret != "" {
			req.Header.Set("X-Ticket-Secret", secret)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	SetInboundSecret("")
	if code := post("anything", "x"); code != http.StatusServiceUnavailable {
		t.Errorf("未配密钥时 status = %d, want 503", code)
	}

	SetInboundSecret("s3cret")
	if code := post("wrong", "From: a@b.com\r\n\r\nx"); code != http.StatusForbidden {
		t.Errorf("密钥错误时 status = %d, want 403", code)
	}
	if code := post("s3cret", "not a mail"); code != http.StatusBadRequest {
		t.Errorf("密钥正确但内容非法时 status = %d, want 400", code)
	}
}
