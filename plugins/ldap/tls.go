package ldap

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
	"time"
)

// tlsConfig 构造 LDAPS/StartTLS 配置：配置了 CA 时只信任该 CA（内网自签），否则用系统根证书。
func tlsConfig(caPEM, serverName string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	if strings.TrimSpace(caPEM) == "" {
		return cfg, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, errors.New("ldap: CA 证书无法解析（需要 PEM 格式）")
	}
	cfg.RootCAs = pool
	return cfg, nil
}

// validCACert 在保存配置时提前校验，避免到登录时才失败。
func validCACert(caPEM string) bool {
	pool := x509.NewCertPool()
	return pool.AppendCertsFromPEM([]byte(caPEM))
}

// 配置项默认值。
const (
	DefaultPort     = 389
	DefaultTimeout  = 5000
	DefaultFilter   = "(uid=%s)"
	DefaultAttrUser = "uid"
	DefaultAttrMail = "mail"
	DefaultAttrCN   = "cn"
)

// dialTimeout 返回毫秒配置对应的 time.Duration(<=0 用 5s)。
func dialTimeout(ms int) time.Duration {
	if ms <= 0 {
		return 5 * time.Second
	}
	return time.Duration(ms) * time.Millisecond
}
