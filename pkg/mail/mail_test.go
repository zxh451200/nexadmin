package mail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	stdmail "net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// fakeSMTP 是最小的 SMTP 服务端，记录收到的命令用来断言。
type fakeSMTP struct {
	ln       net.Listener
	cert     tls.Certificate
	implicit bool   // SMTPS
	starttls bool   // 声明并支持 STARTTLS
	auth     string // EHLO 里声明的 AUTH 机制，如 "PLAIN LOGIN"
	user     string
	pass     string

	mu       sync.Mutex
	gotTLS   bool
	authMech string
	from     string
	rcpts    []string
	data     string
	authFail bool
}

func newFake(t *testing.T, f *fakeSMTP) *fakeSMTP {
	t.Helper()
	f.cert = selfSigned(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if f.implicit {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{f.cert}})
	}
	f.ln = ln
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	w := func(s string) { _, _ = io.WriteString(c, s+"\r\n") }
	if _, ok := c.(*tls.Conn); ok {
		f.mu.Lock()
		f.gotTLS = true
		f.mu.Unlock()
	}
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "EHLO"):
			_, _ = io.WriteString(c, "250-fake\r\n")
			if f.starttls && !f.isTLS(c) {
				_, _ = io.WriteString(c, "250-STARTTLS\r\n")
			}
			if f.auth != "" {
				_, _ = io.WriteString(c, "250-AUTH "+f.auth+"\r\n")
			}
			w("250 8BITMIME")
		case up == "STARTTLS":
			w("220 go ahead")
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{f.cert}})
			if err := tc.Handshake(); err != nil {
				return
			}
			f.mu.Lock()
			f.gotTLS = true
			f.mu.Unlock()
			c, r = tc, bufio.NewReader(tc)
			w = func(s string) { _, _ = io.WriteString(tc, s+"\r\n") }
		case strings.HasPrefix(up, "AUTH PLAIN"):
			parts := strings.Fields(line)
			raw, _ := base64.StdEncoding.DecodeString(parts[len(parts)-1])
			f.finishAuth("PLAIN", string(raw) == "\x00"+f.user+"\x00"+f.pass, w)
		case up == "AUTH LOGIN":
			w("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
			u, _ := r.ReadString('\n')
			w("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
			p, _ := r.ReadString('\n')
			ub, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(u))
			pb, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(p))
			f.finishAuth("LOGIN", string(ub) == f.user && string(pb) == f.pass, w)
		case strings.HasPrefix(up, "MAIL FROM:"):
			f.mu.Lock()
			f.from = line[len("MAIL FROM:"):]
			f.mu.Unlock()
			w("250 ok")
		case strings.HasPrefix(up, "RCPT TO:"):
			f.mu.Lock()
			f.rcpts = append(f.rcpts, line[len("RCPT TO:"):])
			f.mu.Unlock()
			w("250 ok")
		case up == "DATA":
			w("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			f.mu.Lock()
			f.data = b.String()
			f.mu.Unlock()
			w("250 queued")
		case up == "QUIT":
			w("221 bye")
			return
		default:
			w("250 ok")
		}
	}
}

func (f *fakeSMTP) isTLS(c net.Conn) bool { _, ok := c.(*tls.Conn); return ok }

func (f *fakeSMTP) finishAuth(mech string, ok bool, w func(string)) {
	f.mu.Lock()
	f.authMech, f.authFail = mech, !ok
	f.mu.Unlock()
	if ok {
		w("235 ok")
	} else {
		w("535 bad credentials")
	}
}

