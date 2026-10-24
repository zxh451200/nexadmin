// Package oauth 是第三方快捷登录的适配器集合：企业微信、钉钉、飞书、Google(OIDC)、GitHub、通用 OIDC。
// 每个适配器只做两件事：生成授权地址（AuthURL）和用授权码换取用户身份（Exchange）。
// 只用标准库 net/http（+ golang-jwt 校验 id_token），不引各家 SDK；所有外部调用都带超时，并支持 auth.proxy 代理。
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Identity 是第三方返回的用户身份，登录流程据此匹配本地账号。
type Identity struct {
	Provider      string
	ExternalID    string // 该 provider 内稳定且唯一的用户标识
	Name          string
	Email         string
	EmailVerified bool // 只有 provider 明确声明“已验证”（或由企业目录管理员维护）才为 true
	Phone         string
	Avatar        string
	EmployeeNo    string // 工号（企业类 provider 提供时）
}

// Provider 是统一的适配器接口。
type Provider interface {
	// Name 返回 provider 键（wecom / dingtalk / feishu / google / github / oidc）。
	Name() string
	// UsesPKCE 为 true 时，调用方应生成 PKCE 并同时传给 AuthURL 与 Exchange。
	UsesPKCE() bool
	// AuthURL 返回授权地址。pkce 为 nil 时不带 PKCE 参数。
	AuthURL(state, redirect string, pkce *PKCE) string
	// Exchange 用授权码换取身份。redirect 必须与 AuthURL 时一致；pkce 同 AuthURL。
	Exchange(ctx context.Context, code, redirect string, pkce *PKCE) (*Identity, error)
}

// Config 是单个 provider 的配置（来自 auth.providers.<name>，已展开 ${ENV}）。
type Config struct {
	ClientID     string
	ClientSecret string
	AgentID      string   // 企业微信应用 AgentId
	Issuer       string   // oidc 必填；google 可覆盖
	Scopes       []string // oidc 可选，默认 openid email profile
	Display      string   // 展示名（oidc）

	// 以下端点一般不用配；oidc 缺 discovery 时手工指定，测试里指向 httptest 服务。
	AuthURL     string
	TokenURL    string
	UserInfoURL string
	JWKSURL     string
}

// ErrExchange 表示换取身份失败。具体原因包在错误链里，只应进服务端日志，不要回给前端。
var ErrExchange = errors.New("oauth: exchange failed")

func fail(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrExchange, fmt.Sprintf(format, a...))
}

// PKCE 是 RFC 7636 的 S256 参数。
type PKCE struct {
	Verifier  string
	Challenge string
}

// NewPKCE 生成随机 verifier 与对应的 S256 challenge。
func NewPKCE() (*PKCE, error) {
	v, err := RandomToken(32)
	if err != nil {
		return nil, err
	}
	return PKCEFromVerifier(v), nil
}

// PKCEFromVerifier 由已有 verifier 还原 PKCE（回调时从缓存取回 verifier）。
func PKCEFromVerifier(v string) *PKCE {
	sum := sha256.Sum256([]byte(v))
	return &PKCE{Verifier: v, Challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
}

// RandomToken 返回 n 字节随机数的 base64url 文本。
func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DefaultTimeout 是外部调用的默认超时。
const DefaultTimeout = 10 * time.Second

// NewHTTPClient 创建带超时的客户端；proxy 非空时所有请求走该代理（auth.proxy）。
func NewHTTPClient(proxy string, timeout time.Duration) (*http.Client, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("oauth: invalid auth.proxy %q", proxy)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Timeout: timeout, Transport: tr}, nil
}

// New 按名称创建适配器。hc 为 nil 时用默认超时的直连客户端。
func New(name string, c Config, hc *http.Client) (Provider, error) {
	if hc == nil {
		hc, _ = NewHTTPClient("", 0)
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, fmt.Errorf("oauth: %s: client_id and client_secret are required", name)
	}
	switch name {
	case "wecom":
		return newWecom(c, hc)
	case "dingtalk":
		return newDingtalk(c, hc), nil
	case "feishu":
		return newFeishu(c, hc), nil
	case "github":
		return newGithub(c, hc), nil
	case "google":
		c.Issuer = firstNonEmpty(c.Issuer, "https://accounts.google.com")
		return newOIDC("google", c, hc)
	case "oidc":
		return newOIDC("oidc", c, hc)
	}
	return nil, fmt.Errorf("oauth: unknown provider %q", name)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

type httpDoer = *http.Client

const maxBody = 1 << 20

// call 发起请求并把 JSON 响应解到 out。非 2xx 视为失败；错误里不带响应体，避免把令牌写进日志。
func call(ctx context.Context, hc *http.Client, method, u string, hdr map[string]string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return fail("build request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fail("%s %s: %v", method, hostOf(u), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fail("read %s: %v", hostOf(u), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fail("%s returned HTTP %d", hostOf(u), resp.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fail("decode %s: %v", hostOf(u), err)
	}
	return nil
}

func hostOf(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return p.Host + p.Path
	}
	return "?"
}

func postForm(ctx context.Context, hc *http.Client, u string, v url.Values, out any) error {
	return call(ctx, hc, http.MethodPost, u, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, strings.NewReader(v.Encode()), out)
}

func postJSON(ctx context.Context, hc *http.Client, u string, in any, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return fail("encode: %v", err)
	}
	return call(ctx, hc, http.MethodPost, u, map[string]string{"Content-Type": "application/json"}, strings.NewReader(string(b)), out)
}

func getJSON(ctx context.Context, hc *http.Client, u string, hdr map[string]string, out any) error {
	return call(ctx, hc, http.MethodGet, u, hdr, nil, out)
}

// withQuery 在 base 后追加查询参数（保持 base 里已有的参数）。
func withQuery(base string, v url.Values) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + v.Encode()
}
