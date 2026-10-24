package cron

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

// SSRF 防护（http 处理器）
//
// 任务由后台管理员配置，但管理员不应能借服务器去探测内网（元数据服务 169.254.169.254、
// 数据库、Redis、内部管理端口等），所以 http 处理器默认拒绝一切非公网目标。三层防线：
//  1. 保存/执行前：只允许 http/https；URL 里不能带账号密码；字面 IP 和 localhost 直接判定。
//  2. 执行前：域名解析出的"所有"地址都必须是公网地址（任何一个是内网就拒绝）。
//  3. 连接时：net.Dialer.Control 对"实际要连接的地址"再校验一次，堵住 DNS rebinding
//     （校验时解析到公网、连接时解析到内网）以及重定向到内网的情况；每次重定向都会重新走第 3 层。
//
// 被拒绝的范围：回环、私有网段（10/8、172.16/12、192.168/16、fc00::/7）、链路本地（169.254/16、fe80::/10）、
// 未指定地址、组播、CGNAT（100.64/10）、基准测试网段（198.18/15）、保留与文档网段，以及 IPv4-mapped IPv6。
// 同时不使用环境变量里的代理（避免请求经代理绕过校验）。

// ErrBlockedAddress 表示目标地址不在允许范围内。
var ErrBlockedAddress = errors.New("cron: target address is not allowed (internal/private network)")

var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8",       // "this" network
	"100.64.0.0/10",   // CGNAT
	"192.0.0.0/24",    // IETF 协议分配
	"192.0.2.0/24",    // TEST-NET-1
	"198.18.0.0/15",   // 基准测试
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"240.0.0.0/4",     // 保留 + 广播
	"64:ff9b::/96",    // NAT64（可借此访问内网 IPv4）
	"2001:db8::/32",   // 文档
)

func mustPrefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

// checkIP 判断一个 IP 是否可以被 http 处理器访问；不允许时返回 ErrBlockedAddress。
func checkIP(ip net.IP) error {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return fmt.Errorf("%w: invalid ip", ErrBlockedAddress)
	}
	addr = addr.Unmap().WithZone("") // ::ffff:10.0.0.1 -> 10.0.0.1
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() || !addr.IsGlobalUnicast() {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, addr)
	}
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, addr)
		}
	}
	return nil
}

// lookupFunc 抽出来便于单测注入。
type lookupFunc func(ctx context.Context, host string) ([]net.IP, error)

func defaultLookup(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// validateURLStatic 只做不联网的检查（保存任务时用）。
func validateURLStatic(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("cron: invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("cron: only http and https urls are allowed")
	}
	if u.User != nil {
		return nil, errors.New("cron: credentials in url are not allowed")
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("cron: url has no host")
	}
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".internal") ||
		strings.HasSuffix(lower, ".local") {
		return nil, fmt.Errorf("%w: %s", ErrBlockedAddress, host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := checkIP(ip); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// validateURL 在 validateURLStatic 之上解析域名，要求所有解析结果都是公网地址。
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
		return nil, fmt.Errorf("cron: resolve %s: %w", u.Hostname(), err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("cron: resolve %s: no addresses", u.Hostname())
	}
	for _, ip := range ips {
		if err := checkIP(ip); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// safeDialControl 在真正 connect 之前校验目标地址（第 3 层防线）。
func safeDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
	}
	return checkIP(ip)
}

// newSafeClient 返回禁用代理、连接时校验地址、限制重定向的 HTTP 客户端。
func newSafeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: safeDialControl}
	tr := &http.Transport{
		Proxy:                 nil, // 不走环境代理
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          2,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("cron: too many redirects")
			}
			// 重定向目标同样只允许 http/https 和非内网主机（连接时 Control 还会再兜底一次）
			_, err := validateURLStatic(req.URL.String())
			return err
		},
	}
}
