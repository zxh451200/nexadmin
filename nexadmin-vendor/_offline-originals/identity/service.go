package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const maxSourcesPerTenant = 20

// Service 管理身份源配置。登录相关逻辑在 ldap_auth.go / cas.go。
type Service struct {
	st    store
	log   *zap.Logger
	dial  ldapDialFunc
	audit auditFunc

	cache      cache.Cache
	auth       *auth.Service
	httpClient func(CASConfig) (*http.Client, error) // 测试可替换；默认见 casHTTPClient
}

// NewService 创建服务。
func NewService(st store, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{st: st, log: log, dial: dialReal}
}

// tenantOf 取请求租户。没有租户（或跳过隔离的 ctx）一律拒绝，不回退默认租户。
func tenantOf(ctx context.Context) (idgen.ID, error) {
	id, ok := tenant.From(ctx)
	if !ok || tenant.Skipped(ctx) || id == 0 {
		return 0, response.BadRequest("error.bad_request").WithMsg("tenant is required")
	}
	return id, nil
}

func (s *Service) enabledSources(ctx context.Context, tid idgen.ID, typ string) ([]Source, error) {
	return s.st.listEnabled(ctx, tid, typ)
}

func mapNotFound(err error) error {
	if errors.Is(err, errNotFound) {
		return response.NotFound("error.not_found")
	}
	return err
}

// SourceForm 是创建 / 更新请求体。Config 按 type 解析为 LDAPConfig / CASConfig。
type SourceForm struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Enabled bool            `json:"enabled"`
	Config  json.RawMessage `json:"config"`
}

func (s *Service) List(ctx context.Context) ([]SourceView, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.list(ctx, tid)
	if err != nil {
		return nil, err
	}
	out := make([]SourceView, 0, len(rows))
	for _, r := range rows {
		out = append(out, viewOf(r))
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id idgen.ID) (*SourceView, error) {
	src, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	v := viewOf(*src)
	return &v, nil
}

func (s *Service) load(ctx context.Context, id idgen.ID) (*Source, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	src, err := s.st.get(ctx, tid, id)
	return src, mapNotFound(err)
}

// Save 创建（id=0）或更新身份源。LDAP 绑定口令：新值加密保存；更新时留空表示沿用旧值。
func (s *Service) Save(ctx context.Context, id idgen.ID, f SourceForm) (*SourceView, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	name, err := validDisplayName(f.Name)
	if err != nil {
		return nil, err
	}
	var src *Source
	if id == 0 {
		if n, err := s.st.count(ctx, tid); err != nil {
			return nil, err
		} else if n >= maxSourcesPerTenant {
			return nil, badReq("too many identity sources")
		}
		src = &Source{Type: strings.ToLower(strings.TrimSpace(f.Type))}
		src.TenantID = tid
	} else if src, err = s.load(ctx, id); err != nil {
		return nil, err
	} else if t := strings.ToLower(strings.TrimSpace(f.Type)); t != "" && t != src.Type {
		return nil, badReq("type cannot be changed")
	}
	cfg, err := s.prepareConfig(src, f.Config)
	if err != nil {
		return nil, err
	}
	src.Name, src.Enabled, src.Config = name, f.Enabled, cfg
	if id == 0 {
		err = s.st.create(ctx, src)
	} else {
		err = s.st.save(ctx, src)
	}
	if err != nil {
		return nil, err
	}
	v := viewOf(*src)
	return &v, nil
}

// prepareConfig 校验配置并返回要存库的 JSON（口令已加密）。
func (s *Service) prepareConfig(src *Source, raw json.RawMessage) (string, error) {
	switch src.Type {
	case TypeLDAP:
		var c LDAPConfig
		if err := json.Unmarshal(raw, &c); err != nil {
			return "", badReq("invalid config")
		}
		pw := c.BindPassword
		if err := c.normalize(); err != nil {
			return "", err
		}
		switch {
		case c.BindDN == "":
			c.BindPassword = "" // 匿名搜索，不留口令
		case pw != "":
			if len(pw) > maxLoginPass {
				return "", badReq("bind password too long")
			}
			enc, err := secret.Encrypt(pw) // 未初始化主密钥时失败：宁可拒绝保存，也不落明文
			if err != nil {
				return "", response.Internal().Wrap(err)
			}
			c.BindPassword = enc
		default:
			old, _ := src.ldapConfigOrZero()
			if old.BindPassword == "" {
				return "", badReq("bind password is required when bindDn is set")
			}
			c.BindPassword = old.BindPassword
		}
		b, _ := json.Marshal(c)
		return string(b), nil
	case TypeCAS:
		var c CASConfig
		if err := json.Unmarshal(raw, &c); err != nil {
			return "", badReq("invalid config")
		}
		if err := c.normalize(); err != nil {
			return "", err
		}
		b, _ := json.Marshal(c)
		return string(b), nil
	}
	return "", badReq("type must be ldap or cas")
}

func (s *Source) ldapConfigOrZero() (LDAPConfig, bool) {
	if s.Config == "" {
		return LDAPConfig{}, false
	}
	c, err := s.ldapConfig()
	return c, err == nil
}

func (s *Service) SetEnabled(ctx context.Context, id idgen.ID, enabled bool) (*SourceView, error) {
	src, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	src.Enabled = enabled
	if err := s.st.save(ctx, src); err != nil {
		return nil, err
	}
	v := viewOf(*src)
	return &v, nil
}

func (s *Service) Delete(ctx context.Context, id idgen.ID) error {
	src, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	return s.st.remove(ctx, src.TenantID, src.ID)
}

// viewOf 生成对外视图：口令（含密文）永不出现。配置损坏时 Config 为 nil。
func viewOf(src Source) SourceView {
	v := SourceView{ID: src.ID, Type: src.Type, Name: src.Name, Enabled: src.Enabled, CreatedAt: src.CreatedAt, UpdatedAt: src.UpdatedAt}
	switch src.Type {
	case TypeLDAP:
		if c, err := src.ldapConfig(); err == nil {
			c.HasBindPassword = c.BindPassword != ""
			c.BindPassword = ""
			v.Config = c
		}
	case TypeCAS:
		if c, err := src.casConfig(); err == nil {
			v.Config = c
			v.EntryURL = c.ServiceBaseURL + casLoginPath + "?source=" + src.ID.String()
			v.LogoutURL = c.ServiceBaseURL + casLogoutPath + "?source=" + src.ID.String()
		}
	}
	return v
}
