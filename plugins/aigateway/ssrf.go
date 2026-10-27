package aigateway

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

// SSRF 防护（自 plugins/cron/ssrf.go 移植，错误前缀改为 aigateway）。
//
// 模型 BaseURL 由管理员配置，但管理员不应能借服务器去探测内网（元数据服务
// 169.254.169.254、数据库、Redis、内部管理端口等），所以默认拒绝一切非公网目标。
// 三层防线：保存前静态校验；执行前解析域名全公网；连接时 Dialer.Control 再校验
// 实际地址（堵 DNS rebinding 与重定向）。
//
// 已知取舍：自建模型常在内网（vLLM/Ollama），被此防线拒绝时只能经反代以域名
// 暴露公网口，或在本插件后续版本引入显式内网白名单后再放开。
var ErrBlockedAddress = errors.New("aigateway: target address is not allowed (internal/private network)")

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

// checkIP 判断一个 IP 是否可以访问；不允许时返回 ErrBlockedAddress。
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

// validateURLStatic 只做不联网的检查（保存配置时用）。
func validateURLStatic(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("aigateway: invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("aigateway: only http and https urls are allowed")
	}
	if u.User != nil {
		return nil, errors.New("aigateway: credentials in url are not allowed")
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("aigateway: url has no host")
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
		return nil, fmt.Errorf("aigateway: resolve %s: %w", u.Hostname(), err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("aigateway: resolve %s: no addresses", u.Hostname())
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
				return errors.New("aigateway: too many redirects")
			}
			_, err := validateURLStatic(req.URL.String())
			return err
		},
	}
}
