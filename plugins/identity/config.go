package identity

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"nexadmin/core/auth"
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
	DirectorySync      *DirectorySyncConfig `json:"directorySync,omitempty"`
	URL                string               `json:"url"`
	StartTLS           bool                 `json:"startTls"`
	AllowPlaintext     bool                 `json:"allowPlaintext,omitempty"` // 管理员显式确认：ldap:// 且不用 StartTLS 时口令明文传输。默认 false
	CACert             string               `json:"caCert,omitempty"`         // 可选 PEM；配置后只信任该 CA（自签 / 私有 CA）。没有“跳过校验”选项
	BindDN             string               `json:"bindDn,omitempty"`
	BindPassword       string               `json:"bindPassword,omitempty"`
	HasBindPassword    bool                 `json:"hasBindPassword"`
	BaseDN             string               `json:"baseDn"`
	UserFilter         string               `json:"userFilter"` // 含 {username} 的 RFC 4515 过滤器模板
	AttrUsername       string               `json:"attrUsername"`
	AttrNickname       string               `json:"attrNickname,omitempty"`
	AttrEmail          string               `json:"attrEmail,omitempty"`
	AttrPhone          string               `json:"attrPhone,omitempty"`
	AttrGroups         string               `json:"attrGroups"`
	GroupRoleMappings  []GroupRoleMapping   `json:"groupRoleMappings"`
	RequireMappedGroup bool                 `json:"requireMappedGroup"`
	TimeoutSec         int                  `json:"timeoutSec"`
	MatchExisting      bool                 `json:"matchExisting"`
	AutoCreate         bool                 `json:"autoCreate"`
	DefaultRole        string               `json:"defaultRole,omitempty"`
}

type DirectorySyncConfig struct {
	Enabled     bool   `json:"enabled"`
	Filter      string `json:"filter"`
	SubjectAttr string `json:"subjectAttr"`
	// DeactivateMissing 开启后，同步会停用“由该目录绑定过、但这次已不在目录里”的本地账号（默认关）。
	// 只动由该目录绑定过的账号，不碰本地账号；目录返回数不到已绑定数一半时整体中止，防止目录故障 / 过滤器写错造成误停。
	DeactivateMissing bool `json:"deactivateMissing,omitempty"`
}

func (c LDAPConfig) scopedDirectory() bool {
	return c.DirectorySync != nil && c.DirectorySync.Enabled
}

func (c LDAPConfig) validateDirectory() error {
	if !c.scopedDirectory() {
		return nil
	}
	d := c.DirectorySync
	if d.SubjectAttr != "entryUUID" && d.SubjectAttr != "objectGUID" {
		return badReq("identity.subject_attr_invalid")
	}
	if d.Filter == "" || len(d.Filter) > 500 || strings.Contains(d.Filter, userPlaceholder) {
		return badReq("identity.directory_filter_invalid")
	}
	if _, err := compileFilter(d.Filter); err != nil {
		return badReq("identity.directory_filter_broken")
	}
	return nil
}

// cleartext 报告该配置是否会明文传输口令。
func (c LDAPConfig) cleartext() bool {
	u, err := url.Parse(c.URL)
	return err == nil && strings.EqualFold(u.Scheme, "ldap") && !c.StartTLS
}

func (c LDAPConfig) timeout() time.Duration {
	t := c.TimeoutSec
	if t <= 0 {
		t = defaultTimeout
	}
	return time.Duration(min(t, maxTimeout)) * time.Second
}

func badReq(key string, args ...any) error { return response.BadRequest(key, args...) }

func validRole(r string) bool {
	return len(r) <= 64 && utf8.ValidString(r) && !strings.EqualFold(r, "super") && !strings.ContainsFunc(r, func(c rune) bool { return c < 0x20 || c == 0x7f })
}

// GroupRoleMapping 仅自动建号时授予角色；匹配时替代 defaultRole，无匹配则回退。
// 组值逐字节精确匹配（含大小写、空格）；不解析 DN、不提取 CN、不展开嵌套组。
// 等价但写法不同的 DN 不匹配，必须填写目录实际返回的完整值。
type GroupRoleMapping struct {
	ExternalGroup string   `json:"externalGroup"`
	RoleCodes     []string `json:"roleCodes"`
}

