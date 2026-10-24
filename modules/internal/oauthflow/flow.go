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
	PurposeLogin = "login"
	PurposeBind  = "bind"
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
	hc    *http.Client

	mu       sync.Mutex // 让同进程内 state / bindToken 的“读后删”不被并发重放
	adapters map[string]oauth.Provider
	build    func(name string, c config.ProviderConf) (oauth.Provider, error)
}

var (
	once   sync.Once
	shared *Flow
)

// Shared 返回进程内共享的 Flow。auth.proxy 配置无效时只记日志并按直连处理（并让第三方登录因超时自然不可用）。
func Shared(cfg *config.Config, c cache.Cache, log *zap.Logger) *Flow {
	once.Do(func() {
		f, err := New(cfg, c, log)
		if err != nil {
			log.Error("oauth: invalid auth.proxy, third-party login disabled", zap.Error(err))
			f = &Flow{cfg: &config.Config{}, cache: c, log: log, adapters: map[string]oauth.Provider{}}
		}
		shared = f
	})
	return shared
}

// New 创建 Flow：外部调用统一 10 秒超时，auth.proxy 非空则走代理。
func New(cfg *config.Config, c cache.Cache, log *zap.Logger) (*Flow, error) {
	hc, err := oauth.NewHTTPClient(cfg.Auth.Proxy, oauth.DefaultTimeout)
	if err != nil {
		return nil, err
	}
	f := &Flow{cfg: cfg, cache: c, log: log, hc: hc, adapters: map[string]oauth.Provider{}}
	f.build = func(name string, pc config.ProviderConf) (oauth.Provider, error) {
		return oauth.New(name, oauth.Config{ClientID: pc.ClientID, ClientSecret: pc.ClientSecret, AgentID: pc.AgentID,
			Issuer: pc.Issuer, Scopes: pc.Scopes, Display: pc.Name, AuthURL: pc.AuthURL, TokenURL: pc.TokenURL,
			UserInfoURL: pc.UserInfoURL, JWKSURL: pc.JWKSURL}, f.hc)
	}
	return f, nil
}

// Conf 返回 provider 配置；未配置、缺必填项或不是受支持的 provider 返回 false。
func (f *Flow) Conf(key string) (config.ProviderConf, bool) {
	if !auth.KnownProvider(key) {
		return config.ProviderConf{}, false
	}
	return f.cfg.Auth.ProviderConf(key)
}

// Available 返回已配置且可用的 provider 键，顺序固定（与 auth.KnownProviders 一致）。
func (f *Flow) Available() []string {
	out := []string{}
	for _, k := range auth.KnownProviders {
		if _, ok := f.Conf(k); ok {
			out = append(out, k)
		}
	}
	return out
}

func (f *Flow) adapter(key string) (oauth.Provider, config.ProviderConf, error) {
	pc, ok := f.Conf(key)
	if !ok || f.build == nil {
		return nil, pc, ErrNotConfigured
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.adapters[key]; ok {
		return a, pc, nil
	}
	a, err := f.build(key, pc)
	if err != nil {
		return nil, pc, errors.Join(ErrNotConfigured, err)
	}
	f.adapters[key] = a
	return a, pc, nil
}

// AllowedRedirect 回调地址必须与配置白名单精确一致（防开放重定向 / 授权码被转发到别处）。
func AllowedRedirect(pc config.ProviderConf, redirect string) bool {
	if redirect == "" {
		return false
	}
	for _, u := range pc.RedirectURIs {
		if u == redirect {
			return true
		}
	}
	return false
}

func stateKey(state string) string {
	h := sha256.Sum256([]byte(state))
	return cache.GlobalKey("oauth", "state", hex.EncodeToString(h[:]))
}

// Start 生成一次性 state（5 分钟）并返回授权地址。redirect 必须在白名单内；OIDC 同时生成 PKCE。
func (f *Flow) Start(ctx context.Context, key, redirect, purpose string, uid idgen.ID) (string, error) {
	a, pc, err := f.adapter(key)
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
	a, pc, err := f.adapter(key)
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
