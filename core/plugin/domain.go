package plugin

import (
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// hostConfig 是授权域名校验用到的配置。
type hostConfig struct {
	Hosts         []string // 配置里的站点域名（site.domain / site.domains / sso.issuer 的 host）
	AllowDevHosts bool     // license.allow_dev_hosts
}

// loadHostConfig 读取 <root>/config/default.yaml + config.yaml + 环境变量 NEXADMIN_*。
// core/config 没有这些段且不能改，所以这里单独读一次（做法同 modules/sso）。
func loadHostConfig(root string) hostConfig {
	v := viper.New()
	v.SetConfigType("yaml")
	for _, name := range []string{"default.yaml", "config.yaml"} {
		p := filepath.Join(root, "config", name)
		if _, err := os.Stat(p); err == nil {
			v.SetConfigFile(p)
			_ = v.MergeInConfig()
		}
	}
	v.SetEnvPrefix("NEXADMIN")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var hc hostConfig
	hc.AllowDevHosts = v.GetBool("license.allow_dev_hosts")
	add := func(s string) {
		if h := normalizeHost(s); h != "" {
			hc.Hosts = append(hc.Hosts, h)
		}
	}
	add(v.GetString("site.domain"))
	for _, s := range v.GetStringSlice("site.domains") {
		add(s)
	}
	if iss := strings.TrimSpace(v.GetString("sso.issuer")); iss != "" {
		if u, err := url.Parse(iss); err == nil {
			add(u.Host)
		}
	}
	return hc
}

// LoadDisabledPlugins 读取 disabled_plugins 配置（停用的插件 id 列表，见 manager 的 evaluate）。
// 与 loadHostConfig 同理：core/config 不承载插件段，这里单独读。
func LoadDisabledPlugins(root string) []string {
	v := viper.New()
	v.SetConfigType("yaml")
	for _, name := range []string{"default.yaml", "config.yaml"} {
		p := filepath.Join(root, "config", name)
		if _, err := os.Stat(p); err == nil {
			v.SetConfigFile(p)
			_ = v.MergeInConfig()
		}
	}
	v.SetEnvPrefix("NEXADMIN")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	var out []string
	for _, s := range v.GetStringSlice("disabled_plugins") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// normalizeHost 去掉 scheme / 端口 / 路径并转小写；返回纯主机名。
func normalizeHost(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil {
			s = u.Host
		}
	}
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return strings.ToLower(strings.Trim(strings.TrimSuffix(s, "."), "[]"))
}

// isDevHost 判断是否本机开发地址。
func isDevHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasSuffix(h, ".localhost")
}

// MatchDomain 判断 host 是否匹配授权域名列表：精确匹配，或 *.example.com 通配
// （匹配 example.com 的任意子域，不匹配 example.com 本身）。比较不区分大小写，忽略端口。
func MatchDomain(domains []string, host string) bool {
	host = normalizeHost(host)
	if host == "" {
		return false
	}
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if strings.HasPrefix(d, "*.") {
			suffix := d[1:] // ".example.com"
			if len(host) > len(suffix) && strings.HasSuffix(host, suffix) {
				return true
			}
			continue
		}
		if normalizeHost(d) == host {
			return true
		}
	}
	return false
}
