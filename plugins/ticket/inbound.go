package ticket

// 邮件进单：把一封原始 MIME 邮件变成工单（新单）或该单的一条客户回复。
//
// 做成「入站 webhook + 可插拔取信」而不是内置 IMAP：QQ / Exchange / 企业邮都要授权码，
// 那是部署方的凭据，不该进仓库；任何能把邮件原文 POST 过来的组件（IMAP 轮询器、
// 邮件网关、SES/SendGrid inbound）都能接，本包只负责解析与落单。
//
// 入口：POST /plugins/ticket/inbound/email，头 X-Ticket-Secret 必须与密钥一致。
// 未配置密钥时入口整体不可用（503）——不会出现「谁都能建单」的口子。

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"nexadmin/core/response"
)

// maxInboundEmailBytes 是单封邮件原文的上限：邮件正文再长也只该落成一条工单描述。
const maxInboundEmailBytes = 2 << 20

// InboundEmail 是一封入站邮件的解析结果。
type InboundEmail struct {
	From      string // 纯地址，小写
	FromName  string
	Subject   string
	Body      string
	MessageID string
	// References 是 In-Reply-To 与 References 里的全部 Message-ID，用于找回原单。
	References []string
	// AutoReply 标记自动回复（节假日应答、退信、群发）——这类邮件不能进单也不能触发规则。
	AutoReply bool
}

var inboundSecret atomic.Value // string

// SetInboundSecret 设置入站共享密钥；空串等于关闭该入口。
func SetInboundSecret(secret string) { inboundSecret.Store(strings.TrimSpace(secret)) }

func inboundSecretValue() string {
	if v := inboundSecret.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// parseInboundEmail 解析原始邮件：优先 text/plain，只有 HTML 时退化成去标签文本；
// 引用的历史内容会被剪掉（回复只留本次说的话）。未知字符集保留原样，不因解码失败丢内容。
func parseInboundEmail(raw []byte) (*InboundEmail, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, response.BadRequest("ticket.inbound_parse_failed")
	}
	out := &InboundEmail{}
	if addr, err := mail.ParseAddress(msg.Header.Get("From")); err == nil {
		out.From = strings.ToLower(strings.TrimSpace(addr.Address))
		out.FromName = addr.Name
	} else {
		out.From = strings.ToLower(strings.TrimSpace(msg.Header.Get("From")))
	}
	if out.From == "" {
		return nil, response.BadRequest("ticket.inbound_parse_failed")
	}
	out.Subject = decodeHeader(msg.Header.Get("Subject"))
	out.MessageID = firstMessageID(msg.Header.Get("Message-ID"))
	out.References = append(messageIDs(msg.Header.Get("In-Reply-To")), messageIDs(msg.Header.Get("References"))...)
	out.AutoReply = looksAutoReply(msg.Header, out.Subject)
	out.Body = trimQuoted(strings.TrimSpace(messageText(msg)))
	if strings.TrimSpace(out.Body) == "" {
		return nil, response.BadRequest("ticket.inbound_parse_failed")
	}
	return out, nil
}

// messageText 走一遍 MIME 结构，取第一段非空 text/plain；都没有就用 HTML 去标签。
func messageText(msg *mail.Message) string {
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return ""
		}
		reader := multipart.NewReader(msg.Body, boundary)
		var html string
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			ct, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			sub := &mail.Message{Header: mail.Header(part.Header), Body: part}
			switch {
			case strings.HasPrefix(ct, "multipart/"):
				if t := messageText(sub); strings.TrimSpace(t) != "" {
					return t
				}
			case ct == "" || ct == "text/plain":
				if t := decodePart(sub); strings.TrimSpace(t) != "" {
					return t
				}
			case ct == "text/html":
				html = decodePart(sub)
			}
		}
		if html != "" {
			return stripHTML(html)
		}
		return ""
	}
	// 顶层就是 text/html 的邮件（不少系统通知/工单系统都是这种）也要去标签，
	// 否则正文会把整段 HTML 塞进工单。
	if mediaType == "text/html" {
		return stripHTML(decodePart(msg))
	}
	return decodePart(msg)
}

