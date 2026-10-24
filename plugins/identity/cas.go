package identity

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"nexadmin/core/auth"
	"nexadmin/pkg/idgen"
)

// CAS 入口路径（都在 /api/v1 之下，登录相关的公开接口）。
const (
	casLoginPath    = "/api/v1/plugins/identity/cas/login"
	casCallbackPath = "/api/v1/plugins/identity/cas/callback"
	casLogoutPath   = "/api/v1/plugins/identity/cas/logout"

	casNS        = "http://www.yale.edu/tp/cas"
	maxCASBody   = 1 << 20
	maxTicketLen = 256
	casLoginType = "cas"
	casValidate2 = "/serviceValidate"
	casValidate3 = "/p3/serviceValidate"
)

var ticketRe = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// casServiceURL 生成 CAS 的 service 参数：完全由服务端配置（serviceBaseUrl）+ 来源 ID + 一次性 state 构成，
// 不读取请求的 Host / X-Forwarded-* 等任何请求头。ticket 校验时必须原样再传同一个值。
func casServiceURL(cfg CASConfig, id idgen.ID, state string) string {
	q := url.Values{}
	q.Set("source", id.String())
	q.Set("state", state)
	return cfg.ServiceBaseURL + casCallbackPath + "?" + q.Encode()
}

func casLoginRedirect(cfg CASConfig, service string) string {
	return cfg.ServerURL + "/login?" + url.Values{"service": {service}}.Encode()
}

// casLogoutRedirect 跳转到 CAS 登出地址；service 固定为可信的 serviceBaseUrl。
func casLogoutRedirect(cfg CASConfig) string {
	return cfg.ServerURL + "/logout?" + url.Values{"service": {cfg.ServiceBaseURL + "/"}}.Encode()
}

// ---- 响应解析（XXE 安全） ----

type casServiceResponse struct {
	XMLName xml.Name    `xml:"serviceResponse"`
	Success *casSuccess `xml:"authenticationSuccess"`
	Failure *casFailXML `xml:"authenticationFailure"`
}

type casSuccess struct {
	User       string    `xml:"user"`
	Attributes *casAttrs `xml:"attributes"`
}

type casAttrs struct {
	Items []casAttr `xml:",any"`
}

type casAttr struct {
	XMLName  xml.Name
	Value    string    `xml:",chardata"`
	Children []casAttr `xml:",any"`
}

// values 支持重复属性及一层 <groups><string>...</string></groups> 列表。
// 不递归猜测任意 XML 容器，也不按逗号拆分；组值保留原始文本用于精确匹配。
func (a casAttr) values() ([]string, error) {
	if len(a.Children) == 0 {
		return []string{a.Value}, nil
	}
	if strings.TrimSpace(a.Value) != "" {
		return nil, errors.New("cas: mixed attribute content")
	}
	var values []string
	for _, child := range a.Children {
		if child.XMLName.Local != "string" || len(child.Children) != 0 {
			return nil, errors.New("cas: unsupported attribute list")
		}
		values = append(values, child.Value)
	}
	return values, nil
}

type casFailXML struct {
	Code string `xml:"code,attr"`
}

// casResult 是校验成功后的主体与属性（属性名小写）。
type casResult struct {
	User  string
	Attrs map[string][]string
}

var (
	errCASStatus   = errors.New("cas: unexpected HTTP status")
	errCASResponse = errors.New("cas: invalid response")
)

// casFailure 表示 CAS 服务器明确拒绝了票据（如 INVALID_TICKET）。
type casFailure struct{ Code string }

func (e *casFailure) Error() string { return "cas: ticket rejected: " + e.Code }

// parseCASResponse 解析 serviceValidate 响应。
// 防 XXE：encoding/xml 不会展开外部实体，也不解析 DTD；这里仍显式拒绝任何 DOCTYPE / ENTITY 声明（Directive 记号），
// 未声明的实体引用在严格模式下直接报错，响应体大小由调用方限制。
func parseCASResponse(body []byte) (*casResult, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	d.Strict = true
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cas: malformed XML: %w", err)
		}
		if _, ok := tok.(xml.Directive); ok {
			return nil, errors.New("cas: DTD / entity declarations are not allowed")
		}
	}
	var r casServiceResponse
	d2 := xml.NewDecoder(bytes.NewReader(body))
	d2.Strict = true
	if err := d2.Decode(&r); err != nil {
		return nil, fmt.Errorf("cas: malformed XML: %w", err)
	}
	if r.XMLName.Space != casNS {
		return nil, errors.New("cas: unexpected response namespace")
	}
	switch {
	case r.Success != nil && r.Failure == nil:
		user := strings.TrimSpace(r.Success.User)
		if user == "" || len(user) > 256 || strings.ContainsFunc(user, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
			return nil, errors.New("cas: invalid principal")
		}
		res := &casResult{User: user, Attrs: map[string][]string{}}
		if r.Success.Attributes != nil {
			for _, a := range r.Success.Attributes.Items {
				k := strings.ToLower(a.XMLName.Local)
				values, err := a.values()
				if err != nil {
					return nil, err
				}
				res.Attrs[k] = append(res.Attrs[k], values...)
			}
		}
		return res, nil
	case r.Failure != nil && r.Success == nil:
		return nil, &casFailure{Code: clip(r.Failure.Code, 64)}
	}
	return nil, errors.New("cas: response has neither success nor failure")
}

