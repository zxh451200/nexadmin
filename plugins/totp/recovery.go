package totp

// recovery.go 是恢复码：验证器丢失时用一枚一次性码通过登录闸门（对齐 plugins/security
// 的实现口径——同字母表/同格式/同 HMAC 存储）。确认绑定后自动生成一批，可随时重新生成
// （旧码整批作废）；登录挑战里用户把恢复码填进动态码框即可，gate 按 kind=recovery 消费。

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

// RecoveryRow 对应 plg_totp_recovery：只存 HMAC 哈希，明文只在生成那一刻返回一次。
type RecoveryRow struct {
	ID        idgen.ID `gorm:"primaryKey;autoIncrement:false" json:"-"`
	TenantID  uint64   `gorm:"column:tenant_id;uniqueIndex:uk_plg_totp_recovery,priority:1" json:"-"`
	UserID    uint64   `gorm:"column:user_id;uniqueIndex:uk_plg_totp_recovery,priority:2" json:"userId"`
	CodeHash  string   `gorm:"column:code_hash;size:64;not null;uniqueIndex:uk_plg_totp_recovery,priority:3" json:"-"`
	UsedAt    *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// BeforeCreate 生成主键：真实迁移表的 id 是 NOT NULL 且无默认值。
func (m *RecoveryRow) BeforeCreate(*gorm.DB) error {
	if m.ID == 0 {
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		m.ID = id
	}
	return nil
}

func (RecoveryRow) TableName() string { return "plg_totp_recovery" }

const (
	recoveryCount    = 10
	recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"
)

var (
	pepperOnce sync.Once
	pepper     []byte
	pepperErr  error
)

// recoveryPepper 返回由 core/secret 主密钥派生的子密钥：数据库泄露后没有主密钥
// 就无法离线验证恢复码猜测。
func recoveryPepper() ([]byte, error) {
	pepperOnce.Do(func() { pepper, pepperErr = secret.Derive("totp/recovery-code/v1") })
	return pepper, pepperErr
}

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

// looksLikeRecovery 判断一段输入是不是恢复码形状（xxxxx-xxxxx）：与 6 位纯数字动态码
// 天然不同，TOTP 校验失败后才走恢复码消费，不会互相污染。
func looksLikeRecovery(code string) bool {
	if len(code) != 11 || code[5] != '-' {
		return false
	}
	for i, c := range code {
		if i == 5 {
			continue
		}
		if !strings.ContainsRune(recoveryAlphabet, c) {
			return false
		}
	}
	return true
}

func (s *Service) hashRecovery(tid, uid uint64, code string) (string, error) {
	key, err := recoveryPepper()
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(strconv.FormatUint(tid, 10) + ":" + strconv.FormatUint(uid, 10) + ":" + normRecovery(code)))
	return hex.EncodeToString(m.Sum(nil)), nil
}

// tidOf 取 ctx 里的租户（恢复码表按租户隔离，与 TOTPSecret 同口径）。
func (s *Service) tidOf(ctx context.Context) uint64 {
	tid, _ := s.dims(ctx, 0, "")
	return tid
}

// RegenerateRecovery 重新生成恢复码：旧码整批作废，返回新明文（只显示这一次）。
// 已启用的用户才能生成——恢复码是启用的附属物。
func (s *Service) RegenerateRecovery(ctx context.Context, uid uint64) ([]string, error) {
	row, _, err := s.rowByUser(ctx, uid)
	if err != nil {
		return nil, response.BadRequest("totp.not_enrolled")
	}
	if !row.Enabled {
		return nil, response.BadRequest("totp.not_enabled")
	}
	return s.replaceRecovery(ctx, row.TenantID, uid)
}

// replaceRecovery 生成 10 枚新码并整批替换旧码（Confirm 成功后与重新生成共用）。
func (s *Service) replaceRecovery(ctx context.Context, tid, uid uint64) ([]string, error) {
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return codes, s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND user_id = ?", tid, uid).Delete(&RecoveryRow{}).Error; err != nil {
			return err
		}
		rows := make([]RecoveryRow, 0, len(codes))
		for _, c := range codes {
			h, err := s.hashRecovery(tid, uid, c)
			if err != nil {
				return err
			}
			rows = append(rows, RecoveryRow{TenantID: tid, UserID: uid, CodeHash: h, CreatedAt: now, UpdatedAt: now})
		}
		return tx.Create(&rows).Error
	})
}

// consumeRecovery 消费一枚恢复码：条件更新（used_at IS NULL）保证并发/重放下只有
// 一次成功。tid 取自 ctx——表按租户隔离。
func (s *Service) consumeRecovery(ctx context.Context, uid uint64, code string) (bool, error) {
	tid := s.tidOf(ctx)
	h, err := s.hashRecovery(tid, uid, code)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&RecoveryRow{}).
		Where("tenant_id = ? AND user_id = ? AND code_hash = ? AND used_at IS NULL", tid, uid, h).
		Update("used_at", now)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// VerifyRecoveryForGate 是登录闸门 recovery kind 的校验：消费一枚恢复码
// （一次性、原子防重放）。失败计次由 core 的挑战限次负责。
func (s *Service) VerifyRecoveryForGate(ctx context.Context, uid uint64, code string) error {
	ok, err := s.consumeRecovery(ctx, uid, code)
	if err != nil {
		return err
	}
	if !ok {
		return response.BadRequest("totp.code_invalid")
	}
	return nil
}

// RecoveryLeft 返回该用户剩余未用的恢复码数。
func (s *Service) RecoveryLeft(ctx context.Context, uid uint64) int64 {
	tid := s.tidOf(ctx)
	var n int64
	if err := s.db.WithContext(ctx).Model(&RecoveryRow{}).
		Where("tenant_id = ? AND user_id = ? AND used_at IS NULL", tid, uid).Count(&n).Error; err != nil {
		return 0
	}
	return n
}

// Disable 时恢复码随绑定一起作废（绑定没了恢复码就是死数据，留着只会误导）。
func (s *Service) wipeRecovery(ctx context.Context, uid uint64) {
	tid := s.tidOf(ctx)
	_ = s.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", tid, uid).Delete(&RecoveryRow{}).Error
}
