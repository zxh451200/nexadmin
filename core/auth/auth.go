// Package auth 是鉴权核心：JWT 双 token、令牌吊销、登录提供者接口、权限点登记。
// core 只管令牌和路由；“谁能登录、有哪些权限”由用户模块实现 Provider 接口提供。
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// User 与前端 AuthUser 一一对应。
type User struct {
	ID       idgen.ID `json:"id"`
	Username string   `json:"username"`
	Nickname string   `json:"nickname"`
	Avatar   string   `json:"avatar"`
	TenantID idgen.ID `json:"tenantId"`
	Roles    []string `json:"roles"`
	Perms    []string `json:"perms"` // "*" = 全部；"user:*" = 该前缀下全部
	Super    bool     `json:"-"`     // 超管：可进 /api/v1/admin、可跨租户
	// Platform 表示该用户是否平台级调用方（判据单源在 tenant.IsPlatformCallerID）。两种用法都靠它：
	// 平台共享表（嵌 PlatformModel、没有 tenant_id）的写入口只认这种调用方；表里根本没有租户列的
	// 插件（在线客服、官网、设备中心）整个后台也只对这种调用方开放。前端据此把写入口收成只读、
	// 把整页换成解释页，而不是让人撞 403。
	Platform bool `json:"platform"`
	// LoginMethod 由 Provider.Login 在凭据通过时填写实际的校验方式（如 "ldap"）；空 = 按请求类型（password / phone）。
	LoginMethod string `json:"-"`
	// TenantExpireSoon 非空表示所属租户即将到期（由 Service.Decorate 填充，登录与 /auth/me 返回）。
	TenantExpireSoon *ExpireSoon `json:"tenantExpireSoon,omitempty"`
}

// ExpireSoon 是租户即将到期的提示信息，前端据此显示提示条。
type ExpireSoon struct {
	ExpireAt time.Time `json:"expireAt"`
	DaysLeft int       `json:"daysLeft"`
}

// IsPlatform 报告该用户是否平台级调用方，与 core.IsPlatformCaller 同一条判据：超管，或落在默认租户里。
// 非超管的请求租户由 middleware.Tenant 强制等于令牌租户（不等就 403 tenant_mismatch），
// 所以按用户自身算与按请求算一致；令牌里没有租户时，解析链的兜底同样是默认租户，一并算平台内。
func (u *User) IsPlatform() bool {
	return tenant.IsPlatformCallerID(u.TenantID, u.Super)
}

// LoginRequest 是 POST /auth/login 的请求体（字段与前端 LoginPayload 一致）。
type LoginRequest struct {
	Type       string `json:"type"` // password | phone
	Username   string `json:"username"`
	Password   string `json:"password"`
	Captcha    string `json:"captcha"`
	CaptchaKey string `json:"captchaKey"`
	Phone      string `json:"phone"`
	Code       string `json:"code"`

	IP        string `json:"-"`
	UserAgent string `json:"-"`
}

// Provider 由用户模块实现，并通过 core.WithAuth(...) 或让模块本身实现该接口来注册。
// 失败时返回 *response.Error（如 response.BadRequest("auth.invalid_credentials")、response.Locked(300)）。
type Provider interface {
	// Login 校验凭据（含验证码、锁定、登录日志）并返回用户。
	Login(ctx context.Context, req LoginRequest) (*User, error)
	// Profile 按用户 ID 读取最新用户信息与权限；ctx 里已带该用户的租户。
	Profile(ctx context.Context, userID idgen.ID) (*User, error)
}

// PasswordVerifier reauthenticates a local credential without issuing a session
// or falling back to external login. Implementations must enforce lockout.
type PasswordVerifier interface {
	VerifyPassword(context.Context, idgen.ID, string) error
}

