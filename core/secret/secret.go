// Package secret 提供静态加密：默认 AES-256-GCM（v1），可选国密 SM4-GCM（v2）。
// 密文形如 enc:v1:<base64url(nonce||ciphertext)> 或 enc:v2:...（SM4）。
// 解密按前缀自动分派，v1/v2 可在同一密钥下共存（存量 v1 密文不受算法切换影响）。
//
// 主密钥来源（优先级从高到低）：环境变量 NEXADMIN_SECRET_KEY（32 字节，hex 或 base64）；
// 否则 <dataDir>/secret.key（hex 编码，首次启动自动生成，权限 0600）。
// 丢失主密钥意味着已加密的值无法恢复，部署时需要和数据库一起备份。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/tjfoc/gmsm/sm4"
)

const (
	// Prefix 标记一个值是 v1 密文（AES-256-GCM）；没有前缀的值按明文处理（兼容存量数据）。
	Prefix = "enc:v1:"
	// PrefixSM4 标记一个值是 v2 密文（SM4-GCM，国密）。
	PrefixSM4 = "enc:v2:"
	// EnvKey 是覆盖密钥文件的环境变量。
	EnvKey = "NEXADMIN_SECRET_KEY"
	// KeyFile 是 dataDir 下的密钥文件名。
	KeyFile = "secret.key"

	// AlgoAES 是默认算法；AlgoSM4 是国密 SM4-GCM（等保/密评场景用）。
	AlgoAES = "aes-gcm"
	AlgoSM4 = "sm4-gcm"
)

// sm4DeriveInfo 是从主密钥派生 SM4 子密钥的域分隔串（SM4 密钥 16 字节）。
const sm4DeriveInfo = "nexadmin/secret/v2/sm4"

var (
	// ErrNotInitialized 表示默认 Cipher 尚未初始化。
	ErrNotInitialized = errors.New("secret: not initialized")
	// ErrDecrypt 表示密文被篡改、格式不对或主密钥不匹配（不区分原因，避免成为预言机）。
	ErrDecrypt = errors.New("secret: decrypt failed (tampered data or wrong key)")
	// ErrUnknownAlgorithm 表示请求了不支持的算法名。
	ErrUnknownAlgorithm = errors.New("secret: unknown algorithm")
)

// Cipher 是绑定了一把主密钥的加解密器，可并发使用。
// aesAEAD 恒存在（v1 解密永远可用）；aead 是当前加密用的 aead（按算法选择）；
// 启用 SM4 后 v2 加解密可用。
type Cipher struct {
	aead    cipher.AEAD // 当前加密用的 aead（按算法选择）
	aesAEAD cipher.AEAD // v1（AES-256-GCM）专用 aead，解密旧密文用
	sm4aead cipher.AEAD // 启用 SM4 时非 nil
	key     []byte      // 主密钥副本，仅用于 Derive（派生子密钥），不对外暴露
	algo    string
}

func newAESAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// sm4Key 从 32 字节主密钥派生 SM4 的 16 字节子密钥：HMAC-SHA256(主密钥, sm4DeriveInfo)[:16]。
func sm4Key(master []byte) []byte {
	m := hmac.New(sha256.New, master)
	m.Write([]byte(sm4DeriveInfo))
	return m.Sum(nil)[:16]
}

func newSM4AEAD(master []byte) (cipher.AEAD, error) {
	block, err := sm4.NewCipher(sm4Key(master))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// New 用 32 字节主密钥创建默认（AES-256-GCM）Cipher。
func New(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secret: key must be 32 bytes, got %d", len(key))
	}
	aead, err := newAESAEAD(key)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead, aesAEAD: aead, key: append([]byte(nil), key...), algo: AlgoAES}, nil
}

// NewWithAlgorithm 用 32 字节主密钥创建指定算法的 Cipher：
// algo = AlgoAES（默认）或 AlgoSM4。SM4 模式下新密文用 enc:v2:，旧 v1 密文仍可解。
func NewWithAlgorithm(key []byte, algo string) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secret: key must be 32 bytes, got %d", len(key))
	}
	if algo == "" {
		algo = AlgoAES
	}
	c, err := New(key)
	if err != nil {
		return nil, err
	}
	switch algo {
	case AlgoAES:
		return c, nil
	case AlgoSM4:
		aead, err := newSM4AEAD(c.key)
		if err != nil {
			return nil, err
		}
		c.sm4aead, c.aead, c.algo = aead, aead, AlgoSM4
		return c, nil
	default:
		return nil, fmt.Errorf("%w: %q (want %q or %q)", ErrUnknownAlgorithm, algo, AlgoAES, AlgoSM4)
	}
} // Derive 用主密钥按用途派生 32 字节子密钥：HMAC-SHA256(主密钥, "nexadmin/derive/v1:"+purpose)。
// 同一主密钥 + 同一 purpose 结果固定；不同 purpose 互不相关。用于 HMAC pepper 之类，不要拿它当加密密钥。
func (c *Cipher) Derive(purpose string) []byte {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte("nexadmin/derive/v1:" + purpose))
	return m.Sum(nil)
}

