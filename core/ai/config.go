// Package ai 是统一 AI 网关（设计文档 §16）：只实现 OpenAI 兼容的 Chat Completions，
// 用标准 net/http 调用，能接通义千问、DeepSeek、智谱以及 Ollama / vLLM 等内网模型。
//
// 安全约定：
//   - 系统提示固定，用户输入只放在 JSON 编码的 user 内容里；
//   - 模型输出一律当不可信数据：筛选条件按白名单重新校验，译文校验占位符与标签；
//   - API Key 不进日志、不回传前端；审计只记元信息（用户、接口、耗时、token），不记提示词与回复；
//   - 上游报错细节不透给调用方，只返回友好的分类错误。
package ai

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 默认值与上限。
const (
	DefaultTimeout   = 30 * time.Second
	MaxTimeout       = 120 * time.Second
	DefaultMaxTokens = 2048
	MaxMaxTokens     = 8192
)

// Config 是一次调用所需的模型配置。
type Config struct {
	Enabled bool
	BaseURL string
	APIKey  string
	Model   string
	// EmbedBaseURL / EmbedAPIKey 是向量化（embeddings）的独立地址与密钥：向量化可以走另一家供应商
	//（如对话用千问、向量用 OpenAI）。留空 = 沿用 BaseURL / APIKey（OpenAI 兼容网关的 /embeddings
	// 挂在同一地址下的常见形态）。
	EmbedBaseURL string
	EmbedAPIKey  string
	// EmbedModel 是向量化（embeddings）模型名；空 = 未启用向量化。
	EmbedModel string
	Timeout    time.Duration
	MaxTokens  int
	// AllowPrivate 允许 BaseURL / EmbedBaseURL 指向私网 / 回环（来自 ai.allow_private_base_url）；
	// 链路本地与云元数据永远拒绝。
	AllowPrivate bool
	// Redact 决定发往上游前脱敏哪些敏感字段。FromSettings 默认全开；直接构造 Config 时零值 = 不脱敏。
	Redact RedactOptions
}

// String 和 GoString 不含密钥，避免被 %v / %+v / %#v 误打进日志。
func (c Config) String() string {
	return fmt.Sprintf("ai.Config{enabled=%t base_url=%s model=%s embed_base_url=%s embed_model=%s key_set=%t embed_key_set=%t}",
		c.Enabled, c.BaseURL, c.Model, c.EmbedBaseURL, c.EmbedModel, c.APIKey != "", c.EmbedAPIKey != "")
}

// GoString 见 String。
func (c Config) GoString() string { return c.String() }

// Normalize 补默认值并限制范围。
func (c Config) Normalize() Config {
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.Model = strings.TrimSpace(c.Model)
	c.EmbedBaseURL = strings.TrimSpace(c.EmbedBaseURL)
	c.EmbedAPIKey = strings.TrimSpace(c.EmbedAPIKey)
	c.EmbedModel = strings.TrimSpace(c.EmbedModel)
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.Timeout > MaxTimeout {
		c.Timeout = MaxTimeout
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = DefaultMaxTokens
	}
	if c.MaxTokens > MaxMaxTokens {
		c.MaxTokens = MaxMaxTokens
	}
	return c
}

// EmbedEndpointBase 返回向量化生效的 Base URL：未单独配置时沿用对话模型的地址。
func (c Config) EmbedEndpointBase() string {
	if c.EmbedBaseURL != "" {
		return c.EmbedBaseURL
	}
	return c.BaseURL
}

// EmbedEndpointKey 返回向量化生效的 API Key：
//   - 单独配置了 EmbedAPIKey → 用它；
//   - 否则仅在向量地址与对话地址相同（或未单独配置地址）时沿用 APIKey——
//     一个供应商的密钥绝不发往另一个供应商的地址。
func (c Config) EmbedEndpointKey() string {
	if c.EmbedAPIKey != "" {
		return c.EmbedAPIKey
	}
	if c.EmbedBaseURL == "" || strings.EqualFold(strings.TrimRight(c.EmbedBaseURL, "/"), strings.TrimRight(c.BaseURL, "/")) {
		return c.APIKey
	}
	return ""
}

