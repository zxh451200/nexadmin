// Package secret 提供静态加密：AES-256-GCM，密文形如 enc:v1:<base64url(nonce||ciphertext)>。
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
)

const (
	// Prefix 标记一个值是 v1 密文；没有这个前缀的值按明文处理（兼容存量数据）。
	Prefix = "enc:v1:"
	// EnvKey 是覆盖密钥文件的环境变量。
	EnvKey = "NEXADMIN_SECRET_KEY"
	// KeyFile 是 dataDir 下的密钥文件名。
	KeyFile = "secret.key"
)

var (
	// ErrNotInitialized 表示默认 Cipher 尚未初始化。
	ErrNotInitialized = errors.New("secret: not initialized")
	// ErrDecrypt 表示密文被篡改、格式不对或主密钥不匹配（不区分原因，避免成为预言机）。
	ErrDecrypt = errors.New("secret: decrypt failed (tampered data or wrong key)")
)

// Cipher 是绑定了一把主密钥的加解密器，可并发使用。
type Cipher struct {
	aead cipher.AEAD
	key  []byte // 主密钥副本，仅用于 Derive（派生子密钥），不对外暴露
}

// New 用 32 字节主密钥创建 Cipher。
func New(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secret: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead, key: append([]byte(nil), key...)}, nil
}

// Derive 用主密钥按用途派生 32 字节子密钥：HMAC-SHA256(主密钥, "nexadmin/derive/v1:"+purpose)。
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

// Encrypt 加密明文，返回 enc:v1: 开头的密文；每次使用随机 nonce，所以同一明文每次结果都不同。
func (c *Cipher) Encrypt(plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plain), []byte(Prefix))
	return Prefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密 enc:v1: 密文；没有前缀的值视为存量明文原样返回。密文无效时返回 ErrDecrypt。
func (c *Cipher) Decrypt(s string) (string, error) {
	if !IsEncrypted(s) {
		return s, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, Prefix))
	n := c.aead.NonceSize()
	if err != nil || len(raw) < n+c.aead.Overhead() {
		return "", ErrDecrypt
	}
	plain, err := c.aead.Open(nil, raw[:n], raw[n:], []byte(Prefix))
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

// IsEncrypted 报告 s 是否带 v1 密文前缀。
func IsEncrypted(s string) bool { return strings.HasPrefix(s, Prefix) }

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

// InitDefault 按“环境变量 > dataDir/secret.key（不存在则生成）”加载主密钥并设为默认 Cipher。
func InitDefault(dataDir string) error {
	key, err := LoadKey(dataDir)
	if err != nil {
		return err
	}
	c, err := New(key)
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
