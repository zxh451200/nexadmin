package tenant

import (
	"net"
	"regexp"
	"strings"

	"nexadmin/core/response"
)

// 域名标签：字母数字开头结尾，中间可含连字符，1-63 位（含 punycode 的 xn--）。
var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// 保留的顶级/特殊用途后缀（RFC 2606 / 6761 等）与示例域名：不能被绑定为租户域名。
var (
	reservedSuffixes = []string{"localhost", "local", "localdomain", "internal", "invalid", "test", "example", "lan", "home.arpa", "onion"}
	reservedApex     = []string{"example.com", "example.net", "example.org"}
)

func domainErr(key string) error {
	return response.BadRequest(key)
}

// NormalizeDomain 规整并校验租户域名：去空白与结尾的点、转小写；
// 必须是至少两段的主机名（不含端口、路径、协议），不能是 IP、localhost 或保留域名。
func NormalizeDomain(raw string) (string, error) {
	d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if d == "" || len(d) > 253 || strings.ContainsAny(d, " \t/:@?#*_") {
		return "", domainErr("tenant.domain_invalid")
	}
	if net.ParseIP(d) != nil {
		return "", domainErr("tenant.domain_reserved") // IP 地址
	}
	labels := strings.Split(d, ".")
	for _, l := range labels {
		if !labelRe.MatchString(l) {
			return "", domainErr("tenant.domain_invalid")
		}
	}
	if len(labels) < 2 {
		if d == "localhost" {
			return "", domainErr("tenant.domain_reserved")
		}
		return "", domainErr("tenant.domain_invalid")
	}
	// 顶级标签不能全是数字（拦截 1.2.3.4 之类的点分数字，含八进制/缩写写法）
	if tld := labels[len(labels)-1]; strings.Trim(tld, "0123456789") == "" {
		return "", domainErr("tenant.domain_reserved")
	}
	if IsReservedDomain(d) {
		return "", domainErr("tenant.domain_reserved")
	}
	return d, nil
}

// IsReservedDomain 判断（已规整的）域名是否属于保留域名或其子域。
func IsReservedDomain(d string) bool {
	for _, s := range reservedSuffixes {
		if d == s || strings.HasSuffix(d, "."+s) {
			return true
		}
	}
	for _, a := range reservedApex {
		if d == a || strings.HasSuffix(d, "."+a) {
			return true
		}
	}
	return false
}
