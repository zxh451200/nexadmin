package saml

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/russellhaering/goxmldsig"
)

const maxSPKeyBytes = 64 << 10

// parseSigner 解析 PEM 私钥（PKCS#8 / PKCS#1 / SEC1），三种格式都见过。
func parseSigner(pemText string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemText)))
	if block == nil {
		return nil, errors.New("saml: 私钥不是 PEM 格式")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if s, ok := k.(crypto.Signer); ok {
			return s, nil
		}
		return nil, errors.New("saml: 私钥类型不支持")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, errors.New("saml: 私钥解析失败（支持 PKCS#8 / PKCS#1 / SEC1）")
}

func parseSingleCert(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(certPEM)))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("saml: 证书不是 PEM 格式")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.New("saml: 证书解析失败: " + err.Error())
	}
	return cert, nil
}

// checkSPKeyStrength 挡掉明显偏弱的密钥：RSA < 2048 / EC < P-256。
func checkSPKeyStrength(s crypto.Signer) error {
	switch k := s.(type) {
	case *rsa.PrivateKey:
		if k.N.BitLen() < 2048 {
			return errors.New("saml: RSA 私钥至少 2048 位")
		}
	case *ecdsa.PrivateKey:
		if k.Curve.Params().BitSize < 256 {
			return errors.New("saml: EC 私钥至少 256 位")
		}
	}
	return nil
}

// buildImportedSPKeyPair 校验管理员导入的证书 / 私钥并归一化：证书重新编码、私钥统一成
// PKCS#8 后加密。校验项：PEM 格式、证书未过期、密钥强度、公私钥匹配。
func buildImportedSPKeyPair(certPEM, keyPEM string) (certOut, keyEnc string, err error) {
	if len(certPEM) > maxSPKeyBytes || len(keyPEM) > maxSPKeyBytes {
		return "", "", errors.New("saml: 证书或私钥超过 64 KiB")
	}
	cert, err := parseSingleCert(certPEM)
	if err != nil {
		return "", "", err
	}
	if time.Now().After(cert.NotAfter) {
		return "", "", errors.New("saml: 证书已过期")
	}
	signer, err := parseSigner(keyPEM)
	if err != nil {
		return "", "", err
	}
	if err := checkSPKeyStrength(signer); err != nil {
		return "", "", err
	}
	pub, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return "", "", errors.New("saml: 读取私钥公钥失败")
	}
	if !bytes.Equal(pub, cert.RawSubjectPublicKeyInfo) {
		return "", "", errors.New("saml: 私钥与证书不匹配")
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(signer)
	if err != nil {
		return "", "", errors.New("saml: 私钥编码失败")
	}
	keyEnc, err = encryptSecret(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})))
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})), keyEnc, nil
}

// ImportSPKey 用管理员提供的证书 / 私钥替换当前 SP 密钥对；原密钥对进入 prev，
// 在 spPrevGrace 内继续参与解密（IdP 可能还在按旧证书加密断言），已发布未启用的 next 清掉。
func (s *Service) ImportSPKey(ctx context.Context, id, certPEM, keyPEM string) (*Source, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	certOut, keyEnc, err := buildImportedSPKeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"sp_cert":         certOut,
		"sp_key_enc":      keyEnc,
		"next_sp_cert":    "",
		"next_sp_key_enc": "",
	}
	if strings.TrimSpace(m.SPCert) != "" {
		updates["prev_sp_cert"] = m.SPCert
		updates["prev_sp_key_enc"] = m.SPKeyEnc
		updates["prev_sp_expires_at"] = time.Now().UTC().Add(spPrevGrace)
	}
	if err := s.db.WithContext(ctx).Model(&Source{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// signMetadataXML 用 SP 私钥对已序列化的元数据做 enveloped XML 签名（RSA-SHA256）。
func signMetadataXML(raw []byte, certPEM, keyPEM string) ([]byte, error) {
	cert, err := parseSingleCert(certPEM)
	if err != nil {
		return nil, err
	}
	signer, err := parseSigner(keyPEM)
	if err != nil {
		return nil, err
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		return nil, errors.New("saml: 元数据 XML 解析失败: " + err.Error())
	}
	sc := dsig.NewDefaultSigningContext(dsig.TLSCertKeyStore(tls.Certificate{
		Certificate: [][]byte{cert.Raw},
		PrivateKey:  signer,
	}))
	// 用 exclusive C14N 1.0（SAML 生态的通行口径）：默认的 C14N11 会和按 exclusive
	// 规范化 SignedInfo 的验签方不匹配，实测表现为 crypto/rsa: verification error。
	sc.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	signed, err := sc.SignEnveloped(doc.Root())
	if err != nil {
		return nil, errors.New("saml: 元数据签名失败: " + err.Error())
	}
	out := etree.NewDocument()
	out.SetRoot(signed)
	return out.WriteToBytes()
}