func client(t *testing.T, f *fakeSMTP, sec string) *Client {
	t.Helper()
	c, err := New(Config{
		Host: "127.0.0.1", Port: f.port(), Security: sec, Username: "bot", Password: "pw",
		FromName: "Nex 管理台", FromAddr: "noreply@example.com", Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.cfg.tlsConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 测试用自签证书
	return c
}

func TestSendPlainAuthMultipart(t *testing.T) {
	f := newFake(t, &fakeSMTP{auth: "PLAIN LOGIN", user: "bot", pass: "pw"})
	c := client(t, f, SecurityNone)
	err := c.Send(context.Background(), Message{To: []string{"Bob <bob@example.org>"}, Subject: "你好 hello", Text: "纯文本 body", HTML: "<p>HTML 正文</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if f.authMech != "PLAIN" || f.authFail {
		t.Fatalf("auth mech=%q fail=%v", f.authMech, f.authFail)
	}
	if !strings.Contains(f.from, "noreply@example.com") || len(f.rcpts) != 1 || !strings.Contains(f.rcpts[0], "bob@example.org") {
		t.Fatalf("envelope from=%q rcpts=%v", f.from, f.rcpts)
	}
	msg, err := stdmail.ReadMessage(strings.NewReader(f.data))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mime.WordDecoder)
	if s, _ := dec.DecodeHeader(msg.Header.Get("Subject")); s != "你好 hello" {
		t.Fatalf("subject = %q (raw %q)", s, msg.Header.Get("Subject"))
	}
	if from, _ := dec.DecodeHeader(msg.Header.Get("From")); !strings.Contains(from, "Nex 管理台") {
		t.Fatalf("from = %q", from)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" {
		t.Fatalf("content-type %q %v", mt, err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var types, bodies []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(quotedprintable.NewReader(p))
		types = append(types, p.Header.Get("Content-Type"))
		bodies = append(bodies, string(b))
	}
	if len(types) != 2 || !strings.HasPrefix(types[0], "text/plain") || !strings.HasPrefix(types[1], "text/html") {
		t.Fatalf("parts = %v", types)
	}
	if bodies[0] != "纯文本 body" || bodies[1] != "<p>HTML 正文</p>" {
		t.Fatalf("bodies = %q", bodies)
	}
}

func TestSendLoginAuthAndBadPassword(t *testing.T) {
	f := newFake(t, &fakeSMTP{auth: "LOGIN", user: "bot", pass: "pw"})
	c := client(t, f, SecurityNone)
	if err := c.Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "s", Text: "t"}); err != nil {
		t.Fatal(err)
	}
	if f.authMech != "LOGIN" {
		t.Fatalf("mech = %q", f.authMech)
	}
	c.cfg.Password = "wrong"
	if err := c.Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "s", Text: "t"}); err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("want auth error, got %v", err)
	}
}

func TestSendSTARTTLSAndSSL(t *testing.T) {
	f := newFake(t, &fakeSMTP{starttls: true, auth: "PLAIN", user: "bot", pass: "pw"})
	if err := client(t, f, SecuritySTARTTLS).Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "s", Text: "t"}); err != nil {
		t.Fatal(err)
	}
	if !f.gotTLS {
		t.Fatal("STARTTLS was not negotiated")
	}
	g := newFake(t, &fakeSMTP{implicit: true, auth: "PLAIN", user: "bot", pass: "pw"})
	if err := client(t, g, SecuritySSL).Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "s", Text: "t"}); err != nil {
		t.Fatal(err)
	}
	if !g.gotTLS {
		t.Fatal("SMTPS was not negotiated")
	}
	// 服务器不支持 STARTTLS 时必须失败，而不是降级成明文。
	h := newFake(t, &fakeSMTP{})
	if err := client(t, h, SecuritySTARTTLS).Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "s", Text: "t"}); err == nil {
		t.Fatal("expected error when STARTTLS is unavailable")
	}
}

