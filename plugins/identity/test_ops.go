package identity

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/i18n"
	"nexadmin/core/middleware"
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

// 测试接口会让服务端主动连管理员配置的地址，所以必须限频，并且只返回粗粒度结论。
// 地址本身受 ssrf.go 约束（默认拒绝回环 / 私网 / 链路本地 / 元数据地址）。
// 限频维度是 租户+用户：有 cache 时用 core/middleware 的 Limiter（多实例共享，与其余限流同一套实现），
// 没有 cache（手工装配）时退回进程内滑动窗口，不会因缺少依赖而裸奔。
const (
	testRateMax    = 10
	testRateWindow = time.Minute
)

func testRateKey(tid, uid idgen.ID) string { return "t:" + tid.String() + "|u:" + uid.String() }

func (s *Service) allowTest(tid, uid idgen.ID) bool {
	key := testRateKey(tid, uid)
	if s.cache != nil {
		s.testLimiterOnce.Do(func() {
			s.testLimiter = middleware.NewLimiter(s.cache, s.log, "identity-test", testRateMax, testRateWindow)
		})
		ok, _ := s.testLimiter.Hit(context.Background(), key)
		return ok
	}
	s.testMu.Lock()
	defer s.testMu.Unlock()
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	if s.testHits == nil {
		s.testHits = map[string][]time.Time{}
	}
	for k, hits := range s.testHits { // 顺带清理过期记录，map 不会无限增长
		keep := hits[:0]
		for _, t := range hits {
			if now.Sub(t) < testRateWindow {
				keep = append(keep, t)
			}
		}
		if len(keep) == 0 {
			delete(s.testHits, k)
		} else {
			s.testHits[k] = keep
		}
	}
	if len(s.testHits[key]) >= testRateMax {
		return false
	}
	s.testHits[key] = append(s.testHits[key], now)
	return true
}

// callerID 取发起请求的用户 ID（测试里没有登录态时为 0）。
func callerID(ctx context.Context) idgen.ID {
	if cl := auth.ClaimsFromContext(ctx); cl != nil {
		return cl.UserID
	}
	return 0
}

var errTestRate = response.New(429, "identity.test_rate_limited")

// safeMessage 把底层错误归类成固定文案：目录 / CAS 服务器返回的诊断文本、IP:端口、证书细节只进服务端日志，
// 不回给调用方，避免测试接口被用来探测内网拓扑或读取内部服务的响应内容。
// msgKey 按请求语言取文案。
func msgKey(key string, args ...any) string { return i18n.T(context.Background(), key, args...) }

func (s *Service) safeMessage(ctx context.Context, stage string, err error) string {
	s.log.Warn("identity: test failed", zap.String("stage", stage), zap.Error(err))
	var le *ldapError
	var ne net.Error
	var ce *tls.CertificateVerificationError
	var ua x509.UnknownAuthorityError
	var he x509.HostnameError
	switch {
	case errors.Is(err, errBlockedHost):
		return msgKey("identity.test.host_blocked")
	case errors.Is(err, errPlaintextNotAllowed):
		return msgKey("identity.test.plaintext_blocked")
	case errors.As(err, &le):
		return msgKey("identity.test.directory_rejected", le.Code)
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return msgKey("identity.test.timeout")
	case errors.As(err, &ce), errors.As(err, &ua), errors.As(err, &he), strings.Contains(err.Error(), "TLS"):
		return msgKey("identity.test.tls_failed")
	case errors.Is(err, errBER), errors.Is(err, errCASResponse), strings.Contains(err.Error(), "unexpected"):
		return msgKey("identity.test.invalid_response")
	case errors.Is(err, errCASStatus):
		return msgKey("identity.test.cas_status")
	case errors.As(err, &ne):
		return msgKey("identity.test.connect_failed")
	}
	return msgKey("identity.test.failed")
}

// TestConnection 检查身份源是否可用：LDAP 为 连接 -> 服务账号 bind -> 搜索 BaseDN；CAS 为请求 serviceValidate 并解析响应。
// 只读目录，不会创建 / 修改任何本地数据。
func (s *Service) TestConnection(ctx context.Context, id idgen.ID) (*TestResult, error) {
	src, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.allowTest(src.TenantID, callerID(ctx)) {
		return nil, errTestRate
	}
	switch src.Type {
	case TypeLDAP:
		cfg, err := src.ldapConfig()
		if err != nil {
			return &TestResult{Stage: "connect", Message: msgKey("identity.test.config_invalid")}, nil
		}
		return s.testLDAP(ctx, cfg), nil
	case TypeCAS:
		cfg, err := src.casConfig()
		if err != nil {
			return &TestResult{Stage: "connect", Message: msgKey("identity.test.config_invalid")}, nil
		}
		return s.testCAS(ctx, cfg), nil
	}
	return nil, badReq("identity.source_type_unsupported")
}

func (s *Service) testLDAP(ctx context.Context, cfg LDAPConfig) *TestResult {
	a := &ldapAuthenticator{svc: s, dial: s.dial}
	sess, err := a.dial(ctx, cfg)
	if err != nil {
		return &TestResult{Stage: "connect", Message: s.safeMessage(ctx, "connect", err)}
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
			return &TestResult{Stage: "bind", Message: s.safeMessage(ctx, "bind", err)}
		}
	}
	if _, err := sess.Search(cfg.BaseDN, scopeBase, "(objectClass=*)", []string{"1.1"}, 1); err != nil {
		return &TestResult{Stage: "search", Message: s.safeMessage(ctx, "search", err)}
	}
	res := &TestResult{OK: true}
	if cfg.cleartext() {
		res.Message = "warning: this connection is not encrypted; passwords are sent in cleartext"
	}
	return res
}

// TestLogin 用给定账号密码走一遍真实的 LDAP 登录校验（搜索 + 用户 bind），返回映射后的属性，不建号、不签发令牌。
func (s *Service) TestLogin(ctx context.Context, id idgen.ID, username, password string) (*TestResult, error) {
	src, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if src.Type != TypeLDAP {
		return nil, badReq("identity.login_test_ldap_only")
	}
	if !validLoginInput(username, password) {
		return nil, response.BadRequest("identity.credentials_required")
	}
	if !s.allowTest(src.TenantID, callerID(ctx)) {
		return nil, errTestRate
	}
	cfg, err := src.ldapConfig()
	if err != nil {
		return &TestResult{Stage: "connect", Message: msgKey("identity.test.config_invalid")}, nil
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
	return &TestResult{Stage: "connect", Message: s.safeMessage(ctx, "connect", err)}, nil
}
