package security

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// ---- 用真实 auth.Service + auth.Handler 验证与 core/auth 扩展点的对接 ----

type testProvider struct{ users map[idgen.ID]auth.User }

func (p *testProvider) Login(_ context.Context, req auth.LoginRequest) (*auth.User, error) {
	for _, u := range p.users {
		if u.Username == req.Username && req.Password == "right" {
			c := u
			return &c, nil
		}
	}
	return nil, response.BadRequest("auth.invalid_credentials")
}

func (p *testProvider) Profile(_ context.Context, id idgen.ID) (*auth.User, error) {
	if u, ok := p.users[id]; ok {
		c := u
		return &c, nil
	}
	return nil, response.Unauthorized("error.unauthorized")
}

type authEnv struct {
	*env
	svc *auth.Service
	r   *gin.Engine
	p   *testProvider
}

const (
	carolID idgen.ID = 7
	rootID  idgen.ID = 1
	nobody  idgen.ID = 8
	adminID idgen.ID = 9
)

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := newEnv(t)
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = strings.Repeat("j", 32)
	cfg.Auth.AccessTTL, cfg.Auth.RefreshTTL = time.Minute, time.Hour
	svc, err := auth.NewService(cfg, e.s.cache)
	if err != nil {
		t.Fatal(err)
	}
	p := &testProvider{users: map[idgen.ID]auth.User{
		carolID: {ID: carolID, Username: "carol", TenantID: 1, Roles: []string{"staff"}, Perms: []string{}},
		rootID:  {ID: rootID, Username: "root", TenantID: 1, Roles: []string{"super"}, Perms: []string{"*"}, Super: true},
		nobody:  {ID: nobody, Username: "nobody", TenantID: 1, Perms: []string{}},
		adminID: {ID: adminID, Username: "admin", TenantID: 1, Roles: []string{"ops"}, Perms: []string{"security:*"}},
	}}
	svc.SetProvider(p)
	e.s.lookup = p.Profile
	svc.RegisterPreLogin(e.s.preLogin)
	svc.RegisterRefreshCheck(e.s.refreshCheck)
	if err := svc.RegisterLoginGate(&gate{s: e.s}); err != nil {
		t.Fatal(err)
	}
	auth.SetDefault(svc)
	t.Cleanup(func() { auth.SetDefault(nil) })
	h := &auth.Handler{Svc: svc, Cfg: cfg}
	r := gin.New()
	r.Use(middleware.ParseToken(svc))
	r.POST("/login", h.Login)
	r.POST("/verify", h.LoginVerify)
	r.POST("/refresh", h.Refresh)
	api := r.Group("/api/v1")
	mount(api, &handler{svc: e.s, profile: p.Profile})
	return &authEnv{env: e, svc: svc, r: r, p: p}
}

type reply struct {
	Code int            `json:"code"`
	Data map[string]any `json:"data"`
}

func (x *authEnv) do(method, path, ip, token string, body any) (int, reply) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = net.JoinHostPort(ip, "4321")
	w := httptest.NewRecorder()
	x.r.ServeHTTP(w, req)
	var rp reply
	_ = json.Unmarshal(w.Body.Bytes(), &rp)
	return w.Code, rp
}

func (x *authEnv) login(user, ip string) (int, reply) {
	return x.do("POST", "/login", ip, "", map[string]string{"type": "password", "username": user, "password": "right"})
}

func (x *authEnv) verify(chID, code, kind, ip string) (int, reply) {
	b := map[string]string{"challengeId": chID, "code": code}
	if kind != "" {
		b["kind"] = kind
	}
	return x.do("POST", "/verify", ip, "", b)
}

func chOf(t *testing.T, r reply) (string, []any) {
	t.Helper()
	if _, has := r.Data["accessToken"]; has {
		t.Fatal("a challenge response must not contain tokens")
	}
	ch, _ := r.Data["challenge"].(map[string]any)
	id, _ := ch["id"].(string)
	if id == "" {
		t.Fatalf("expected a challenge, got %+v", r)
	}
	m, _ := ch["methods"].([]any)
	return id, m
}

func hasTokens(r reply) bool { s, _ := r.Data["accessToken"].(string); return s != "" }

