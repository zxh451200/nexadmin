package identity

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 客户端侧的硬上限：单条 LDAP 报文大小、一次搜索接收的条目数。
const (
	maxLDAPMessage = 1 << 20
	maxLDAPEntries = 50
	startTLSOID    = "1.3.6.1.4.1.1466.20037"
)

var (
	errEmptyPassword = errors.New("ldap: empty password rejected")
	errEmptyDN       = errors.New("ldap: empty DN rejected")
)

// ldapError 是服务端返回的非成功结果码。Msg 只放服务端的诊断文本，永远不包含我们发出的口令。
type ldapError struct {
	Code int
	Msg  string
}

func (e *ldapError) Error() string { return fmt.Sprintf("ldap: result code %d: %s", e.Code, e.Msg) }

const (
	ldapSuccess            = 0
	ldapSizeLimitExceeded  = 4
	ldapInvalidCredentials = 49
)

// ldapEntry 是一条搜索结果；Attrs 的键为小写属性名。
type ldapEntry struct {
	DN    string
	Attrs map[string][]string
}

func (e ldapEntry) first(attr string) string {
	if v := e.Attrs[strings.ToLower(attr)]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// parseLDAPURL 只接受 ldap:// 与 ldaps://，不允许用户信息、路径、查询串；返回 scheme、主机、端口。
func parseLDAPURL(raw string) (scheme, host, port string, err error) {
	u, perr := url.Parse(strings.TrimSpace(raw))
	if perr != nil {
		return "", "", "", errors.New("invalid LDAP address")
	}
	scheme = strings.ToLower(u.Scheme)
	if scheme != "ldap" && scheme != "ldaps" {
		return "", "", "", errors.New("LDAP address must start with ldap:// or ldaps://")
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", "", "", errors.New("LDAP address must be scheme://host[:port] only")
	}
	host, port = u.Hostname(), u.Port()
	if host == "" || strings.ContainsAny(host, " \t\r\n") {
		return "", "", "", errors.New("LDAP address has no host")
	}
	if port == "" {
		port = "389"
		if scheme == "ldaps" {
			port = "636"
		}
	}
	if n, perr := strconv.Atoi(port); perr != nil || n < 1 || n > 65535 {
		return "", "", "", errors.New("LDAP port must be 1-65535")
	}
	return scheme, host, port, nil
}

// newTLSConfig 默认完整校验证书链与主机名；配置了 CA 时只信任该 CA（用于自签 / 私有 CA）。没有跳过校验的开关。
func newTLSConfig(host, caPEM string) (*tls.Config, error) {
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if strings.TrimSpace(caPEM) != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(caPEM)) {
			return nil, errors.New("CA certificate is not valid PEM")
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}

type ldapConn struct {
	nc   net.Conn
	br   *bufio.Reader
	id   int
	stop func() bool
}

// dialLDAP 建立连接（ldaps 直接 TLS；ldap + StartTLS 则升级）。整个会话受 timeout 与 ctx 约束。
func dialLDAP(ctx context.Context, cfg LDAPConfig) (*ldapConn, error) {
	scheme, host, port, err := parseLDAPURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	timeout := cfg.timeout()
	d := net.Dialer{Timeout: timeout}
	nc, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, fmt.Errorf("ldap: connect: %w", err)
	}
	_ = nc.SetDeadline(time.Now().Add(timeout))
	c := &ldapConn{nc: nc}
	c.stop = context.AfterFunc(ctx, func() { _ = nc.SetDeadline(time.Now()) })
	fail := func(e error) (*ldapConn, error) { _ = c.Close(); return nil, e }

	tcfg, err := newTLSConfig(host, cfg.CACert)
	if err != nil {
		return fail(err)
	}
	if scheme == "ldaps" {
		tc := tls.Client(nc, tcfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return fail(fmt.Errorf("ldap: TLS handshake: %w", err))
		}
		c.nc = tc
	}
	c.br = bufio.NewReader(c.nc)
	if scheme == "ldap" && cfg.StartTLS {
		if err := c.startTLS(ctx, tcfg); err != nil {
			return fail(err)
		}
	}
	return c, nil
}

func (c *ldapConn) startTLS(ctx context.Context, tcfg *tls.Config) error {
	id := c.nextID()
	if err := c.write(id, enc(tagExtReq, encStr(tagExtName, startTLSOID))); err != nil {
		return err
	}
	op, err := c.readOp(id, tagExtResp)
	if err != nil {
		return err
	}
	if err := resultError(op); err != nil {
		return fmt.Errorf("ldap: StartTLS refused: %w", err)
	}
	tc := tls.Client(c.nc, tcfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("ldap: StartTLS handshake: %w", err)
	}
	c.nc = tc
	c.br = bufio.NewReader(tc)
	return nil
}

// Close 发送 Unbind（尽力而为）并关闭连接。
func (c *ldapConn) Close() error {
	if c.stop != nil {
		c.stop()
	}
	if c.br != nil {
		_ = c.write(c.nextID(), enc(tagUnbindReq))
	}
	return c.nc.Close()
}

func (c *ldapConn) nextID() int { c.id++; return c.id }

func (c *ldapConn) write(id int, op []byte) error {
	_, err := c.nc.Write(enc(tagSeq, encInt(tagInt, id), op))
	return err
}

// readMsg 读下一条报文，返回消息 ID 与协议操作 TLV（标签、值）。
func (c *ldapConn) readMsg() (id int, tag byte, val []byte, err error) {
	raw, err := readTLV(c.br, maxLDAPMessage)
	if err != nil {
		return 0, 0, nil, err
	}
	t, body, _, err := parseTLV(raw)
	if err != nil || t != tagSeq {
		return 0, 0, nil, errBER
	}
	it, iv, rest, err := parseTLV(body)
	if err != nil || it != tagInt {
		return 0, 0, nil, errBER
	}
	if id, err = decodeInt(iv); err != nil {
		return 0, 0, nil, err
	}
	tag, val, _, err = parseTLV(rest)
	return id, tag, val, err
}

// readOp 读取对 id 的响应；遇到其他 ID（含未经请求的通知）按协议错误处理。
func (c *ldapConn) readOp(id int, want byte) ([]byte, error) {
	got, tag, val, err := c.readMsg()
	if err != nil {
		return nil, err
	}
	if got != id || tag != want {
		return nil, fmt.Errorf("ldap: unexpected response (id=%d tag=0x%02x)", got, tag)
	}
	return val, nil
}

// resultError 解析 LDAPResult 的前三个字段；成功返回 nil，否则 *ldapError。
func resultError(op []byte) error {
	code, msg, err := parseResult(op)
	if err != nil {
		return err
	}
	if code != ldapSuccess {
		return &ldapError{Code: code, Msg: msg}
	}
	return nil
}

func parseResult(op []byte) (code int, diag string, err error) {
	t, v, rest, err := parseTLV(op)
	if err != nil || t != tagEnum {
		return 0, "", errBER
	}
	if code, err = decodeInt(v); err != nil {
		return 0, "", err
	}
	if t, _, rest, err = parseTLV(rest); err != nil || t != tagOctet { // matchedDN
		return 0, "", errBER
	}
	t, v, _, err = parseTLV(rest)
	if err != nil || t != tagOctet {
		return 0, "", errBER
	}
	return code, string(v), nil
}
