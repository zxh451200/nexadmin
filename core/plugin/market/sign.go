package market

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// 包签名格式：对 zip 的 SHA-256 摘要做 ed25519 签名，签名与公钥都用标准 base64 文本。
// 私钥格式与 `nexadmin plugin keygen` 一致（PKCS#8 PEM）。

// LoadSigningKey 读取 PKCS#8 PEM 的 ed25519 私钥。
func LoadSigningKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(raw)
	if blk == nil {
		return nil, errors.New("私钥文件不是 PEM 格式")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok || len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("不是 ed25519 私钥")
	}
	return priv, nil
}

func fileDigest(path string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// SignFile 对文件摘要签名，返回 64 字节签名。
func SignFile(path string, priv ed25519.PrivateKey) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("私钥长度不正确")
	}
	digest, err := fileDigest(path)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(priv, digest[:]), nil
}

// VerifyFile 校验文件签名。
func VerifyFile(path string, sig []byte, pub ed25519.PublicKey) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("公钥长度不正确")
	}
	if len(sig) != ed25519.SignatureSize {
		return errors.New("签名长度不正确")
	}
	digest, err := fileDigest(path)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, digest[:], sig) {
		return errors.New("签名不匹配")
	}
	return nil
}

// EncodePublicKey 输出标准 base64 公钥（单行）。
func EncodePublicKey(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// DecodePublicKey 解析 base64 公钥文本（允许首尾空白）。
func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("公钥不是合法 base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("公钥长度不正确")
	}
	return ed25519.PublicKey(raw), nil
}

// EncodeSignature 输出标准 base64 签名文本。
func EncodeSignature(sig []byte) string {
	return base64.StdEncoding.EncodeToString(sig)
}

// DecodeSignature 解析 base64 签名文本。
func DecodeSignature(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("签名不是合法 base64: %w", err)
	}
	if len(raw) != ed25519.SignatureSize {
		return nil, errors.New("签名长度不正确")
	}
	return raw, nil
}
