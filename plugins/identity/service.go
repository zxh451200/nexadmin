package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const maxSourcesPerTenant = 20

func casSessionScope(source idgen.ID) string {
	return "identity:cas:" + source.String()
}

// Service 管理身份源配置。登录相关逻辑在 ldap_auth.go / cas.go。
type Service struct {
	st    store
	log   *zap.Logger
	dial  ldapDialFunc
	audit auditFunc

	cache      cache.Cache
	auth       *auth.Service
	httpClient func(CASConfig) (*http.Client, error) // 测试可替换；默认见 casHTTPClient

	testMu          sync.Mutex
	testHits        map[string][]time.Time // 无 cache 时的进程内回退：租户+用户 -> “测试连接 / 测试登录”的调用时间
	testLimiter     *middleware.Limiter
	testLimiterOnce sync.Once
	now             func() time.Time
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
		return 0, response.BadRequest("tenant.context_required")
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
			return nil, badReq("identity.source_limit")
		}
		src = &Source{Type: strings.ToLower(strings.TrimSpace(f.Type))}
		src.TenantID = tid
	} else if src, err = s.load(ctx, id); err != nil {
		return nil, err
	} else if t := strings.ToLower(strings.TrimSpace(f.Type)); t != "" && t != src.Type {
		return nil, badReq("identity.type_immutable")
	}
	cfg, err := s.prepareConfig(ctx, src, f.Config)
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
func (s *Service) prepareConfig(ctx context.Context, src *Source, raw json.RawMessage) (string, error) {
	switch src.Type {
	case TypeLDAP:
		var c LDAPConfig
		if err := json.Unmarshal(raw, &c); err != nil {
			return "", badReq("identity.config_invalid")
		}
		pw := c.BindPassword
		if err := c.normalize(); err != nil {
			return "", err
		}
		if err := requireMatchPerm(ctx, c.MatchExisting); err != nil {
			return "", err
		}
		switch {
		case c.BindDN == "":
			c.BindPassword = "" // 匿名搜索，不留口令
		case pw != "":
			if len(pw) > maxLoginPass {
				return "", badReq("identity.bind_password_too_long")
			}
			enc, err := secret.Encrypt(pw) // 未初始化主密钥时失败：宁可拒绝保存，也不落明文
			if err != nil {
				return "", response.Internal().Wrap(err)
			}
			c.BindPassword = enc
		default:
			old, _ := src.ldapConfigOrZero()
			if old.BindPassword == "" {
				return "", badReq("identity.bind_password_required")
			}
			// 沿用旧口令只在“口令的去向”不变时成立：改了地址 / 服务账号 / 信任的 CA / 传输方式，
			// 否则有编辑权限但不知道口令的人可以把旧口令发到自己的服务器。
			if old.URL != c.URL || old.BindDN != c.BindDN || old.CACert != c.CACert || old.StartTLS != c.StartTLS {
				return "", badReq("identity.bind_password_reenter")
			}
			c.BindPassword = old.BindPassword
		}
		b, _ := json.Marshal(c)
		return string(b), nil
	case TypeCAS:
		var c CASConfig
		if err := json.Unmarshal(raw, &c); err != nil {
			return "", badReq("identity.config_invalid")
		}
		if err := c.normalize(); err != nil {
			return "", err
		}
		if err := requireMatchPerm(ctx, c.MatchExisting); err != nil {
			return "", err
		}
		b, _ := json.Marshal(c)
		return string(b), nil
	}
	return "", badReq("identity.type_invalid")
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
	if enabled && sourceMatchesExisting(src) {
		if err := requireMatchPerm(ctx, true); err != nil {
			return nil, err
		}
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

// permMatch 是“允许外部目录按用户名绑定已有本地账号（matchExisting）”的专门权限。
// 只有 identity:edit 的人可以把源指向自己控制的 LDAP / CAS 服务器，若 matchExisting 也归他管，
// 他就能用目录里的同名账号登录进任何非超管本地账号，所以这个开关必须单独授权。
const permMatch = "identity:match"

type matchPermKey struct{}

// withMatchPerm 由 handler 在确认调用者拥有 identity:match 后调用；没有它的 context 一律视为无权。
func withMatchPerm(ctx context.Context) context.Context {
	return context.WithValue(ctx, matchPermKey{}, true)
}

func matchAllowed(ctx context.Context) bool {
	v, _ := ctx.Value(matchPermKey{}).(bool)
	return v
}

// requireMatchPerm：结果配置开启了 matchExisting 时（无论是新开还是沿用），调用者必须拥有 identity:match。
func requireMatchPerm(ctx context.Context, matchExisting bool) error {
	if matchExisting && !matchAllowed(ctx) {
		return response.Forbidden("identity.match_perm_required", permMatch)
	}
	return nil
}

func sourceMatchesExisting(src *Source) bool {
	switch src.Type {
	case TypeLDAP:
		c, err := src.ldapConfig()
		return err != nil || c.MatchExisting // 配置损坏时按最严格处理
	case TypeCAS:
		c, err := src.casConfig()
		return err != nil || c.MatchExisting
	}
	return false
}
