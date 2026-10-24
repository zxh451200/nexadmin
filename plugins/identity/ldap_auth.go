package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
)

const (
	maxLoginName = 256
	maxLoginPass = 512
)

// ldapSession 是登录流程用到的最小 LDAP 能力，真实实现是 *ldapConn，测试可用替身。
type ldapSession interface {
	Bind(dn, password string) error
	Search(base string, scope int, filter string, attrs []string, sizeLimit int) ([]ldapEntry, error)
	Close() error
}

type ldapDialFunc func(ctx context.Context, cfg LDAPConfig) (ldapSession, error)

func dialReal(ctx context.Context, cfg LDAPConfig) (ldapSession, error) {
	c, err := dialLDAP(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}

var (
	errUserNotFound   = errors.New("identity: user not found or ambiguous")
	errBadCredentials = errors.New("identity: invalid credentials")
)

// ldapAuthenticator 实现 auth.ExternalAuthenticator：按“当前租户”的启用 LDAP 源依次尝试。
type ldapAuthenticator struct {
	svc  *Service
	dial ldapDialFunc
	log  *zap.Logger
}

var _ auth.ExternalAuthenticator = (*ldapAuthenticator)(nil)

func (*ldapAuthenticator) Name() string { return TypeLDAP }

// validLoginInput 拒绝空 / 过长 / 含控制字符的输入。空口令在这里就返回，不会去连目录。
func validLoginInput(username, password string) bool {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" || len(username) > maxLoginName || len(password) > maxLoginPass {
		return false
	}
	return !strings.ContainsFunc(username, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// Authenticate：服务账号搜索用户 DN -> 用用户 DN + 输入口令 bind。
// 租户只取自 ctx（登录请求经租户中间件解析），A 租户的源不会被 B 租户的登录用到。
func (a *ldapAuthenticator) Authenticate(ctx context.Context, username, password string) (*auth.ExternalIdentity, error) {
	if !validLoginInput(username, password) {
		return nil, auth.ErrExternalRejected
	}
	tid, ok := tenant.From(ctx)
	if !ok || tenant.Skipped(ctx) {
		return nil, auth.ErrExternalRejected
	}
	srcs, err := a.svc.enabledSources(ctx, tid, TypeLDAP)
	if err != nil {
		return nil, err
	}
	var infra error
	for i := range srcs {
		cfg, err := srcs[i].ldapConfig()
		if err != nil {
			infra = fmt.Errorf("identity: source %s config: %w", srcs[i].ID, err)
			continue
		}
		u, err := a.verify(ctx, cfg, username, password)
		switch {
		case err == nil:
			id := u.identity(cfg)
			if cfg.scopedDirectory() {
				id.ProviderKey = directoryProvider(srcs[i].ID)
				id.Subject = u.Subject
				id.MatchExisting = false
			} else {
				// 绑定键带 source id：同租户里另一个源的同名用户不会落到这个源绑定的账号上。
				// 升级前的旧绑定（键为 "ldap"）由用户模块在下一次登录时迁移到这个键。
				id.ProviderKey = userProvider(srcs[i].ID, TypeLDAP)
			}
			return id, nil
		case errors.Is(err, errUserNotFound), errors.Is(err, errBadCredentials):
			continue
		default:
			infra = fmt.Errorf("identity: source %s: %w", srcs[i].ID, err)
		}
	}
	if infra != nil {
		return nil, infra // 只进服务端日志，对外仍是“密码错误”
	}
	return nil, auth.ErrExternalRejected
}

// ldapUser 是一次成功 bind 的结果。
type ldapUser struct {
	Subject         string
	DN              string
	Username        string
	Nickname        string
	Email           string
	Phone           string
	MappedRoleCodes []string
}

func (u *ldapUser) identity(cfg LDAPConfig) *auth.ExternalIdentity {
	return &auth.ExternalIdentity{
		Subject: u.Username, Username: u.Username, Nickname: u.Nickname, Email: u.Email, Phone: u.Phone,
		MatchExisting: cfg.MatchExisting, AutoCreate: cfg.AutoCreate, DefaultRole: cfg.DefaultRole,
		MappedRoleCodes: append([]string(nil), u.MappedRoleCodes...),
	}
}

// connect 拨号并（如配置了服务账号）完成服务账号 bind。
func (a *ldapAuthenticator) connect(ctx context.Context, cfg LDAPConfig) (ldapSession, error) {
	sess, err := a.dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if cfg.cleartext() && a.log != nil {
		a.log.Warn("identity: LDAP user passwords are being sent in cleartext (allowPlaintext is on)")
	}
	if cfg.BindDN == "" { // 匿名搜索
		return sess, nil
	}
	pw, err := plainBindPassword(cfg.BindPassword)
	if err == nil && pw == "" {
		err = errors.New("service account has no password configured")
	}
	if err == nil {
		err = sess.Bind(cfg.BindDN, pw)
	}
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("service bind: %w", err)
	}
	return sess, nil
}

// verify 完整执行一次登录校验：搜索 -> 用户 bind。口令为空由 validLoginInput / Bind 双重拒绝。
func (a *ldapAuthenticator) verify(ctx context.Context, cfg LDAPConfig, username, password string) (*ldapUser, error) {
	if !validLoginInput(username, password) {
		return nil, errBadCredentials
	}
	if err := validateGroupMappings(cfg.GroupRoleMappings, cfg.RequireMappedGroup); err != nil {
		return nil, err
	}
	sess, err := a.connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sess.Close() }()
	entries, err := sess.Search(cfg.BaseDN, scopeSub, buildUserFilter(cfg.UserFilter, username), cfg.attrs(), 2)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if len(entries) != 1 || entries[0].DN == "" { // 0 条：无此用户；多条：过滤器不唯一，宁可拒绝也不猜
		return nil, errUserNotFound
	}
	e := entries[0]
	subject := ""
	if cfg.scopedDirectory() {
		if err := cfg.validateDirectory(); err != nil {
			return nil, err
		}
		subject, err = directorySubject(e, cfg.DirectorySync.SubjectAttr)
		if err != nil {
			return nil, err
		}
	}
	if err := sess.Bind(e.DN, password); err != nil {
		var le *ldapError
		if errors.As(err, &le) || errors.Is(err, errEmptyPassword) {
			return nil, errBadCredentials
		}
		return nil, fmt.Errorf("user bind: %w", err)
	}
	name := strings.TrimSpace(e.first(cfg.AttrUsername))
	if name == "" {
		name = username
	}
	if !localUsernameRe.MatchString(name) {
		return nil, fmt.Errorf("directory account name does not fit the local username rule")
	}
	attrGroups := cfg.AttrGroups
	if attrGroups == "" {
		attrGroups = "memberOf"
	}
	codes, err := mappedGroupRoles(e.Attrs[strings.ToLower(attrGroups)], cfg.GroupRoleMappings, cfg.RequireMappedGroup)
	if err != nil {
		return nil, err
	}
	return &ldapUser{Subject: subject, DN: e.DN, Username: name, MappedRoleCodes: codes, Nickname: clip(e.first(cfg.AttrNickname), 64),
		Email: clip(e.first(cfg.AttrEmail), 100), Phone: clip(e.first(cfg.AttrPhone), 32)}, nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 { // 不截断在多字节字符中间
		n--
	}
	return s[:n]
}
