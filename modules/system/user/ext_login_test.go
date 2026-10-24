package user

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// ---- 测试替身与装配 ----

func newAuthSvc(t *testing.T, e *env) *auth.Service {
	t.Helper()
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = strings.Repeat("s", 32)
	cfg.Auth.AccessTTL, cfg.Auth.RefreshTTL = time.Minute, time.Hour
	svc, err := auth.NewService(cfg, e.d.Cache)
	if err != nil {
		t.Fatal(err)
	}
	e.d.Auth = svc
	return svc
}

type testGate struct{ require bool }

func (testGate) Name() string { return "2fa" }
func (g testGate) Check(context.Context, auth.GateInput) (*auth.GateRequirement, error) {
	if g.require {
		return &auth.GateRequirement{Methods: []string{"totp"}}, nil
	}
	return nil, nil
}
func (testGate) Verify(context.Context, auth.GateChallenge, auth.GateAnswer) error { return nil }

type testExt struct {
	id *auth.ExternalIdentity
}

func (testExt) Name() string { return "ldap" }
func (x testExt) Authenticate(_ context.Context, _, pw string) (*auth.ExternalIdentity, error) {
	if pw != "ldap-pw" {
		return nil, auth.ErrExternalRejected
	}
	return x.id, nil
}

func testCtx(e *env, method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(e.ctx)
	c.Request.RemoteAddr = "10.1.1.1:5555"
	return c, w
}

func superUser() *User {
	u := &User{Username: "root", Status: statusOn, IsSuper: true}
	u.ID, u.TenantID = idgen.ID(5), 1
	return u
}

// ---- 第三方登录出口必须过 LoginGate ----

func TestOAuthSessionIssuesTokensWhenNoGate(t *testing.T) {
	e := newEnv(t)
	newAuthSvc(t, e)
	e.mock.ExpectQuery("sys_user_role").WillReturnRows(sqlmock.NewRows([]string{"code", "builtin"}))
	c, w := testCtx(e, "POST", "/x", "")
	(&oauthHandler{m: &module{}, d: e.d, flow: e.flow}).session(c, superUser(), "github")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"accessToken"`) || strings.Contains(w.Body.String(), `"challenge"`) {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOAuthSessionWithGateWithholdsTokens(t *testing.T) {
	e := newEnv(t)
	svc := newAuthSvc(t, e)
	if err := svc.RegisterLoginGate(testGate{require: true}); err != nil {
		t.Fatal(err)
	}
	e.mock.ExpectQuery("sys_user_role").WillReturnRows(sqlmock.NewRows([]string{"code", "builtin"}))
	c, w := testCtx(e, "POST", "/x", "")
	(&oauthHandler{m: &module{}, d: e.d, flow: e.flow}).session(c, superUser(), "github")
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"challenge"`) || strings.Contains(body, "accessToken") || strings.Contains(body, "refreshToken") {
		t.Fatalf("OAuth login must stop at the challenge: code=%d body=%s", w.Code, body)
	}
}

// ---- PreLogin 覆盖第三方入口，且先于其它处理 ----

func TestOAuthEntrancesRunPreLoginFirst(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := newEnv(t)
	svc := newAuthSvc(t, e)
	var seen []string
	svc.RegisterPreLogin(func(_ context.Context, _, username string) error {
		seen = append(seen, username)
		return context.DeadlineExceeded // 任意内部原因
	})
	r := gin.New()
	api := r.Group("/api/v1")
	h := &oauthHandler{m: &module{}, d: e.d, flow: e.flow} // 不走 registerOAuth：它会初始化进程级共享 Flow，污染其它测试
	api.POST("/auth/oauth/:provider/callback", h.callback)
	api.POST("/auth/oauth/bind", h.bind)
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/auth/oauth/github/callback", `{"code":"c","state":"nope"}`},
		{"/api/v1/auth/oauth/bind", `{"bindToken":"t","username":"alice","password":"p"}`},
	} {
		code, m := do(r, "POST", tc.path, tc.body)
		if code != http.StatusForbidden || strings.Contains(strings.ToLower(m["message"].(string)), "deadline") {
			t.Fatalf("%s: code=%d body=%v", tc.path, code, m)
		}
	}
	if len(seen) != 2 || seen[0] != "" || seen[1] != "alice" {
		t.Fatalf("pre-login calls = %v", seen)
	}
}

// ---- 外部校验链：与本地失败同路径，不引入枚举差异 ----

func failCount(t *testing.T, e *env, name string) string {
	t.Helper()
	v, _ := e.d.Cache.Get(e.ctx, failKey(e.ctx, name))
	return v
}

func TestPasswordLoginExternalRejectedCountsLikeLocalFailure(t *testing.T) {
	e := newEnv(t)
	svc := newAuthSvc(t, e)
	if err := svc.RegisterExternalAuthenticator(testExt{}); err != nil {
		t.Fatal(err)
	}
	m := &module{}
	m.deps.Store(e.d)
	for i, name := range []string{"ghost", "ghost"} { // 用户不存在
		e.mock.ExpectQuery("FROM `sys_user`").WillReturnRows(userRows("1"))
		_, err := m.passwordLogin(e.ctx, auth.LoginRequest{Type: "password", Username: name, Password: "bad"})
		if re, ok := err.(*response.Error); !ok || re.Key != "auth.invalid_credentials" {
			t.Fatalf("attempt %d: err=%v", i, err)
		}
	}
	if got := failCount(t, e, "ghost"); got != "2" {
		t.Fatalf("external rejection must count as a normal failure, counter=%q", got)
	}
}