func validateGroupMappings(ms []GroupRoleMapping, required bool) error {
	if len(ms) > 100 || (required && len(ms) == 0) {
		return badReq("identity.group_mappings_too_many")
	}
	groups, total := map[string]bool{}, 0
	for _, m := range ms {
		if strings.TrimSpace(m.ExternalGroup) == "" || len(m.ExternalGroup) > 2048 || !utf8.ValidString(m.ExternalGroup) ||
			strings.ContainsFunc(m.ExternalGroup, func(r rune) bool { return r < 0x20 || r == 0x7f }) || groups[m.ExternalGroup] {
			return badReq("identity.external_group_invalid")
		}
		groups[m.ExternalGroup] = true
		if len(m.RoleCodes) == 0 || len(m.RoleCodes) > 100 {
			return badReq("identity.role_codes_count_invalid")
		}
		seen := map[string]bool{}
		for _, code := range m.RoleCodes {
			if code == "" || code != strings.TrimSpace(code) || !validRole(code) || seen[code] {
				return badReq("identity.role_codes_invalid")
			}
			seen[code] = true
		}
		total += len(m.RoleCodes)
	}
	if total > 1000 {
		return badReq("identity.role_codes_too_many")
	}
	return nil
}

// mappedGroupRoles 运行期再次校验配置；多值属性逐项匹配，不按逗号拆分 DN。
func mappedGroupRoles(groups []string, ms []GroupRoleMapping, required bool) ([]string, error) {
	if err := validateGroupMappings(ms, required); err != nil {
		return nil, err
	}
	set, seen := map[string]bool{}, map[string]bool{}
	for _, g := range groups {
		set[g] = true
	}
	var out []string
	for _, m := range ms {
		if set[m.ExternalGroup] {
			for _, code := range m.RoleCodes {
				if !seen[code] {
					out = append(out, code)
					seen[code] = true
				}
			}
		}
	}
	if required && len(out) == 0 {
		return nil, auth.ErrExternalRejected
	}
	return out, nil
}

func normalizeCommon(timeout *int, caCert, role *string, attrs ...*string) error {
	if *timeout <= 0 {
		*timeout = defaultTimeout
	}
	if *timeout > maxTimeout {
		return badReq("identity.timeout_invalid", maxTimeout)
	}
	*caCert = strings.TrimSpace(*caCert)
	if len(*caCert) > maxCACertBytes {
		return badReq("identity.ca_cert_too_long")
	}
	if *caCert != "" {
		if _, err := newTLSConfig("x", *caCert); err != nil {
			return response.BadRequest("identity.ca_cert_invalid").Wrap(err)
		}
	}
	*role = strings.TrimSpace(*role)
	if !validRole(*role) {
		return badReq("identity.default_role_invalid")
	}
	for _, a := range attrs {
		*a = strings.TrimSpace(*a)
		if *a != "" && !attrNameRe.MatchString(*a) {
			return badReq("identity.attr_name_invalid")
		}
	}
	return nil
}

// normalize 校验并规整 LDAP 配置（不处理口令，见 Service.prepareLDAP）。
func (c *LDAPConfig) normalize() error {
	c.URL = strings.TrimSpace(c.URL)
	scheme, host, port, err := parseLDAPURL(c.URL)
	if err != nil {
		return response.BadRequest("identity.ldap_url_invalid").Wrap(err)
	}
	if err := checkConfiguredHost(host); err != nil {
		return err
	}
	c.URL = scheme + "://" + net.JoinHostPort(host, port)
	if scheme == "ldaps" {
		c.StartTLS = false
	}
	if scheme == "ldaps" || c.StartTLS {
		c.AllowPlaintext = false
	} else if !c.AllowPlaintext {
		return badReq("identity.plaintext_forbidden")
	}
	c.BindDN, c.BaseDN = strings.TrimSpace(c.BindDN), strings.TrimSpace(c.BaseDN)
	if c.BaseDN == "" || len(c.BaseDN) > 500 || len(c.BindDN) > 500 {
		return badReq("identity.basedn_required")
	}
	c.UserFilter = strings.TrimSpace(c.UserFilter)
	if !strings.Contains(c.UserFilter, userPlaceholder) || len(c.UserFilter) > 500 {
		return badReq("identity.user_filter_placeholder", userPlaceholder)
	}
	if _, err := compileFilter(buildUserFilter(c.UserFilter, "probe")); err != nil {
		return badReq("identity.user_filter_invalid")
	}
	if strings.TrimSpace(c.AttrUsername) == "" {
		c.AttrUsername = "uid"
	}
	if strings.TrimSpace(c.AttrGroups) == "" {
		c.AttrGroups = "memberOf"
	}
	if err := normalizeCommon(&c.TimeoutSec, &c.CACert, &c.DefaultRole, &c.AttrUsername, &c.AttrNickname, &c.AttrEmail, &c.AttrPhone, &c.AttrGroups); err != nil {
		return err
	}
	if err := validateGroupMappings(c.GroupRoleMappings, c.RequireMappedGroup); err != nil {
		return err
	}
	c.HasBindPassword = false
	return c.validateDirectory()
}