// 附件：整封升级为 multipart/mixed，正文仍按 alternative；附件以 base64 编码且带 Content-Disposition。
func TestSendWithAttachments(t *testing.T) {
	f := newFake(t, &fakeSMTP{auth: "PLAIN", user: "bot", pass: "pw"})
	c := client(t, f, SecurityNone)
	err := c.Send(context.Background(), Message{
		To: []string{"a@example.org"}, Subject: "周报", Text: "本周数据见附件。",
		Attachments: []Attachment{
			{Name: "report.xlsx", ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Data: []byte("PK\x03\x04fake-xlsx")},
			{Name: "chart.csv", Data: []byte("a,b\n1,2")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := stdmail.ReadMessage(strings.NewReader(f.data))
	if err != nil {
		t.Fatal(err)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("content-type %q %v", mt, err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var texts, names, contents []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		switch {
		case p.Header.Get("Content-Disposition") != "":
			names = append(names, p.FileName())
			b, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, p))
			contents = append(contents, string(b))
		default:
			b, _ := io.ReadAll(p)
			texts = append(texts, string(b))
		}
	}
	if len(texts) != 1 || !strings.Contains(texts[0], "本周数据见附件。") {
		t.Fatalf("body parts = %v", texts)
	}
	if len(names) != 2 || names[0] != "report.xlsx" || names[1] != "chart.csv" {
		t.Fatalf("attachment names = %v", names)
	}
	if contents[0] != "PK\x03\x04fake-xlsx" || contents[1] != "a,b\n1,2" {
		t.Fatalf("attachment contents mismatch: %q %q", contents[0], contents[1])
	}
}

// 附件名带 CRLF：整封拒绝，一个字节都不出站。
func TestAttachmentNameInjectionRejected(t *testing.T) {
	f := newFake(t, &fakeSMTP{})
	c := client(t, f, SecurityNone)
	err := c.Send(context.Background(), Message{
		To: []string{"a@example.org"}, Subject: "s", Text: "t",
		Attachments: []Attachment{{Name: "x.xlsx\r\nBcc: evil@example.org", Data: []byte("d")}},
	})
	if err == nil || f.data != "" {
		t.Fatalf("err = %v, data = %d bytes", err, len(f.data))
	}
}

// attachmentNames 从收到的报文里取每个附件的 Content-Disposition 文件名；
// 有 Content-Disposition 却没有文件名时直接失败——那正是「静默丢头」的症状。
func attachmentNames(t *testing.T, data string) []string {
	t.Helper()
	msg, err := stdmail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("content-type %q err %v", mt, err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var names []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		disp := p.Header.Get("Content-Disposition")
		if disp == "" {
			continue // 正文段
		}
		fn := p.FileName()
		if fn == "" {
			t.Fatalf("attachment has a Content-Disposition but no filename: %q", disp)
		}
		names = append(names, fn)
	}
	return names
}

// 缺陷 2：非 ASCII（中文）附件名必须带着文件名出站，不能退化成无名附件。
// 中文文件名是让 Content-Disposition 生成最容易出问题的输入，这里按实际 MIME 往返验证。
func TestSendAttachmentNonASCIIFilenameKept(t *testing.T) {
	f := newFake(t, &fakeSMTP{auth: "PLAIN", user: "bot", pass: "pw"})
	c := client(t, f, SecurityNone)
	want := []string{"月度报表.xlsx", "sales 周报 1月.xlsx", `quote"月.xlsx`, "semi;colon.xlsx"}
	atts := make([]Attachment, 0, len(want))
	for _, n := range want {
		atts = append(atts, Attachment{Name: n, Data: []byte("payload")})
	}
	if err := c.Send(context.Background(), Message{
		To: []string{"a@example.org"}, Subject: "s", Text: "t", Attachments: atts,
	}); err != nil {
		t.Fatal(err)
	}
	got := attachmentNames(t, f.data)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("attachment filenames = %q, want %q", got, want)
	}
}

// contentDisposition 的任何输入都要产出带文件名的头：FormatMediaType 失败时走手工 RFC 2231 回退。
// FormatMediaType 对「attachment + filename」这两个固定字段不会返回空串，所以回退分支单独测，
// 保证那条路一旦生效也不是无名附件。
func TestContentDispositionAlwaysCarriesFilename(t *testing.T) {
	for _, n := range []string{"a.xlsx", "月度报表.xlsx", "a b.xlsx", `a"b.xlsx`, "a;b.xlsx", "emoji😀.xlsx", "\xff\xfe.xlsx"} {
		disp := contentDisposition(n)
		if disp == "" {
			t.Fatalf("contentDisposition(%q) = \"\" — header would be dropped", n)
		}
		if !strings.Contains(disp, "attachment") {
			t.Fatalf("contentDisposition(%q) = %q, want an attachment disposition", n, disp)
		}
	}
	// RFC 2231 手工回退路径要能被标准库解回原名（收件人看到的文件名不受损）。
	for _, n := range []string{"月度报表.xlsx", "a b.xlsx", `a"b.xlsx`, "semi;colon.xlsx", "100%.xlsx"} {
		hdr := "attachment; filename*=UTF-8''" + rfc2231Encode(n)
		_, params, err := mime.ParseMediaType(hdr)
		if err != nil {
			t.Fatalf("fallback header %q does not parse: %v", hdr, err)
		}
		if params["filename"] != n {
			t.Fatalf("fallback round-trip: got %q, want %q (header %q)", params["filename"], n, hdr)
		}
	}
}

// 缺陷 1：附件体积上限。单个超限与整封总量超限都要明确报错，且一个字节都不出站。
func TestAttachmentSizeLimitsRejected(t *testing.T) {
	f := newFake(t, &fakeSMTP{auth: "PLAIN", user: "bot", pass: "pw"})
	c := client(t, f, SecurityNone)

	overSingle := []Attachment{{Name: "big.xlsx", Data: make([]byte, MaxAttachmentSize+1)}}
	err := c.Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "s", Text: "t", Attachments: overSingle})
	if err == nil {
		t.Fatal("expected single-attachment over-limit rejection")
	}
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("err = %v, want ErrAttachmentTooLarge", err)
	}
	for _, want := range []string{"big.xlsx", "10.0 MB"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must name the attachment and the limit, got %q (missing %q)", err, want)
		}
	}

	// 每个都合规、总量超限：3 张表各 6 MB = 18 MB > 17 MB 上限。
	overTotal := []Attachment{
		{Name: "a.xlsx", Data: make([]byte, 6*1000*1000)},
		{Name: "b.xlsx", Data: make([]byte, 6*1000*1000)},
		{Name: "c.xlsx", Data: make([]byte, 6*1000*1000)},
	}
	err = c.Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "s", Text: "t", Attachments: overTotal})
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("err = %v, want ErrAttachmentTooLarge for the total limit", err)
	}
	if !strings.Contains(err.Error(), "17.0 MB") {
		t.Fatalf("total-limit error must name the limit, got %q", err)
	}
	// 两次超限都发生在进入 SMTP 会话之前：一个字节都不许出站（等价于 CRLF 用例的断言强度）。
	if f.data != "" || len(f.rcpts) != 0 {
		t.Fatalf("nothing should reach the server, got rcpts=%v data=%d bytes", f.rcpts, len(f.data))
	}

	// 刚好卡在上限内（单附件 10 MB、总量 17 MB）必须放行。
	ok := []Attachment{
		{Name: "x.xlsx", Data: make([]byte, MaxAttachmentSize)},
		{Name: "y.xlsx", Data: make([]byte, MaxAttachmentsTotalSize-MaxAttachmentSize)},
	}
	if err := c.Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "s", Text: "t", Attachments: ok}); err != nil {
		t.Fatalf("at-limit attachments must be accepted, got %v", err)
	}
	if f.data == "" {
		t.Fatal("at-limit message should have reached the server")
	}

	for _, atts := range [][]Attachment{overSingle, overTotal} {
		if err := CheckAttachments(atts); !errors.Is(err, ErrAttachmentTooLarge) {
			t.Fatalf("CheckAttachments = %v, want ErrAttachmentTooLarge", err)
		}
	}
}

