package security

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/cluster"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

const (
	gateName      = "2fa" // 登录日志里的 login_type
	recoveryCount = 10
	defaultGrace  = 7
	minGrace      = 1 // 至少 1 天：0 天会让强制策略生效当刻用户来不及绑定（等于锁死）
	maxGrace      = 90

	defaultMaxFails = 5
	defaultFailWin  = 10 * time.Minute
	defaultLockFor  = 15 * time.Minute
	// userFailFactor：同一用户跨所有 IP 的累计失败达到 maxFails*userFailFactor 才触发“用户级冷却”（只挡 TOTP，恢复码可绕过）。
	// 单个 IP 先在 maxFails 次时被锁，所以一个攻击者几乎只能锁住自己的 IP，而不是把受害者全局锁住。
	userFailFactor = 3
	setupBindTTL   = 15 * time.Minute // setup 生成的待确认密钥必须在这段时间内、在同一会话里 enable

	ruleCacheTTL = 5 * time.Second
	maxRules     = 500
)

var (
	// errInvalidCode 是验证失败的统一错误。core 的 /auth/login/verify 会把非 4xx 业务错误统一成 auth.verify_failed。
	errInvalidCode  = errors.New("security: invalid verification code")
	errIPDenied     = errors.New("security: ip not allowed")
	errBindRequired = errors.New("security: two-factor binding required and grace period expired")
)

// User 是调用方（handler / 门）传入的当前用户信息。
type User struct {
	TID, ID idgen.ID
	Name    string
	Roles   []string
	Super   bool
	IP      string
	// Session 是当前访问令牌的 jti（setup 与 enable 必须在同一会话）；Principal 非空表示 PAT / 应用签名调用（不允许改 2FA）。
	Session, Principal string
	// ConfirmLockout 是管理员对“这次规则变更会锁住我当前 IP”的二次确认（仅 IP 规则写操作使用）。
	ConfirmLockout bool
}

// Service 承载全部业务逻辑；handler 与 auth 扩展点都只是它的薄封装。
type Service struct {
	st    store
	cache cache.Cache
	log   *zap.Logger
	now   func() time.Time

	maxFails int
	failWin  time.Duration
	lockFor  time.Duration

	rmu    sync.Mutex
	rcache map[idgen.ID]ruleSnap

	// IP 规则应急开关（bypass.go）。
	ipRulesDisabled bool
	loopbackExempt  bool

	bus *cluster.Bus // 规则变更广播；nil = 只剩本地缓存 + 5 秒 TTL

	// reauth 用当前密码重新认证（setup 前）；nil 时 setup 失败即关闭。lookup 读取目标用户（重置时判断是否超管）。
	reauth func(ctx context.Context, u User, password string) error
	lookup func(ctx context.Context, uid idgen.ID) (*auth.User, error)

	pepperOnce sync.Once
	pepper     []byte
	pepperErr  error
}

type ruleSnap struct {
	views []ruleView
	at    time.Time
}

func newService(st store, c cache.Cache, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	s := &Service{st: st, cache: c, log: log, now: time.Now,
		maxFails: defaultMaxFails, failWin: defaultFailWin, lockFor: defaultLockFor,
		rcache: map[idgen.ID]ruleSnap{}}
	s.loadBypass(nil)
	return s
}

func badReq(key string, args ...any) *response.Error { return response.BadRequest(key, args...) }

func (s *Service) k(parts ...string) string {
	return cache.GlobalKey(append([]string{"security"}, parts...)...)
}

// ---- 失败计数 / 锁定 / 重放（全部走缓存，多实例共享）----

func (s *Service) incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	c, ok := cache.As[cache.Counter](s.cache)
	if !ok { // 没有原子计数就无法保证限次与防重放：失败即关闭，不退化成非原子读改写
		return 0, errors.New("security: cache does not support atomic counters")
	}
	n, _, err := c.Incr(ctx, key, ttl)
	return n, err
}

type attemptIPKey struct{}

// withAttemptIP 把发起验证的客户端 IP 放进 ctx（verifyAny 据此做“用户+IP”维度的失败计数）。
func withAttemptIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, attemptIPKey{}, ip)
}

func attemptIP(ctx context.Context) string { ip, _ := ctx.Value(attemptIPKey{}).(string); return ip }

func (s *Service) userLockKey(tid, uid idgen.ID) string {
	return s.k("lock", tid.String(), uid.String())
}

func (s *Service) ipLockKey(tid, uid idgen.ID, ip string) string {
	return s.k("iplock", tid.String(), uid.String(), ip)
}

