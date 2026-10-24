package config

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	coreai "nexadmin/core/ai"
	"nexadmin/core/response"
	"nexadmin/core/secret"
)

// 设置页的分区，顺序与前端 AllSettings 一致。
var sections = []string{"site", "security", "cache", "storage", "ai", "sms", "auth", "notify"}

// secretMask 与前端 SECRET_MASK 一致：密钥类字段已设置时 GET 返回它；保存时仍是它则保持原值。
const secretMask = "********"

// secretFields 是需要打码的字段名（任意层级）。
var secretFields = map[string]bool{"password": true, "secretKey": true, "apiKey": true, "clientSecret": true}

func isSection(s string) bool {
	for _, x := range sections {
		if x == s {
			return true
		}
	}
	return false
}

func provider() map[string]any {
	return map[string]any{"enabled": false, "clientId": "", "clientSecret": ""}
}

// defaultSettings 是各分区的默认值（与前端 mock 一致），同时决定每个分区允许哪些字段。
func defaultSettings() map[string]map[string]any {
	oidc := provider()
	oidc["issuer"] = ""
	return map[string]map[string]any{
		"site":     {"siteName": "NexAdmin", "logoUrl": "", "copyright": "© 2026 NexAdmin", "defaultLanguage": "zh-CN", "watermark": false},
		"security": {"maxAttempts": "5", "lockMinutes": "15"},
		"cache":    {"type": "memory", "address": "", "password": ""},
		"storage":  {"type": "local", "endpoint": "", "bucket": "", "region": "", "accessKey": "", "secretKey": ""},
		"ai":       {"enabled": true, "baseUrl": "https://api.example.com/v1", "apiKey": "", "model": "qwen-plus", "timeoutSec": 30, "maxTokens": 2048, "redact": true},
		"sms":      {"provider": "none", "accessKey": "", "secretKey": "", "signName": "", "templateCode": ""},
		"notify":   notifyDefaults(),
		"auth": {
			"account": true, "phone": false,
			"providers": map[string]any{
				"wecom": provider(), "dingtalk": provider(), "feishu": provider(),
				"google": provider(), "github": provider(), "oidc": oidc,
			},
			"matchBy": "phone", "autoCreate": false, "allowedDomains": "", "proxy": "",
		},
	}
}

// merge 把 patch 合并进 base：只接受 base 里已有的字段，嵌套对象递归合并。
func merge(base map[string]any, patch map[string]any) map[string]any {
	out := make(map[string]any, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, pv := range patch {
		bv, ok := out[k]
		if !ok {
			continue // 未知字段丢弃
		}
		bm, bIsMap := bv.(map[string]any)
		pm, pIsMap := pv.(map[string]any)
		if bIsMap && pIsMap {
			out[k] = merge(bm, pm)
		} else if !bIsMap {
			out[k] = pv
		}
	}
	return out
}

// keepSecrets 递归处理：值是占位符的，保留旧值。
func keepSecrets(next, prev any) any {
	if next == secretMask {
		if prev == nil {
			return ""
		}
		return prev
	}
	if ns, ok := next.([]any); ok { // 列表（如机器人）：按元素的 name 找回旧值，删除 / 重排后不会串号
		ps, _ := prev.([]any)
		out := make([]any, len(ns))
		for i, e := range ns {
			var old any
			if em, ok := e.(map[string]any); ok {
				for _, p := range ps {
					if pm, ok := p.(map[string]any); ok && em["name"] != nil && pm["name"] == em["name"] {
						old = pm
						break
					}
				}
			}
			out[i] = keepSecrets(e, old)
		}
		return out
	}
	nm, ok := next.(map[string]any)
	if !ok {
		return next
	}
	pm, _ := prev.(map[string]any)
	out := make(map[string]any, len(nm))
	for k, v := range nm {
		out[k] = keepSecrets(v, pm[k])
	}
	return out
}

// maskSecrets 递归处理：已设置的密钥字段返回占位符。
func maskSecrets(v any) any {
	if l, ok := v.([]any); ok {
		out := make([]any, len(l))
		for i, e := range l {
			out[i] = maskSecrets(e)
		}
		return out
	}
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		if s, isStr := val.(string); isStr && secretFields[k] && s != "" {
			out[k] = secretMask
		} else {
			out[k] = maskSecrets(val)
		}
	}
	return out
}

