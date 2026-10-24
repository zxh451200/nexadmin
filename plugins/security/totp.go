package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 默认且验证器 App 普遍只支持 SHA1
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RFC 6238：HMAC-SHA1、6 位、30 秒步长，验证时允许前后各 1 个步长的时钟漂移。
const (
	totpDigits = 6
	totpPeriod = 30
	totpWindow = 1
	issuerName = "NexAdmin"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newSecret 生成 160 位随机密钥（base32，无填充）。
func newSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

// hotp 是 RFC 4226 的 HOTP（动态截断）。digits 通常是 6；RFC 6238 附录的测试向量是 8。
func hotp(key []byte, counter uint64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// totpCode 返回 secret 在时间步 step 的 6 位码。
func totpCode(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	if step < 0 {
		return "", fmt.Errorf("security: negative time step")
	}
	return hotp(key, uint64(step), totpDigits), nil
}

// matchTOTP 在 now 的 ±totpWindow 个步长内查找与 code 相同的码，返回命中的时间步。
// 三个步长都会比较（常量时间），不因命中而提前返回。
func matchTOTP(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	cur := totpStep(now)
	var hit int64
	found := false
	for d := int64(-totpWindow); d <= totpWindow; d++ {
		want, err := totpCode(secret, cur+d)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 && !found {
			hit, found = cur+d, true
		}
	}
	return hit, found
}

// otpauthURI 生成验证器 App 识别的 otpauth:// 链接（Key URI Format）。
func otpauthURI(secret, account string) string {
	label := url.PathEscape(issuerName + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuerName)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
