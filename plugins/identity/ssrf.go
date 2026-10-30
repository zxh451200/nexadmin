package identity

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/spf13/viper"
)

// SSRF 防护：LDAP 地址与 CAS serverUrl 都由租户管理员填写，服务端会主动连接它们（登录、测试连接、目录同步）。
// 默认拒绝回环 / 私网 / 链路本地（含云元数据 169.254.169.254）/ 未指定 / 组播 / CGNAT 地址。
// 校验分两层：连接前解析并检查全部 A/AAAA 记录；真正 connect 时在 Dialer.Control 里对实际 IP 再查一次（防 DNS 重绑定）。
// 内网部署的目录服务本来就在私网里，需要部署方显式放行：配置 identity.allow_private_hosts: true
// （config/*.yaml 或环境变量 NEXADMIN_IDENTITY_ALLOW_PRIVATE_HOSTS=true）。

var allowPrivateHosts atomic.Bool

var errBlockedHost = errors.New("identity: address is not allowed (loopback, private, link-local or metadata range)")

func setAllowPrivateHosts(v bool) { allowPrivateHosts.Store(v) }

// loadAllowPrivateHosts 读取 identity.allow_private_hosts。core/config 没有这个段且不能改，
// 所以与 core/plugin 读授权域名的做法一致，单独读一次 config/default.yaml + config.yaml + 环境变量。
func loadAllowPrivateHosts() bool {
	v := viper.New()
	v.SetConfigType("yaml")
	for _, name := range []string{"default.yaml", "config.yaml"} {
		p := filepath.Join("config", name)
		if _, err := os.Stat(p); err == nil {
			v.SetConfigFile(p)
			_ = v.MergeInConfig()
		}
	}
	v.SetEnvPrefix("NEXADMIN")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	return v.GetBool("identity.allow_private_hosts")
}

var blockedNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{
		"0.0.0.0/8",     // "this network"
		"100.64.0.0/10", // CGNAT
		"192.0.0.0/24",  // IETF 协议分配
		"198.18.0.0/15", // 基准测试
		"240.0.0.0/4",   // 保留
		"64:ff9b::/96",  // NAT64：可映射到任意 IPv4
		"2002::/16",     // 6to4
		"fd00:ec2::/32", // AWS IPv6 元数据
	} {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}()

// blockedIP 报告 ip 是否属于默认禁止访问的地址段。
func blockedIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4 // 含 IPv4-mapped IPv6
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// checkConfiguredHost 在保存配置时做不联网的字面量检查（IP 字面量 / localhost）；域名在连接时解析后再查。
func checkConfiguredHost(host string) error {
	if allowPrivateHosts.Load() {
		return nil
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return badReq("identity.host_blocked")
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && blockedIP(ip) {
		return badReq("identity.host_blocked")
	}
	return nil
}

// checkDialHost 连接前解析 host，任何一条记录落在禁止段即拒绝。
func checkDialHost(ctx context.Context, host string) error {
	if allowPrivateHosts.Load() {
		return nil
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		if blockedIP(ip) {
			return errBlockedHost
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return errBlockedHost
	}
	for _, a := range addrs {
		if blockedIP(a.IP) {
			return errBlockedHost
		}
	}
	return nil
}

// ssrfControl 是 net.Dialer.Control：对即将 connect 的实际 IP 再校验一次。
func ssrfControl(_, address string, _ syscall.RawConn) error {
	if allowPrivateHosts.Load() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errBlockedHost
	}
	ip := net.ParseIP(host)
	if ip == nil || blockedIP(ip) {
		return errBlockedHost
	}
	return nil
}
