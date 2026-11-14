// Package oauthflow 是第三方登录/绑定的流程底座：构造适配器、生成并消费一次性 state（存 core/cache）、
// 校验回调地址白名单、保存“待绑定”短期令牌。账号匹配与签发 JWT 由用户模块完成，个人中心绑定也复用这里。
package oauthflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/oauth"
)

// 时效。
const (
	StateTTL     = 5 * time.Minute  // 授权 state 有效期
	BindTokenTTL = 10 * time.Minute // “绑定已有账号”短期令牌
	MaxBindTries = 5                // 同一个 bindToken 最多尝试几次账号密码
)

// Purpose 区分 state 的用途。
const (
	PurposeLogin    = "login"
	PurposeBind     = "bind"
	PurposeRegister = "register" // 注册页发起：匹配不上时按自助注册建号，不受 provider 的 auto_create 约束
)

// 错误。回调阶段的所有错误对外统一成同一条提示，具体原因只写日志。
var (
	ErrNotConfigured = errors.New("oauthflow: provider not configured")
	ErrRedirect      = errors.New("oauthflow: redirect_uri not in whitelist")
	ErrState         = errors.New("oauthflow: invalid or expired state")
	ErrBindToken     = errors.New("oauthflow: invalid or expired bind token")
)

// StateData 是 state 对应的服务端记录。
type StateData struct {
	Provider string `json:"p"`
	Redirect string `json:"r"`
	Verifier string `json:"v,omitempty"` // PKCE verifier，只存服务端
	Purpose  string `json:"u"`
	UID      string `json:"i,omitempty"` // 绑定场景：发起绑定的用户
}

// Flow 是流程服务。
type Flow struct {
	cfg   *config.Config
	cache cache.Cache
	log   *zap.Logger

	mu       sync.Mutex // 让同进程内 state / bindToken 的“读后删”不被并发重放
	adapters map[string]adapterEntry
	build    func(name string, c config.ProviderConf, hc *http.Client) (oauth.Provider, error)

	// hcMu 保护出站客户端：代理可以在设置页里改，变了下一次流程重建（见 client）。
	hcMu  sync.Mutex
	proxy string
	hc    *http.Client
}

// adapterEntry 把适配器和它构建时的配置指纹配对：设置页改了凭据 / 端点 / 代理，
// 指纹变化，下一次就走新配置重建，而不是继续用旧 client_id 去换码。
type adapterEntry struct {
	a  oauth.Provider
	fp string
}

var (
	once   sync.Once
	shared *Flow
)

// Shared 返回进程内共享的 Flow。config.yaml 的 auth.proxy 无效时不阻塞启动
// （第三方登录会在发起授权时以「未配置」失败，日志里保留原因）；设置页里有可用代理时照常工作。
func Shared(cfg *config.Config, c cache.Cache, log *zap.Logger) *Flow {
	once.Do(func() { shared = New(cfg, c, log) })
	return shared
}

// New 创建 Flow：外部调用统一 10 秒超时，代理按运行时设置（config.yaml 的 auth.proxy 兜底）。
func New(cfg *config.Config, c cache.Cache, log *zap.Logger) *Flow {
	f := &Flow{cfg: cfg, cache: c, log: log, adapters: map[string]adapterEntry{}, proxy: cfg.Auth.Proxy}
	if hc, err := oauth.NewHTTPClient(cfg.Auth.Proxy, oauth.DefaultTimeout); err == nil {
		f.hc = hc
	} else {
		log.Warn("oauth: invalid auth.proxy in config.yaml, waiting for runtime setting", zap.Error(err))
	}
	f.build = func(name string, pc config.ProviderConf, hc *http.Client) (oauth.Provider, error) {
		return oauth.New(name, oauth.Config{ClientID: pc.ClientID, ClientSecret: pc.ClientSecret, AgentID: pc.AgentID,
			Issuer: pc.Issuer, Scopes: pc.Scopes, Display: pc.Name, AuthURL: pc.AuthURL, TokenURL: pc.TokenURL,
			UserInfoURL: pc.UserInfoURL, JWKSURL: pc.JWKSURL}, hc)
	}
	return f
}

// client 返回当前代理对应的 HTTP 客户端；代理没变就复用。代理变了（设置页保存 / 配置回退）重建。
func (f *Flow) client(ctx context.Context) (*http.Client, string, error) {
	proxy := auth.OAuthProxy(ctx, f.cfg.Auth.Proxy)
	f.hcMu.Lock()
	defer f.hcMu.Unlock()
	if f.hc != nil && f.proxy == proxy {
		return f.hc, proxy, nil
	}
	hc, err := oauth.NewHTTPClient(proxy, oauth.DefaultTimeout)
	if err != nil {
		f.hc, f.proxy = nil, proxy
		return nil, proxy, err
	}
	f.hc, f.proxy = hc, proxy
	return hc, proxy, nil
}

// Conf 返回运行时生效的 provider 配置；未配置、缺必填项或不是受支持的 provider 返回 false。
func (f *Flow) Conf(ctx context.Context, key string) (config.ProviderConf, bool) {
	if !auth.KnownProvider(key) {
		return config.ProviderConf{}, false
	}
	return f.cfg.Auth.ProviderConfFrom(auth.EffectiveProviders(ctx, f.cfg.Auth.Providers), key)
}

// Available 返回已配置且可用的 provider 键，顺序固定（与 auth.KnownProviders 一致）。
func (f *Flow) Available(ctx context.Context) []string {
	out := []string{}
	for _, k := range auth.KnownProviders {
		if _, ok := f.Conf(ctx, k); ok {
			out = append(out, k)
		}
	}
	return out
}

