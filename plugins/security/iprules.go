package security

import (
	"errors"
	"net/netip"
	"strings"
)

// 规则动作与作用范围。
const (
	ActionAllow = "allow"
	ActionDeny  = "deny"

	ScopeAll   = "all"   // 全站登录：PreLogin 阶段对所有登录入口生效
	ScopeSuper = "super" // 仅超管登录：凭据通过、知道是超管之后（LoginGate.Check）才生效
)

// normalizeCIDR 把单个 IP 或 CIDR 规范化为 netip.Prefix 的字符串（单个 IP 补 /32 或 /128，主机位清零，
// IPv4 映射的 IPv6 还原成 IPv4）。拒绝带 zone 的地址。
func normalizeCIDR(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 {
		return netip.Prefix{}, errors.New("empty or too long")
	}
	var p netip.Prefix
	if strings.Contains(s, "/") {
		var err error
		if p, err = netip.ParsePrefix(s); err != nil {
			return netip.Prefix{}, err
		}
	} else {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return netip.Prefix{}, err
		}
		if a.Zone() != "" { // PrefixFrom 会悄悄丢掉 zone，所以要在这之前拒绝
			return netip.Prefix{}, errors.New("zone not allowed")
		}
		p = netip.PrefixFrom(a, a.BitLen())
	}
	if a := p.Addr(); a.Is4In6() {
		bits := p.Bits() - 96
		if bits < 0 { // ::ffff:0:0/80 之类跨越映射边界的写法没有 IPv4 含义
			return netip.Prefix{}, errors.New("unsupported IPv4-mapped prefix length")
		}
		p = netip.PrefixFrom(a.Unmap(), bits)
	}
	return p.Masked(), nil
}

// parseClientIP 解析 c.ClientIP() 的结果；无法解析时 ok=false。
func parseClientIP(s string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return netip.Addr{}, false
	}
	return a.WithZone("").Unmap(), true
}

// ruleView 是匹配用的最小规则视图。
type ruleView struct {
	Action  string
	Scope   string
	Prefix  netip.Prefix
	Enabled bool
}

// decide 判断 ip 在 scope 这一组规则下是否放行：deny 命中立即拒绝（优先于 allow）；
// 该组有启用的 allow 规则时，ip 必须命中其中之一；没有任何启用规则时放行。
// ip 无法解析（ok=false）时：只要有 allow 规则就拒绝，deny 规则无从命中。
func decide(rules []ruleView, scope string, ip netip.Addr, ok bool) bool {
	hasAllow, allowed := false, false
	for _, r := range rules {
		if !r.Enabled || r.Scope != scope {
			continue
		}
		hit := ok && r.Prefix.Contains(ip)
		switch r.Action {
		case ActionDeny:
			if hit {
				return false
			}
		case ActionAllow:
			hasAllow = true
			if hit {
				allowed = true
			}
		}
	}
	return !hasAllow || allowed
}

// allowLogin 综合判断：scope=all 的规则对所有人生效，scope=super 的规则只对超管额外生效。
func allowLogin(rules []ruleView, ip string, super bool) bool {
	a, ok := parseClientIP(ip)
	if !decide(rules, ScopeAll, a, ok) {
		return false
	}
	if super && !decide(rules, ScopeSuper, a, ok) {
		return false
	}
	return true
}
