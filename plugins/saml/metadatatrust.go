package saml

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"github.com/beevik/etree"
	"github.com/russellhaering/goxmldsig"
)

// parseCertPEM 解析一段或一段以上的 PEM 证书（元数据 CA 允许带中间证书链）。
func parseCertPEM(raw string) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	rest := []byte(raw)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("saml: 证书解析失败: %w", err)
		}
		out = append(out, cert)
	}
	if len(out) == 0 {
		return nil, errors.New("saml: 没有找到 PEM 证书")
	}
	return out, nil
}

// tlsRootCAs 把内网 CA 变成信任库；未配置时返回 nil，走系统根证书。
func tlsRootCAs(caPEM string) (*x509.CertPool, error) {
	if strings.TrimSpace(caPEM) == "" {
		return nil, nil
	}
	certs, err := parseCertPEM(caPEM)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	for _, c := range certs {
		pool.AddCert(c)
	}
	return pool, nil
}

// verifyMetadataSignature 校验元数据根元素上的 XML 签名，且签名证书必须在 certPEM 里。
// certPEM 为空表示不做钉扎校验（保持原来只依赖 TLS 的行为）。
func verifyMetadataSignature(raw, certPEM string) error {
	if strings.TrimSpace(certPEM) == "" {
		return nil
	}
	certs, err := parseCertPEM(certPEM)
	if err != nil {
		return err
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromString(raw); err != nil {
		return fmt.Errorf("saml: 元数据 XML 解析失败: %w", err)
	}
	vc := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: certs})
	if _, err := vc.Validate(doc.Root()); err != nil {
		return fmt.Errorf("saml: 元数据签名校验失败（元数据未签名或不是该证书签的）: %w", err)
	}
	return nil
}