// CheckAttachments 是给调用方的同步判定入口（报表订阅靠它不看日志就知道附件发不出去）。
func TestCheckAttachmentsIsSynchronous(t *testing.T) {
	if err := CheckAttachments(nil); err != nil {
		t.Fatalf("nil attachments must pass: %v", err)
	}
	if err := CheckAttachments([]Attachment{}); err != nil {
		t.Fatalf("empty attachments must pass: %v", err)
	}
	if err := CheckAttachments([]Attachment{{Name: "ok.xlsx", Data: []byte("x")}}); err != nil {
		t.Fatalf("small attachment must pass: %v", err)
	}
	err := CheckAttachments([]Attachment{{Name: "月度报表.xlsx", Data: make([]byte, MaxAttachmentSize+1)}})
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("err = %v, want ErrAttachmentTooLarge", err)
	}
	// 文案要把实际大小与上限都写出来：这条最终以通知/日志形式暴露给运维。
	msg := err.Error()
	if !strings.Contains(msg, "月度报表.xlsx") || !strings.Contains(msg, "10.0 MB") {
		t.Fatalf("error text must carry the name and the limit: %q", msg)
	}
	// 匿名附件也要有个可辨识的序号，而不是空括号。
	err = CheckAttachments([]Attachment{{Data: make([]byte, MaxAttachmentSize+1)}})
	if err == nil || !strings.Contains(err.Error(), "attachment 1 ") {
		t.Fatalf("unnamed attachment must fall back to an index label: %v", err)
	}
}