// fingerprint 是「provider 配置 + 代理」的稳定摘要，用来判断缓存的适配器是否还对着同一份配置。
func fingerprint(pc config.ProviderConf, proxy string) string {
	h := sha256.New()
	for _, s := range []string{pc.ClientID, pc.ClientSecret, pc.AgentID, pc.Issuer, pc.Name, pc.AuthURL, pc.TokenURL, pc.UserInfoURL, pc.JWKSURL, proxy} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	h.Write([]byte(strings.Join(pc.Scopes, ",")))
	return hex.EncodeToString(h.Sum(nil))
}

func (f *Flow) adapter(ctx context.Context, key string) (oauth.Provider, config.ProviderConf, error) {
	pc, ok := f.Conf(ctx, key)
	if !ok || f.build == nil {
		return nil, pc, ErrNotConfigured
	}
	hc, proxy, err := f.client(ctx)
	if err != nil {
		return nil, pc, errors.Join(ErrNotConfigured, err)
	}
	fp := fingerprint(pc, proxy)
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.adapters[key]; ok && e.fp == fp {
		return e.a, pc, nil
	}
	a, err := f.build(key, pc, hc)
	if err != nil {
		return nil, pc, errors.Join(ErrNotConfigured, err)
	}
	f.adapters[key] = adapterEntry{a: a, fp: fp}
	return a, pc, nil
}

// AllowedRedirect 回调地址必须与配置白名单精确一致（防开放重定向 / 授权码被转发到别处）。
func AllowedRedirect(pc config.ProviderConf, redirect string) bool {
	if redirect == "" {
		return false
	}
	want := []string{redirect}
	// 后台收敛到 /admin 前缀后，旧白名单里的 /login/oauth/... 仍然有效：
	// 发起授权时任一形态命中即视为同一回调（回调落地后由服务端 301 到 /admin）。
	if strings.Contains(redirect, adminPrefix+"/login/oauth/") {
		want = append(want, withoutAdminPrefix(redirect))
	} else if strings.Contains(redirect, "/login/oauth/") {
		want = append(want, withAdminPrefix(redirect))
	}
	for _, u := range pc.RedirectURIs {
		for _, w := range want {
			if u == w {
				return true
			}
		}
	}
	return false
}

const adminPrefix = "/admin"

// withAdminPrefix 给 /login/oauth/... 回调补上 /admin 前缀。
func withAdminPrefix(u string) string {
	return strings.Replace(u, "/login/oauth/", adminPrefix+"/login/oauth/", 1)
}

// withoutAdminPrefix 去掉 /admin 前缀，用于兼容旧配置。
func withoutAdminPrefix(u string) string {
	return strings.Replace(u, adminPrefix+"/login/oauth/", "/login/oauth/", 1)
}

func stateKey(state string) string {
	h := sha256.Sum256([]byte(state))
	return cache.GlobalKey("oauth", "state", hex.EncodeToString(h[:]))
}

// Start 生成一次性 state（5 分钟）并返回授权地址。redirect 必须在白名单内；OIDC 同时生成 PKCE。
func (f *Flow) Start(ctx context.Context, key, redirect, purpose string, uid idgen.ID) (string, error) {
	a, pc, err := f.adapter(ctx, key)
	if err != nil {
		return "", err
	}
	if !AllowedRedirect(pc, redirect) {
		return "", ErrRedirect
	}
	state, err := oauth.RandomToken(24)
	if err != nil {
		return "", err
	}
	rec := StateData{Provider: key, Redirect: redirect, Purpose: purpose}
	var pk *oauth.PKCE
	if a.UsesPKCE() {
		if pk, err = oauth.NewPKCE(); err != nil {
			return "", err
		}
		rec.Verifier = pk.Verifier
	}
	if uid != 0 {
		rec.UID = uid.String()
	}
	raw, _ := json.Marshal(rec)
	u := a.AuthURL(state, redirect, pk)
	if u == "" {
		return "", errors.Join(ErrNotConfigured, errors.New("authorization endpoint unavailable"))
	}
	if err := f.cache.Set(ctx, stateKey(state), string(raw), StateTTL); err != nil {
		return "", err
	}
	return u, nil
}

// Take 取出并作废 state（一次性）。不存在、已用过、已过期、provider 不符都返回 ErrState。
// 用 cache.GetDel 原子消费：多实例下同一个 state 只有一个回调能取到。
func (f *Flow) Take(ctx context.Context, key, state string) (*StateData, error) {
	if state == "" {
		return nil, ErrState
	}
	raw, err := cache.GetDel(f.cache, ctx, stateKey(state))
	if errors.Is(err, cache.ErrNotFound) {
		return nil, ErrState
	}
	if err != nil {
		return nil, err
	}
	var rec StateData
	if json.Unmarshal([]byte(raw), &rec) != nil || rec.Provider != key {
		return nil, ErrState
	}
	return &rec, nil
}

// Finish 校验并消费 state，再用授权码向第三方换取身份。
func (f *Flow) Finish(ctx context.Context, key, code, state string) (*oauth.Identity, *StateData, error) {
	rec, err := f.Take(ctx, key, state)
	if err != nil {
		return nil, nil, err
	}
	a, pc, err := f.adapter(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	if !AllowedRedirect(pc, rec.Redirect) { // 配置在授权期间被改掉也要拒绝
		return nil, nil, ErrRedirect
	}
	var pk *oauth.PKCE
	if rec.Verifier != "" {
		pk = oauth.PKCEFromVerifier(rec.Verifier)
	}
	id, err := a.Exchange(ctx, code, rec.Redirect, pk)
	if err != nil {
		return nil, nil, err
	}
	if id.Provider == "" {
		id.Provider = key
	}
	return id, rec, nil
}