// Derive 用默认 Cipher 派生子密钥；默认 Cipher 未初始化时返回 ErrNotInitialized。
func Derive(purpose string) ([]byte, error) {
	c := def.Load()
	if c == nil {
		return nil, ErrNotInitialized
	}
	return c.Derive(purpose), nil
}

// Encrypt 加密明文；每次使用随机 nonce，所以同一明文每次结果都不同。
// 默认算法返回 enc:v1:（AES），SM4 模式返回 enc:v2:。
func (c *Cipher) Encrypt(plain string) (string, error) {
	prefix := Prefix
	if c.algo == AlgoSM4 {
		prefix = PrefixSM4
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plain), []byte(prefix))
	return prefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密 v1（AES）/ v2（SM4）密文；没有前缀的值视为存量明文原样返回。
// 算法按前缀分派：v2 密文在未启用 SM4 的 Cipher 上无法解（返回 ErrDecrypt）。
func (c *Cipher) Decrypt(s string) (string, error) {
	switch {
	case strings.HasPrefix(s, Prefix):
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, Prefix))
		n := c.aesAEAD.NonceSize()
		if err != nil || len(raw) < n+c.aesAEAD.Overhead() {
			return "", ErrDecrypt
		}
		plain, err := c.aesAEAD.Open(nil, raw[:n], raw[n:], []byte(Prefix))
		if err != nil {
			return "", ErrDecrypt
		}
		return string(plain), nil
	case strings.HasPrefix(s, PrefixSM4):
		if c.sm4aead == nil {
			return "", ErrDecrypt // 该 Cipher 未启用 SM4，v2 密文一律拒绝
		}
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, PrefixSM4))
		n := c.sm4aead.NonceSize()
		if err != nil || len(raw) < n+c.sm4aead.Overhead() {
			return "", ErrDecrypt
		}
		plain, err := c.sm4aead.Open(nil, raw[:n], raw[n:], []byte(PrefixSM4))
		if err != nil {
			return "", ErrDecrypt
		}
		return string(plain), nil
	default:
		return s, nil
	}
}

// IsEncrypted 报告 s 是否带 v1/v2 密文前缀。
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, Prefix) || strings.HasPrefix(s, PrefixSM4)
}

var def atomic.Pointer[Cipher]

// SetDefault 设置全局默认 Cipher（启动时调用一次；测试里也用它注入密钥）。
func SetDefault(c *Cipher) { def.Store(c) }

// Default 返回全局默认 Cipher，未初始化时为 nil。
func Default() *Cipher { return def.Load() }

// Encrypt 用默认 Cipher 加密。
func Encrypt(plain string) (string, error) {
	c := def.Load()
	if c == nil {
		return "", ErrNotInitialized
	}
	return c.Encrypt(plain)
}

// Decrypt 用默认 Cipher 解密；明文（无前缀）原样返回，所以调用方可以无条件调用。
func Decrypt(s string) (string, error) {
	if !IsEncrypted(s) {
		return s, nil
	}
	c := def.Load()
	if c == nil {
		return "", ErrNotInitialized
	}
	return c.Decrypt(s)
}

// InitDefault 按“环境变量 > dataDir/secret.key（不存在则生成）”加载主密钥并设为默认 Cipher（AES）。
func InitDefault(dataDir string) error {
	return InitDefaultWithAlgorithm(dataDir, AlgoAES)
}

// InitDefaultWithAlgorithm 同 InitDefault，但允许指定静态加密算法（AlgoAES / AlgoSM4）。
func InitDefaultWithAlgorithm(dataDir, algo string) error {
	key, err := LoadKey(dataDir)
	if err != nil {
		return err
	}
	c, err := NewWithAlgorithm(key, algo)
	if err != nil {
		return err
	}
	SetDefault(c)
	return nil
}

// LoadKey 返回主密钥：环境变量优先，否则读取/生成 dataDir/secret.key。
func LoadKey(dataDir string) ([]byte, error) {
	if v := strings.TrimSpace(os.Getenv(EnvKey)); v != "" {
		return parseKey(v, EnvKey)
	}
	path := filepath.Join(dataDir, KeyFile)
	if b, err := os.ReadFile(path); err == nil {
		return parseKey(strings.TrimSpace(string(b)), path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("secret: read %s: %w", path, err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("secret: create %s: %w", dataDir, err)
	}
	// O_EXCL：多个进程同时首次启动时只有一个能创建，其余读取它写入的那把。
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil, fmt.Errorf("secret: read %s: %w", path, rerr)
			}
			return parseKey(strings.TrimSpace(string(b)), path)
		}
		return nil, fmt.Errorf("secret: create %s: %w", path, err)
	}
	if _, err := f.WriteString(hex.EncodeToString(key)); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("secret: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("secret: write %s: %w", path, err)
	}
	return key, nil
}

func parseKey(v, from string) ([]byte, error) {
	if b, err := hex.DecodeString(v); err == nil && len(b) == 32 {
		return b, nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(v); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("secret: key from %s must be 32 bytes encoded as hex (64 chars) or base64", from)
}