// Claims 是 JWT 载荷。Kind 区分 access / refresh；access 里的 RID 是配对 refresh 的 jti，登出时一并吊销。
type Claims struct {
	UserID   idgen.ID `json:"uid"`
	TenantID idgen.ID `json:"tid"`
	Username string   `json:"un"`
	Super    bool     `json:"su,omitempty"`
	Kind     string   `json:"knd"`
	RID      string   `json:"rid,omitempty"`
	// SessionStart 是本次会话首次登录的时间（Unix 秒）。刷新轮换原样带走，作为「最长会话」的锚点，
	// 避免每次刷新都把计时重新推后（那样等于永不过期）。
	SessionStart    int64           `json:"sst,omitempty"`
	ExternalSession ExternalSession `json:"ext,omitempty"`
	// SessionBinding 是登录发起方（插件）提供的不透明会话归属（SAML = "saml:<源ID>:<NameID>"）。
	// 刷新时原样带走，SessionTracker 据此继续跟踪轮换后的令牌。
	SessionBinding string `json:"sb,omitempty"`
	// Principal / Perms 不进 JWT，只由开放平台中间件为个人访问令牌（PrincipalPAT）与签名调用的应用（PrincipalApp）填充。
	// PrincipalApp 没有对应的用户，权限直接取 Perms（见 middleware.Perm）。
	Principal string   `json:"-"`
	Perms     []string `json:"-"`
	jwt.RegisteredClaims
}

// 非会话令牌的主体类型（Claims.Principal；空串 = 普通登录会话）。
const (
	PrincipalPAT = "pat"
	PrincipalApp = "app"
)

// Pair 是一对令牌，字段名与前端一致。
type Pair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// ErrInvalidToken 表示令牌无效、过期、类型不对或已吊销。
var ErrInvalidToken = errors.New("auth: invalid token")

// ErrNoProvider 表示没有注册登录提供者。
var ErrNoProvider = errors.New("auth: no provider registered")

const (
	kindAccess  = "access"
	kindRefresh = "refresh"
)

// SessionPolicy 是运行时可调的会话时限；字段为 0 时沿用配置里的默认值。
//   - IdleTTL：refresh token 的有效期 = 闲置多久没操作就要重新登录（每次刷新滑动续期）
//   - MaxLifetime：从首次登录算起的最长会话，到点必须重新登录（与是否活跃无关）
type SessionPolicy struct {
	AccessTTL   time.Duration
	IdleTTL     time.Duration
	MaxLifetime time.Duration
}

type sessionPolicyFunc func() SessionPolicy

// Service 负责签发、校验、吊销令牌。
type Service struct {
	secret        []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
	cache         cache.Cache
	singleSession bool
	provider      atomic.Pointer[Provider]
	policy        atomic.Pointer[sessionPolicyFunc]

	x       extState      // 登录扩展点注册表（见 extend.go）；零值可用
	chTTL   time.Duration // 二次验证挑战有效期（0 = 默认）
	chTries int           // 挑战最大尝试次数（0 = 默认）
}

// NewService 创建服务。jwt_secret 为空时读取/生成 data/jwt.secret（权限 0600）。
func NewService(cfg *config.Config, c cache.Cache) (*Service, error) {
	secret := cfg.Auth.JWTSecret
	if secret == "" {
		var err error
		if secret, err = loadOrCreateSecret(filepath.Join(cfg.DataDir, "jwt.secret")); err != nil {
			return nil, err
		}
	}
	if len(secret) < 32 {
		return nil, errors.New("auth: auth.jwt_secret must be at least 32 characters")
	}
	if cfg.Auth.SingleSession {
		if _, ok := cache.As[cache.Lease](c); !ok {
			return nil, errors.New("auth: single_session requires atomic cache.Lease")
		}
	}
	// 预热验证码素材：21 张背景解码 + 滑块/点选两个照片版 builder 构建约 0.3s，放后台
	// 跑，进程起来后的第一个用户请求不必扛冷启动（机器繁忙时冷启动曾观测到秒级尾刺）。
	go warmCaptchaAssets()
	return &Service{secret: []byte(secret), accessTTL: cfg.Auth.AccessTTL, refreshTTL: cfg.Auth.RefreshTTL, cache: c, singleSession: cfg.Auth.SingleSession}, nil
}

// warmCaptchaAssets 预热验证码的惰性素材（见 slider_render_photo.go 的 sync.Once 池）。
// 失败静默：素材挂了各出题入口本就有手绘回退，预热不改变任何失败路径。
func warmCaptchaAssets() {
	_, _ = slidePhotoCaptcha()
	_, _ = clickCaptcha()
}

func loadOrCreateSecret(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("auth: read %s: %w", path, err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return "", fmt.Errorf("auth: write %s: %w", path, err)
	}
	return secret, nil
}

// SigningSecret 返回解析后的签名密钥（配置值，或配置为空时自动生成并落盘 data/jwt.secret 的那把）。
// 插件要复用同一把密钥签发本地令牌（如官网草稿预览链接）时从这里取：
// 直接读 cfg.Auth.JWTSecret 拿不到自动生成的那把，密钥为空就永远签不出来。
func (s *Service) SigningSecret() []byte { return s.secret }