var carol = User{TID: 1, ID: carolID, Name: "carol", Roles: []string{"staff"}}

func TestLoginWithoutTwoFactorIssuesTokens(t *testing.T) {
	x := newAuthEnv(t)
	if code, r := x.login("carol", "203.0.113.1"); code != 200 || !hasTokens(r) {
		t.Fatalf("user without 2FA must log in normally: %d %+v", code, r)
	}
}

func TestEnabledUserGetsChallengeThenTokens(t *testing.T) {
	x := newAuthEnv(t)
	sec, codes := x.enable2FA(t, carol)
	ip := "203.0.113.1"

	code, r := x.login("carol", ip)
	if code != 200 {
		t.Fatalf("login: %d %+v", code, r)
	}
	id, methods := chOf(t, r) // 断言：拿到 challenge，没有令牌
	if len(methods) != 2 || methods[0] != "totp" || methods[1] != "recovery" {
		t.Fatalf("methods = %v, frontend expects totp + recovery", methods)
	}

	if code, r = x.verify(id, "000000", "", ip); code != 400 || hasTokens(r) {
		t.Fatalf("wrong code must be rejected without tokens: %d %+v", code, r)
	}
	if code, r = x.verify(id, x.code(t, sec), "", ip); code != 200 || !hasTokens(r) {
		t.Fatalf("right code must issue tokens: %d %+v", code, r)
	}
	if code, _ = x.verify(id, x.code(t, sec), "", ip); code != 400 {
		t.Fatalf("challenge is single-use, got %d", code)
	}

	// 重放：同一时间步的码在新挑战上也不能再用
	_, r = x.login("carol", ip)
	id2, _ := chOf(t, r)
	if code, r = x.verify(id2, x.code(t, sec), "", ip); code != 400 || hasTokens(r) {
		t.Fatalf("replayed code must be rejected: %d %+v", code, r)
	}
	x.ck.Add(totpPeriod * time.Second)
	_, r = x.login("carol", ip)
	id3, _ := chOf(t, r)
	if code, r = x.verify(id3, x.code(t, sec), "", ip); code != 200 || !hasTokens(r) {
		t.Fatalf("next-step code: %d %+v", code, r)
	}

	// 恢复码：kind=recovery，一次性
	_, r = x.login("carol", ip)
	id4, _ := chOf(t, r)
	if code, r = x.verify(id4, codes[0], "recovery", ip); code != 200 || !hasTokens(r) {
		t.Fatalf("recovery code: %d %+v", code, r)
	}
	_, r = x.login("carol", ip)
	id5, _ := chOf(t, r)
	if code, r = x.verify(id5, codes[0], "recovery", ip); code != 400 || hasTokens(r) {
		t.Fatalf("used recovery code must fail: %d %+v", code, r)
	}
}

func TestVerifyLockoutThroughAuth(t *testing.T) {
	x := newAuthEnv(t)
	sec, _ := x.enable2FA(t, carol)
	ip := "203.0.113.1"
	var last int
	for i := 0; i < x.s.maxFails; i++ { // 每次换新挑战，core 的单挑战次数限制帮不上忙，靠插件的按用户计数
		_, r := x.login("carol", ip)
		id, _ := chOf(t, r)
		last, _ = x.verify(id, "000000", "", ip)
	}
	if last != 423 {
		t.Fatalf("reaching the failure limit must lock the user (423), got %d", last)
	}
	_, r := x.login("carol", ip)
	id, _ := chOf(t, r)
	if code, rr := x.verify(id, x.code(t, sec), "", ip); code != 423 || hasTokens(rr) {
		t.Fatalf("locked user must be refused even with a right code: %d", code)
	}
}

