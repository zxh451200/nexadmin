package openapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Webhook 出站请求的 SSRF 防护（本模块独立实现）。
//
// 三层防线：
//  1. 保存/发送前的静态检查：只允许 http/https、不带账号密码、拒绝 localhost 类主机名和字面内网 IP；
//  2. 发送前解析域名，解析出的"每一个"地址都必须是公网地址；
//  3. 连接时 net.Dialer.Control 对真正要 connect 的地址再校验一次（挡 DNS rebinding）。
//
// 另外：不使用环境代理；永不跟随重定向（3xx 直接按失败记录），所以"重定向到内网"根本不会发生。
// 被拒绝：回环、私有、链路本地（含云元数据 169.254.169.254）、未指定、组播、CGNAT、基准测试、保留/文档网段、IPv4-mapped IPv6。

// errBlockedAddress 表示目标地址不在允许范围内。
var errBlockedAddress = errors.New("webhook: target address is not allowed (internal/private network)")

var blockedNets = mustPrefixes(
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
	"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "2001:db8::/32",
)

func mustPrefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

func checkIP(ip net.IP) error {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return fmt.Errorf("%w: invalid ip", errBlockedAddress)
	}
	addr = addr.Unmap().WithZone("")
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() || !addr.IsGlobalUnicast() {
		return fmt.Errorf("%w: %s", errBlockedAddress, addr)
	}
	for _, p := range blockedNets {
		if p.Contains(addr) {
			return fmt.Errorf("%w: %s", errBlockedAddress, addr)
		}
	}
	return nil
}

type lookupFunc func(ctx context.Context, host string) ([]net.IP, error)

func defaultLookup(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// validateURLStatic 只做不联网的检查（保存 Webhook 时用）。
func validateURLStatic(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("webhook: invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("webhook: only http and https urls are allowed")
	}
	if u.User != nil {
		return nil, errors.New("webhook: credentials in url are not allowed")
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("webhook: url has no host")
	}
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".internal") || strings.HasSuffix(lower, ".local") {
		return nil, fmt.Errorf("%w: %s", errBlockedAddress, host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := checkIP(ip); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// validateURL 在静态检查之上解析域名，要求所有解析结果都是公网地址。
func validateURL(ctx context.Context, raw string, lookup lookupFunc) (*url.URL, error) {
	u, err := validateURLStatic(raw)
	if err != nil {
		return nil, err
	}
	if net.ParseIP(u.Hostname()) != nil {
		return u, nil
	}
	ips, err := lookup(ctx, u.Hostname())
	if err != nil {
		return nil, fmt.Errorf("webhook: resolve %s: %w", u.Hostname(), err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("webhook: resolve %s: no addresses", u.Hostname())
	}
	for _, ip := range ips {
		if err := checkIP(ip); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// safeDialControl 在 connect 之前校验"实际要连接的地址"。
func safeDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: %s", errBlockedAddress, host)
	}
	return checkIP(ip)
}

// newClient 返回禁用代理、连接时校验地址、永不跟随重定向的 HTTP 客户端；control 可注入（单测用）。
func newClient(timeout time.Duration, control func(network, address string, c syscall.RawConn) error) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: control}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
		MaxIdleConns:          2,
	}
	return &http.Client{
		Transport:     tr,
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
