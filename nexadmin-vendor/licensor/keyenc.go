package main

// 私钥口令加密文件格式（只用标准库）。
//
//	偏移  长度  内容
//	0     7     魔数 "LICSRVK"
//	7     1     版本，当前 1
//	8     4     PBKDF2 迭代次数，大端 uint32
//	12    16    盐（随机）
//	28    12    AES-GCM nonce（随机）
//	40    ...   密文 + 16 字节 GCM tag
//
// 密钥派生: PBKDF2-HMAC-SHA256(口令, 盐, 迭代次数) → 32 字节 → AES-256-GCM。
// 头部 40 字节作为 GCM 附加认证数据（AAD），篡改盐/迭代次数/版本都会导致解密失败。
// 明文是 PKCS#8 PEM 私钥原文。

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	keyMagic      = "LICSRVK"
	keyVersion    = 1
	kdfIterations = 600000   // 新文件使用的迭代次数
	minIterations = 600000   // 解密时拒绝低于此值的文件（防降级）
	maxIterations = 10000000 // 拒绝过大的值（防被篡改的文件拖死进程；GCM 校验在派生之后，所以要先限上限）
	saltLen       = 16
	nonceLen      = 12
	headerLen     = len(keyMagic) + 1 + 4 + saltLen + nonceLen
	minPassLen    = 8
	passEnv       = "LICENSOR_PASSPHRASE"
	maxPassTries  = 3
)

var (
	errBadPass   = errors.New("口令错误，或密钥文件已被损坏/篡改")
	errBadFormat = errors.New("密钥文件格式不受支持或已损坏")
)

// isEncryptedKey 通过魔数识别密文文件。
func isEncryptedKey(raw []byte) bool { return bytes.HasPrefix(raw, []byte(keyMagic)) }

func deriveKey(pass string, salt []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, pass, salt, iter, 32)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// encryptKey 用口令加密明文，返回完整文件内容。
func encryptKey(plain []byte, pass string, iter int) ([]byte, error) {
	if len(pass) < minPassLen {
		return nil, fmt.Errorf("口令至少 %d 个字符", minPassLen)
	}
	if iter < minIterations || iter > maxIterations {
		return nil, fmt.Errorf("迭代次数必须在 %d..%d", minIterations, maxIterations)
	}
	hdr := make([]byte, headerLen)
	copy(hdr, keyMagic)
	hdr[len(keyMagic)] = keyVersion
	binary.BigEndian.PutUint32(hdr[len(keyMagic)+1:], uint32(iter))
	salt := hdr[len(keyMagic)+5 : len(keyMagic)+5+saltLen]
	nonce := hdr[len(keyMagic)+5+saltLen:]
	if _, err := io.ReadFull(rand.Reader, hdr[len(keyMagic)+5:]); err != nil {
		return nil, err
	}
	key, err := deriveKey(pass, salt, iter)
	if err != nil {
		return nil, err
	}
	defer wipe(key)
	g, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return g.Seal(hdr, nonce, plain, hdr[:headerLen:headerLen]), nil
}

// decryptKey 解密文件内容。口令错误与密文被篡改统一返回 errBadPass，不区分原因。
func decryptKey(blob []byte, pass string) ([]byte, error) {
	if len(blob) < headerLen+16 || !isEncryptedKey(blob) || blob[len(keyMagic)] != keyVersion {
		return nil, errBadFormat
	}
	iter := int(binary.BigEndian.Uint32(blob[len(keyMagic)+1:]))
	if iter < minIterations || iter > maxIterations {
		return nil, errBadFormat
	}
	salt := blob[len(keyMagic)+5 : len(keyMagic)+5+saltLen]
	nonce := blob[len(keyMagic)+5+saltLen : headerLen]
	key, err := deriveKey(pass, salt, iter)
	if err != nil {
		return nil, errBadPass
	}
	defer wipe(key)
	g, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	plain, err := g.Open(nil, nonce, blob[headerLen:], blob[:headerLen])
	if err != nil {
		return nil, errBadPass
	}
	return plain, nil
}

// parseKeyPEM 解析 PKCS#8 PEM 的 ed25519 私钥。
func parseKeyPEM(raw []byte) (ed25519.PrivateKey, error) {
	blk, _ := pem.Decode(raw)
	if blk == nil {
		return nil, errors.New("私钥文件不是 PEM 格式")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("不是 ed25519 私钥")
	}
	return priv, nil
}

// wipe 尽力清零（Go 无法保证不留副本，仅减少残留）。
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// loadKeyWith 读取私钥：密文走口令解密（环境变量优先，否则 prompt，最多 3 次），明文按旧逻辑。
// 返回值 encrypted 表示文件是否为密文；明文时调用方应打印加密建议。
func loadKeyWith(path string, env func(string) string, prompt passPrompt, errw io.Writer) (ed25519.PrivateKey, []string, bool, error) {
	var warns []string
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
		warns = append(warns, fmt.Sprintf("私钥文件权限是 %04o，建议 chmod 600", fi.Mode().Perm()))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false, err
	}
	if !isEncryptedKey(raw) {
		priv, err := parseKeyPEM(raw)
		return priv, warns, false, err
	}
	plain, err := decryptWithRetry(raw, env, prompt, errw)
	if err != nil {
		return nil, nil, true, err
	}
	defer wipe(plain)
	priv, err := parseKeyPEM(plain)
	return priv, warns, true, err
}

// decryptWithRetry 取口令并解密。环境变量口令错误不重试；交互口令最多 3 次。
func decryptWithRetry(blob []byte, env func(string) string, prompt passPrompt, errw io.Writer) ([]byte, error) {
	if p := env(passEnv); p != "" {
		plain, err := decryptKey(blob, p)
		if err != nil {
			if errors.Is(err, errBadPass) {
				return nil, fmt.Errorf("环境变量 %s 中的口令无法解开私钥", passEnv)
			}
			return nil, err
		}
		return plain, nil
	}
	for i := 1; i <= maxPassTries; i++ {
		p, err := prompt("请输入私钥口令: ")
		if err != nil {
			return nil, err
		}
		plain, err := decryptKey(blob, p)
		if err == nil {
			return plain, nil
		}
		if !errors.Is(err, errBadPass) {
			return nil, err
		}
		if i < maxPassTries {
			fmt.Fprintf(errw, "口令错误（第 %d/%d 次），请重试\n", i, maxPassTries)
		}
	}
	return nil, fmt.Errorf("口令错误次数达到 %d 次，已退出", maxPassTries)
}