func TestPreLoginIPRulesThroughAuth(t *testing.T) {
	x := newAuthEnv(t)
	ctx := context.Background()
	admin := User{TID: 1, ID: adminID, Name: "admin", IP: "10.0.0.1"}
	mustRule := func(f RuleForm) {
		t.Helper()
		if _, err := x.s.SaveRule(ctx, admin, 0, f); err != nil {
			t.Fatal(err)
		}
	}
	mustRule(RuleForm{Action: "deny", CIDR: "198.51.100.0/24", Scope: "all", Enabled: true})
	if code, r := x.login("carol", "198.51.100.5"); code != 403 || hasTokens(r) {
		t.Fatalf("denied ip must get 403: %d", code)
	}
	if code, _ := x.login("carol", "198.51.101.5"); code != 200 {
		t.Fatalf("other ip: %d", code)
	}
	// 二次验证入口同样走 PreLogin
	sec, _ := x.enable2FA(t, carol)
	_, r := x.login("carol", "198.51.101.5")
	id, _ := chOf(t, r)
	if code, _ := x.verify(id, x.code(t, sec), "", "198.51.100.5"); code != 403 {
		t.Fatalf("verify from a denied ip must be 403, got %d", code)
	}
	// 白名单非空：只放行名单内（含 IPv6）
	mustRule(RuleForm{Action: "allow", CIDR: "10.0.0.0/8", Scope: "all", Enabled: true})
	mustRule(RuleForm{Action: "allow", CIDR: "2001:db8::/32", Scope: "all", Enabled: true})
	if code, _ := x.login("nobody", "203.0.113.1"); code != 403 {
		t.Fatalf("ip outside allow list: %d", code)
	}
	if code, _ := x.login("nobody", "10.1.2.3"); code != 200 {
		t.Fatalf("ip in allow list: %d", code)
	}
	if code, _ := x.login("nobody", "2001:db8::77"); code != 200 {
		t.Fatalf("ipv6 in allow list: %d", code)
	}
	// 规则缓存在保存时立即失效
	mustRule(RuleForm{Action: "deny", CIDR: "10.1.2.3", Scope: "all", Enabled: true})
	if code, _ := x.login("nobody", "10.1.2.3"); code != 403 {
		t.Fatalf("deny beats allow and takes effect immediately: %d", code)
	}
}