// SetProvider 注册登录提供者。
func (s *Service) SetProvider(p Provider) { s.provider.Store(&p) }

// SetSessionPolicyProvider 注册「当前会话时限」的来源（通常是设置页里的安全设置），
// 签发与刷新时调用，改了立即生效；传 nil 恢复为只读配置。
func (s *Service) SetSessionPolicyProvider(fn func() SessionPolicy) {
	if fn == nil {
		s.policy.Store(nil)
		return
	}
	f := sessionPolicyFunc(fn)
	s.policy.Store(&f)
}

// sessionPolicy 合并注册的运行时时限与配置默认值：0 表示该维度沿用配置（最长会话除外，0 = 不设上限）。
func (s *Service) sessionPolicy() SessionPolicy {
	p := SessionPolicy{AccessTTL: s.accessTTL, IdleTTL: s.refreshTTL}
	fp := s.policy.Load()
	if fp == nil {
		return p
	}
	q := (*fp)()
	if q.AccessTTL > 0 {
		p.AccessTTL = q.AccessTTL
	}
	if q.IdleTTL > 0 {
		p.IdleTTL = q.IdleTTL
	}
	p.MaxLifetime = q.MaxLifetime
	return p
}

// Provider 返回登录提供者，未注册返回 nil。
func (s *Service) Provider() Provider {
	if p := s.provider.Load(); p != nil {
		return *p
	}
	return nil
}

func newJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// IssuePair 为用户签发 access + refresh。
func (s *Service) IssuePair(u *User) (Pair, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.issuePair(ctx, u, "", 0, "")
}

// singleSessionKey 是单会话租约的名称。
func singleSessionKey(u *User) string {
	return cache.GlobalKey("auth", "single-session", u.TenantID.String(), u.ID.String())
}

func (s *Service) issuePair(ctx context.Context, u *User, previousRefresh string, sessionStart int64, binding string) (Pair, error) {
	session := externalSessionFrom(ctx)
	if err := s.checkExternalSession(ctx, session); err != nil {
		return Pair{}, err
	}
	now := time.Now()
	pol := s.sessionPolicy()
	// 首次登录从此刻起算；续期沿用令牌里带来的起点（秒级精度，只会更早到期，不会延长）。
	startedAt := now
	if sessionStart > 0 {
		startedAt = time.Unix(sessionStart, 0)
	}
	accessTTL, refreshTTL := pol.AccessTTL, pol.IdleTTL
	// 最长会话是绝对上限：access / refresh 的有效期都不能越过它，否则刷新会无限续命。
	if pol.MaxLifetime > 0 {
		left := startedAt.Add(pol.MaxLifetime).Sub(now)
		if left <= 0 {
			return Pair{}, ErrInvalidToken
		}
		accessTTL = min(accessTTL, left)
		refreshTTL = min(refreshTTL, left)
	}
	if sessionStart <= 0 {
		sessionStart = startedAt.Unix()
	}
	rid := newJTI()
	accessJTI := newJTI()
	mk := func(kind, jti, pair string, ttl time.Duration) (string, error) {
		exp := now.Add(ttl)
		if session.Key != "" && session.Until.Before(exp) {
			exp = session.Until
		}
		cl := Claims{
			UserID: u.ID, TenantID: u.TenantID, Username: u.Username, Super: u.Super, Kind: kind, RID: pair,
			SessionStart:     sessionStart,
			ExternalSession:  session,
			SessionBinding:   binding,
			RegisteredClaims: jwt.RegisteredClaims{ID: jti, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp)},
		}
		return jwt.NewWithClaims(jwt.SigningMethodHS256, cl).SignedString(s.secret)
	}
	access, err := mk(kindAccess, accessJTI, rid, accessTTL)
	if err != nil {
		return Pair{}, err
	}
	refresh, err := mk(kindRefresh, rid, "", refreshTTL)
	if err != nil {
		return Pair{}, err
	}
	if s.singleSession {
		leases, ok := cache.As[cache.Lease](s.cache)
		if !ok {
			return Pair{}, errors.New("auth: single_session requires atomic cache.Lease")
		}
		ttl := max(accessTTL, refreshTTL)
		if err := leases.UpdateLeases(ctx, singleSessionKey(u), ttl, func(entries map[string]cache.LeaseEntry) error {
			// Compare and replace in one atomic operation: a refresh cannot
			// reclaim the account after a newer login has replaced it.
			if previousRefresh != "" {
				if _, ok := entries[kindRefresh+":"+previousRefresh]; !ok {
					return ErrInvalidToken
				}
			}
			for key := range entries {
				delete(entries, key)
			}
			entries[kindAccess+":"+accessJTI] = cache.LeaseEntry{Until: now.Add(accessTTL)}
			entries[kindRefresh+":"+rid] = cache.LeaseEntry{Until: now.Add(refreshTTL)}
			return nil
		}); err != nil {
			return Pair{}, err
		}
	}
	if err := s.checkExternalSession(ctx, session); err != nil {
		return Pair{}, err
	}
	return Pair{AccessToken: access, RefreshToken: refresh}, nil
}