// EmbedReady 表示向量化可用：已启用、EmbedModel 非空，且生效地址合法、密钥可满足
//（独立地址必须有它自己的 Key，或地址在本机 / 内网允许无 Key——与对话的 Ready 同一规则）。
func (c Config) EmbedReady() bool {
	if !c.Enabled || strings.TrimSpace(c.EmbedModel) == "" {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(c.EmbedEndpointBase()))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	return c.EmbedEndpointKey() != "" || localHost(u.Hostname())
}

// Ready 表示已启用且配置完整：地址是 http(s)、模型名非空；公网地址必须有 Key，
// 本机 / 内网地址（Ollama、vLLM）可以不填 Key。
func (c Config) Ready() bool {
	if !c.Enabled || strings.TrimSpace(c.Model) == "" {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(c.BaseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	return strings.TrimSpace(c.APIKey) != "" || localHost(u.Hostname())
}

func (c Config) endpoint(path string) string {
	return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + "/" + strings.TrimLeft(path, "/")
}

// embedEndpoint 是向量化请求的地址前缀（独立配置或沿用对话地址）。
func (c Config) embedEndpoint(path string) string {
	return strings.TrimRight(strings.TrimSpace(c.EmbedEndpointBase()), "/") + "/" + strings.TrimLeft(path, "/")
}

// localHost 判断主机是否本机 / 内网（无 Key 也允许）。
func localHost(h string) bool {
	if h == "localhost" {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return !strings.Contains(h, ".") // 容器内服务名，如 http://ollama:11434/v1
}

// FromSettings 把设置页 ai 分区（sys_setting 的 JSON）解析成 Config；
// 分区里缺失或为空的字段回落到 fallback（向导写入 config.yaml 的 ai.*）。
func FromSettings(row map[string]any, fallback Config) Config {
	out := fallback
	out.Redact = DefaultRedact() // 默认开启；设置页 redact=false 才关闭
	if row == nil {
		out.Enabled = fallback.BaseURL != ""
		return out.Normalize()
	}
	if b, ok := row["redact"].(bool); ok && !b {
		out.Redact = RedactOptions{}
	}
	out.Enabled = true
	if b, ok := row["enabled"].(bool); ok {
		out.Enabled = b
	}
	if s := str(row["baseUrl"]); s != "" {
		if s != strings.TrimSpace(fallback.BaseURL) {
			out.APIKey = "" // 不得把 config.yaml 里的旧密钥发往另一个地址
		}
		out.BaseURL = s
	}
	if s := str(row["apiKey"]); s != "" {
		out.APIKey = s
	}
	if s := str(row["model"]); s != "" {
		out.Model = s
	}
	// Embedding 独立地址 / 密钥：同一套「换了地址，旧密钥不得跟过去」的规则。
	if s := str(row["embedBaseUrl"]); s != "" {
		fallbackEmbedBase := strings.TrimSpace(fallback.EmbedBaseURL)
		if fallbackEmbedBase == "" {
			fallbackEmbedBase = strings.TrimSpace(fallback.BaseURL)
		}
		if s != fallbackEmbedBase {
			out.EmbedAPIKey = "" // config.yaml 里的旧向量密钥不发往新地址
			out.EmbedBaseURL = s
		} else {
			out.EmbedBaseURL = s
		}
	}
	if s := str(row["embedApiKey"]); s != "" {
		out.EmbedAPIKey = s
	}
	if s := str(row["embedModel"]); s != "" {
		out.EmbedModel = s
	}
	if n := num(row["timeoutSec"]); n > 0 {
		out.Timeout = time.Duration(n) * time.Second
	}
	if n := num(row["maxTokens"]); n > 0 {
		out.MaxTokens = n
	}
	return out.Normalize()
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// num 接受 JSON 数字与数字字符串（设置页的数值字段可能存成字符串）。
func num(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	}
	return 0
}
