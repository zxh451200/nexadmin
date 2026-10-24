package config

import (
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/mitchellh/mapstructure"
)

// ProviderConf 是 auth.providers.<name> 的强类型视图（字符串里的 ${ENV} 已展开）。
type ProviderConf struct {
	ClientID            string   `mapstructure:"client_id"`
	ClientSecret        string   `mapstructure:"client_secret"`
	RedirectURIs        []string `mapstructure:"redirect_uris"` // 回调地址白名单，精确匹配
	MatchBy             string   `mapstructure:"match_by"`      // phone | email | employee_no；空 = 不自动匹配
	AutoCreate          bool     `mapstructure:"auto_create"`   // 匹配不上时自动建号并赋默认角色，默认关闭
	AllowedEmailDomains []string `mapstructure:"allowed_email_domains"`
	DefaultRole         string   `mapstructure:"default_role"` // auto_create 赋予的角色编码

	AgentID     string   `mapstructure:"agent_id"` // 企业微信
	Issuer      string   `mapstructure:"issuer"`   // oidc；google 可覆盖
	Name        string   `mapstructure:"name"`     // oidc 展示名
	Scopes      []string `mapstructure:"scopes"`
	AuthURL     string   `mapstructure:"auth_url"` // 以下端点一般不用配，oidc 无 discovery 时手工指定
	TokenURL    string   `mapstructure:"token_url"`
	UserInfoURL string   `mapstructure:"userinfo_url"`
	JWKSURL     string   `mapstructure:"jwks_url"`
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv 把字符串里的 ${NAME} 换成环境变量值（未设置换成空串）。
func ExpandEnv(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string { return os.Getenv(m[2 : len(m)-1]) })
}

// ProviderConf 返回 auth.providers.<name>。未配置、配置不是对象或缺少必填项（client_id / client_secret /
// 至少一个 redirect_uris；oidc 还要 issuer 或手工端点）时 ok=false —— 这样的 provider 视为“没配置”，登录页不显示。
func (a Auth) ProviderConf(name string) (ProviderConf, bool) {
	raw, found := a.Providers[name]
	m, isMap := raw.(map[string]any)
	if !found || !isMap {
		return ProviderConf{}, false
	}
	var c ProviderConf
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{Result: &c, WeaklyTypedInput: true, TagName: "mapstructure"})
	if err != nil || dec.Decode(m) != nil {
		return ProviderConf{}, false
	}
	c.ClientID, c.ClientSecret = strings.TrimSpace(ExpandEnv(c.ClientID)), strings.TrimSpace(ExpandEnv(c.ClientSecret))
	c.AgentID, c.Issuer = ExpandEnv(c.AgentID), strings.TrimSpace(ExpandEnv(c.Issuer))
	c.MatchBy = strings.ToLower(strings.TrimSpace(c.MatchBy))
	uris := c.RedirectURIs[:0]
	for _, u := range c.RedirectURIs {
		if u = strings.TrimSpace(ExpandEnv(u)); u != "" {
			uris = append(uris, u)
		}
	}
	c.RedirectURIs = uris
	domains := make([]string, 0, len(c.AllowedEmailDomains))
	for _, d := range c.AllowedEmailDomains {
		if d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@")); d != "" {
			domains = append(domains, d)
		}
	}
	c.AllowedEmailDomains = domains
	if c.ClientID == "" || c.ClientSecret == "" || len(c.RedirectURIs) == 0 {
		return ProviderConf{}, false
	}
	if name == "wecom" && c.AgentID == "" {
		return ProviderConf{}, false
	}
	if name == "oidc" && c.Issuer == "" && (c.AuthURL == "" || c.TokenURL == "" || c.JWKSURL == "") {
		return ProviderConf{}, false
	}
	return c, true
}

// ProviderNames 返回已配置且可用的第三方登录 provider 键（按字母序）。
func (a Auth) ProviderNames() []string {
	out := make([]string, 0, len(a.Providers))
	for k := range a.Providers {
		if _, ok := a.ProviderConf(k); ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
