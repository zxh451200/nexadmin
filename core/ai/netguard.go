package ai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrBlockedAddress 表示 AI base_url 指向了不允许的地址（链路本地 / 云元数据 / 默认情况下的私网与回环）。
var ErrBlockedAddress = errors.New("ai: base_url address not allowed")

// Resolver 是 DNS 解析接口；测试可以替换成假实现来模拟 DNS rebinding。
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// 无论是否允许私网，都拒绝的云元数据主机名。
var metadataHosts = map[string]bool{
	"metadata": true, "metadata.google.internal": true, "instance-data": true,
	"instance-data.ec2.internal": true, "metadata.tencentyun.com": true,
}

// 无论是否允许私网，都拒绝的元数据地址（链路本地段另由 IsLinkLocal* 覆盖）。
var metadataIPs = []net.IP{
	net.ParseIP("fd00:ec2::254"),   // AWS IMDS IPv6
	net.ParseIP("100.100.100.200"), // 阿里云
	net.ParseIP("192.0.0.192"),     // Oracle Cloud
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// embedded 取出 IPv4 映射 / NAT64 (64:ff9b::/96) 里嵌入的 IPv4，防止用 IPv6 写法绕过检查。
func embedded(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	if len(ip) == net.IPv6len && ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b {
		allZero := true
		for _, b := range ip[4:12] {
			if b != 0 {
				allZero = false
			}
		}
		if allZero {
			return net.IP(ip[12:16])
		}
	}
	return ip
}

// checkIP 检查一个已解析的 IP：永久拒绝链路本地 / 元数据 / 未指定 / 多播 / 广播；
// allowPrivate 为 false 时再拒绝回环、私网与运营商级 NAT 段。
func checkIP(ip net.IP, allowPrivate bool) error {
	ip = embedded(ip)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.Equal(net.IPv4bcast) {
		return ErrBlockedAddress
	}
	for _, m := range metadataIPs {
		if ip.Equal(m) {
			return ErrBlockedAddress
		}
	}
	if !allowPrivate && (ip.IsLoopback() || ip.IsPrivate() || cgnat.Contains(ip)) {
		return ErrBlockedAddress
	}
	return nil
}

func lookup(ctx context.Context, r Resolver, host string) ([]net.IP, error) {
	if r == nil {
		r = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := r.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("%w: cannot resolve host", ErrBlockedAddress)
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

// checkHost 检查主机名或 IP 字面量；主机名解析出的每个地址都必须通过（混合应答也拒绝）。
func checkHost(ctx context.Context, host string, allowPrivate bool, r Resolver) ([]net.IP, error) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" || metadataHosts[host] {
		return nil, ErrBlockedAddress
	}
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, checkIP(ip, allowPrivate)
	}
	if !allowPrivate && (host == "localhost" || strings.HasSuffix(host, ".localhost")) {
		return nil, ErrBlockedAddress
	}
	ips, err := lookup(ctx, r, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if err := checkIP(ip, allowPrivate); err != nil {
			return nil, err
		}
	}
	return ips, nil
}

// ValidateBaseURL 在保存配置时校验 AI base_url：仅 http / https、无内嵌账号、
// 主机（含 DNS 解析结果）不得是链路本地 / 云元数据，默认也不得是私网 / 回环。
func ValidateBaseURL(ctx context.Context, raw string, allowPrivate bool, r Resolver) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return ErrBadInput
	}
	_, err = checkHost(ctx, u.Hostname(), allowPrivate, r)
	return err
}

// guardedTransport 在连接时再次校验目标 IP（防 DNS rebinding）：先解析并检查全部地址，
// 再直接拨号到已检查的 IP，Dialer.Control 对实际连接的地址做最后一道检查。不使用环境代理。
func guardedTransport(allowPrivate bool, r Resolver) *http.Transport {
	d := &net.Dialer{
		Timeout: 10 * time.Second, KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return ErrBlockedAddress
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return ErrBlockedAddress
			}
			return checkIP(ip, allowPrivate)
		},
	}
	return &http.Transport{
		Proxy: nil, // 代理会绕过拨号校验
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, ErrBlockedAddress
			}
			ips, err := checkHost(ctx, host, allowPrivate, r)
			if err != nil {
				return nil, err
			}
			var last error
			for _, ip := range ips {
				c, derr := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if derr == nil {
					return c, nil
				}
				last = derr
			}
			return nil, last
		},
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        16,
		IdleConnTimeout:     60 * time.Second,
	}
}