// checkLocked 在 key 处于冷却期时返回 423（带剩余秒数）。
func (s *Service) checkLocked(ctx context.Context, key string) error {
	v, err := s.cache.Get(ctx, key)
	if errors.Is(err, cache.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	exp, _ := strconv.ParseInt(v, 10, 64)
	left := int(time.Unix(exp, 0).Sub(s.now()).Seconds()) + 1
	if left < 1 {
		left = 1
	}
	return response.Locked(left)
}

// setLock 让 key 进入 lockFor 的冷却期，并返回 423。
func (s *Service) setLock(ctx context.Context, key string) error {
	exp := s.now().Add(s.lockFor).Unix()
	if err := s.cache.Set(ctx, key, strconv.FormatInt(exp, 10), s.lockFor); err != nil {
		return err
	}
	return response.Locked(int(s.lockFor.Seconds()))
}

// attempt 记录本次尝试占用名额后两个维度的计数。
type attempt struct{ ip, user int64 }

// beginAttempt 先原子占一个名额再校验（先计数再校验：并发猜测也逃不掉上限）。两个维度：
//   - 用户+IP：maxFails 次失败后，该 IP 对该用户的验证（含恢复码）进入 lockFor 冷却；别的 IP 不受影响。
//   - 用户：跨 IP 累计 maxFails*userFailFactor 次失败后，该用户的 TOTP 验证进入冷却；
//     恢复码不受用户级冷却限制（密码正确的合法用户可凭恢复码绕过，也避免被人故意锁死），但仍受 IP 维度限制。
//
// 计数用 cache.Counter 的原子 Incr（带 TTL），缓存不支持就失败即关闭。
func (s *Service) beginAttempt(ctx context.Context, tid, uid idgen.ID, recovery bool) (*attempt, error) {
	ip := attemptIP(ctx)
	if err := s.checkLocked(ctx, s.ipLockKey(tid, uid, ip)); err != nil {
		return nil, err
	}
	if !recovery {
		if err := s.checkLocked(ctx, s.userLockKey(tid, uid)); err != nil {
			return nil, err
		}
	}
	a := &attempt{}
	var err error
	if a.ip, err = s.incr(ctx, s.k("failip", tid.String(), uid.String(), ip), s.failWin); err != nil {
		return nil, err
	}
	if a.ip > int64(s.maxFails) {
		return nil, s.setLock(ctx, s.ipLockKey(tid, uid, ip))
	}
	if !recovery {
		if a.user, err = s.incr(ctx, s.k("failuser", tid.String(), uid.String()), s.failWin); err != nil {
			return nil, err
		}
		if a.user > int64(s.maxFails*userFailFactor) {
			return nil, s.setLock(ctx, s.userLockKey(tid, uid))
		}
	}
	return a, nil
}

// recordFail 在校验失败后判断是否触发冷却；返回给调用方的错误。
func (s *Service) recordFail(ctx context.Context, tid, uid idgen.ID, a *attempt, recovery bool) error {
	if a == nil {
		return errInvalidCode
	}
	if a.ip >= int64(s.maxFails) {
		return s.setLock(ctx, s.ipLockKey(tid, uid, attemptIP(ctx)))
	}
	if !recovery && a.user >= int64(s.maxFails*userFailFactor) {
		return s.setLock(ctx, s.userLockKey(tid, uid))
	}
	return errInvalidCode
}

// clearFails 在验证成功后清掉该用户的失败计数与冷却（成功说明持有者是真本人，连用户级冷却也一并解除）。
func (s *Service) clearFails(ctx context.Context, tid, uid idgen.ID) {
	ip := attemptIP(ctx)
	_ = s.cache.Del(ctx, s.k("failip", tid.String(), uid.String(), ip), s.k("failuser", tid.String(), uid.String()),
		s.ipLockKey(tid, uid, ip), s.userLockKey(tid, uid))
}

// markUsed 原子登记“该用户在时间步 step 已用过码”，返回 false 表示重放。
func (s *Service) markUsed(ctx context.Context, tid, uid idgen.ID, step int64) (bool, error) {
	ttl := time.Duration(2*totpWindow+1)*totpPeriod*time.Second + time.Minute
	n, err := s.incr(ctx, s.k("used", tid.String(), uid.String(), strconv.FormatInt(step, 10)), ttl)
	return n == 1, err
}

// ---- 验证 ----

func (s *Service) decryptSecret(row *TOTPRow) (string, error) {
	if row == nil || row.SecretEnc == "" {
		return "", errors.New("security: no secret")
	}
	if !secret.IsEncrypted(row.SecretEnc) { // 库里出现明文视为数据异常，不接受
		return "", errors.New("security: stored secret is not encrypted")
	}
	return secret.Decrypt(row.SecretEnc)
}

// checkTOTP 校验 TOTP 并登记防重放；仅返回是否通过。
func (s *Service) checkTOTP(ctx context.Context, tid, uid idgen.ID, row *TOTPRow, code string) (bool, error) {
	sec, err := s.decryptSecret(row)
	if err != nil {
		return false, err
	}
	step, ok := matchTOTP(sec, code, s.now())
	if !ok {
		return false, nil
	}
	fresh, err := s.markUsed(ctx, tid, uid, step)
	if err != nil {
		return false, err
	}
	return fresh, nil
}

// verifyAny 校验已启用用户的验证码（kind: ""/totp/recovery）。失败按“用户+IP / 用户”两个维度限次并可能触发冷却。
// 发起验证的客户端 IP 通过 withAttemptIP 放在 ctx 里。
func (s *Service) verifyAny(ctx context.Context, tid, uid idgen.ID, kind, code string) error {
	recovery := kind == "recovery"
	if kind != "" && kind != "totp" && !recovery {
		return errInvalidCode
	}
	a, err := s.beginAttempt(ctx, tid, uid, recovery)
	if err != nil {
		return err
	}
	row, err := s.st.GetTOTP(ctx, tid, uid)
	if err != nil {
		return err
	}
	if row == nil || !row.Enabled {
		return errInvalidCode
	}
	var ok bool
	if recovery {
		ok, err = s.consumeRecovery(ctx, tid, uid, code)
	} else {
		ok, err = s.checkTOTP(ctx, tid, uid, row, code)
	}
	if err != nil {
		return err
	}
	if !ok {
		return s.recordFail(ctx, tid, uid, a, recovery)
	}
	s.clearFails(ctx, tid, uid)
	return nil
}

// ---- 恢复码 ----

const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

func newRecoveryCodes() ([]string, error) {
	out := make([]string, recoveryCount)
	max := big.NewInt(int64(len(recoveryAlphabet)))
	for i := range out {
		b := make([]byte, 10)
		for j := range b {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				return nil, err
			}
			b[j] = recoveryAlphabet[n.Int64()]
		}
		out[i] = string(b[:5]) + "-" + string(b[5:])
	}
	return out, nil
}

