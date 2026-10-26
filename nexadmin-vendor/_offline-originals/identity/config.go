package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"nexadmin/core/response"
	"nexadmin/core/secret"
)

var (
	attrNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
	// 与用户模块的用户名规则一致：2-64 位字母数字 _ . @ -。外部目录给出的账号名不合规就拒绝，不做猜测式转换。
	localUsernameRe = regexp.MustCompile(`^[A-Za-z0-9_.@-]{2,64}$`)
)

const (
	defaultTimeout = 10
	maxTimeout     = 30
	maxCACertBytes = 16 << 10
)

// LDAPConfig 是 LDAP 身份源配置。BindPassword 在库里是 enc:v1: 密文；对外永远为空，只给 HasBindPassword。
type LDAPConfig struct {
	URL             string `json:"url"`
	StartTLS        bool   `json:"startTls"`
	CACert          string `json:"caCert,omitempty"` // 可选 PEM；配置后只信任该 CA（自签 / 私有 CA）。没有“跳过校验”选项
	BindDN          string `json:"bindDn,omitempty"`
	BindPassword    string `json:"bindPassword,omitempty"`
	HasBindPassword bool   `json:"hasBindPassword"`
	BaseDN          string `json:"baseDn"`
	UserFilter      string `json:"userFilter"` // 含 {username} 的 RFC 4515 过滤器模板
	AttrUsername    string `json:"attrUsername"`
	AttrNickname    string `json:"attrNickname,omitempty"`
	AttrEmail       string `json:"attrEmail,omitempty"`
	AttrPhone       string `json:"attrPhone,omitempty"`
	TimeoutSec      int    `json:"timeoutSec"`
	MatchExisting   bool   `json:"matchExisting"`
	AutoCreate      bool   `json:"autoCreate"`
	DefaultRole     string `json:"defaultRole,omitempty"`
}

func (c LDAPConfig) timeout() time.Duration {
	t := c.TimeoutSec
	if t <= 0 {
		t = defaultTimeout
	}
	return time.Duration(min(t, maxTimeout)) * time.Second
}

func badReq(msg string) error { return response.BadRequest("error.bad_request").WithMsg(msg) }

func validRole(r string) bool {
	return len(r) <= 64 && !strings.ContainsFunc(r, func(c rune) bool { return c < 0x20 || c == 0x7f })
}

func normalizeCommon(timeout *int, caCert, role *string, attrs ...*string) error {
	if *timeout <= 0 {
		*timeout = defaultTimeout
	}
	if *timeout > maxTimeout {
		return badReq(fmt.Sprintf("timeoutSec must be 1-%d", maxTimeout))
	}
	*caCert = strings.TrimSpace(*caCert)
	if len(*caCert) > maxCACertBytes {
		return badReq("caCert too long")
	}
	if *caCert != "" {
		if _, err := newTLSConfig("x", *caCert); err != nil {
			return badReq(err.Error())
		}
	}
	*role = strings.TrimSpace(*role)
	if !validRole(*role) {
		return badReq("invalid defaultRole")
	}
	for _, a := range attrs {
		*a = strings.TrimSpace(*a)
		if *a != "" && !attrNameRe.MatchString(*a) {
			return badReq("invalid attribute name")
		}
	}
	return nil
}

// normalize 校验并规整 LDAP 配置（不处理口令，见 Service.prepareLDAP）。
func (c *LDAPConfig) normalize() error {
	c.URL = strings.TrimSpace(c.URL)
	scheme, host, port, err := parseLDAPURL(c.URL)
	if err != nil {
		return badReq(err.Error())
	}
	c.URL = scheme + "://" + net.JoinHostPort(host, port)
	if scheme == "ldaps" {
		c.StartTLS = false
	}
	c.BindDN, c.BaseDN = strings.TrimSpace(c.BindDN), strings.TrimSpace(c.BaseDN)
	if c.BaseDN == "" || len(c.BaseDN) > 500 || len(c.BindDN) > 500 {
		return badReq("baseDn is required (max 500 characters)")
	}
	c.UserFilter = strings.TrimSpace(c.UserFilter)
	if !strings.Contains(c.UserFilter, userPlaceholder) || len(c.UserFilter) > 500 {
		return badReq("userFilter must contain " + userPlaceholder)
	}
	if _, err := compileFilter(buildUserFilter(c.UserFilter, "probe")); err != nil {
		return badReq("userFilter is not a valid LDAP filter")
	}
	if strings.TrimSpace(c.AttrUsername) == "" {
		c.AttrUsername = "uid"
	}
	if err := normalizeCommon(&c.TimeoutSec, &c.CACert, &c.DefaultRole, &c.AttrUsername, &c.AttrNickname, &c.AttrEmail, &c.AttrPhone); err != nil {
		return err
	}
	c.HasBindPassword = false
	return nil
}

