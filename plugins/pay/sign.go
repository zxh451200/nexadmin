package pay

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"strings"
)

// RSA 密钥与签名（支付宝 RSA2 与微信支付 APIv3 共用）：
// 私钥支持 PKCS8/PKCS1 的 PEM 或纯 Base64；公钥支持证书、PUBLIC KEY PEM、
// 或纯 Base64 的 PKIX/PKCS1。签名统一 SHA256withRSA + PKCS#1 v1.5。

func rsaSignSHA256(key *rsa.PrivateKey, message string) string {
	sum := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func rsaVerifySHA256(pub *rsa.PublicKey, message, sigB64 string) bool {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil {
		return false
	}
	sum := sha256.Sum256([]byte(message))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig) == nil
}

func parseRSAPrivateKey(s string) (*rsa.PrivateKey, error) {
	der, err := pemOrBase64DER(s)
	if err != nil {
		return nil, err
	}
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("不是 RSA 私钥")
	}
	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}
	return nil, errors.New("格式不支持（需要 PKCS8 或 PKCS1）")
}

func parseRSAPublicKey(s string) (*rsa.PublicKey, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "BEGIN") {
		block, _ := pem.Decode([]byte(normalizePEM(s)))
		if block == nil {
			return nil, errors.New("PEM 解析失败")
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, err
			}
			pk, ok := cert.PublicKey.(*rsa.PublicKey)
			if !ok {
				return nil, errors.New("证书不是 RSA 公钥")
			}
			return pk, nil
		}
		return parseRSAPublicDER(block.Bytes)
	}
	der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		return nil, errors.New("Base64 解析失败")
	}
	return parseRSAPublicDER(der)
}

func parseRSAPublicDER(der []byte) (*rsa.PublicKey, error) {
	if k, err := x509.ParsePKIXPublicKey(der); err == nil {
		if rk, ok := k.(*rsa.PublicKey); ok {
			return rk, nil
		}
		return nil, errors.New("不是 RSA 公钥")
	}
	if k, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return k, nil
	}
	return nil, errors.New("公钥格式不支持")
}

func pemOrBase64DER(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "BEGIN") {
		block, _ := pem.Decode([]byte(normalizePEM(s)))
		if block == nil {
			return nil, errors.New("PEM 解析失败")
		}
		return block.Bytes, nil
	}
	der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		return nil, errors.New("Base64 解析失败")
	}
	return der, nil
}

// normalizePEM 允许在 JSON/环境变量里用字面量 \n 存密钥。
func normalizePEM(s string) string {
	return strings.ReplaceAll(s, `\n`, "\n")
}

// 渠道包装：支付宝的密钥格式与通用解析一致（PEM 或纯 Base64 DER）。
// 单独命名是为了某家渠道将来要加证书链/去空格等特殊处理时不动通用解析函数。
func alipayPrivateKey(s string) (*rsa.PrivateKey, error) { return parseRSAPrivateKey(s) }

func alipayPublicKey(s string) (*rsa.PublicKey, error) { return parseRSAPublicKey(s) }
