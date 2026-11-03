package totp

import (
	"context"
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/cache"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// TOTPSecret 是某个用户的两步验证密钥(租户级,一行一用户)。
// secret 经 core/secret 加密落库;last_step 是重放防护(命中步长必须严格递增)。
type TOTPSecret struct {
	ID        idgen.ID `gorm:"primaryKey;autoIncrement:false" json:"-"`
	TenantID  uint64   `gorm:"column:tenant_id" json:"-"`
	UserID    uint64   `gorm:"column:user_id;uniqueIndex:uk_plg_totp_user" json:"userId"`
	SecretEnc string   `gorm:"column:secret_enc" json:"-"`
	Enabled   bool     `gorm:"column:enabled" json:"enabled"`
	LastStep  int64    `gorm:"column:last_step" json:"-"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// BeforeCreate 生成主键：真实迁移表的 id 是 NOT NULL 且无默认值。
func (m *TOTPSecret) BeforeCreate(*gorm.DB) error {
	if m.ID == 0 {
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		m.ID = id
	}
	return nil
}

func (TOTPSecret) TableName() string { return "plg_totp_secret" }

// 个人中心 confirm/disable 的失败限次(与 plugins/security 的默认值一致):
// 同一「用户+IP」在 failWindow 内猜错 maxFails 次后,该来源的验证(含正确码)进入 lockFor 冷却。
// 维度带 IP 是为了攻击者只能锁住自己的来源,不会把本人其他设备的正常操作一起锁死;
// 登录闸门自己的挑战限次由 core 保证,这里只保住个人中心这两个口。
const (
	maxFails   = 5
	failWindow = 10 * time.Minute
	lockFor    = 15 * time.Minute
)

// ReauthFn 用当前登录密码重新认证(Disable 前置);由 Provider 的 PasswordVerifier 提供。
type ReauthFn func(ctx context.Context, uid uint64, password string) error

// Service 是两步验证业务层。
type Service struct {
	db     *gorm.DB
	c      cache.Cache
	reauth ReauthFn
}

// NewService 创建 TOTP 服务。c/reauth 允许为 nil(测试手工构造):
// 无缓存时退化为不限次(生产 Deps.Cache 恒非 nil);无密码校验器时 Disable 直接 503。
func NewService(db *gorm.DB, c cache.Cache, reauth ReauthFn) *Service {
	return &Service{db: db, c: c, reauth: reauth}
}

var errNotEnrolled = errors.New("totp: not enrolled")

func (s *Service) rowByUser(ctx context.Context, uid uint64) (*TOTPSecret, string, error) {
	var m TOTPSecret
	err := s.db.WithContext(ctx).Take(&m, "user_id = ?", uid).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", errNotEnrolled
	}
	if err != nil {
		return nil, "", err
	}
	plain, err := secretDecrypt(m.SecretEnc)
	if err != nil {
		return nil, "", err
	}
	return &m, plain, nil
}

// StatusView 是个人中心的两步验证状态。
type StatusView struct {
	Enrolled bool `json:"enrolled"`
	Enabled  bool `json:"enabled"`
}

// Status 查询当前用户的绑定状态。
func (s *Service) Status(ctx context.Context, uid uint64) StatusView {
	row, _, err := s.rowByUser(ctx, uid)
	if err != nil {
		return StatusView{}
	}
	return StatusView{Enrolled: true, Enabled: row.Enabled}
}

// ---- 失败限次(全部走缓存,多实例共享)----

func (s *Service) key(parts ...string) string {
	return cache.GlobalKey(append([]string{"totp"}, parts...)...)
}

func (s *Service) failKey(tid, uid uint64, ip string) string {
	return s.key("fail", strconv.FormatUint(tid, 10), strconv.FormatUint(uid, 10), ip)
}

func (s *Service) lockKey(tid, uid uint64, ip string) string {
	return s.key("lock", strconv.FormatUint(tid, 10), strconv.FormatUint(uid, 10), ip)
}

func (s *Service) dims(ctx context.Context, uid uint64, ip string) (tid uint64, src string) {
	if t, ok := tenant.From(ctx); ok {
		tid = uint64(t)
	}
	return tid, ip
}

// checkLock 在该来源处于冷却期时返回 423(带剩余秒数)。冷却对正确码同样生效:
// 与 security 插件一致,冷却期内一律不比对,让爆破者拿不到任何判定信号。
func (s *Service) checkLock(ctx context.Context, uid uint64, ip string) error {
	if s.c == nil {
		return nil
	}
	tid, ip := s.dims(ctx, uid, ip)
	v, err := s.c.Get(ctx, s.lockKey(tid, uid, ip))
	if errors.Is(err, cache.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	exp, perr := strconv.ParseInt(v, 10, 64)
	if perr != nil {
		return nil
	}
	left := int(time.Unix(exp, 0).Sub(time.Now()).Seconds()) + 1
	if left < 1 {
		left = 1
	}
	return response.Locked(left)
}

// recordFail 在校验失败后记一次;达到 maxFails 次让该来源进入 lockFor 冷却。
// 计数用 cache.Counter 的原子 Incr(带 TTL);缓存不支持原子计数时失败即关闭,不退化成非原子读改写。
func (s *Service) recordFail(ctx context.Context, uid uint64, ip string) error {
	if s.c == nil {
		return response.BadRequest("totp.code_invalid")
	}
	tid, ip := s.dims(ctx, uid, ip)
	c, ok := cache.As[cache.Counter](s.c)
	if !ok {
		return errors.New("totp: cache does not support atomic counters")
	}
	n, _, err := c.Incr(ctx, s.failKey(tid, uid, ip), failWindow)
	if err != nil {
		return err
	}
	if n >= int64(maxFails) {
		exp := time.Now().Add(lockFor).Unix()
		if serr := s.c.Set(ctx, s.lockKey(tid, uid, ip), strconv.FormatInt(exp, 10), lockFor); serr != nil {
			return serr
		}
		return response.Locked(int(lockFor.Seconds()))
	}
	return response.BadRequest("totp.code_invalid")
}

// clearFails 在验证成功后清掉该来源的失败计数与冷却。
func (s *Service) clearFails(ctx context.Context, uid uint64, ip string) {
	if s.c == nil {
		return
	}
	tid, ip := s.dims(ctx, uid, ip)
	_ = s.c.Del(ctx, s.failKey(tid, uid, ip), s.lockKey(tid, uid, ip))
}

// EnrollStart 生成(或重置)未确认的密钥,返回 Base32 密钥与 otpauth:// URI。
// 已启用时必须先 Disable 才能重新绑定。
func (s *Service) EnrollStart(ctx context.Context, uid uint64, account, issuer string) (secretB32, uri string, err error) {
	if row, _, rerr := s.rowByUser(ctx, uid); rerr == nil && row.Enabled {
		return "", "", response.Conflict("totp.already_enabled")
	}
	sec, gerr := GenerateSecret()
	if gerr != nil {
		return "", "", gerr
	}
	enc, eerr := secretEncrypt(sec)
	if eerr != nil {
		return "", "", eerr
	}
	m := TOTPSecret{UserID: uid, SecretEnc: enc, Enabled: false, LastStep: 0}
	if tid, ok := tenant.From(ctx); ok {
		m.TenantID = uint64(tid)
	}
	m.ID, gerr = idgen.Next()
	if gerr != nil {
		return "", "", gerr
	}
	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"secret_enc": enc, "enabled": false, "last_step": 0, "updated_at": time.Now().UTC(),
			})}).Create(&m).Error; err != nil {
		return "", "", err
	}
	return sec, OTPAuthURI(issuer, account, sec), nil
}

// Confirm 用验证码确认绑定(密钥必须已生成且未启用)。验证码猜测按「用户+IP」限次。
func (s *Service) Confirm(ctx context.Context, uid uint64, code, ip string) error {
	if err := s.checkLock(ctx, uid, ip); err != nil {
		return err
	}
	row, sec, err := s.rowByUser(ctx, uid)
	if err != nil {
		return response.BadRequest("totp.not_enrolled")
	}
	if row.Enabled {
		return response.Conflict("totp.already_enabled")
	}
	step, verr := Verify(sec, code, time.Now().UTC())
	if verr != nil {
		return s.recordFail(ctx, uid, ip)
	}
	if err := s.db.WithContext(ctx).Model(&TOTPSecret{}).Where("id = ?", int64(row.ID)).
		Updates(map[string]any{"enabled": true, "last_step": step, "updated_at": time.Now().UTC()}).Error; err != nil {
		return err
	}
	s.clearFails(ctx, uid, ip)
	return nil
}

// Disable 解绑:先验当前登录密码(防被盗会话直接关掉二次验证),再验动态码;两者都过才删绑定。
// 密码错误的锁定由 Provider 的 PasswordVerifier 自己负责(接口契约如此)。
func (s *Service) Disable(ctx context.Context, uid uint64, code, password, ip string) error {
	if s.reauth == nil {
		return response.Unavailable("auth.provider_missing")
	}
	if err := s.checkLock(ctx, uid, ip); err != nil {
		return err
	}
	if err := s.reauth(ctx, uid, password); err != nil {
		return err
	}
	row, sec, err := s.rowByUser(ctx, uid)
	if err != nil {
		return response.BadRequest("totp.not_enrolled")
	}
	if _, verr := Verify(sec, code, time.Now().UTC()); verr != nil {
		return s.recordFail(ctx, uid, ip)
	}
	if err := s.db.WithContext(ctx).Where("id = ?", int64(row.ID)).Delete(&TOTPSecret{}).Error; err != nil {
		return err
	}
	s.clearFails(ctx, uid, ip)
	return nil
}

// VerifyForGate 是 LoginGate.Verify 的校验核心:码有效且步长严格递增(防重放)。
// 必须传入带租户的 ctx：表有 tenant_id，租户过滤器在无租户上下文会拒绝查询。
func (s *Service) VerifyForGate(ctx context.Context, uid uint64, code string) error {
	row, sec, err := s.rowByUser(ctx, uid)
	if err != nil {
		return response.BadRequest("totp.not_enrolled")
	}
	if !row.Enabled {
		return response.BadRequest("totp.not_enabled")
	}
	step, verr := Verify(sec, code, time.Now().UTC())
	if verr != nil {
		return response.BadRequest("totp.code_invalid")
	}
	if step <= row.LastStep {
		return response.BadRequest("totp.code_replayed")
	}
	// 条件更新做原子比较交换：并发提交同一验证码时只有一个请求能把 last_step 推进。
	res := s.db.WithContext(ctx).Model(&TOTPSecret{}).
		Where("id = ? AND last_step < ?", int64(row.ID), step).
		Update("last_step", step)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return response.BadRequest("totp.code_replayed")
	}
	return nil
}

// EnrolledAndEnabled 报告用户是否已启用两步验证。
// 查询错误必须原样返回，由 gate 拒绝登录（fail closed），不能当成「未绑定」放行。
func (s *Service) EnrolledAndEnabled(ctx context.Context, uid uint64) (bool, error) {
	row, _, err := s.rowByUser(ctx, uid)
	if errors.Is(err, errNotEnrolled) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return row.Enabled, nil
}

// secretEncrypt/secretDecrypt 包装 core/secret:测试环境没有默认密钥时退化为明文/原样。
func secretEncrypt(plain string) (string, error) {
	if secret.Default() == nil {
		return plain, nil
	}
	return secret.Encrypt(plain)
}

func secretDecrypt(enc string) (string, error) {
	if secret.Default() == nil || !secret.IsEncrypted(enc) {
		return enc, nil
	}
	return secret.Decrypt(enc)
}