func TestPasswordLoginExternalDeniedIdentityStillCountsAsFailure(t *testing.T) {
	e := newEnv(t)
	svc := newAuthSvc(t, e)
	_ = svc.RegisterExternalAuthenticator(testExt{id: &auth.ExternalIdentity{Username: "dave"}}) // 不允许匹配 / 自动创建
	m := &module{}
	m.deps.Store(e.d)
	e.mock.ExpectQuery("FROM `sys_user`").WillReturnRows(userRows("1"))                     // 本地没有该用户
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"id"})) // 未绑定
	_, err := m.passwordLogin(e.ctx, auth.LoginRequest{Type: "password", Username: "dave", Password: "ldap-pw"})
	if re, ok := err.(*response.Error); !ok || re.Key != "auth.invalid_credentials" {
		t.Fatalf("err=%v", err)
	}
	if got := failCount(t, e, "dave"); got != "1" {
		t.Fatalf("counter=%q", got)
	}
	e.done(t)
}

// ---- 外部身份 -> 本地用户：遵守额度、用户名规则、角色默认值 ----

func expectCreateUntilQuota(e *env) {
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	e.mock.ExpectQuery("count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(0)) // 用户名未占用
	// 建号事务：Begin -> 锁并校验默认角色(sys_role ... FOR UPDATE) -> 锁租户行 -> 计数 -> 插入
	e.mock.ExpectBegin()
	e.mock.ExpectQuery("sys_role.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id", "code"}).AddRow(33, "guest"))
	expectTenantLock(e.mock)
}

var autoIdent = auth.ExternalIdentity{Subject: "uid=erin", Username: "erin", Nickname: "Erin", Email: "erin@corp.com", AutoCreate: true}

func TestResolveExternalAutoCreateRespectsQuota(t *testing.T) {
	useSource(t, limitSource{1: {ID: 1, Status: "active", MaxUsers: 3}})
	e := newEnv(t)
	m := &module{}
	m.deps.Store(e.d)
	expectCreateUntilQuota(e)
	e.mock.ExpectQuery("count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(3)) // 额度已满
	e.mock.ExpectRollback()
	_, err := m.ResolveExternalUser(e.ctx, "ldap", autoIdent)
	if quotaReason(err) != tenant.ReasonQuotaUsers {
		t.Fatalf("full quota must reject auto-create with the quota error, got %v", err)
	}
	e.done(t) // 严格顺序：额度拒绝后没有任何 INSERT
}

func TestResolveExternalAutoCreateSucceedsWithDefaultRole(t *testing.T) {
	useSource(t, limitSource{1: {ID: 1, Status: "active", MaxUsers: 3}})
	e := newEnv(t)
	m := &module{}
	m.deps.Store(e.d)
	expectCreateUntilQuota(e)
	e.mock.ExpectQuery("count\\(\\*\\) FROM `sys_user`").WillReturnRows(countRows(1))
	e.mock.ExpectExec("INSERT INTO `sys_user`").WillReturnResult(sqlmock.NewResult(1, 1))
	e.mock.ExpectExec("INSERT INTO `sys_user_role`").WillReturnResult(sqlmock.NewResult(1, 1)) // 默认角色
	e.mock.ExpectExec("INSERT INTO `sys_user_identity`").WillReturnResult(sqlmock.NewResult(1, 1))
	e.mock.ExpectCommit()
	e.mock.ExpectBegin()
	e.mock.ExpectExec("UPDATE `sys_user`").WillReturnResult(sqlmock.NewResult(1, 1)) // last_login_at
	e.mock.ExpectCommit()
	e.mock.ExpectQuery("sys_user_role").WillReturnRows(sqlmock.NewRows([]string{"code", "builtin"}).AddRow("guest", false))
	e.mock.ExpectQuery("DISTINCT m.perm").WillReturnRows(sqlmock.NewRows([]string{"perm"}))
	u, err := m.ResolveExternalUser(e.ctx, "ldap", autoIdent)
	if err != nil || u.Username != "erin" || u.LoginMethod != "ldap" || len(u.Roles) != 1 || u.Roles[0] != "guest" {
		t.Fatalf("u=%+v err=%v", u, err)
	}
	e.done(t)
}

func TestResolveExternalRefusals(t *testing.T) {
	e := newEnv(t)
	m := &module{}
	m.deps.Store(e.d)
	// 用户名不合规：不碰数据库
	if _, err := m.ResolveExternalUser(e.ctx, "ldap", auth.ExternalIdentity{Username: "bad name!", AutoCreate: true}); err != errExternalDenied {
		t.Fatalf("err=%v", err)
	}
	// 默认不匹配、不建号
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := m.ResolveExternalUser(e.ctx, "ldap", auth.ExternalIdentity{Username: "frank"}); err != errExternalDenied {
		t.Fatalf("err=%v", err)
	}
	// 同名账号是超管：即使 MatchExisting 也不绑定
	e.mock.ExpectQuery("sys_user_identity").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	e.mock.ExpectQuery("FROM `sys_user` WHERE username").WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "username", "status", "is_super"}).AddRow(5, 1, "admin", "1", true))
	if _, err := m.ResolveExternalUser(e.ctx, "ldap", auth.ExternalIdentity{Username: "admin", MatchExisting: true}); err != errExternalDenied {
		t.Fatalf("err=%v", err)
	}
	e.done(t)
}
