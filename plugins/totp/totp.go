package totp

// totp.go 是 RFC 6238 TOTP 的纯标准库实现(HMAC-SHA1 / 30 秒步长 / 6 位数字,±1 步长容差),
// 密钥 Base32 编码。与 Google Authenticator / 1Password / 微软验证器兼容。

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 步长与位数(RFC 6238 默认)。
const (
	StepSeconds = 30
	Digits      = 6
	SecretBytes = 20 // 160 位密钥
	// ToleranceSteps 是允许的时钟偏移容差(±1 步 = ±30 秒)。
	ToleranceSteps = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret 生成新密钥(20 字节随机 → Base32 无填充)。
func GenerateSecret() (string, error) {
	b := make([]byte, SecretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

// OTPAuthURI 生成 otpauth:// 迁移 URI(验证器 App 扫码 / 手动录入用)。
func OTPAuthURI(issuer, account, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=%d&period=%d",
		urlEscape(issuer), urlEscape(account), secret, urlEscape(issuer), Digits, StepSeconds)
}

// CodeAt 计算指定时间步长的 TOTP 码。
func CodeAt(secret string, t time.Time) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", fmt.Errorf("totp: bad secret: %w", err)
	}
	step := t.Unix() / StepSeconds
	return hotp(key, step), nil
}

// Verify 校验验证码,允许 ±ToleranceSteps 的时钟偏移。返回命中的步长(重放防护用:
// 命中步长必须严格大于上一次成功验证的步长)。
func Verify(secret, code string, now time.Time) (int64, error) {
	code = strings.TrimSpace(code)
	if len(code) != Digits {
		return 0, errors.New("totp: code must be 6 digits")
	}
	key, err := b32.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return 0, fmt.Errorf("totp: bad secret: %w", err)
	}
	cur := now.Unix() / StepSeconds
	for _, off := range []int64{0, -ToleranceSteps, ToleranceSteps} {
		step := cur + off
		if hotp(key, step) == code {
			return step, nil
		}
	}
	return 0, errors.New("totp: code mismatch")
}

// hotp 是 RFC 4226 HMAC-SHA1 截断。
func hotp(key []byte, step int64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0F
	val := (int64(sum[off])&0x7F)<<24 | int64(sum[off+1])<<16 | int64(sum[off+2])<<8 | int64(sum[off+3])
	return fmt.Sprintf("%0*d", Digits, val%1000000)
}

func urlEscape(s string) string {
	return strings.NewReplacer(":", "%3A", "/", "%2F", "?", "%3F", " ", "%20").Replace(s)
}
