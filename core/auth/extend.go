package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 本文件是登录扩展点（给插件用，插件不改 core）：前置检查、外部凭据校验、二次验证门、外部会话入口。
// 全部 nil 安全（Service 为 nil 时空操作）、线程安全（读写锁 + 写时复制），一个都没注册时登录行为与原来完全一致。
// 插件在 Start / Register 里通过 d.Auth.RegisterXxx(...) 注册；注册没有“注销”，进程生命周期内有效。

// ---- 登录前置检查 ----

// PreLoginFunc 在任何登录路径最前面调用（密码、手机号、第三方回调、绑定、二次验证、LoginAs）。
// 返回非 nil 即拒绝登录；对外统一是 403 auth.login_denied，原因只进服务端日志，不泄露规则细节。
// username 可能为空（手机号 / 第三方 / 二次验证入口此时不知道账号）。
type PreLoginFunc func(ctx context.Context, ip, username string) error

// RefreshCheckFunc 在刷新令牌换新令牌之前调用（Service.Refresh）。刷新不经过登录门，所以登录后才变化的限制
// （IP 黑白名单等）要靠它复检。返回非 nil 即拒绝：core 吊销该刷新令牌并对外返回 403 auth.login_denied。
// 只有经 auth.Handler 进来的请求才带 RequestInfo；没有 HTTP 来源的内部调用不会触发。
type RefreshCheckFunc func(ctx context.Context, u *User, ri RequestInfo) error

// ---- 外部凭据校验（LDAP 之类）----

// ExternalIdentity 是外部校验器 / 外部会话返回的“已验证身份”。core 据此映射或自动创建本地用户。
type ExternalIdentity struct {
	// ProviderKey scopes a subject to one directory source. Empty preserves legacy bindings.
	ProviderKey string
	Subject     string // 外部系统里的稳定标识（LDAP uid/DN、CAS principal），租户内唯一；空则取 Username
	Username    string // 本地账号名，须符合用户名规则（2-64 位字母数字 _ . @ -）
	Nickname    string
	Email       string
	Phone       string

	// MatchExisting 允许把该身份绑定到“同名且尚未绑定该方式”的已有本地账号（超级管理员永不匹配）。默认 false：不猜。
	MatchExisting bool
	// AutoCreate 允许在匹配不上时自动建号：无密码，受租户用户数额度限制。
	// MappedRoleCodes 非空时替代 DefaultRole，否则使用 DefaultRole（空为 guest）。
	// 角色仅建号时授予，已有用户保留本地角色；这不是持续角色同步。
	AutoCreate  bool
	DefaultRole string
	// MappedRoleCodes 是可信插件显式组映射后的本地角色编码，绝不能直接使用外部组名。
	// Resolver 必须重新校验当前租户、启用状态及非内置/非 super；无效角色拒绝建号。
	MappedRoleCodes []string
}

// ErrExternalRejected 表示外部校验器认为凭据无效或不适用（等同 nil 身份）；继续尝试下一个校验器。
var ErrExternalRejected = errors.New("auth: external credentials rejected")

// ExternalAuthenticator 在本地账号密码失败（含用户不存在）后按注册顺序尝试。
// 返回 (nil, nil) 或 ErrExternalRejected = 不通过；其他错误（网络等）也按不通过处理并写日志，对外与密码错误无差别。
// 失败计数 / 锁定仍由本地逻辑负责。实现里不得把密码写进日志或错误。
type ExternalAuthenticator interface {
	// Name 是登录方式标识（写入登录日志 login_type 与外部身份绑定的 provider），如 "ldap"。
	Name() string
	Authenticate(ctx context.Context, username, password string) (*ExternalIdentity, error)
}

// ExternalUserResolver 由 Provider 的实现者（用户模块）可选实现：把已验证的外部身份映射 / 创建为本地用户。
type ExternalUserResolver interface {
	ResolveExternalUser(ctx context.Context, method string, id ExternalIdentity) (*User, error)
}

// ExternalAuthTimeout 是单个外部校验器的调用上限。
const ExternalAuthTimeout = 10 * time.Second

// ---- 登录日志 ----

// 登录失败原因分类（与用户模块登录日志一致）。
const (
	FailCredentials = "credentials"
	FailBlocked     = "blocked"
	FailOther       = "other"
)

