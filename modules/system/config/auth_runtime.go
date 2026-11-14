package config

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"nexadmin/core/auth"
	"nexadmin/core/i18n"
	"nexadmin/core/response"
)

// 本文件把设置页「登录方式」分区（sys_setting.auth）接成运行时数据源：
// 开关、凭据、回调地址、账号匹配规则与出站代理都从这里注入 core/auth，
// 保存后下一次请求即生效，不用改 config.yaml、也不用重启。
// config.yaml 的角色退为「回退 / 种子」：设置页没动过的 provider 与登录方式仍按文件走。

// providerSettings 是 provider 条目在前端的保存形态（camelCase）。
type providerSettings struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	RedirectURIs string `json:"redirectUris"` // 每行一个完整 URL
	AgentID      string `json:"agentId"`      // 仅企业微信
	Issuer       string `json:"issuer"`       // 仅 OIDC
}

// authSection 是 auth 分区的保存值。指针字段用来区分「管理员设过」与「老版本没写过这一项」。
type authSection struct {
	Account        *bool                       `json:"account"`
	Phone          *bool                       `json:"phone"`
	Providers      map[string]providerSettings `json:"providers"`
	MatchBy        string                      `json:"matchBy"`
	AutoCreate     bool                        `json:"autoCreate"`
	AllowedDomains string                      `json:"allowedDomains"`
	Proxy          string                      `json:"proxy"`
}

// splitList 把多行 / 逗号分隔的输入拆成去重前的干净列表。
func splitList(s string) []string {
	out := make([]string, 0, 2)
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == ';' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// normalizeMatchBy 把前端的 camelCase 选项翻成配置键使用的 snake_case。
func normalizeMatchBy(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "email":
		return "email"
	case "employeeno", "employee_no":
		return "employee_no"
	case "phone":
		return "phone"
	}
	return ""
}

// savedAuth 读取设置页 auth 分区的保存值（密钥已解密）。没有保存过、内容损坏都返回 ok=false。
func (s *Service) savedAuth(ctx context.Context) (authSection, bool) {
	var out authSection
	var content string
	err := s.db.WithContext(ctx).Table("sys_setting").Select("content").Where("section = ?", "auth").Limit(1).Scan(&content).Error
	if err != nil || strings.TrimSpace(content) == "" {
		return out, false
	}
	var raw map[string]any
	if json.Unmarshal([]byte(content), &raw) != nil {
		return out, false
	}
	s.decryptSecrets("auth", raw)
	b, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(b, &out) != nil {
		return out, false
	}
	return out, true
}

// decodeProvider 把任意形态的 provider 条目（库里的 JSON / 合并后的 map）解成设置形态。
// touched=false 表示这一条没有任何管理员填过的内容，应当让 config.yaml 原样生效。
func decodeProvider(m map[string]any) (providerSettings, bool) {
	var p providerSettings
	if m == nil {
		return p, false
	}
	if b, err := json.Marshal(m); err != nil || json.Unmarshal(b, &p) != nil {
		return p, false
	}
	touched := p.Enabled || strings.TrimSpace(p.ClientID) != "" || strings.TrimSpace(p.ClientSecret) != "" ||
		strings.TrimSpace(p.AgentID) != "" || strings.TrimSpace(p.Issuer) != "" || len(splitList(p.RedirectURIs)) > 0
	return p, touched
}

// override 把一条 provider 设置翻成 core/auth 的覆盖项。touched=false 时 ok=false（不覆盖）。
func (s *Service) override(key string, p providerSettings, sec authSection) (auth.ProviderOverride, bool) {
	uris := splitList(p.RedirectURIs)
	if !p.Enabled && strings.TrimSpace(p.ClientID) == "" && strings.TrimSpace(p.ClientSecret) == "" &&
		strings.TrimSpace(p.AgentID) == "" && strings.TrimSpace(p.Issuer) == "" && len(uris) == 0 {
		return auth.ProviderOverride{}, false
	}
	if !p.Enabled {
		// 有关闭之前填过的凭据：管理员显式关掉，config.yaml 里配了也不再生效。
		return auth.ProviderOverride{Disabled: true}, true
	}
	fields := map[string]any{}
	for k, v := range map[string]string{
		"client_id": p.ClientID, "client_secret": p.ClientSecret,
		"agent_id": p.AgentID, "issuer": p.Issuer,
	} {
		if v = strings.TrimSpace(v); v != "" {
			fields[k] = v
		}
	}
	if len(uris) > 0 {
		fields["redirect_uris"] = uris
	}
	// 「账号匹配与限制」是分区级默认：只补 provider 自己（config.yaml）没写的键，不覆盖显式配置。
	base, _ := s.authCfg.Providers[key].(map[string]any)
	setDefault := func(k string, v any) {
		if _, exists := base[k]; !exists {
			fields[k] = v
		}
	}
	if v := normalizeMatchBy(sec.MatchBy); v != "" {
		setDefault("match_by", v)
	}
	setDefault("auto_create", sec.AutoCreate)
	if ds := splitList(sec.AllowedDomains); len(ds) > 0 {
		setDefault("allowed_email_domains", ds)
	}
	return auth.ProviderOverride{Fields: fields}, true
}