func revokedKey(jti string) string { return cache.GlobalKey("auth", "revoked", jti) }

// rotatedKey 记录「这个 refresh 已被轮换、后继是谁」（value = 新 refresh 的 JTI）。
// refresh token 是 bearer 凭证：轮换后旧值再次出现即被盗/重放，按被盗处理——
// 吊销后继对并拒绝，合法用户与攻击者都强制重新登录（对齐 OAuth 轮换复用检测的通行做法）。
func rotatedKey(jti string) string { return cache.GlobalKey("auth", "rotated", jti) }

// revokeRefreshTTL / revokeAccessTTL 是吊销键的存活期：取配置与运行时 policy 的较大值。
// policy 可被调大（SetSessionPolicyProvider），只按配置值存吊销键会在令牌仍有效时
// 提前过期——登出后的令牌就「复活」了。
func (s *Service) revokeRefreshTTL() time.Duration {
	return max(s.refreshTTL, s.sessionPolicy().IdleTTL)
}

func (s *Service) revokeAccessTTL() time.Duration {
	return max(s.accessTTL, s.sessionPolicy().AccessTTL)
}

func (s *Service) parse(ctx context.Context, token, kind string) (*Claims, error) {
	var cl Claims
	t, err := jwt.ParseWithClaims(token, &cl, func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !t.Valid || cl.Kind != kind || cl.ID == "" {
		return nil, ErrInvalidToken
	}
	if err := s.checkExternalSession(ctx, cl.ExternalSession); err != nil {
		return nil, err
	}
	revoked, err := s.cache.Exists(ctx, revokedKey(cl.ID))
	if err != nil {
		return nil, fmt.Errorf("auth: check revocation: %w", err) // 缓存不可用时拒绝放行
	}
	if revoked {
		return nil, ErrInvalidToken
	}
	if s.singleSession {
		raw, err := s.cache.Get(ctx, singleSessionKey(&User{ID: cl.UserID, TenantID: cl.TenantID}))
		if errors.Is(err, cache.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		if err != nil {
			return nil, fmt.Errorf("auth: check single session: %w", err)
		}
		var entries map[string]cache.LeaseEntry
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return nil, fmt.Errorf("auth: decode single session: %w", err)
		}
		if entry, ok := entries[kind+":"+cl.ID]; !ok || !time.Now().Before(entry.Until) {
			return nil, ErrInvalidToken
		}
	}
	return &cl, nil
}

// ParseAccess 校验 access token。
func (s *Service) ParseAccess(ctx context.Context, token string) (*Claims, error) {
	return s.parse(ctx, token, kindAccess)
}

func (s *Service) revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if jti == "" || ttl <= 0 {
		return nil
	}
	return s.cache.Set(ctx, revokedKey(jti), "1", ttl)
}

// Profile 用令牌里的租户读取用户最新信息。
func (s *Service) Profile(ctx context.Context, cl *Claims) (*User, error) {
	p := s.Provider()
	if p == nil {
		return nil, ErrNoProvider
	}
	if cl.TenantID != 0 {
		ctx = tenant.With(ctx, cl.TenantID)
	}
	return p.Profile(ctx, cl.UserID)
}

// CheckTenant 检查用户所属租户是否可用（未过期、未停用；宽限期内放行）。平台超管不受影响。
// 失败返回 403 + reason TENANT_EXPIRED。登录、刷新令牌、第三方登录签发令牌前都要调用。
func (s *Service) CheckTenant(ctx context.Context, u *User) error {
	if u == nil || u.Super || u.TenantID == 0 {
		return nil
	}
	return tenant.CheckActive(ctx, u.TenantID)
}

