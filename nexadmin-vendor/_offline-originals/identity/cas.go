package identity

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
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
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

type casFailXML struct {
	Code string `xml:"code,attr"`
}

// casResult 是校验成功后的主体与属性（属性名小写）。
type casResult struct {
	User  string
	Attrs map[string][]string
}

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
				res.Attrs[k] = append(res.Attrs[k], strings.TrimSpace(a.Value))
			}
		}
		return res, nil
	case r.Failure != nil && r.Success == nil:
		return nil, &casFailure{Code: clip(r.Failure.Code, 64)}
	}
	return nil, errors.New("cas: response has neither success nor failure")
}

// identity 把 CAS 主体映射成 auth.ExternalIdentity；账号名不合规直接拒绝。
func (r *casResult) identity(cfg CASConfig) (*auth.ExternalIdentity, error) {
	attr := func(name string) string {
		if name == "" {
			return ""
		}
		if v := r.Attrs[strings.ToLower(name)]; len(v) > 0 {
			return v[0]
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
	return &auth.ExternalIdentity{
		Subject: name, Username: name,
		Nickname: clip(attr(cfg.AttrNickname), 64), Email: clip(attr(cfg.AttrEmail), 100), Phone: clip(attr(cfg.AttrPhone), 32),
		MatchExisting: cfg.MatchExisting, AutoCreate: cfg.AutoCreate, DefaultRole: cfg.DefaultRole,
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
	return &http.Client{
		Timeout:       cfg.timeout(),
		Transport:     &http.Transport{TLSClientConfig: tcfg, Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: cfg.timeout()},
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
		return nil, fmt.Errorf("cas: server returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCASBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxCASBody {
		return nil, errors.New("cas: response too large")
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
		return &TestResult{Stage: "connect", Message: err.Error()}
	}
	if _, err := parseCASResponse(body); err != nil {
		var cf *casFailure
		if !errors.As(err, &cf) {
			return &TestResult{Stage: "validate", Message: err.Error()}
		}
	}
	return &TestResult{OK: true}
}