// section 读取某个分区的当前值：默认值叠加库里保存的内容。
func (s *Service) section(ctx context.Context, name string) (map[string]any, error) {
	base := defaultSettings()[name]
	var row Setting
	err := s.db.WithContext(ctx).Where("section = ?", name).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return base, nil
	}
	if err != nil {
		return nil, err
	}
	var saved map[string]any
	if json.Unmarshal([]byte(row.Content), &saved) != nil {
		return base, nil // 内容损坏时回落默认值，不让设置页打不开
	}
	s.decryptSecrets(name, saved)
	return merge(base, saved), nil
}

// encryptSecrets 返回 v 的副本，其中密钥类字段（secretFields，任意层级）的非空字符串值换成 enc:v1: 密文。
// 入参必须是明文视图（section 已解密）；用户填的值即使恰好以 enc:v1: 开头也会被当作明文再加密一次。
func encryptSecrets(v any) (any, error) {
	if l, ok := v.([]any); ok {
		out := make([]any, len(l))
		for i, e := range l {
			enc, err := encryptSecrets(e)
			if err != nil {
				return nil, err
			}
			out[i] = enc
		}
		return out, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return v, nil
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		if str, isStr := val.(string); isStr && secretFields[k] && str != "" {
			enc, err := secret.Encrypt(str)
			if err != nil {
				return nil, err
			}
			out[k] = enc
			continue
		}
		sub, err := encryptSecrets(val)
		if err != nil {
			return nil, err
		}
		out[k] = sub
	}
	return out, nil
}

// decryptSecrets 就地解密 m 里的密钥类字段。无前缀的存量明文原样保留（下次保存时才加密）；
// 密文无效（被篡改或主密钥不对）时该字段置空并记警告，让设置页仍能打开、管理员可以重新填写。
func (s *Service) decryptSecrets(section string, m map[string]any) {
	for k, val := range m {
		switch x := val.(type) {
		case string:
			if !secretFields[k] || !secret.IsEncrypted(x) {
				continue
			}
			plain, err := secret.Decrypt(x)
			if err != nil {
				if s.log != nil {
					s.log.Warn("settings: cannot decrypt secret, treating as unset", zap.String("section", section), zap.String("field", k), zap.Error(err))
				}
				plain = ""
			}
			m[k] = plain
		case map[string]any:
			s.decryptSecrets(section, x)
		case []any:
			for _, e := range x {
				if em, ok := e.(map[string]any); ok {
					s.decryptSecrets(section, em)
				}
			}
		}
	}
}

// DecryptSecretsForRuntime 解密运行时需要使用的设置值，不返回前端打码视图。
// 它与设置页使用同一套字段白名单和密钥格式，避免业务模块把 enc:v1: 密文直接交给下游客户端。
func DecryptSecretsForRuntime(m map[string]any) {
	for k, val := range m {
		switch x := val.(type) {
		case string:
			if !secretFields[k] || !secret.IsEncrypted(x) {
				continue
			}
			if plain, err := secret.Decrypt(x); err == nil {
				m[k] = plain
			} else {
				m[k] = ""
			}
		case map[string]any:
			DecryptSecretsForRuntime(x)
		case []any:
			for _, e := range x {
				if em, ok := e.(map[string]any); ok {
					DecryptSecretsForRuntime(em)
				}
			}
		}
	}
}

// AllSettings 返回全部分区（密钥已打码）。
func (s *Service) AllSettings(ctx context.Context) (map[string]any, error) {
	out := make(map[string]any, len(sections))
	for _, name := range sections {
		v, err := s.section(ctx, name)
		if err != nil {
			return nil, err
		}
		out[name] = maskSecrets(v)
	}
	return out, nil
}