// identityFor 在 identity 的基础上写入按源隔离的绑定键（cas-<source id>）。
func (r *casResult) identityFor(cfg CASConfig, source idgen.ID) (*auth.ExternalIdentity, error) {
	id, err := r.identity(cfg)
	if err != nil {
		return nil, err
	}
	id.ProviderKey = userProvider(source, TypeCAS)
	return id, nil
}

// identity 把 CAS 主体映射成 auth.ExternalIdentity；账号名不合规直接拒绝。
func (r *casResult) identity(cfg CASConfig) (*auth.ExternalIdentity, error) {
	attr := func(name string) string {
		if name == "" {
			return ""
		}
		if v := r.Attrs[strings.ToLower(name)]; len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}
	name := r.User
	if v := attr(cfg.AttrUsername); v != "" {
		name = v
	}
	if !localUsernameRe.MatchString(name) {
		return nil, errors.New("cas: principal does not fit the local username rule")
	}
	attrGroups := cfg.AttrGroups
	if attrGroups == "" {
		attrGroups = "groups"
	}
	codes, err := mappedGroupRoles(r.Attrs[strings.ToLower(attrGroups)], cfg.GroupRoleMappings, cfg.RequireMappedGroup)
	if err != nil {
		return nil, err
	}
	return &auth.ExternalIdentity{
		Subject: name, Username: name,
		Nickname: clip(attr(cfg.AttrNickname), 64), Email: clip(attr(cfg.AttrEmail), 100), Phone: clip(attr(cfg.AttrPhone), 32),
		MatchExisting: cfg.MatchExisting, AutoCreate: cfg.AutoCreate, DefaultRole: cfg.DefaultRole,
		MappedRoleCodes: codes,
	}, nil
}

// ---- 向 CAS 服务器发请求 ----

func (s *Service) casHTTPClient(cfg CASConfig) (*http.Client, error) {
	u, err := url.Parse(cfg.ServerURL)
	if err != nil {
		return nil, err
	}
	tcfg, err := newTLSConfig(u.Hostname(), cfg.CACert) // 默认完整校验；没有跳过校验的选项
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: cfg.timeout(), Control: ssrfControl} // 对实际 connect 的 IP 再查一次（防 DNS 重绑定）
	return &http.Client{
		Timeout: cfg.timeout(),
		Transport: &http.Transport{
			TLSClientConfig: tcfg, Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: cfg.timeout(),
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				if err := checkDialHost(ctx, host); err != nil {
					return nil, err
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, // 不跟随重定向
	}, nil
}

// casFetch GET serverUrl+path?query，返回最多 maxCASBody 字节的响应体；非 200 视为失败。
func (s *Service) casFetch(ctx context.Context, cfg CASConfig, path string, q url.Values) ([]byte, error) {
	hc := s.httpClient
	if hc == nil {
		hc = s.casHTTPClient
	}
	client, err := hc(cfg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.ServerURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/xml, application/xml")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cas: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", errCASStatus, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCASBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxCASBody {
		return nil, fmt.Errorf("%w: too large", errCASResponse)
	}
	return body, nil
}

func casValidatePath(cfg CASConfig) string {
	if cfg.Version == "2" {
		return casValidate2
	}
	return casValidate3
}

// casValidateTicket 向 CAS 校验 ticket。service 必须与发起登录时的完全一致。
func (s *Service) casValidateTicket(ctx context.Context, cfg CASConfig, service, ticket string) (*casResult, error) {
	if len(ticket) == 0 || len(ticket) > maxTicketLen || !ticketRe.MatchString(ticket) {
		return nil, errors.New("cas: malformed ticket")
	}
	body, err := s.casFetch(ctx, cfg, casValidatePath(cfg), url.Values{"service": {service}, "ticket": {ticket}})
	if err != nil {
		return nil, err
	}
	return parseCASResponse(body)
}

func (s *Service) testCAS(ctx context.Context, cfg CASConfig) *TestResult {
	// 不带 ticket 的请求：正常的 CAS 服务器会返回 INVALID_REQUEST 的 authenticationFailure XML，借此确认地址和协议版本。
	body, err := s.casFetch(ctx, cfg, casValidatePath(cfg), url.Values{"service": {cfg.ServiceBaseURL + "/"}, "ticket": {"ST-connectivity-test"}})
	if err != nil {
		return &TestResult{Stage: "connect", Message: s.safeMessage("connect", err)}
	}
	if _, err := parseCASResponse(body); err != nil {
		var cf *casFailure
		if !errors.As(err, &cf) {
			return &TestResult{Stage: "validate", Message: s.safeMessage("validate", fmt.Errorf("%w: %v", errCASResponse, err))}
		}
	}
	return &TestResult{OK: true}
}
