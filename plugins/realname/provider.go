package realname

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nexadmin/core/response"
)

// Provider 把「姓名 + 证件号」交给外部实名核验服务。国内按次计费的厂商签名方式各不相同，
// 一律作为 Provider 实现外挂进来，本插件不写死任何一家。
type Provider interface {
	Verify(ctx context.Context, req VerifyRequest) (VerifyOutcome, error)
}

// VerifyRequest 是送出去核验的明文输入：只在出网调用的一瞬间存在，不进日志、不落库。
type VerifyRequest struct {
	Name   string `json:"name"`
	Number string `json:"number"`
	IdType string `json:"idType"`
}

// VerifyOutcome 是厂商返回的一致性结论。
type VerifyOutcome struct {
	Matched bool
	Message string
}

// ProviderGeneric 是内置的通用 HTTP 契约适配器：适用于自建网关或已按本契约封装的服务。
const ProviderGeneric = "generic"

var providerNames = map[string]bool{ProviderGeneric: true}

// newProvider 在未启用在线核验时返回 (nil, nil)：关闭不是错误，只是结论停在「格式通过」。
// hc 传 nil 用默认客户端（按配置超时）；测试注入自己的 client。
func newProvider(cfg *Config, hc *http.Client) (Provider, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	switch cfg.Provider {
	case "":
		return nil, nil
	case ProviderGeneric:
		if err := validateEndpoint(cfg.Endpoint); err != nil {
			return nil, err
		}
		if hc == nil {
			hc = &http.Client{Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond}
		}
		return &genericProvider{endpoint: cfg.Endpoint, appKey: cfg.AppKey, secret: cfg.AppSecretEnc, hc: hc}, nil
	}
	return nil, response.BadRequest("realname.provider_unknown")
}

type genericProvider struct {
	endpoint string
	appKey   string
	secret   string
	hc       *http.Client
}

// Verify 走固定契约：POST {name,number,idType}，响应 200 且体为 {"matched":bool,"message":str}。
// 非 200、缺 matched 字段、超时都算核验未完成（StatusError），不能当成"不一致"。
func (p *genericProvider) Verify(ctx context.Context, req VerifyRequest) (VerifyOutcome, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return VerifyOutcome{}, response.New(http.StatusBadGateway, "realname.provider_bad_response").Wrap(err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return VerifyOutcome{}, response.New(http.StatusBadGateway, "realname.endpoint_invalid").Wrap(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.appKey != "" {
		httpReq.Header.Set("X-App-Key", p.appKey)
	}
	if p.secret != "" {
		httpReq.Header.Set("X-App-Secret", p.secret)
	}

	res, err := p.hc.Do(httpReq)
	if err != nil {
		return VerifyOutcome{}, response.New(http.StatusBadGateway, "realname.provider_unreachable").Wrap(err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return VerifyOutcome{}, response.New(http.StatusBadGateway, "realname.provider_unreachable").Wrap(err)
	}
	if res.StatusCode != http.StatusOK {
		return VerifyOutcome{}, response.New(http.StatusBadGateway, "realname.provider_status", res.StatusCode)
	}
	var out struct {
		Matched *bool  `json:"matched"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Matched == nil {
		return VerifyOutcome{}, response.New(http.StatusBadGateway, "realname.provider_bad_response").Wrap(err)
	}
	return VerifyOutcome{Matched: *out.Matched, Message: strings.TrimSpace(out.Message)}, nil
}

// validateEndpoint 只接受 https：核验请求带着姓名和证件号，明文传输等于把 PII 摊在链路上。
// 宁可让配置保存失败，也不放行一个 http 地址。
func validateEndpoint(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.Scheme != "https" {
		return response.BadRequest("realname.endpoint_invalid")
	}
	return nil
}