// SaveSection 保存一个分区：占位符保持原值，其余覆盖；返回打码后的新值。
func (s *Service) SaveSection(ctx context.Context, name string, body map[string]any) (map[string]any, error) {
	if !isSection(name) {
		return nil, response.NotFound("error.not_found")
	}
	if name == "storage" {
		// 文件存储只读 config/config.yaml 的 storage.*（向导 / init --config 写入）；这里保存的值没有任何地方会读，
		// 让它“保存成功”却不生效比拒绝更糟，所以明确拒绝。
		return nil, response.BadRequest("error.bad_request").WithMsg("文件存储由 config/config.yaml 的 storage.* 决定，设置页只读；请改配置文件后重启")
	}
	cur, err := s.section(ctx, name)
	if err != nil {
		return nil, err
	}
	merged := merge(cur, keepSecrets(body, cur).(map[string]any))
	if name == "notify" {
		if merged, err = normalizeNotify(merged); err != nil {
			return nil, err
		}
	}
	if name == "ai" {
		if err := s.validateAI(ctx, cur, body, merged); err != nil {
			return nil, err
		}
	}
	stored, err := encryptSecrets(merged) // 落库的是密文；merged（明文）只用来生成打码后的返回值
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	res := db.Model(&Setting{}).Where("section = ?", name).Update("content", string(raw))
	if res.Error != nil {
		return nil, res.Error
	}
	var n int64
	if err := db.Model(&Setting{}).Where("section = ?", name).Count(&n).Error; err != nil {
		return nil, err
	}
	if n == 0 {
		if err := db.Create(&Setting{Section: name, Content: string(raw)}).Error; err != nil {
			return nil, err
		}
	}
	return maskSecrets(merged).(map[string]any), nil
}

// PublicSettings 是无需登录的站点信息（前端 PublicSettings）。
type PublicSettings struct {
	SiteName  string `json:"siteName"`
	LogoURL   string `json:"logoUrl"`
	Copyright string `json:"copyright"`
	Watermark bool   `json:"watermark"`
}

// Public 返回公开站点信息。
func (s *Service) Public(ctx context.Context) (*PublicSettings, error) {
	site, err := s.section(ctx, "site")
	if err != nil {
		return nil, err
	}
	str := func(k string) string { v, _ := site[k].(string); return v }
	wm, _ := site["watermark"].(bool)
	return &PublicSettings{SiteName: str("siteName"), LogoURL: str("logoUrl"), Copyright: str("copyright"), Watermark: wm}, nil
}

// TestResult 是连接测试结果（前端 TestResult），reason 对应 settings.test.reason.* 文案。
type TestResult struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// TestSection 用表单里待测试的值（占位符回落到已保存值）做配置完整性检查。
// 这里只检查必填项，不会真的连接外部服务。
func (s *Service) TestSection(ctx context.Context, name string, body map[string]any) (*TestResult, error) {
	if !isSection(name) {
		return nil, response.NotFound("error.not_found")
	}
	if name == "notify" {
		return s.testNotify(ctx, body) // 真实发送一条测试消息（SMTP / 机器人），见 notify.go
	}
	cur, err := s.section(ctx, name)
	if err != nil {
		return nil, err
	}
	v := merge(cur, keepSecrets(body, cur).(map[string]any))
	str := func(k string) string { x, _ := v[k].(string); return x }
	fail := func(reason string) (*TestResult, error) { return &TestResult{Reason: reason}, nil }
	ok := &TestResult{OK: true}
	switch name {
	case "cache":
		if str("type") == "valkey" && str("address") == "" {
			return fail("noAddress")
		}
	case "storage":
		if str("type") == "s3" && (str("endpoint") == "" || str("bucket") == "" || str("accessKey") == "" || str("secretKey") == "") {
			return fail("incomplete")
		}
	case "ai":
		if str("baseUrl") == "" || str("apiKey") == "" || str("model") == "" {
			return fail("incomplete")
		}
	case "sms":
		switch str("provider") {
		case "none":
			return fail("noProvider")
		case "log":
		default:
			if str("accessKey") == "" || str("secretKey") == "" {
				return fail("incomplete")
			}
		}
	}
	return ok, nil
}

func trimURL(v any) string {
	str, _ := v.(string)
	return strings.TrimRight(strings.TrimSpace(str), "/")
}

// validateAI 校验 ai 分区：baseUrl 必须通过 SSRF 检查（scheme、链路本地 / 云元数据、默认私网与回环）；
// 修改 baseUrl 时必须同时重新提交 apiKey（不是占位符），不得把旧密钥发往新地址。
func (s *Service) validateAI(ctx context.Context, cur, body, merged map[string]any) error {
	next := trimURL(merged["baseUrl"])
	if next != "" {
		if err := coreai.ValidateBaseURL(ctx, next, s.aiAllowPrivate, s.aiResolver); err != nil {
			return bad("AI 地址不合法：仅支持 http/https，且不能指向链路本地、云元数据地址（默认也不能指向私网 / 回环，需部署方开启 ai.allow_private_base_url）")
		}
	}
	if next != trimURL(cur["baseUrl"]) {
		key, present := body["apiKey"].(string)
		if !present || key == secretMask {
			return bad("修改 AI 地址时必须同时重新填写 API Key")
		}
	}
	return nil
}