// providerOverrides 是注入 core/auth 的 provider 覆盖（找不到任何动过的条目时返回 nil = 全部按 config.yaml）。
func (s *Service) providerOverrides(ctx context.Context) map[string]auth.ProviderOverride {
	sec, ok := s.savedAuth(ctx)
	if !ok || len(sec.Providers) == 0 {
		return nil
	}
	out := make(map[string]auth.ProviderOverride, len(sec.Providers))
	for key, p := range sec.Providers {
		if !auth.KnownProvider(key) {
			continue
		}
		if ov, touched := s.override(key, p, sec); touched {
			out[key] = ov
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// loginMethods 是注入 core/auth 的「登录方式」源；nil = 设置页没保存过，沿用 config.yaml 的 auth.methods。
func (s *Service) loginMethods(ctx context.Context) []string {
	sec, ok := s.savedAuth(ctx)
	if !ok {
		return nil
	}
	yaml := func(m string) bool {
		for _, x := range s.authCfg.Methods {
			if x == m {
				return true
			}
		}
		return false
	}
	account := yaml("password")
	if sec.Account != nil {
		account = *sec.Account
	}
	phone := yaml("phone")
	if sec.Phone != nil {
		phone = *sec.Phone
	}
	methods := make([]string, 0, 2)
	if account {
		methods = append(methods, "password")
	}
	if phone {
		methods = append(methods, "phone")
	}
	return methods
}

// oauthProxy 是注入 core/auth 的出站代理源；空值不接管，继续用 config.yaml 的 auth.proxy。
func (s *Service) oauthProxy(ctx context.Context) (string, bool) {
	sec, ok := s.savedAuth(ctx)
	if !ok {
		return "", false
	}
	if v := strings.TrimSpace(sec.Proxy); v != "" {
		return v, true
	}
	return "", false
}

// hydrateAuth 把「设置页没动过、但 config.yaml 里已配置」的登录方式回填成开启态：
// 设置页展示的就是这台机器真正生效的配置，管理员不会「看不到它」而误以为没配置；
// 保存时这些值原样写回，密钥留空 = 继续用配置文件里的那个。
func (s *Service) hydrateAuth(ctx context.Context, v map[string]any) {
	_, hasSaved := s.savedAuth(ctx)
	pm, _ := v["providers"].(map[string]any)
	for _, key := range auth.KnownProviders {
		m, _ := pm[key].(map[string]any)
		if m == nil {
			continue
		}
		if hasSaved {
			if _, touched := decodeProvider(m); touched {
				continue
			}
		}
		pc, ok := s.authCfg.ProviderConf(key)
		if !ok {
			continue
		}
		m["enabled"] = true
		m["clientId"] = pc.ClientID
		m["redirectUris"] = strings.Join(pc.RedirectURIs, "\n")
		if key == "wecom" {
			m["agentId"] = pc.AgentID
		}
		if key == "oidc" {
			m["issuer"] = pc.Issuer
		}
	}
	if !hasSaved {
		v["account"] = s.hasMethod("password")
		v["phone"] = s.hasMethod("phone")
		v["proxy"] = s.authCfg.Proxy
	}
}

func (s *Service) hasMethod(m string) bool {
	for _, x := range s.authCfg.Methods {
		if x == m {
			return true
		}
	}
	return false
}

// validateAuth 在保存前校验：开启的 provider 必须真正能用（设置页 + config.yaml 合起来完整），
// 且至少保留一种可用的登录方式——否则「保存成功」之后没人能进后台。
func (s *Service) validateAuth(ctx context.Context, merged map[string]any) error {
	pm, _ := merged["providers"].(map[string]any)
	enabled := 0
	for key, raw := range pm {
		if !auth.KnownProvider(key) {
			continue
		}
		m, _ := raw.(map[string]any)
		p, _ := decodeProvider(m)
		if !p.Enabled {
			continue
		}
		sec := authSection{MatchBy: str(merged["matchBy"]), AutoCreate: merged["autoCreate"] == true, AllowedDomains: str(merged["allowedDomains"])}
		ov, _ := s.override(key, p, sec)
		effect := auth.MergeProviders(s.authCfg.Providers, map[string]auth.ProviderOverride{key: ov})
		if _, ok := s.authCfg.ProviderConfFrom(effect, key); !ok {
			name := i18n.T(ctx, "auth.provider."+key)
			if name == "" || name == "auth.provider."+key {
				name = auth.ProviderDisplay(key)
			}
			return response.BadRequest("config.auth.provider_incomplete", name)
		}
		enabled++
	}
	account := merged["account"] == true
	phone := merged["phone"] == true && s.smsProvider != ""
	if !account && !phone && enabled == 0 {
		return response.BadRequest("config.auth.no_login_method")
	}
	if proxy := strings.TrimSpace(str(merged["proxy"])); proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return response.BadRequest("config.auth.proxy_invalid")
		}
	}
	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
