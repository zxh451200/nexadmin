package sso

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"strings"
)

// oauthErr 是标准 OAuth2 错误（RFC 6749 §5.2），HTTP 层按它输出。
type oauthErr struct {
	Status int
	Code   string
	Desc   string
}

func (e *oauthErr) Error() string { return e.Code + ": " + e.Desc }

func oerr(status int, code, desc string) *oauthErr {
	return &oauthErr{Status: status, Code: code, Desc: desc}
}

func randToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// hashSecret 生成 "s256$<salt>$<sha256(salt+secret)>"。密钥本身是 256 位随机数，加盐 sha256 足够，
// 且比 bcrypt 便宜得多（token 端点是公开入口，不能被慢哈希拖垮）。
func hashSecret(secret string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	s := hex.EncodeToString(salt)
	return "s256$" + s + "$" + sha256Hex(s+secret)
}

// checkSecret 常量时间比较。
func checkSecret(stored, secret string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 || parts[0] != "s256" {
		return false
	}
	want := sha256Hex(parts[1] + secret)
	return subtle.ConstantTimeCompare([]byte(parts[2]), []byte(want)) == 1
}

var dummySecretHash = hashSecret("dummy-secret-for-constant-time")

var (
	reChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	reVerifier  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

// verifyPKCE 校验 S256：BASE64URL(SHA256(verifier)) == challenge（常量时间）。
func verifyPKCE(challenge, verifier string) bool {
	if !reVerifier.MatchString(verifier) || !reChallenge.MatchString(challenge) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}
