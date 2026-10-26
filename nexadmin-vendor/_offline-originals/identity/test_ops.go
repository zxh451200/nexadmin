package identity

import (
	"context"
	"errors"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// TestResult 是「测试连接 / 测试用户登录」的结果。不会包含任何口令。
type TestResult struct {
	OK       bool   `json:"ok"`
	Stage    string `json:"stage,omitempty"` // connect | bind | search | login | validate
	Message  string `json:"message,omitempty"`
	DN       string `json:"dn,omitempty"`
	Username string `json:"username,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
}

// TestConnection 检查身份源是否可用：LDAP 为 连接 -> 服务账号 bind -> 搜索 BaseDN；CAS 为请求 serviceValidate 并解析响应。
// 只读目录，不会创建 / 修改任何本地数据。
func (s *Service) TestConnection(ctx context.Context, id idgen.ID) (*TestResult, error) {
	src, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	switch src.Type {
	case TypeLDAP:
		cfg, err := src.ldapConfig()
		if err != nil {
			return &TestResult{Stage: "connect", Message: "stored configuration is invalid"}, nil
		}
		return s.testLDAP(ctx, cfg), nil
	case TypeCAS:
		cfg, err := src.casConfig()
		if err != nil {
			return &TestResult{Stage: "connect", Message: "stored configuration is invalid"}, nil
		}
		return s.testCAS(ctx, cfg), nil
	}
	return nil, badReq("unsupported source type")
}

func (s *Service) testLDAP(ctx context.Context, cfg LDAPConfig) *TestResult {
	a := &ldapAuthenticator{svc: s, dial: s.dial}
	sess, err := a.dial(ctx, cfg)
	if err != nil {
		return &TestResult{Stage: "connect", Message: err.Error()}
	}
	defer func() { _ = sess.Close() }()
	if cfg.BindDN != "" {
		pw, err := plainBindPassword(cfg.BindPassword)
		if err == nil && pw == "" {
			err = errors.New("service account has no password configured")
		}
		if err == nil {
			err = sess.Bind(cfg.BindDN, pw)
		}
		if err != nil {
			return &TestResult{Stage: "bind", Message: err.Error()}
		}
	}
	if _, err := sess.Search(cfg.BaseDN, scopeBase, "(objectClass=*)", []string{"1.1"}, 1); err != nil {
		return &TestResult{Stage: "search", Message: err.Error()}
	}
	return &TestResult{OK: true}
}

// TestLogin 用给定账号密码走一遍真实的 LDAP 登录校验（搜索 + 用户 bind），返回映射后的属性，不建号、不签发令牌。
func (s *Service) TestLogin(ctx context.Context, id idgen.ID, username, password string) (*TestResult, error) {
	src, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if src.Type != TypeLDAP {
		return nil, badReq("only LDAP sources support user login test")
	}
	if !validLoginInput(username, password) {
		return nil, response.BadRequest("error.bad_request").WithMsg("username and password are required")
	}
	cfg, err := src.ldapConfig()
	if err != nil {
		return &TestResult{Stage: "connect", Message: "stored configuration is invalid"}, nil
	}
	a := &ldapAuthenticator{svc: s, dial: s.dial}
	u, err := a.verify(ctx, cfg, username, password)
	switch {
	case err == nil:
		return &TestResult{OK: true, Stage: "login", DN: u.DN, Username: u.Username, Nickname: u.Nickname, Email: u.Email, Phone: u.Phone}, nil
	case errors.Is(err, errUserNotFound):
		return &TestResult{Stage: "search", Message: "user not found (or filter matched more than one entry)"}, nil
	case errors.Is(err, errBadCredentials):
		return &TestResult{Stage: "login", Message: "invalid credentials"}, nil
	}
	return &TestResult{Stage: "connect", Message: err.Error()}, nil
}