// attrs 返回要向目录请求的属性列表（去重、去空）。
func (c LDAPConfig) attrs() []string {
	seen, out := map[string]bool{}, []string{}
	for _, a := range []string{c.AttrUsername, c.AttrNickname, c.AttrEmail, c.AttrPhone} {
		if k := strings.ToLower(a); a != "" && !seen[k] {
			seen[k] = true
			out = append(out, a)
		}
	}
	return out
}

// CASConfig 是 CAS 身份源配置。ServiceBaseURL 是本系统对外的可信根地址（服务端配置，绝不取自请求头）。
type CASConfig struct {
	ServerURL      string `json:"serverUrl"`      // 如 https://cas.example.com/cas
	Version        string `json:"version"`        // "2" -> /serviceValidate；"3" -> /p3/serviceValidate
	ServiceBaseURL string `json:"serviceBaseUrl"` // 如 https://admin.example.com
	CACert         string `json:"caCert,omitempty"`
	AttrUsername   string `json:"attrUsername,omitempty"` // 为空取 cas:user
	AttrNickname   string `json:"attrNickname,omitempty"`
	AttrEmail      string `json:"attrEmail,omitempty"`
	AttrPhone      string `json:"attrPhone,omitempty"`
	TimeoutSec     int    `json:"timeoutSec"`
	MatchExisting  bool   `json:"matchExisting"`
	AutoCreate     bool   `json:"autoCreate"`
	DefaultRole    string `json:"defaultRole,omitempty"`
}

// parseHTTPBase 校验 http(s) 基地址：不含用户信息 / 查询 / 片段，端口合法；去掉末尾斜杠。
func parseHTTPBase(raw, what string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, badReq(what + " must be an http(s) URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(u.Host, " \t\r\n") {
		return nil, badReq(what + " must not contain credentials, query or fragment")
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return nil, badReq(what + " has an invalid port")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

func (c *CASConfig) normalize() error {
	su, err := parseHTTPBase(c.ServerURL, "serverUrl")
	if err != nil {
		return err
	}
	bu, err := parseHTTPBase(c.ServiceBaseURL, "serviceBaseUrl")
	if err != nil {
		return err
	}
	if bu.Path != "" {
		return badReq("serviceBaseUrl must not have a path")
	}
	c.ServerURL, c.ServiceBaseURL = su.String(), bu.String()
	switch strings.TrimSpace(c.Version) {
	case "", "3":
		c.Version = "3"
	case "2":
		c.Version = "2"
	default:
		return badReq("version must be 2 or 3")
	}
	return normalizeCommon(&c.TimeoutSec, &c.CACert, &c.DefaultRole, &c.AttrUsername, &c.AttrNickname, &c.AttrEmail, &c.AttrPhone)
}

func (c CASConfig) timeout() time.Duration {
	return LDAPConfig{TimeoutSec: c.TimeoutSec}.timeout()
}

// 解析存库的配置 JSON。
func (s *Source) ldapConfig() (LDAPConfig, error) {
	var c LDAPConfig
	if s.Type != TypeLDAP {
		return c, errors.New("identity: not an LDAP source")
	}
	return c, json.Unmarshal([]byte(s.Config), &c)
}

func (s *Source) casConfig() (CASConfig, error) {
	var c CASConfig
	if s.Type != TypeCAS {
		return c, errors.New("identity: not a CAS source")
	}
	return c, json.Unmarshal([]byte(s.Config), &c)
}

// plainBindPassword 解密存库的绑定口令。密文无效时返回错误（不回退成明文）。
func plainBindPassword(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !secret.IsEncrypted(stored) {
		return "", errors.New("identity: stored bind password is not encrypted")
	}
	return secret.Decrypt(stored)
}

func validDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", badReq("name must be 1-100 characters")
	}
	return name, nil
}