// attrs 返回要向目录请求的属性列表（去重、去空）。
func (c LDAPConfig) attrs() []string {
	seen, out := map[string]bool{}, []string{}
	if c.scopedDirectory() {
		a := c.DirectorySync.SubjectAttr
		seen[strings.ToLower(a)] = true
		out = append(out, a)
	}
	for _, a := range []string{c.AttrUsername, c.AttrNickname, c.AttrEmail, c.AttrPhone} {
		if k := strings.ToLower(a); a != "" && !seen[k] {
			seen[k] = true
			out = append(out, a)
		}
	}
	if len(c.GroupRoleMappings) > 0 {
		a := c.AttrGroups
		if a == "" {
			a = "memberOf"
		}
		if !seen[strings.ToLower(a)] {
			out = append(out, a)
		}
	}
	return out
}

// CASConfig 是 CAS 身份源配置。ServiceBaseURL 是本系统对外的可信根地址（服务端配置，绝不取自请求头）。
type CASConfig struct {
	ServerURL          string             `json:"serverUrl"`      // 如 https://cas.example.com/cas
	Version            string             `json:"version"`        // "2" -> /serviceValidate；"3" -> /p3/serviceValidate
	ServiceBaseURL     string             `json:"serviceBaseUrl"` // 如 https://admin.example.com
	CACert             string             `json:"caCert,omitempty"`
	AttrUsername       string             `json:"attrUsername,omitempty"` // 为空取 cas:user
	AttrNickname       string             `json:"attrNickname,omitempty"`
	AttrEmail          string             `json:"attrEmail,omitempty"`
	AttrPhone          string             `json:"attrPhone,omitempty"`
	AttrGroups         string             `json:"attrGroups"`
	GroupRoleMappings  []GroupRoleMapping `json:"groupRoleMappings"`
	RequireMappedGroup bool               `json:"requireMappedGroup"`
	TimeoutSec         int                `json:"timeoutSec"`
	MatchExisting      bool               `json:"matchExisting"`
	AutoCreate         bool               `json:"autoCreate"`
	DefaultRole        string             `json:"defaultRole,omitempty"`
}

// parseHTTPBase 校验 http(s) 基地址：不含用户信息 / 查询 / 片段，端口合法；去掉末尾斜杠。
func parseHTTPBase(raw, what string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, badReq("identity.url_scheme_invalid", what)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(u.Host, " \t\r\n") {
		return nil, badReq("identity.url_parts_invalid", what)
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return nil, badReq("identity.url_port_invalid", what)
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
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
		return badReq("identity.service_base_path_invalid")
	}
	// 票据校验响应决定“谁登录了”，明文 HTTP 下网络攻击者可伪造任意身份：只允许 https（本机回环地址仅用于开发联调）。
	if su.Scheme != "https" && !isLoopbackHost(su.Hostname()) {
		return badReq("identity.server_url_https_required")
	}
	if err := checkConfiguredHost(su.Hostname()); err != nil {
		return err
	}
	c.ServerURL, c.ServiceBaseURL = su.String(), bu.String()
	switch strings.TrimSpace(c.Version) {
	case "", "3":
		c.Version = "3"
	case "2":
		c.Version = "2"
	default:
		return badReq("identity.cas_version_invalid")
	}
	if strings.TrimSpace(c.AttrGroups) == "" {
		c.AttrGroups = "groups"
	}
	if err := normalizeCommon(&c.TimeoutSec, &c.CACert, &c.DefaultRole, &c.AttrUsername, &c.AttrNickname, &c.AttrEmail, &c.AttrPhone, &c.AttrGroups); err != nil {
		return err
	}
	return validateGroupMappings(c.GroupRoleMappings, c.RequireMappedGroup)
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
		return "", badReq("identity.name_invalid")
	}
	return name, nil
}
