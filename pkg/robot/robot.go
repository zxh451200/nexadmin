// Package robot 是群机器人通知能力（不是模块）：钉钉（含加签）、飞书（含签名）、企业微信机器人，
// 文本 / markdown 两种格式，统一 Send(ctx, title, content)。
//
// 安全：webhook URL 仅允许 https；本包独立实现 SSRF 防护——在拨号时（DNS 解析之后）检查实际 IP，
// 拒绝回环、内网、链路本地、CGNAT 等地址，因此域名指向内网和 DNS 重绑定都会被拦住；不跟随重定向；响应体限长。
package robot

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// 机器人类型。
const (
	DingTalk = "dingtalk"
	Feishu   = "feishu"
	WeCom    = "wecom"
)

// Config 是一个机器人的配置。
type Config struct {
	Type     string // dingtalk | feishu | wecom
	URL      string // webhook 地址，必须是 https
	Secret   string // 钉钉加签密钥 / 飞书签名校验密钥；企业微信不用；可空
	Markdown bool   // true = markdown，false = 纯文本
	Timeout  time.Duration

	// 以下仅测试使用（包内）：放行回环地址、信任自签证书、固定时间戳。
	allowLoopback bool
	tlsConfig     *tls.Config
	now           func() time.Time
}

// Client 是一个机器人客户端，可并发使用。
type Client struct {
	cfg  Config
	http *http.Client
}

// ErrBlockedAddr 表示目标地址属于被拒绝的网段。
var ErrBlockedAddr = errors.New("robot: destination address is not allowed")

// ValidateURL 只做静态检查：https、有主机名、无内嵌凭据；IP 字面量直接按网段判断。
// 域名的实际解析结果在拨号时再检查。
func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("robot: invalid url: %w", err)
	}
	if u.Scheme != "https" {
		return nil, errors.New("robot: url must use https")
	}
	if u.Hostname() == "" || u.User != nil {
		return nil, errors.New("robot: url must have a host and no credentials")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && blocked(ip) {
		return nil, ErrBlockedAddr
	}
	return u, nil
}

var extraBlocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64：可借此到达内网 IPv4
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// blocked 判断一个 IP 是否属于不允许访问的网段。IPv4 映射的 IPv6 先还原再判断。
func blocked(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, p := range extraBlocked {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// New 校验配置并创建客户端。
func New(cfg Config) (*Client, error) {
	cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
	switch cfg.Type {
	case DingTalk, Feishu, WeCom:
	default:
		return nil, fmt.Errorf("robot: unknown type %q (want dingtalk, feishu or wecom)", cfg.Type)
	}
	if _, err := ValidateURL(cfg.URL); err != nil {
		if !(cfg.allowLoopback && errors.Is(err, ErrBlockedAddr)) {
			return nil, err
		}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	allowLoop := cfg.allowLoopback
	dialer := &net.Dialer{
		Timeout: cfg.Timeout,
		// Control 在 DNS 解析完成、真正 connect 之前被调用，address 是实际要连的 ip:port。
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return ErrBlockedAddr
			}
			if blocked(ap.Addr()) && !(allowLoop && ap.Addr().Unmap().IsLoopback()) {
				return ErrBlockedAddr
			}
			return nil
		},
	}
	tr := &http.Transport{
		Proxy:                 nil, // 不走环境变量代理：代理会绕过上面的地址检查
		DialContext:           dialer.DialContext,
		TLSClientConfig:       cfg.tlsConfig,
		TLSHandshakeTimeout:   cfg.Timeout,
		ResponseHeaderTimeout: cfg.Timeout,
		DisableKeepAlives:     true,
	}
	return &Client{cfg: cfg, http: &http.Client{
		Transport:     tr,
		Timeout:       cfg.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Send 发送一条消息；失败（网络错误 / 5xx / 429）时重试一次。title 可空。
func (c *Client) Send(ctx context.Context, title, content string) error {
	err := c.once(ctx, title, content)
	if err == nil || !retryable(err) || ctx.Err() != nil {
		return err
	}
	select {
	case <-time.After(300 * time.Millisecond):
	case <-ctx.Done():
		return err
	}
	return c.once(ctx, title, content)
}

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return fmt.Sprintf("robot: http status %d", e.code) }

func retryable(err error) bool {
	if errors.Is(err, ErrBlockedAddr) {
		return false
	}
	var he *httpStatusError
	if errors.As(err, &he) {
		return he.code >= 500 || he.code == http.StatusTooManyRequests
	}
	var ae *apiError
	return !errors.As(err, &ae) // 业务错误码（签名错、关键词不匹配等）重试没有意义；其余视为网络错误
}

func (c *Client) once(ctx context.Context, title, content string) error {
	target, body, err := c.build(title, content)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("robot: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK { // 含 3xx：不跟随重定向，一律当失败
		return &httpStatusError{code: resp.StatusCode}
	}
	return c.checkReply(raw)
}
