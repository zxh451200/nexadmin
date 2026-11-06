package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"path/filepath"
	"strings"
	"time"
)

// Message 是一封待发送的邮件。Text 与 HTML 至少一个非空，两者都有时按 multipart/alternative 发送。
// Attachments 非空时整封升级为 multipart/mixed：正文（alternative 或单段）+ 逐个附件。
type Message struct {
	To          []string
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
}

// Attachment 是一个邮件附件：Name 必须带扩展名（Content-Type 空时按扩展名推断），Data 是完整字节。
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// ErrHeaderInjection 表示头部字段里出现了 CR/LF。
var ErrHeaderInjection = errors.New("mail: header field contains CR or LF")

// 附件体积上限（以编码前原始字节计，1 MB = 1e6 字节，与服务商口径一致）。定上限是为了：
//   - 限制内存——每个收件人都要在 build 里重建一份 MIME，附件内容会 base64 进内存字符串，
//     大表全量导出（xlsx）可以把堆顶穿；
//   - 避开 SMTP 服务商的报文上限——base64 膨胀约 4/3，10 MB 单附件编码后约 13.3 MB，
//     17 MB 整封编码后约 22.7 MB，给正文与头部留出余量后仍在 Gmail / Outlook 的 25 MB 之内。
//
// 超限不静默丢：build / Send 返回包装 ErrAttachmentTooLarge 的错误，调用方可据此判定附件没发出去。
const (
	// MaxAttachmentSize 是单个附件的原始字节上限（10 MB）。
	MaxAttachmentSize int64 = 10 * 1000 * 1000
	// MaxAttachmentsTotalSize 是一封邮件所有附件的原始字节总量上限（17 MB）。
	MaxAttachmentsTotalSize int64 = 17 * 1000 * 1000
)

// ErrAttachmentTooLarge 表示附件体积超过上面的上限。
//
// 不走 response/i18n：这是 SMTP 发送通道的服务端错误（最终落到 notify 发送日志的 Record.Err
// 与 zap 日志），不是 HTTP 响应，不占双语词典；上限数值直接写进文案，运维在通知/日志里一眼可见。
var ErrAttachmentTooLarge = errors.New("mail: attachment size limit exceeded")

// CheckAttachments 只校验附件体积上限（单个与总量），超限返回包装 ErrAttachmentTooLarge 的错误，
// 文案带上实际大小与上限。发送前同步调用它，调用方无需读日志即可知道附件发不出去。
func CheckAttachments(atts []Attachment) error { return checkAttachmentSizes(atts) }

// checkAttachmentSizes 是 build 与 CheckAttachments 共用的体积校验。
func checkAttachmentSizes(atts []Attachment) error {
	var total int64
	for i, a := range atts {
		n := int64(len(a.Data))
		if n > MaxAttachmentSize {
			name := attachmentLabel(a.Name, i)
			return fmt.Errorf("%w: %s attachment is %s, over the %s per-attachment limit",
				ErrAttachmentTooLarge, name, humanSize(n), humanSize(MaxAttachmentSize))
		}
		total += n
	}
	if total > MaxAttachmentsTotalSize {
		return fmt.Errorf("%w: attachments total %s, over the %s per-message limit",
			ErrAttachmentTooLarge, humanSize(total), humanSize(MaxAttachmentsTotalSize))
	}
	return nil
}

// attachmentLabel 给错误文案用的附件标识：有名用名，无名退化成序号。
func attachmentLabel(name string, i int) string {
	if name = strings.TrimSpace(name); name != "" {
		return fmt.Sprintf("attachment %d (%s)", i+1, name)
	}
	return fmt.Sprintf("attachment %d", i+1)
}

// humanSize 按十进制 MB 格式化字节数（1 MB = 1e6 字节），与服务商口径一致，例 "12.3 MB"。
func humanSize(b int64) string { return fmt.Sprintf("%.1f MB", float64(b)/1e6) }

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
// 有附件时整封升级为 multipart/mixed：正文（alternative 或单段）在前，附件以 base64 逐个跟后。
func build(fromName, from string, to []string, subject, text, html string, atts []Attachment, now time.Time) ([]byte, error) {
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
	for i, a := range atts {
		if err := checkHeader("attachment name", a.Name); err != nil {
			return nil, fmt.Errorf("%w (attachment %d): %w", ErrHeaderInjection, i+1, err)
		}
		if len(a.Data) == 0 {
			return nil, fmt.Errorf("mail: attachment %d (%s) is empty", i+1, a.Name)
		}
	}
	if err := checkAttachmentSizes(atts); err != nil {
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
	// bodyPart 把「text / html / alternative」按既有口径写进当前 writer；有附件时作为 mixed 的第一段。
	part := func(ct, content string) {
		b.WriteString("Content-Type: " + ct + "; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(qp(content))
	}
	bodyPart := func() {
		switch {
		case text != "" && html != "":
			boundary := "nex_alt_" + randHex(12)
			b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
			b.WriteString("--" + boundary + "\r\n")
			part("text/plain", text)
			b.WriteString("\r\n--" + boundary + "\r\n")
			part("text/html", html)
			b.WriteString("\r\n--" + boundary + "--\r\n")
		case html != "":
			part("text/html", html)
		default:
			part("text/plain", text)
		}
	}
	if len(atts) == 0 {
		bodyPart()
		return b.Bytes(), nil
	}
	mixed := "nex_mix_" + randHex(12)
	h("Content-Type", `multipart/mixed; boundary="`+mixed+`"`)
	b.WriteString("\r\n--" + mixed + "\r\n")
	bodyPart()
	for i, a := range atts {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		ct := a.ContentType
		if ct == "" {
			ct = mime.TypeByExtension("." + strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")))
			if ct == "" {
				ct = "application/octet-stream"
			}
		}
		disp := contentDisposition(name)
		b.WriteString("\r\n--" + mixed + "\r\n")
		b.WriteString("Content-Type: " + ct + "\r\n")
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		b.WriteString("Content-Disposition: " + disp + "\r\n")
		b.WriteString("\r\n")
		b.WriteString(base64Wrap(a.Data))
	}
	b.WriteString("\r\n--" + mixed + "--\r\n")
	return b.Bytes(), nil
}

// contentDisposition 生成附件的 Content-Disposition，保证文件名一定带上：
// 先走 mime.FormatMediaType；它返回空串（无法把 filename 编成合法 header 参数）时，
// 回退手工构造 RFC 2231 写法 filename*=UTF-8 加两个单引号再加百分号编码——绝不静默丢头，
// 否则收件人只会看到一个无名附件。两条路径都保证非空，所以本函数不返回错误。
// name 由调用方保证非空（build 里空名已退化成 attachment-N），CR/LF 也已在 build 前置校验拒掉。
func contentDisposition(name string) string {
	if disp := mime.FormatMediaType("attachment", map[string]string{"filename": name}); disp != "" {
		return disp
	}
	return "attachment; filename*=UTF-8''" + rfc2231Encode(name)
}

// rfc2231Encode 按 RFC 5987 attr-char（ALPHA / DIGIT / "!#$&+-.=^_`|~"）百分号编码 UTF-8 字节串。
func rfc2231Encode(s string) string {
	const attrChars = "!#$&+-.=^_`|~"
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte(attrChars, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}
	return b.String()
}

// base64Wrap 编码附件并按 RFC 2045 的 76 列折行。
func base64Wrap(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for i, r := 0, []rune(enc); i < len(r); i += 76 {
		j := i + 76
		if j > len(r) {
			j = len(r)
		}
		b.WriteString(string(r[i:j]))
		b.WriteString("\r\n")
	}
	return b.String()
}
