package saml

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/viper"
)

// 元数据 URL 由租户管理员填写、服务端会主动访问（拉取 / 自检），所以默认拒绝回环、
// 私网、链路本地（含云元数据 169.254.169.254）等地址。内网 IdP 需要部署方显式放行：
// config 里写 saml.allow_private_hosts: true（或环境变量 NEXADMIN_SAML_ALLOW_PRIVATE_HOSTS=true）。
// 与 plugins/identity 的目录地址同一套策略：连接前解析全部 A/AAAA 记录，connect 时再校验实际 IP（防 DNS 重绑定）。

var allowPrivateHosts atomic.Bool

var errBlockedHost = errors.New("saml: 地址不被允许（回环 / 私网 / 链路本地 / 元数据网段）")

func setAllowPrivateHosts(v bool) { allowPrivateHosts.Store(v) }

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
	return v.GetBool("saml.allow_private_hosts")
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

func blockedIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
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

// checkFetchHost 解析 host，任何一条记录落在禁止段即拒绝。
func checkFetchHost(ctx context.Context, host string) error {
	if allowPrivateHosts.Load() {
		return nil
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return errBlockedHost
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

// guardDial 在真正 connect 前对实际 IP 再校验一次（防 DNS 重绑定）。
func guardDial(_, address string, _ syscall.RawConn) error {
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

// fetchMetadata 拉取 IdP 元数据：只允许 http(s)、最多 2 次重定向、10 秒超时、1 MiB 上限。
func fetchMetadata(ctx context.Context, raw string) ([]byte, error) {
	if err := validateMetadataURL(raw); err != nil {
		return nil, err
	}
	if err := checkFetchHost(ctx, hostOf(raw)); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/xml, text/xml, */*")
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			// 不走环境代理：代理会把连接变成「到代理」，SSRF 校验就失去意义。
			Proxy:               nil,
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second, Control: guardDial}).DialContext,
			TLSHandshakeTimeout: 5 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 2 {
				return errors.New("saml: 元数据地址重定向过多")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("saml: 元数据地址重定向到了非 http(s) 地址")
			}
			return checkFetchHost(req.Context(), req.URL.Hostname())
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("saml: 拉取元数据失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("saml: 拉取元数据失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes+1))
	if err != nil {
		return nil, fmt.Errorf("saml: 读取元数据失败: %w", err)
	}
	if len(body) > maxMetadataBytes {
		return nil, errors.New("saml: 元数据超过 1 MiB")
	}
	return body, nil
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