// Decorate 给用户补上租户即将到期提示（tenantExpireSoon）。超管不提示；查不到租户信息时静默跳过（提示不是关键路径）。
func (s *Service) Decorate(ctx context.Context, u *User) {
	if u == nil || u.Super || u.TenantID == 0 {
		return
	}
	info, err := tenant.Lookup(ctx, u.TenantID)
	if err != nil || info == nil {
		return
	}
	now := time.Now()
	if n, ok := info.ExpireSoon(now); ok {
		u.TenantExpireSoon = &ExpireSoon{ExpireAt: info.ExpireAt.UTC(), DaysLeft: n}
	}
}

// Refresh 用 refresh token 换一对新令牌（旧 refresh 立即作废，并留下轮换标记）。
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Pair, error) {
	cl, err := s.parse(ctx, refreshToken, kindRefresh)
	if err != nil {
		return Pair{}, err
	}
	// 轮换复用检测：这个 refresh 之前已经轮换过（标记里存着后继 JTI）——本次使用是被盗
	// 或重放，把后继对一并吊销并拒绝。非 singleSession 模式没有租约保护，全靠这道闸。
	if s.cache != nil {
		if succ, gerr := s.cache.Get(ctx, rotatedKey(cl.ID)); gerr == nil && succ != "" {
			_ = s.revoke(ctx, succ, s.revokeRefreshTTL())
			_ = s.revoke(ctx, cl.ID, s.revokeRefreshTTL())
			return Pair{}, ErrInvalidToken
		}
	}
	u, err := s.Profile(ctx, cl)
	if err != nil {
		return Pair{}, err
	}
	if err := s.CheckTenant(ctx, u); err != nil { // 租户过期/停用后不能再用刷新令牌续命
		return Pair{}, err
	}
	if ri, ok := RequestInfoFrom(ctx); ok { // 刷新不经过登录门：IP 规则等登录后才变化的限制在这里复检
		for _, fn := range s.refreshChecks() {
			if err := fn(ctx, u, ri); err != nil {
				_ = s.revoke(ctx, cl.ID, time.Until(cl.ExpiresAt.Time)) // 被拒的刷新令牌立即作废
				return Pair{}, response.Forbidden("auth.login_denied").Wrap(err)
			}
		}
	}
	if err := s.revoke(ctx, cl.ID, time.Until(cl.ExpiresAt.Time)); err != nil {
		return Pair{}, err
	}
	pair, err := s.issuePair(WithExternalSession(ctx, cl.ExternalSession), u, cl.ID, cl.SessionStart, cl.SessionBinding)
	if err != nil {
		return Pair{}, err
	}
	// 记录轮换链：旧 refresh JTI → 新 refresh JTI，标记活到旧令牌自然过期为止。
	if s.cache != nil {
		if ncl, perr := s.parse(ctx, pair.RefreshToken, kindRefresh); perr == nil {
			_ = s.cache.Set(ctx, rotatedKey(cl.ID), ncl.ID, time.Until(cl.ExpiresAt.Time))
		}
	}
	// 轮换后的令牌继续挂同一个会话归属，外部 IdP 发起的登出才能吊销到它。
	s.notifySessionTrackers(ctx, pair)
	return pair, nil
}

// RevokeSession 登出：吊销当前 access 及其配对的 refresh。
func (s *Service) RevokeSession(ctx context.Context, cl *Claims) error {
	if err := s.revoke(ctx, cl.ID, time.Until(cl.ExpiresAt.Time)); err != nil {
		return err
	}
	return s.revoke(ctx, cl.RID, s.revokeRefreshTTL())
}

// RevokeByJTI 按令牌 ID 吊销一对 access / refresh。外部 IdP 发起的单点登出只拿得到
// NameID（没有本地会话上下文），调用方按登录时记下的 jti 调这里。
func (s *Service) RevokeByJTI(ctx context.Context, accessJTI, refreshJTI string) error {
	if accessJTI != "" {
		if err := s.revoke(ctx, accessJTI, s.revokeAccessTTL()); err != nil {
			return err
		}
	}
	if refreshJTI != "" {
		return s.revoke(ctx, refreshJTI, s.revokeRefreshTTL())
	}
	return nil
}

var def atomic.Pointer[Service]

// SetDefault 设置全局服务，供 core.Perm 等中间件在请求时取用。
func SetDefault(s *Service) { def.Store(s) }

// Default 返回全局服务（可能为 nil）。
func Default() *Service { return def.Load() }