func TestSuperScopeRuleAppliesAfterCredentials(t *testing.T) {
	x := newAuthEnv(t)
	op := User{TID: 1, ID: adminID, Name: "admin", IP: "10.0.0.1"}
	if _, err := x.s.SaveRule(context.Background(), op, 0, RuleForm{Action: "allow", CIDR: "10.0.0.0/8", Scope: "super", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if code, r := x.login("root", "203.0.113.1"); code == 200 || hasTokens(r) {
		t.Fatalf("super admin outside the super allow list must not get tokens: %d", code)
	}
	if code, r := x.login("root", "10.2.3.4"); code != 200 || !hasTokens(r) {
		t.Fatalf("super admin inside the list: %d", code)
	}
	if code, _ := x.login("carol", "203.0.113.1"); code != 200 {
		t.Fatalf("normal users are not affected by scope=super: %d", code)
	}
}

type brokenStore struct{ *memStore }

func (brokenStore) GetTOTP(context.Context, idgen.ID, idgen.ID) (*TOTPRow, error) {
	return nil, errors.New("db down")
}

func TestGateFailsClosed(t *testing.T) {
	x := newAuthEnv(t)
	x.s.st = brokenStore{x.st}
	if code, r := x.login("carol", "203.0.113.1"); code == 200 || hasTokens(r) {
		t.Fatalf("a failing gate must not let the login through: %d", code)
	}
}

// ---- 强制策略 / 宽限期 / 重置 ----

func TestForcedPolicyGracePeriod(t *testing.T) {
	x := newAuthEnv(t)
	ctx := context.Background()
	admin := User{TID: 1, ID: adminID, Name: "admin", IP: "10.0.0.1", Roles: []string{"ops"}}
	ip := "203.0.113.1"
	if _, err := x.s.SavePolicy(ctx, admin, PolicyForm{Mode: ModeAll, GraceDays: 3}); err != nil {
		t.Fatal(err)
	}
	// 宽限期内：未绑定的用户可以登录，并且状态里能看到截止时间
	if code, r := x.login("carol", ip); code != 200 || !hasTokens(r) {
		t.Fatalf("inside grace: %d", code)
	}
	st, _ := x.s.Status(ctx, carol)
	if !st.Required || st.GraceEndsAt == nil || st.CanDisable {
		t.Fatalf("status: %+v", st)
	}
	// 宽限期内他可以自己完成绑定
	x.ck.Add(24 * time.Hour)
	sec, _ := x.enable2FA(t, carol)
	_, r := x.login("carol", ip)
	chOf(t, r)
	// 被强制的用户不能自己关闭
	if err := x.s.Disable(ctx, carol, "", x.code(t, sec)); statusOf(err) != http.StatusForbidden {
		t.Fatalf("forced user must not disable 2FA: %v", err)
	}

	// 另一个未绑定的用户（nobody）：过了宽限期被拦，超管不被拦
	if code, _ := x.login("nobody", ip); code != 200 {
		t.Fatalf("nobody inside grace: %d", code)
	}
	x.ck.Add(4 * 24 * time.Hour)
	if code, r := x.login("nobody", ip); code == 200 || hasTokens(r) {
		t.Fatalf("expired grace must block unbound users: %d", code)
	}
	if code, r := x.login("root", ip); code != 200 || !hasTokens(r) {
		t.Fatalf("super admin is never blocked by grace expiry: %d", code)
	}
	// 管理员重置 -> 删除记录 -> 下次登录获得全新宽限期，不会被永久锁死
	if err := x.s.ResetUser(ctx, admin, 8); err != nil {
		t.Fatal(err)
	}
	if code, r := x.login("nobody", ip); code != 200 || !hasTokens(r) {
		t.Fatalf("after reset the user gets a fresh grace: %d", code)
	}
	// 策略变更重新计算宽限：再次保存策略后，即使旧记录已过期也不会立刻被拦
	x.ck.Add(10 * 24 * time.Hour)
	if code, _ := x.login("nobody", ip); code == 200 {
		t.Fatal("expired again")
	}
	if _, err := x.s.SavePolicy(ctx, admin, PolicyForm{Mode: ModeAll, GraceDays: 3}); err != nil {
		t.Fatal(err)
	}
	if code, _ := x.login("nobody", ip); code != 200 {
		t.Fatalf("a policy change restarts the grace window: %d", code)
	}
}

func TestPolicyByRoleAndValidation(t *testing.T) {
	x := newAuthEnv(t)
	ctx := context.Background()
	admin := User{TID: 1, ID: adminID, Name: "admin", IP: "10.0.0.1"}
	for _, f := range []PolicyForm{
		{Mode: "weird", GraceDays: 3}, {Mode: ModeAll, GraceDays: 0}, {Mode: ModeAll, GraceDays: 91},
		{Mode: ModeRoles, GraceDays: 3}, {Mode: ModeRoles, Roles: []string{"a b"}, GraceDays: 3},
	} {
		if _, err := x.s.SavePolicy(ctx, admin, f); statusOf(err) != http.StatusBadRequest {
			t.Errorf("%+v must be 400, got %v", f, err)
		}
	}
	if _, err := x.s.SavePolicy(ctx, admin, PolicyForm{Mode: ModeRoles, Roles: []string{"ops"}, GraceDays: 1}); err != nil {
		t.Fatal(err)
	}
	ip := "203.0.113.1"
	x.login("carol", ip) // staff 不在名单：不创建宽限记录
	if row, _ := x.st.GetTOTP(ctx, 1, carolID); row != nil {
		t.Fatal("users outside the forced roles must not get a grace row")
	}
	x.login("admin", ip) // ops 在名单内
	if row, _ := x.st.GetTOTP(ctx, 1, adminID); row == nil || row.GraceUntil == nil {
		t.Fatal("forced role must get a grace row")
	}
	x.ck.Add(3 * 24 * time.Hour)
	if code, _ := x.login("carol", ip); code != 200 {
		t.Fatalf("unforced role: %d", code)
	}
	if code, _ := x.login("admin", ip); code == 200 {
		t.Fatal("forced role past grace must be blocked")
	}
}

func TestResetWritesLogAndIsAtomicWithIt(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	admin := User{TID: 1, ID: 9, Name: "admin", IP: "10.0.0.1"}
	e.enable2FA(t, alice)
	e.st.failLog = true
	if err := e.s.ResetUser(ctx, admin, alice.ID); err == nil {
		t.Fatal("reset must fail when the log cannot be written")
	}
	if row, _ := e.st.GetTOTP(ctx, 1, alice.ID); row == nil || !row.Enabled {
		t.Fatal("a failed reset must leave the binding untouched")
	}
	e.st.failLog = false
	if err := e.s.ResetUser(ctx, admin, alice.ID); err != nil {
		t.Fatal(err)
	}
	if row, _ := e.st.GetTOTP(ctx, 1, alice.ID); row != nil {
		t.Fatal("binding must be removed")
	}
	if acts := e.st.logActions(1); len(acts) == 0 || acts[len(acts)-1] != "reset_2fa" {
		t.Fatalf("reset must be logged, got %v", acts)
	}
	if err := e.s.ResetUser(ctx, admin, 12345); statusOf(err) != http.StatusNotFound {
		t.Fatalf("unknown user: %v", err)
	}
}

// ---- 接口权限 ----

func TestEveryRouteRequiresPermission(t *testing.T) {
	x := newAuthEnv(t)
	pair, err := x.svc.IssuePair(&auth.User{ID: nobody, TenantID: 1, Username: "nobody"})
	if err != nil {
		t.Fatal(err)
	}
	var routes []gin.RouteInfo
	for _, ri := range x.r.Routes() {
		if strings.HasPrefix(ri.Path, "/api/v1/plugins/security/") {
			routes = append(routes, ri)
		}
	}
	if len(routes) != 15 {
		t.Fatalf("expected 15 plugin routes, got %d", len(routes))
	}
	for _, ri := range routes {
		path := strings.ReplaceAll(ri.Path, ":id", "123")
		var body any
		if ri.Method != "GET" && ri.Method != "DELETE" {
			body = map[string]any{}
		}
		if code, _ := x.do(ri.Method, path, "203.0.113.1", "", body); code != http.StatusUnauthorized {
			t.Errorf("%s %s without login: %d, want 401", ri.Method, ri.Path, code)
		}
		if code, _ := x.do(ri.Method, path, "203.0.113.1", pair.AccessToken, body); code != http.StatusForbidden {
			t.Errorf("%s %s without permission: %d, want 403", ri.Method, ri.Path, code)
		}
	}
}

func TestSelfServiceThroughHTTP(t *testing.T) {
	x := newAuthEnv(t)
	pair, _ := x.svc.IssuePair(&auth.User{ID: adminID, TenantID: 1, Username: "admin"})
	tok, ip := pair.AccessToken, "10.0.0.1"
	if code, _ := x.do("POST", "/api/v1/plugins/security/2fa/setup", ip, tok, map[string]string{"password": "wrong"}); code != 401 {
		t.Fatalf("setup with a wrong password must be refused: %d", code)
	}
	code, r := x.do("POST", "/api/v1/plugins/security/2fa/setup", ip, tok, map[string]string{"password": "pw"})
	sec, _ := r.Data["secret"].(string)
	uri, _ := r.Data["uri"].(string)
	if code != 200 || sec == "" || !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("setup: %d %+v", code, r)
	}
	if code, _ = x.do("POST", "/api/v1/plugins/security/2fa/enable", ip, tok, map[string]string{"code": "000000"}); code != 400 {
		t.Fatalf("enable with wrong code: %d", code)
	}
	code, r = x.do("POST", "/api/v1/plugins/security/2fa/enable", ip, tok, map[string]string{"code": x.code(t, sec)})
	rc, _ := r.Data["recoveryCodes"].([]any)
	if code != 200 || len(rc) != 10 {
		t.Fatalf("enable: %d %+v", code, r)
	}
	code, r = x.do("GET", "/api/v1/plugins/security/2fa/status", ip, tok, nil)
	if code != 200 || r.Data["enabled"] != true || r.Data["recoveryLeft"] != float64(10) {
		t.Fatalf("status: %d %+v", code, r)
	}
	// 业务主键序列化为字符串
	code, r = x.do("POST", "/api/v1/plugins/security/ip-rules", ip, tok, map[string]any{"action": "deny", "cidr": "203.0.113.0/24", "scope": "all", "enabled": true})
	if _, isStr := r.Data["id"].(string); code != 200 || !isStr {
		t.Fatalf("rule create: %d %+v", code, r)
	}
}