func normRecovery(code string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
}

// hashRecoveryLegacy 是旧格式：无密钥的 SHA256（只带租户 / 用户前缀）。只用于验证存量恢复码，不再生成。
func hashRecoveryLegacy(tid, uid idgen.ID, code string) string {
	h := sha256.Sum256([]byte("nexadmin-security-recovery:" + tid.String() + ":" + uid.String() + ":" + normRecovery(code)))
	return hex.EncodeToString(h[:])
}

// recoveryPepper 返回由 core/secret 主密钥派生的子密钥（只派生一次）。
func (s *Service) recoveryPepper() ([]byte, error) {
	s.pepperOnce.Do(func() { s.pepper, s.pepperErr = secret.Derive("security/recovery-code/v2") })
	return s.pepper, s.pepperErr
}

// hashRecovery 是新格式：HMAC-SHA256(服务端 pepper, tid:uid:code)，十六进制 64 位（与旧格式同长，列宽不变）。
// 数据库泄露后没有主密钥就无法离线验证猜测。
func (s *Service) hashRecovery(tid, uid idgen.ID, code string) (string, error) {
	key, err := s.recoveryPepper()
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(tid.String() + ":" + uid.String() + ":" + normRecovery(code)))
	return hex.EncodeToString(m.Sum(nil)), nil
}

func (s *Service) hashAll(tid, uid idgen.ID, codes []string) ([]string, error) {
	out := make([]string, len(codes))
	for i, c := range codes {
		h, err := s.hashRecovery(tid, uid, c)
		if err != nil {
			return nil, err
		}
		out[i] = h
	}
	return out, nil
}

// consumeRecovery 先按新格式、再按旧格式消费一个恢复码。恢复码一次性：用掉的旧格式码随之作废，
// 剩余的旧格式码只能靠“重新生成”换成新格式（哈希不可逆，无法原地迁移）。
func (s *Service) consumeRecovery(ctx context.Context, tid, uid idgen.ID, code string) (bool, error) {
	nh, err := s.hashRecovery(tid, uid, code)
	if err != nil {
		return false, err
	}
	if ok, err := s.st.ConsumeRecovery(ctx, tid, uid, nh); err != nil || ok {
		return ok, err
	}
	ok, err := s.st.ConsumeRecovery(ctx, tid, uid, hashRecoveryLegacy(tid, uid, code))
	if ok {
		s.log.Info("security: legacy-format recovery code consumed; regenerate recovery codes to move the rest to the keyed format",
			zap.String("user", uid.String()))
	}
	return ok, err
}

