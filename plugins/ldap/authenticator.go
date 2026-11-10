package ldap

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"nexadmin/core/auth"
)

// authenticator 实现 auth.ExternalAuthenticator:本地密码失败后用 LDAP 校验。
// Name 为 "ldap"——core 会把它写进登录日志的 login_type,并作为外部身份绑定的 provider。
type authenticator struct{ svc *Service }

func (a *authenticator) Name() string { return "ldap" }

// Authenticate 连接 LDAP → 服务账号绑定(可选)→ 按过滤器找到用户 DN → 用用户 DN + 密码绑定验证。
// 密码绝不写日志或错误。配置缺失/未启用返回 ErrExternalRejected(等同「本校验器不适用」)。
func (a *authenticator) Authenticate(_ context.Context, username, password string) (*auth.ExternalIdentity, error) {
	if strings.TrimSpace(username) == "" || strings.ContainsAny(username, "\x00\n\r") {
		return nil, auth.ErrExternalRejected
	}
	cfg, err := a.svc.Config(context.Background())
	if err != nil || !cfg.Enabled {
		return nil, auth.ErrExternalRejected
	}
	conn, err := dial(cfg)
	if err != nil {
		return nil, fmt.Errorf("ldap: dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPasswordPlain); err != nil {
			return nil, fmt.Errorf("ldap: service bind: %w", err)
		}
	}

	res, err := conn.Search(ldapSearch(cfg, username))
	if err != nil {
		return nil, fmt.Errorf("ldap: search: %w", err)
	}
	if len(res.Entries) != 1 {
		// 0 = 用户不存在;>1 = 过滤器不唯一(配置错误)。两者都按「凭据无效」处理,不区分。
		return nil, auth.ErrExternalRejected
	}
	entry := res.Entries[0]
	userDN := entry.DN

	// 用用户的 DN + 提交的密码直接绑定 —— 这就是 LDAP 的密码校验。
	if err := conn.Bind(userDN, password); err != nil {
		return nil, auth.ErrExternalRejected
	}

	usernameAttr := firstAttr(entry, cfg.AttrUsername)
	if usernameAttr == "" {
		usernameAttr = username
	}
	return &auth.ExternalIdentity{
		ProviderKey:   "ldap",
		Subject:       userDN,
		Username:      usernameAttr,
		Email:         firstAttr(entry, cfg.AttrEmail),
		Nickname:      firstAttr(entry, cfg.AttrNickname),
		MatchExisting: cfg.MatchExisting,
		AutoCreate:    cfg.AutoCreate,
		DefaultRole:   cfg.DefaultRole,
	}, nil
}

// dial 按 host/port/use_tls 建立连接(ldaps 直连或 ldap + StartTLS)。
// 建连必须带超时（net.Dialer）：ldap.Dial 默认靠 TCP 系统超时，配置错了连接会挂死很久。
func dial(cfg *LdapConfig) (*ldap.Conn, error) {
	addr := cfg.Host + ":" + strconv.Itoa(cfg.Port)
	dialer := &net.Dialer{Timeout: dialTimeout(cfg.TimeoutMS)}
	var conn *ldap.Conn
	var err error
	if cfg.UseTLS {
		tc, terr := tlsConfig(cfg.CACert, cfg.Host)
		if terr != nil {
			return nil, terr
		}
		conn, err = ldap.DialURL("ldaps://"+addr, ldap.DialWithTLSConfig(tc), ldap.DialWithDialer(dialer))
	} else {
		conn, err = ldap.DialURL("ldap://"+addr, ldap.DialWithDialer(dialer))
	}
	if err != nil {
		return nil, err
	}
	if !cfg.UseTLS && cfg.StartTLS {
		tc, terr := tlsConfig(cfg.CACert, cfg.Host)
		if terr != nil {
			_ = conn.Close()
			return nil, terr
		}
		if err = conn.StartTLS(tc); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("ldap: starttls: %w", err)
		}
	}
	return conn, nil
}

// ldapSearch 组装用户查找请求(filter 里 %s 已替换为用户名)。
// timeLimit 的单位是**秒**（go-ldap 的 SearchRequest 签名如此）：把毫秒原样传进去
// 5000ms 会变成 5000 秒，搜索时限形同虚设。
func ldapSearch(cfg *LdapConfig, username string) *ldap.SearchRequest {
	filter := strings.ReplaceAll(cfg.UserFilter, "%s", ldap.EscapeFilter(username))
	seconds := int(dialTimeout(cfg.TimeoutMS) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return ldap.NewSearchRequest(
		cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, seconds, false,
		filter,
		[]string{cfg.AttrUsername, cfg.AttrEmail, cfg.AttrNickname},
		nil,
	)
}

func firstAttr(e *ldap.Entry, name string) string {
	if name == "" {
		return ""
	}
	return strings.TrimSpace(e.GetAttributeValue(name))
}
