package ldap

import (
	"crypto/tls"
	"time"
)

// tlsConfig 是 LDAPS/StartTLS 的 TLS 配置:P0 走系统根证书;
// 自签名 CA 的场景在 P1 加「信任 CA 上传」配置项。
func tlsConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: false}
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