// ---- 自助：状态 / 绑定 / 启用 / 关闭 ----

// Status 是个人 2FA 状态。
type Status struct {
	Enabled      bool       `json:"enabled"`
	Pending      bool       `json:"pending"` // 已生成密钥但尚未确认
	RecoveryLeft int64      `json:"recoveryLeft"`
	Required     bool       `json:"required"`    // 策略要求该用户启用
	GraceEndsAt  *time.Time `json:"graceEndsAt"` // 强制且未绑定时的宽限截止
	CanDisable   bool       `json:"canDisable"`  // 被强制的非超管用户不能自行关闭
}

func (s *Service) policy(ctx context.Context, tid idgen.ID) (*Policy, error) {
	p, err := s.st.GetPolicy(ctx, tid)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return &Policy{Mode: ModeOptional, GraceDays: defaultGrace}, nil
	}
	return p, nil
}

func splitRoles(s string) []string {
	var out []string
	for _, r := range strings.Split(s, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

func (p *Policy) requires(roles []string) bool {
	switch p.Mode {
	case ModeAll:
		return true
	case ModeRoles:
		want := map[string]bool{}
		for _, r := range splitRoles(p.Roles) {
			want[r] = true
		}
		for _, r := range roles {
			if want[r] {
				return true
			}
		}
	}
	return false
}

// graceDeadline 取“用户自己的宽限截止”与“策略最近一次变更 + 宽限天数”中较晚者：
// 策略被改严（或重新开启）时给所有人一个新的完整宽限，不会因为旧行的过期时间而立刻锁人。
func (p *Policy) graceDeadline(row *TOTPRow) time.Time {
	var d time.Time
	if row != nil && row.GraceUntil != nil {
		d = *row.GraceUntil
	}
	if !p.UpdatedAt.IsZero() {
		if t := p.UpdatedAt.Add(days(p.GraceDays)); t.After(d) {
			d = t
		}
	}
	return d
}

func (s *Service) Status(ctx context.Context, u User) (*Status, error) {
	row, err := s.st.GetTOTP(ctx, u.TID, u.ID)
	if err != nil {
		return nil, err
	}
	pol, err := s.policy(ctx, u.TID)
	if err != nil {
		return nil, err
	}
	st := &Status{Required: pol.requires(u.Roles)}
	st.CanDisable = !st.Required || u.Super
	if row != nil {
		st.Enabled = row.Enabled
		st.Pending = !row.Enabled && row.SecretEnc != ""
	}
	if st.Enabled {
		if st.RecoveryLeft, err = s.st.CountRecovery(ctx, u.TID, u.ID); err != nil {
			return nil, err
		}
	} else if st.Required {
		if d := pol.graceDeadline(row); !d.IsZero() {
			st.GraceEndsAt = &d
		}
	}
	return st, nil
}

// SetupResult 是 setup 的返回：密钥和 otpauth 链接只在这里出现。
type SetupResult struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// requireSession 拒绝 PAT / 应用签名：二次验证的绑定、关闭、换码只允许浏览器会话。
func requireSession(u User) error {
	if u.Principal != "" {
		return response.Forbidden("security.session_required")
	}
	return nil
}

func (s *Service) setupKey(tid, uid idgen.ID) string {
	return s.k("setup", tid.String(), uid.String())
}

// Setup 生成（或重新生成）待确认的密钥；已启用时拒绝，必须先关闭。
// 重新认证：必须提供当前登录密码（走 Provider 的 VerifyPassword，带它自己的失败锁定）；没有可用的校验器时失败即关闭。
// 生成后把密钥与当前会话绑定（setupBindTTL 内有效），Enable 必须在同一会话里完成。
func (s *Service) Setup(ctx context.Context, u User, password string) (*SetupResult, error) {
	if err := requireSession(u); err != nil {
		return nil, err
	}
	row, err := s.st.GetTOTP(ctx, u.TID, u.ID)
	if err != nil {
		return nil, err
	}
	if row != nil && row.Enabled {
		return nil, response.Conflict("security.already_enabled")
	}
	if s.reauth == nil {
		return nil, response.Unavailable("auth.provider_missing")
	}
	if err := s.reauth(ctx, u, password); err != nil {
		return nil, err
	}
	sec, err := newSecret()
	if err != nil {
		return nil, err
	}
	enc, err := secret.Encrypt(sec)
	if err != nil {
		return nil, err
	}
	if err := s.st.SavePending(ctx, u.TID, u.ID, u.Name, enc); err != nil {
		return nil, err
	}
	if err := s.cache.Set(ctx, s.setupKey(u.TID, u.ID), u.Session, setupBindTTL); err != nil {
		return nil, err
	}
	s.audit(ctx, u, "setup_2fa", u.Name, "")
	return &SetupResult{Secret: sec, URI: otpauthURI(sec, u.Name)}, nil
}

func (s *Service) invalid(err error) error {
	if errors.Is(err, errInvalidCode) {
		return badReq("security.code_invalid")
	}
	return err
}

// Enable 用待确认密钥生成的验证码确认启用，并返回 10 个一次性恢复码（明文只此一次）。
func (s *Service) Enable(ctx context.Context, u User, code string) ([]string, error) {
	if err := requireSession(u); err != nil {
		return nil, err
	}
	ctx = withAttemptIP(ctx, u.IP)
	row, err := s.st.GetTOTP(ctx, u.TID, u.ID)
	if err != nil {
		return nil, err
	}
	if row == nil || row.Enabled || row.SecretEnc == "" {
		return nil, badReq("security.setup_missing")
	}
	// setup 与 enable 必须是同一会话：别的会话（比如被盗的另一个令牌）不能替你把待确认密钥确认掉。
	bound, err := s.cache.Get(ctx, s.setupKey(u.TID, u.ID))
	if errors.Is(err, cache.ErrNotFound) || (err == nil && bound != u.Session) {
		return nil, badReq("security.setup_expired")
	}
	if err != nil {
		return nil, err
	}
	a, err := s.beginAttempt(ctx, u.TID, u.ID, false)
	if err != nil {
		return nil, err
	}
	ok, err := s.checkTOTP(ctx, u.TID, u.ID, row, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, s.invalid(s.recordFail(ctx, u.TID, u.ID, a, false))
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	hashes, err := s.hashAll(u.TID, u.ID, codes)
	if err != nil {
		return nil, err
	}
	if err := s.st.EnableTOTP(ctx, u.TID, u.ID, hashes); err != nil {
		return nil, err
	}
	_ = s.cache.Del(ctx, s.setupKey(u.TID, u.ID))
	s.clearFails(ctx, u.TID, u.ID)
	s.audit(ctx, u, "enable_2fa", u.Name, "")
	return codes, nil
}

// Disable 关闭 2FA，需要当前有效的验证码（TOTP 或恢复码）。被策略强制的非超管用户不能关闭。
func (s *Service) Disable(ctx context.Context, u User, kind, code string) error {
	if err := requireSession(u); err != nil {
		return err
	}
	ctx = withAttemptIP(ctx, u.IP)
	pol, err := s.policy(ctx, u.TID)
	if err != nil {
		return err
	}
	if pol.requires(u.Roles) && !u.Super {
		return response.Forbidden("security.policy_required")
	}
	if err := s.verifyAny(ctx, u.TID, u.ID, kind, code); err != nil {
		return s.invalid(err)
	}
	if err := s.st.DisableTOTP(ctx, u.TID, u.ID); err != nil {
		return err
	}
	s.audit(ctx, u, "disable_2fa", u.Name, "")
	return nil
}

// RegenerateCodes 校验验证码后重发 10 个恢复码，旧码全部作废。
func (s *Service) RegenerateCodes(ctx context.Context, u User, code string) ([]string, error) {
	if err := requireSession(u); err != nil {
		return nil, err
	}
	if err := s.verifyAny(withAttemptIP(ctx, u.IP), u.TID, u.ID, "totp", code); err != nil {
		return nil, s.invalid(err)
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	hashes, err := s.hashAll(u.TID, u.ID, codes)
	if err != nil {
		return nil, err
	}
	if err := s.st.ReplaceRecovery(ctx, u.TID, u.ID, hashes); err != nil {
		return nil, err
	}
	s.audit(ctx, u, "regen_recovery", u.Name, "")
	return codes, nil
}

// audit 写管理操作日志（尽力而为；重置 2FA 走事务，不经过这里）。
func (s *Service) audit(ctx context.Context, op User, action, target, detail string) {
	l := &LogRow{OperatorID: op.ID, OperatorName: op.Name, Action: action, Target: clip(target, 100), Detail: clip(detail, 500), IP: clip(op.IP, 64)}
	if err := s.st.AddLog(ctx, op.TID, l); err != nil {
		s.log.Warn("security: write log failed", zap.String("action", action), zap.Error(err))
	}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func days(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }
