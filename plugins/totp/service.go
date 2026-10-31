package totp

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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

// Service 是两步验证业务层。
type Service struct{ db *gorm.DB }

// NewService 创建 TOTP 服务。
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

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

// Confirm 用验证码确认绑定(密钥必须已生成且未启用)。
func (s *Service) Confirm(ctx context.Context, uid uint64, code string) error {
	row, sec, err := s.rowByUser(ctx, uid)
	if err != nil {
		return response.BadRequest("totp.not_enrolled")
	}
	if row.Enabled {
		return response.Conflict("totp.already_enabled")
	}
	step, verr := Verify(sec, code, time.Now().UTC())
	if verr != nil {
		return response.BadRequest("totp.code_invalid")
	}
	return s.db.WithContext(ctx).Model(&TOTPSecret{}).Where("id = ?", int64(row.ID)).
		Updates(map[string]any{"enabled": true, "last_step": step, "updated_at": time.Now().UTC()}).Error
}

// Disable 解绑(需验证码;防误触)。
func (s *Service) Disable(ctx context.Context, uid uint64, code string) error {
	row, sec, err := s.rowByUser(ctx, uid)
	if err != nil {
		return response.BadRequest("totp.not_enrolled")
	}
	if _, verr := Verify(sec, code, time.Now().UTC()); verr != nil {
		return response.BadRequest("totp.code_invalid")
	}
	return s.db.WithContext(ctx).Where("id = ?", int64(row.ID)).Delete(&TOTPSecret{}).Error
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

var _ = strings.TrimSpace
