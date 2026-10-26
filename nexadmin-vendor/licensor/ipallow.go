package main

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// stringList 是可重复的 flag，每个值可含逗号分隔的多项。
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// loopbackNets 是默认白名单。
var loopbackNets = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}

// parseNets 解析 IP/CIDR 列表（元素内可再用逗号分隔）。单个 IP 视为 /32 或 /128；IPv4-mapped IPv6 归一为 IPv4。
func parseNets(specs []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, spec := range specs {
		for _, it := range strings.Split(spec, ",") {
			it = strings.TrimSpace(it)
			if it == "" {
				continue
			}
			if strings.Contains(it, "/") {
				p, err := netip.ParsePrefix(it)
				if err != nil {
					return nil, fmt.Errorf("无效的 CIDR %q", it)
				}
				out = append(out, p.Masked())
				continue
			}
			a, err := netip.ParseAddr(it)
			if err != nil {
				return nil, fmt.Errorf("无效的 IP %q", it)
			}
			a = a.Unmap().WithZone("")
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return out, nil
}

func inNets(nets []netip.Prefix, a netip.Addr) bool {
	for _, p := range nets {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// clientIP 按 TCP 的 RemoteAddr 判定来源；只有 RemoteAddr 属于 trusted 时，才取 X-Forwarded-For
// 从右往左第一个不属于 trusted 的 IP。XFF 里出现无法解析的项则返回无效地址（调用方拒绝）。
func clientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	remote = remote.Unmap().WithZone("")
	if len(trusted) == 0 || !inNets(trusted, remote) {
		return remote
	}
	var parts []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		parts = append(parts, strings.Split(h, ",")...)
	}
	for i := len(parts) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			return netip.Addr{}
		}
		a = a.Unmap().WithZone("")
		if !inNets(trusted, a) {
			return a
		}
	}
	return remote // 链上全是受信代理（或无 XFF）：以连接对端为准
}

// ipGuard 是 IP 白名单中间件。拒绝时返回空 body 的 403（不暴露应用信息），只记 IP 和时间。
func ipGuard(allow, trusted []netip.Prefix, next http.Handler, logf func(string, ...any), now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r, trusted)
		if !ip.IsValid() || !inNets(allow, ip) {
			s := "invalid"
			if ip.IsValid() {
				s = ip.String()
			}
			logf("拒绝访问 ip=%s time=%s", s, now().UTC().Format(time.RFC3339))
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkListen：监听非回环地址而没有显式白名单时拒绝启动。
func checkListen(addr string, allowConfigured bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--addr 格式错误: %w", err)
	}
	if host == "localhost" {
		return nil
	}
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil && ip.Unmap().IsLoopback() {
		return nil
	}
	if !allowConfigured {
		return fmt.Errorf("监听 %s 不是回环地址，必须同时设置 --allow-ip（或环境变量 LICENSOR_ALLOW_IP）；已拒绝启动", addr)
	}
	return nil
}