// LoginEvent 是扩展点产生的登录日志事件；Reason 为空表示成功。
type LoginEvent struct {
	Username, Method, IP, UserAgent, Reason string
}

// LoginRecorder 由 Provider 的实现者可选实现：写一条登录日志（login_type = Method）。
type LoginRecorder interface {
	RecordLogin(ctx context.Context, ev LoginEvent)
}

// ---- 二次验证门 ----

// GateInput 是 LoginGate.Check 的入参：凭据已通过、租户已校验、令牌尚未签发。
type GateInput struct {
	User      *User
	Method    string // 凭据校验方式：password / phone / <provider> / ldap / cas ...
	IP        string
	UserAgent string
}

// GateRequirement 非 nil 表示要求二次验证；Methods 是可用的验证方式（给前端展示，如 totp / recovery）。
type GateRequirement struct{ Methods []string }

// GateChallenge 是 LoginGate.Verify 看到的挑战信息（不含挑战 ID 明文）。
type GateChallenge struct {
	UserID   idgen.ID
	TenantID idgen.ID
	Username string
	Method   string
	IP       string
}

// GateAnswer 是用户提交的二次验证内容。Kind 为空表示默认方式；Code 不得写进日志。
type GateAnswer struct{ Kind, Code string }

// LoginGate 在凭据校验成功、签发令牌之前决定放行还是要求二次验证。
type LoginGate interface {
	// Name 是门的标识，≤32 位小写字母数字 _ -，二次验证成功 / 失败时作为登录日志的 login_type（如 "2fa"）。
	Name() string
	// Check 返回 (nil, nil) 放行；返回 requirement 则要求二次验证；返回 error 则拒绝登录（失败即关闭，不会放行）。
	Check(ctx context.Context, in GateInput) (*GateRequirement, error)
	// Verify 校验二次验证内容，通过返回 nil。尝试次数、TTL、一次性、IP 绑定由 core 保证；重放防护（如 TOTP 同码）由实现负责。
	Verify(ctx context.Context, ch GateChallenge, ans GateAnswer) error
}

var methodNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,31}$`)

// 保留的登录方式名：内置方式与第三方 provider，插件不能占用。
func reservedMethod(name string) bool {
	if name == "password" || name == "phone" {
		return true
	}
	return KnownProvider(name)
}

type extState struct {
	mu    sync.RWMutex
	pre   []PreLoginFunc
	rfsh  []RefreshCheckFunc
	ext   []ExternalAuthenticator
	gates []LoginGate
}

// RegisterPreLogin 注册登录前置检查（按注册顺序执行，任一返回错误即拒绝）。nil 函数忽略。
func (s *Service) RegisterPreLogin(fn PreLoginFunc) {
	if s == nil || fn == nil {
		return
	}
	s.x.mu.Lock()
	defer s.x.mu.Unlock()
	s.x.pre = append(append([]PreLoginFunc(nil), s.x.pre...), fn)
}

// RegisterRefreshCheck 注册刷新令牌复检（按注册顺序执行，任一返回错误即拒绝并吊销该刷新令牌）。nil 函数忽略。
func (s *Service) RegisterRefreshCheck(fn RefreshCheckFunc) {
	if s == nil || fn == nil {
		return
	}
	s.x.mu.Lock()
	defer s.x.mu.Unlock()
	s.x.rfsh = append(append([]RefreshCheckFunc(nil), s.x.rfsh...), fn)
}

func (s *Service) refreshChecks() []RefreshCheckFunc {
	if s == nil {
		return nil
	}
	s.x.mu.RLock()
	defer s.x.mu.RUnlock()
	return s.x.rfsh
}

// RegisterExternalAuthenticator 注册外部校验器；名称非法、与内置方式冲突或重复返回错误。
func (s *Service) RegisterExternalAuthenticator(a ExternalAuthenticator) error {
	if s == nil || a == nil {
		return errors.New("auth: nil service or authenticator")
	}
	name := a.Name()
	if !methodNameRe.MatchString(name) || reservedMethod(name) {
		return fmt.Errorf("auth: invalid external authenticator name %q", name)
	}
	s.x.mu.Lock()
	defer s.x.mu.Unlock()
	for _, e := range s.x.ext {
		if e.Name() == name {
			return fmt.Errorf("auth: external authenticator %q already registered", name)
		}
	}
	s.x.ext = append(append([]ExternalAuthenticator(nil), s.x.ext...), a)
	return nil
}

// RegisterLoginGate 注册二次验证门；名称非法、与内置方式冲突或重复返回错误。多个门按注册顺序依次生效。
func (s *Service) RegisterLoginGate(g LoginGate) error {
	if s == nil || g == nil {
		return errors.New("auth: nil service or gate")
	}
	name := g.Name()
	if !methodNameRe.MatchString(name) || reservedMethod(name) {
		return fmt.Errorf("auth: invalid login gate name %q", name)
	}
	s.x.mu.Lock()
	defer s.x.mu.Unlock()
	for _, e := range s.x.gates {
		if e.Name() == name {
			return fmt.Errorf("auth: login gate %q already registered", name)
		}
	}
	s.x.gates = append(append([]LoginGate(nil), s.x.gates...), g)
	return nil
}

func (s *Service) preFuncs() []PreLoginFunc {
	if s == nil {
		return nil
	}
	s.x.mu.RLock()
	defer s.x.mu.RUnlock()
	return s.x.pre
}

func (s *Service) externals() []ExternalAuthenticator {
	if s == nil {
		return nil
	}
	s.x.mu.RLock()
	defer s.x.mu.RUnlock()
	return s.x.ext
}

func (s *Service) gateList() []LoginGate {
	if s == nil {
		return nil
	}
	s.x.mu.RLock()
	defer s.x.mu.RUnlock()
	return s.x.gates
}

// PreLogin 依次执行前置检查。所有登录入口（含用户模块里的第三方登录入口）都要先调它；没有注册时返回 nil。
// 被拒绝时返回 403 auth.login_denied（规则细节只进日志，不返回给调用方），并写一条 blocked 登录日志。
func (s *Service) PreLogin(ctx context.Context, ip, username, ua string) error {
	for _, fn := range s.preFuncs() {
		if err := fn(ctx, ip, username); err != nil {
			s.record(ctx, LoginEvent{Username: username, Method: "-", IP: ip, UserAgent: ua, Reason: FailBlocked})
			return response.Forbidden("auth.login_denied").Wrap(err)
		}
	}
	return nil
}

// TryExternal 在本地凭据失败后按顺序尝试外部校验器，返回第一个通过的 (登录方式, 身份)。
// 没有注册、或都不通过时返回 ("", nil)。校验器的错误只在服务端日志可见（经 onErr 回调），调用方对外仍按密码错误处理。
func (s *Service) TryExternal(ctx context.Context, username, password string, onErr func(method string, err error)) (string, *ExternalIdentity) {
	for _, a := range s.externals() {
		cctx, cancel := context.WithTimeout(ctx, ExternalAuthTimeout)
		id, err := a.Authenticate(cctx, username, password)
		cancel()
		if err != nil {
			if !errors.Is(err, ErrExternalRejected) && onErr != nil {
				onErr(a.Name(), err)
			}
			continue
		}
		if id != nil {
			return a.Name(), id
		}
	}
	return "", nil
}

// ResolveExternal 把已验证的外部身份映射 / 创建为本地用户（由 Provider 实现 ExternalUserResolver）。
func (s *Service) ResolveExternal(ctx context.Context, method string, id ExternalIdentity) (*User, error) {
	if s == nil {
		return nil, ErrNoProvider
	}
	p := s.Provider()
	if p == nil {
		return nil, ErrNoProvider
	}
	r, ok := p.(ExternalUserResolver)
	if !ok {
		return nil, ErrNoProvider
	}
	return r.ResolveExternalUser(ctx, method, id)
}

// record 写登录日志（Provider 未实现 LoginRecorder 时忽略）。
// SyncExternal delegates source-owned provisioning to the user module.
func (s *Service) SyncExternal(ctx context.Context, id ExternalIdentity) (bool, error) {
	if s == nil || s.Provider() == nil {
		return false, ErrNoProvider
	}
	p, ok := s.Provider().(interface {
		SyncExternalUser(context.Context, ExternalIdentity) (bool, error)
	})
	if !ok {
		return false, ErrNoProvider
	}
	return p.SyncExternalUser(ctx, id)
}

func (s *Service) record(ctx context.Context, ev LoginEvent) {
	if s == nil {
		return
	}
	if p := s.Provider(); p != nil {
		if r, ok := p.(LoginRecorder); ok {
			r.RecordLogin(ctx, ev)
		}
	}
}