// decodePart 按 Content-Transfer-Encoding 与 charset 解出文本。
func decodePart(msg *mail.Message) string {
	body, err := io.ReadAll(io.LimitReader(msg.Body, maxInboundEmailBytes))
	if err != nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(msg.Header.Get("Content-Transfer-Encoding"))) {
	case "base64":
		if dec, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(body)), "")); err == nil {
			body = dec
		}
	case "quoted-printable":
		if dec, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body))); err == nil {
			body = dec
		}
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		return string(body)
	}
	if cs := strings.ToLower(strings.TrimSpace(params["charset"])); cs != "" {
		body = toUTF8(body, cs)
	}
	return string(body)
}

// toUTF8 把常见中文邮件字符集转成 UTF-8；不认识的字符集原样返回（宁可留乱码也不丢内容）。
func toUTF8(raw []byte, charset string) []byte {
	var dec encoding.Encoding
	switch charset {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return raw
	case "gb18030", "gbk", "gb2312", "cp936":
		dec = simplifiedchinese.GB18030
	case "iso-8859-1", "latin1":
		dec = charmap.ISO8859_1
	default:
		return raw
	}
	out, _, err := transform.Bytes(dec.NewDecoder(), raw)
	if err != nil {
		return raw
	}
	return out
}

// stripHTML 把 HTML 正文压成纯文本：脚本/样式整段丢弃，换行标签变换行，其余去标签并解实体。
func stripHTML(s string) string {
	for _, tag := range []string{"script", "style"} {
		for {
			start := strings.Index(strings.ToLower(s), "<"+tag)
			if start < 0 {
				break
			}
			end := strings.Index(strings.ToLower(s[start:]), "</"+tag+">")
			if end < 0 {
				s = s[:start]
				break
			}
			s = s[:start] + s[start+end+len(tag)+3:]
		}
	}
	replacer := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n", "</div>", "\n", "</tr>", "\n",
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'",
	)
	s = replacer.Replace(s)
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// trimQuoted 剪掉引用的历史内容：回复只需要本次说的话，把整段旧邮件塞进工单会把时间轴撑爆。
func trimQuoted(body string) string {
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 常见引用分隔：邮件客户端的分隔行与「在 … 写道」抬头。
		if strings.HasPrefix(trimmed, "-----Original Message") ||
			strings.HasPrefix(trimmed, "On ") && strings.HasSuffix(trimmed, "wrote:") ||
			strings.HasPrefix(trimmed, "在 ") && strings.HasSuffix(trimmed, "写道：") {
			break
		}
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// looksAutoReply 认自动回复：自动应答头、群发优先级别、主题自动回复前缀三者任一命中。
func looksAutoReply(h mail.Header, subject string) bool {
	if v := strings.ToLower(strings.TrimSpace(h.Get("Auto-Submitted"))); v != "" && v != "no" {
		return true
	}
	if v := strings.ToLower(strings.TrimSpace(h.Get("Precedence"))); v == "bulk" || v == "junk" || v == "list" || v == "auto_reply" {
		return true
	}
	if strings.TrimSpace(h.Get("X-Autoreply")) != "" || strings.TrimSpace(h.Get("X-Autorespond")) != "" ||
		strings.TrimSpace(h.Get("X-Auto-Response-Suppress")) != "" {
		return true
	}
	low := strings.ToLower(strings.TrimSpace(subject))
	for _, prefix := range []string{"auto-reply", "auto reply", "automatic reply", "out of office", "自动回复", "自动答复", "auto:"} {
		if strings.HasPrefix(low, prefix) {
			return true
		}
	}
	return false
}

func decodeHeader(v string) string {
	if v == "" {
		return ""
	}
	if dec, err := new(mime.WordDecoder).DecodeHeader(v); err == nil {
		return dec
	}
	return v
}

// messageIDs 取出 `<...>` 形式的 Message-ID（大小写与尖括号都不影响比对）。
func messageIDs(v string) []string {
	out := make([]string, 0, 4)
	for {
		start := strings.IndexByte(v, '<')
		if start < 0 {
			break
		}
		end := strings.IndexByte(v[start:], '>')
		if end < 0 {
			break
		}
		out = append(out, v[start:start+end+1])
		v = v[start+end+1:]
	}
	return out
}

func firstMessageID(v string) string {
	if ids := messageIDs(v); len(ids) > 0 {
		return ids[0]
	}
	return strings.TrimSpace(v)
}

// IngestEmail 把解析后的邮件落成工单或回复，返回工单号与「是否新建」。
// 幂等：同一个 Message-ID 第二次进来直接返回原单号，不重复建单/重开。
func (s *Service) IngestEmail(ctx context.Context, m *InboundEmail) (string, bool, error) {
	if m.AutoReply || looksAutoReply(mail.Header{}, m.Subject) {
		// 标记由解析器给，但服务层不盲信调用方：主题里的「自动回复」前缀再挡一道。
		return "", false, response.BadRequest("ticket.inbound_auto_reply")
	}
	if m.MessageID != "" {
		var dup TicketReply
		err := s.db.WithContext(ctx).Where("message_id = ?", m.MessageID).Take(&dup).Error
		if err == nil {
			var t Ticket
			if err := s.db.WithContext(ctx).Select("ticket_no").Where("id = ?", int64(dup.TicketID)).Take(&t).Error; err == nil {
				return t.TicketNo, false, nil
			}
			return "", false, nil
		}
	}
	if no := s.ticketByMessageIDs(ctx, m.References); no != "" {
		_, _, err := s.Reply(ctx, no, ReplyInput{
			ReplierType: ReplierReporter, ReplierID: truncate(m.From, 64), ReplierName: truncate(m.FromName, 64),
			Content: m.Body, MessageID: m.MessageID,
		})
		return no, false, err
	}
	title := strings.TrimSpace(m.Subject)
	if title == "" {
		title = firstLine(m.Body)
	}
	t, err := s.Create(ctx, CreateInput{
		Title: title, Content: m.Body, ReporterType: ReplierReporter,
		ReporterID: truncate(m.From, 64), ReporterName: truncate(m.FromName, 64),
		// 邮件先落单，必填的附加字段由坐席补齐（与公开入口同一口径）。
		EnforceRequiredFields: false, MessageID: m.MessageID,
	})
	if err != nil {
		return "", false, err
	}
	return t.TicketNo, true, nil
}

// ticketByMessageIDs 用回信里引用的历史 Message-ID 找回原单号；找不到返回空串。
func (s *Service) ticketByMessageIDs(ctx context.Context, ids []string) string {
	for _, id := range ids {
		var row TicketReply
		err := s.db.WithContext(ctx).Select("id", "ticket_id").Where("message_id = ?", id).Take(&row).Error
		if err != nil {
			continue
		}
		var t Ticket
		if err := s.db.WithContext(ctx).Select("ticket_no").Where("id = ?", int64(row.TicketID)).Take(&t).Error; err == nil {
			return t.TicketNo
		}
	}
	return ""
}

func firstLine(body string) string {
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[:i]
	}
	body = strings.TrimSpace(body)
	if len([]rune(body)) > 80 {
		body = string([]rune(body)[:80])
	}
	return body
}

// POST /plugins/ticket/inbound/email —— 原始 MIME 邮件进单（无登录，靠共享密钥）。
func (h *handler) inboundEmail(c *gin.Context) {
	secret := inboundSecretValue()
	if secret == "" {
		response.Fail(c, response.Unavailable("ticket.inbound_disabled"))
		return
	}
	got := c.GetHeader("X-Ticket-Secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
		response.Fail(c, response.Forbidden("ticket.inbound_secret_invalid"))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxInboundEmailBytes))
	if err != nil || len(raw) == 0 {
		response.Fail(c, response.BadRequest("ticket.inbound_parse_failed"))
		return
	}
	msg, err := parseInboundEmail(raw)
	if err != nil {
		response.Fail(c, err)
		return
	}
	no, created, err := h.svc.IngestEmail(c.Request.Context(), msg)
	if err != nil {
		response.Fail(c, err)
		return
	}
	respond(c, gin.H{"ticketNo": no, "created": created}, nil)
}
