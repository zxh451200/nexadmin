// Package mail 是邮件发送能力（不是模块）：标准库 net/smtp + crypto/tls 的 SMTP 客户端，
// 支持无加密 / STARTTLS / SMTPS(465)、PLAIN / LOGIN 认证、整体超时；
// 头部字段一律拒绝 CR/LF，收件人用 net/mail 解析校验；正文为 text+html multipart，主题按 RFC 2047 编码。
// 模板见 template.go（html/template 自动转义）。
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// 加密方式。
const (
	SecurityNone     = "none"
	SecuritySTARTTLS = "starttls"
	SecuritySSL      = "ssl"
)

// Config 是 SMTP 连接与发件人配置。
type Config struct {
	Host      string
	Port      int
	Security  string // none | starttls | ssl；空 = 按端口推断（465 → ssl，否则 starttls）
	Username  string // 空 = 不认证
	Password  string
	FromName  string
	FromAddr  string
	Timeout   time.Duration // 整个会话的超时，默认 15s
	LocalName string        // EHLO 名称，默认 localhost

	tlsConfig *tls.Config // 测试用：信任自签证书
}

// Client 是 SMTP 客户端；每次 Send 建立一条新连接，可并发使用。
type Client struct{ cfg Config }

// New 校验配置并创建客户端。
func New(cfg Config) (*Client, error) {
	cfg.Host = strings.TrimSpace(cfg.Host)
	if cfg.Host == "" || strings.ContainsAny(cfg.Host, " \r\n/") {
		return nil, errors.New("mail: invalid smtp host")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, errors.New("mail: invalid smtp port")
	}
	switch strings.ToLower(cfg.Security) {
	case "":
		if cfg.Port == 465 {
			cfg.Security = SecuritySSL
		} else {
			cfg.Security = SecuritySTARTTLS
		}
	case SecurityNone, SecuritySTARTTLS, SecuritySSL:
		cfg.Security = strings.ToLower(cfg.Security)
	default:
		return nil, fmt.Errorf("mail: unknown security %q (want none, starttls or ssl)", cfg.Security)
	}
	from, err := ParseAddress(cfg.FromAddr)
	if err != nil {
		return nil, fmt.Errorf("mail: invalid from address: %w", err)
	}
	cfg.FromAddr = from
	if err := checkHeader("from name", cfg.FromName); err != nil {
		return nil, err
	}
	if err := checkHeader("username", cfg.Username); err != nil {
		return nil, err
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.LocalName == "" {
		cfg.LocalName = "localhost"
	}
	return &Client{cfg: cfg}, nil
}

// Send 发送一封邮件。收件人逐个校验，任何一个非法整封拒绝。
func (c *Client) Send(ctx context.Context, m Message) error {
	to := make([]string, 0, len(m.To))
	for _, r := range m.To {
		a, err := ParseAddress(r)
		if err != nil {
			return err
		}
		to = append(to, a)
	}
	data, err := build(c.cfg.FromName, c.cfg.FromAddr, to, m.Subject, m.Text, m.HTML, time.Now())
	if err != nil {
		return err
	}

	deadline := time.Now().Add(c.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	addr := net.JoinHostPort(c.cfg.Host, strconv.Itoa(c.cfg.Port))
	conn, err := (&net.Dialer{Deadline: deadline}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mail: connect: %w", err)
	}
	_ = conn.SetDeadline(deadline)
	tlsCfg := c.cfg.tlsConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{ServerName: c.cfg.Host, MinVersion: tls.VersionTLS12}
	}
	if c.cfg.Security == SecuritySSL {
		conn = tls.Client(conn, tlsCfg)
	}
	cl, err := smtp.NewClient(conn, c.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("mail: greeting: %w", err)
	}
	defer cl.Close() //nolint:errcheck
	if err := cl.Hello(c.cfg.LocalName); err != nil {
		return fmt.Errorf("mail: ehlo: %w", err)
	}
	if c.cfg.Security == SecuritySTARTTLS {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errors.New("mail: server does not support STARTTLS")
		}
		if err := cl.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("mail: starttls: %w", err)
		}
	}
	if c.cfg.Username != "" {
		auth, err := c.pickAuth(cl)
		if err != nil {
			return err
		}
		if err := cl.Auth(auth); err != nil {
			return fmt.Errorf("mail: auth: %w", err)
		}
	}
	if err := cl.Mail(c.cfg.FromAddr); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	for _, r := range to {
		if err := cl.Rcpt(r); err != nil {
			return fmt.Errorf("mail: RCPT TO: %w", err)
		}
	}
	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("mail: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: end of data: %w", err)
	}
	_ = cl.Quit()
	return nil
}

// pickAuth 在服务器声明的机制里优先选 PLAIN，其次 LOGIN。
func (c *Client) pickAuth(cl *smtp.Client) (smtp.Auth, error) {
	ok, params := cl.Extension("AUTH")
	if !ok {
		return nil, errors.New("mail: server does not support AUTH")
	}
	mechs := " " + strings.ToUpper(params) + " "
	switch {
	case strings.Contains(mechs, " PLAIN "):
		return &plainAuth{user: c.cfg.Username, pass: c.cfg.Password, host: c.cfg.Host}, nil
	case strings.Contains(mechs, " LOGIN "):
		return &loginAuth{user: c.cfg.Username, pass: c.cfg.Password, host: c.cfg.Host}, nil
	}
	return nil, fmt.Errorf("mail: no supported AUTH mechanism (server offers %q)", params)
}

// allowCleartext 只允许在 TLS 之上，或服务器就是本机时发送凭据。
func allowCleartext(s *smtp.ServerInfo, host string) bool {
	if s.TLS {
		return true
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

type plainAuth struct{ user, pass, host string }

func (a *plainAuth) Start(s *smtp.ServerInfo) (string, []byte, error) {
	if !allowCleartext(s, a.host) {
		return "", nil, errors.New("mail: refusing to send password over an unencrypted connection")
	}
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}

func (a *plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("mail: unexpected server challenge")
	}
	return nil, nil
}

type loginAuth struct{ user, pass, host string }

func (a *loginAuth) Start(s *smtp.ServerInfo) (string, []byte, error) {
	if !allowCleartext(s, a.host) {
		return "", nil, errors.New("mail: refusing to send password over an unencrypted connection")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(challenge []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(challenge))) {
	case "username:":
		return []byte(a.user), nil
	case "password:":
		return []byte(a.pass), nil
	}
	return nil, fmt.Errorf("mail: unexpected LOGIN challenge %q", challenge)
}
