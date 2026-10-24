package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
)

// Message 是一封待发送的邮件。Text 与 HTML 至少一个非空，两者都有时按 multipart/alternative 发送。
type Message struct {
	To      []string
	Subject string
	Text    string
	HTML    string
}

// ErrHeaderInjection 表示头部字段里出现了 CR/LF。
var ErrHeaderInjection = errors.New("mail: header field contains CR or LF")

// checkHeader 拒绝含 CR/LF 的头部值（防头注入）。NUL 同样拒绝。
func checkHeader(name, v string) error {
	if strings.ContainsAny(v, "\r\n\x00") {
		return fmt.Errorf("%w: %s", ErrHeaderInjection, name)
	}
	return nil
}

// ParseAddress 校验并规范化一个地址：先拒绝 CR/LF，再交给 net/mail 解析；
// 只返回纯地址（丢弃显示名），所以 "Evil <a@b>, c@d" 这类多地址写法也会因解析失败被拒绝。
func ParseAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if err := checkHeader("address", raw); err != nil {
		return "", err
	}
	a, err := mail.ParseAddress(raw)
	if err != nil {
		return "", fmt.Errorf("mail: invalid address %q: %w", raw, err)
	}
	return a.Address, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func qp(s string) string {
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}

// build 生成完整的 RFC 5322 报文（CRLF 换行）。from 已经是校验过的纯地址，fromName 可空。
func build(fromName, from string, to []string, subject, text, html string, now time.Time) ([]byte, error) {
	if len(to) == 0 {
		return nil, errors.New("mail: no recipient")
	}
	if text == "" && html == "" {
		return nil, errors.New("mail: empty body")
	}
	if err := checkHeader("subject", subject); err != nil {
		return nil, err
	}
	if err := checkHeader("from name", fromName); err != nil {
		return nil, err
	}
	fromHdr := (&mail.Address{Name: fromName, Address: from}).String() // 显示名含非 ASCII 时按 RFC 2047 编码
	toHdr := make([]string, len(to))
	for i, a := range to {
		toHdr[i] = (&mail.Address{Address: a}).String()
	}
	domain := "localhost"
	if i := strings.LastIndex(from, "@"); i >= 0 {
		domain = from[i+1:]
	}

	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", fromHdr)
	h("To", strings.Join(toHdr, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", subject))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", "<"+randHex(12)+"@"+domain+">")
	h("MIME-Version", "1.0")
	body := func(ct, content string) {
		b.WriteString("Content-Type: " + ct + "; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(qp(content))
	}
	switch {
	case text != "" && html != "":
		boundary := "nex_" + randHex(12)
		h("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
		b.WriteString("\r\n--" + boundary + "\r\n")
		body("text/plain", text)
		b.WriteString("\r\n--" + boundary + "\r\n")
		body("text/html", html)
		b.WriteString("\r\n--" + boundary + "--\r\n")
	case html != "":
		body("text/html", html)
	default:
		body("text/plain", text)
	}
	return b.Bytes(), nil
}