func TestHeaderInjectionRejected(t *testing.T) {
	f := newFake(t, &fakeSMTP{})
	c := client(t, f, SecurityNone)
	c.cfg.Username = ""
	bad := []Message{
		{To: []string{"a@example.org"}, Subject: "hi\r\nBcc: evil@example.org", Text: "t"},
		{To: []string{"a@example.org\r\nBcc: evil@example.org"}, Subject: "s", Text: "t"},
		{To: []string{"a@example.org\nX: y"}, Subject: "s", Text: "t"},
		{To: []string{"not-an-address"}, Subject: "s", Text: "t"},
		{To: []string{"a@example.org, b@example.org"}, Subject: "s", Text: "t"},
		{To: nil, Subject: "s", Text: "t"},
	}
	for i, m := range bad {
		if err := c.Send(context.Background(), m); err == nil {
			t.Errorf("case %d: expected rejection", i)
		}
	}
	if f.data != "" || len(f.rcpts) != 0 {
		t.Fatalf("nothing should reach the server, got rcpts=%v", f.rcpts)
	}
	if _, err := New(Config{Host: "h", Port: 25, FromAddr: "a@example.org", FromName: "x\r\nBcc: y"}); err == nil {
		t.Fatal("from name with CRLF must be rejected")
	}
	if _, err := ParseAddress("a@example.org\r\n"); err != nil { // 末尾空白被 TrimSpace，属于无害输入
		t.Fatalf("trailing CRLF is trimmed: %v", err)
	}
	if _, err := ParseAddress("a@exa\r\nmple.org"); err == nil {
		t.Fatal("embedded CRLF must be rejected")
	}
}

func TestTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { // 接受连接但永不应答
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	c, _ := New(Config{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Security: SecurityNone, FromAddr: "a@example.org", Timeout: 300 * time.Millisecond})
	start := time.Now()
	if err := c.Send(context.Background(), Message{To: []string{"b@example.org"}, Subject: "s", Text: "t"}); err == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("timeout not honoured: %v", time.Since(start))
	}
}

func TestTemplatesEscapeAndOverride(t *testing.T) {
	d := Data{SiteName: "Nex\r\nBcc: x@y", Name: `<script>alert(1)</script>`, Code: "123456", Link: "javascript:alert(1)", Username: "a&b", Reason: "created"}
	subj, text, html, err := Templates{}.Render(TplAccountCreated, "zh-CN", d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(subj, "\r\n") {
		t.Fatalf("subject has CR/LF: %q", subj)
	}
	if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") || strings.Contains(html, "javascript:") {
		t.Fatalf("html not escaped: %s", html)
	}
	if !strings.Contains(text, "创建") {
		t.Fatalf("text = %q", text)
	}
	_, _, enHTML, err := Templates{}.Render(TplForgotCode, "en", Data{SiteName: "S", Code: "654321"})
	if err != nil || !strings.Contains(enHTML, "654321") || !strings.Contains(enHTML, "password") {
		t.Fatalf("en render: %v %s", err, enHTML)
	}
	for _, n := range []string{TplForgotCode, TplAccountCreated, TplSecurityAlert, TplNotice} {
		for _, l := range []string{LangZH, LangEN} {
			if _, _, _, err := (Templates{}).Render(n, l, Data{SiteName: "S"}); err != nil {
				t.Errorf("%s/%s: %v", n, l, err)
			}
		}
	}
	if _, _, _, err := (Templates{}).Render("../etc/passwd", "en-US", Data{}); err == nil {
		t.Fatal("unknown template name must be rejected")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, TplForgotCode+".zh-CN.html"), []byte(`<b>自定义 {{.Code}} {{.Name}}</b>`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, text, html, err = Templates{Dir: dir}.Render(TplForgotCode, "zh", Data{Code: "111222", Name: "<i>"})
	if err != nil || !strings.Contains(html, "自定义 111222 &lt;i&gt;") || !strings.Contains(text, "111222") {
		t.Fatalf("override: %v html=%s text=%s", err, html, text)
	}
	_, _, html, _ = Templates{Dir: dir}.Render(TplForgotCode, "en-US", Data{Code: "333"}) // 没有 en-US 覆盖文件 → 内置
	if strings.Contains(html, "自定义") {
		t.Fatalf("override leaked across languages: %s", html)
	}
}
