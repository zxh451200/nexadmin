package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// ---- 测试替身 ----

type extProvider struct {
	mu     sync.Mutex
	events []LoginEvent
	users  map[idgen.ID]*User
}

func (p *extProvider) Login(_ context.Context, req LoginRequest) (*User, error) {
	if req.Password != "right" {
		return nil, response.BadRequest("auth.invalid_credentials")
	}
	c := *p.users[7] // 真实 Provider 每次返回新对象
	return &c, nil
}
func (p *extProvider) Profile(_ context.Context, id idgen.ID) (*User, error) {
	if u, ok := p.users[id]; ok {
		c := *u
		return &c, nil
	}
	return nil, response.Unauthorized("error.unauthorized")
}
func (p *extProvider) ResolveExternalUser(_ context.Context, method string, id ExternalIdentity) (*User, error) {
	if id.Username != "carol" {
		return nil, response.Forbidden("auth.login_denied")
	}
	c := *p.users[7]
	c.LoginMethod = method
	return &c, nil
}
func (p *extProvider) RecordLogin(_ context.Context, ev LoginEvent) {
	p.mu.Lock()
	p.events = append(p.events, ev)
	p.mu.Unlock()
}
func (p *extProvider) methods() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, e := range p.events {
		out = append(out, e.Method+"/"+e.Reason)
	}
	return out
}

type fakeGate struct {
	require atomic.Bool
	checks  atomic.Int32
	verify  atomic.Int32
	okCode  string
}

func (g *fakeGate) Name() string { return "2fa" }
func (g *fakeGate) Check(context.Context, GateInput) (*GateRequirement, error) {
	g.checks.Add(1)
	if g.require.Load() {
		return &GateRequirement{Methods: []string{"totp"}}, nil
	}
	return nil, nil
}
func (g *fakeGate) Verify(_ context.Context, _ GateChallenge, ans GateAnswer) error {
	g.verify.Add(1)
	if ans.Code != g.okCode {
		return errors.New("bad code")
	}
	return nil
}

type extEnv struct {
	e *gin.Engine
	s *Service
	p *extProvider
	g *fakeGate
}

func newExtEnv(t *testing.T) *extEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s := newCaptchaSvc(t)
	s.secret, s.accessTTL, s.refreshTTL = []byte(strings.Repeat("k", 32)), time.Minute, time.Hour
	p := &extProvider{users: map[idgen.ID]*User{7: {ID: 7, Username: "carol"}}}
	s.SetProvider(p)
	h := &Handler{Svc: s, Cfg: &config.Config{}}
	e := gin.New()
	e.POST("/login", h.Login)
	e.POST("/verify", h.LoginVerify)
	e.POST("/refresh", h.Refresh)
	return &extEnv{e: e, s: s, p: p, g: &fakeGate{okCode: "123456"}}
}

type resp struct {
	Code int            `json:"code"`
	Data map[string]any `json:"data"`
}

func (x *extEnv) post(path string, body any, ip string) (int, resp) {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":1234"
	w := httptest.NewRecorder()
	x.e.ServeHTTP(w, req)
	var r resp
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	return w.Code, r
}

func (x *extEnv) login(ip string) (int, resp) {
	return x.post("/login", map[string]string{"type": "password", "username": "carol", "password": "right"}, ip)
}

func challengeID(t *testing.T, r resp) string {
	t.Helper()
	ch, _ := r.Data["challenge"].(map[string]any)
	id, _ := ch["id"].(string)
	if id == "" {
		t.Fatalf("expected a challenge, got %+v", r)
	}
	if _, has := r.Data["accessToken"]; has {
		t.Fatal("challenge response must not contain tokens")
	}
	return id
}

// ---- 用例 ----

func TestNoRegistrationsBehavesAsBefore(t *testing.T) {
	x := newExtEnv(t)
	code, r := x.login("10.0.0.1")
	if code != 200 || r.Data["accessToken"] == nil || r.Data["refreshToken"] == nil || r.Data["user"] == nil || r.Data["challenge"] != nil {
		t.Fatalf("code=%d resp=%+v", code, r)
	}
	if ev := x.p.methods(); len(ev) != 0 {
		t.Fatalf("core must not write extra login logs without extensions: %v", ev)
	}
	if code, _ := x.post("/verify", map[string]string{"challengeId": strings.Repeat("a", 64), "code": "123456"}, "10.0.0.1"); code != 400 {
		t.Fatalf("verify without any challenge must be a plain 400, got %d", code)
	}
	var nilSvc *Service // nil 安全
	nilSvc.RegisterPreLogin(func(context.Context, string, string) error { return nil })
	if err := nilSvc.PreLogin(context.Background(), "1.1.1.1", "u", ""); err != nil {
		t.Fatal(err)
	}
	if m, id := nilSvc.TryExternal(context.Background(), "u", "p", nil); m != "" || id != nil {
		t.Fatal("nil service must not authenticate")
	}
}

func TestGateRequiredNoTokenThenVerify(t *testing.T) {
	x := newExtEnv(t)
	x.g.require.Store(true)
	if err := x.s.RegisterLoginGate(x.g); err != nil {
		t.Fatal(err)
	}
	code, r := x.login("10.0.0.1")
	id := challengeID(t, r)
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
	if code, _ := x.post("/verify", map[string]string{"challengeId": id, "code": "000000"}, "10.0.0.1"); code != 400 {
		t.Fatalf("wrong code must fail, got %d", code)
	}
	code, r = x.post("/verify", map[string]string{"challengeId": id, "code": "123456"}, "10.0.0.1")
	if code != 200 || r.Data["accessToken"] == nil {
		t.Fatalf("verify: code=%d resp=%+v", code, r)
	}
	// 一次性：同一个挑战不能再用
	if code, _ := x.post("/verify", map[string]string{"challengeId": id, "code": "123456"}, "10.0.0.1"); code != 400 {
		t.Fatalf("challenge must be single use, got %d", code)
	}
	if ev := x.p.methods(); len(ev) != 2 || ev[0] != "2fa/credentials" || ev[1] != "2fa/" {
		t.Fatalf("login log events = %v", ev)
	}
}

func TestChallengeBoundToIPExpiresAndCapsAttempts(t *testing.T) {
	x := newExtEnv(t)
	x.g.require.Store(true)
	_ = x.s.RegisterLoginGate(x.g)
	x.s.SetChallengeLimits(0, 3)

	// 换 IP：失败且挑战作废（原 IP 也不能再用）
	_, r := x.login("10.0.0.1")
	id := challengeID(t, r)
	if code, _ := x.post("/verify", map[string]string{"challengeId": id, "code": "123456"}, "10.0.0.2"); code != 400 {
		t.Fatalf("other IP must fail, got %d", code)
	}
	if code, _ := x.post("/verify", map[string]string{"challengeId": id, "code": "123456"}, "10.0.0.1"); code != 400 {
		t.Fatalf("challenge must be dead after IP mismatch, got %d", code)
	}

	// 超过尝试次数：之后即使验证码正确也失败
	_, r = x.login("10.0.0.1")
	id = challengeID(t, r)
	for i := 0; i < 3; i++ {
		if code, _ := x.post("/verify", map[string]string{"challengeId": id, "code": "bad"}, "10.0.0.1"); code != 400 {
			t.Fatalf("attempt %d: %d", i, code)
		}
	}
	before := x.g.verify.Load()
	if code, _ := x.post("/verify", map[string]string{"challengeId": id, "code": "123456"}, "10.0.0.1"); code != 400 {
		t.Fatalf("exhausted challenge must fail even with the right code, got %d", code)
	}
	if x.g.verify.Load() != before {
		t.Fatal("gate must not be consulted once attempts are exhausted")
	}

	// 过期
	x.s.SetChallengeLimits(60*time.Millisecond, 5)
	_, r = x.login("10.0.0.1")
	id = challengeID(t, r)
	time.Sleep(120 * time.Millisecond)
	if code, _ := x.post("/verify", map[string]string{"challengeId": id, "code": "123456"}, "10.0.0.1"); code != 400 {
		t.Fatalf("expired challenge must fail, got %d", code)
	}
}

func TestConcurrentVerifyIssuesOnce(t *testing.T) {
	x := newExtEnv(t)
	x.g.require.Store(true)
	_ = x.s.RegisterLoginGate(x.g)
	_, r := x.login("10.0.0.1")
	id := challengeID(t, r)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code, _ := x.post("/verify", map[string]string{"challengeId": id, "code": "123456"}, "10.0.0.1"); code == 200 {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() > 1 {
		t.Fatalf("challenge consumed %d times", ok.Load())
	}
}

func TestRefreshDoesNotTriggerGate(t *testing.T) {
	x := newExtEnv(t)
	_ = x.s.RegisterLoginGate(x.g) // 先放行，拿到一对令牌
	_, r := x.login("10.0.0.1")
	rt, _ := r.Data["refreshToken"].(string)
	if rt == "" {
		t.Fatalf("resp=%+v", r)
	}
	x.g.require.Store(true)
	checks := x.g.checks.Load()
	code, r := x.post("/refresh", map[string]string{"refreshToken": rt}, "10.0.0.1")
	if code != 200 || r.Data["accessToken"] == nil {
		t.Fatalf("refresh: %d %+v", code, r)
	}
	if x.g.checks.Load() != checks {
		t.Fatal("refresh must not consult the login gate")
	}
}

func TestPreLoginDeniesAllEntrancesWithout403Detail(t *testing.T) {
	x := newExtEnv(t)
	x.g.require.Store(true)
	_ = x.s.RegisterLoginGate(x.g)
	var calls atomic.Int32
	x.s.RegisterPreLogin(func(_ context.Context, ip, _ string) error {
		calls.Add(1)
		if ip == "6.6.6.6" {
			return errors.New("ip 6.6.6.6 matches blacklist rule #12")
		}
		return nil
	})
	for _, c := range []struct {
		path string
		body any
	}{
		{"/login", map[string]string{"type": "password", "username": "carol", "password": "right"}},
		{"/login", map[string]string{"type": "phone", "phone": "13800000000", "code": "123456"}},
		{"/verify", map[string]string{"challengeId": strings.Repeat("a", 64), "code": "123456"}},
	} {
		b, _ := json.Marshal(c.body)
		req := httptest.NewRequest(http.MethodPost, c.path, bytes.NewReader(b))
		req.RemoteAddr = "6.6.6.6:1"
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		x.e.ServeHTTP(w, req)
		if w.Code != 403 || strings.Contains(w.Body.String(), "blacklist") || strings.Contains(w.Body.String(), "6.6.6.6") {
			t.Fatalf("%s: code=%d body=%s", c.path, w.Code, w.Body.String())
		}
	}
	if x.g.checks.Load() != 0 {
		t.Fatal("blocked requests must not reach the gate")
	}
	_, err := x.s.LoginAs(context.Background(), LoginAsRequest{Identity: ExternalIdentity{Username: "carol"}, Method: "cas", IP: "6.6.6.6"})
	var re *response.Error
	if !errors.As(err, &re) || re.Status != 403 {
		t.Fatalf("LoginAs must be blocked with 403: %v", err)
	}
	if code, _ := x.login("1.2.3.4"); code != 200 {
		t.Fatalf("other IPs unaffected, got %d", code)
	}
}

func TestLoginAsGoesThroughTenantGateAndLog(t *testing.T) {
	x := newExtEnv(t)
	ctx := context.Background()
	out, err := x.s.LoginAs(ctx, LoginAsRequest{Identity: ExternalIdentity{Username: "carol"}, Method: "cas", IP: "1.1.1.1"})
	if err != nil || out.Challenge != nil || out.Pair.AccessToken == "" {
		t.Fatalf("no gate: out=%+v err=%v", out, err)
	}
	x.g.require.Store(true)
	_ = x.s.RegisterLoginGate(x.g)
	out, err = x.s.LoginAs(ctx, LoginAsRequest{Identity: ExternalIdentity{Username: "carol"}, Method: "cas", IP: "1.1.1.1"})
	if err != nil || out.Challenge == nil || out.Pair.AccessToken != "" {
		t.Fatalf("gate must withhold the token: out=%+v err=%v", out, err)
	}
	if _, err := x.s.LoginAs(ctx, LoginAsRequest{Identity: ExternalIdentity{Username: "carol"}, Method: "password"}); err == nil {
		t.Fatal("built-in method names must be rejected")
	}
	if _, err := x.s.LoginAs(ctx, LoginAsRequest{Identity: ExternalIdentity{Username: "mallory"}, Method: "cas"}); err == nil {
		t.Fatal("unmappable identity must fail")
	}
	want := []string{"cas/", "cas/", "cas/other"}
	got := x.p.methods()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("login log = %v want %v", got, want)
	}
}

type fakeExt struct {
	name  string
	id    *ExternalIdentity
	err   error
	calls atomic.Int32
}

func (f *fakeExt) Name() string { return f.name }
func (f *fakeExt) Authenticate(_ context.Context, u, p string) (*ExternalIdentity, error) {
	f.calls.Add(1)
	if p != "ldap-pw" {
		return nil, f.err
	}
	return f.id, nil
}

func TestExternalAuthenticatorChainOrderAndValidation(t *testing.T) {
	s := &Service{}
	first := &fakeExt{name: "ldap", err: ErrExternalRejected}
	second := &fakeExt{name: "corpldap", id: &ExternalIdentity{Username: "carol"}}
	for _, a := range []ExternalAuthenticator{first, second} {
		if err := s.RegisterExternalAuthenticator(a); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{"password", "phone", "github", "Bad Name", ""} {
		if err := s.RegisterExternalAuthenticator(&fakeExt{name: bad}); err == nil {
			t.Fatalf("name %q must be rejected", bad)
		}
	}
	if err := s.RegisterExternalAuthenticator(&fakeExt{name: "ldap"}); err == nil {
		t.Fatal("duplicate must be rejected")
	}
	var logged []string
	m, id := s.TryExternal(context.Background(), "carol", "ldap-pw", func(m string, err error) { logged = append(logged, m) })
	if m != "corpldap" || id == nil || first.calls.Load() != 1 || len(logged) != 0 {
		t.Fatalf("m=%q id=%v logged=%v", m, id, logged)
	}
	first.err = errors.New("dial tcp: timeout") // 校验器出错：记日志、继续下一个
	if m, id = s.TryExternal(context.Background(), "carol", "wrong", func(m string, err error) { logged = append(logged, m) }); id != nil || m != "" || len(logged) != 1 {
		t.Fatalf("m=%q id=%v logged=%v", m, id, logged)
	}
}

func TestRegistrationIsRaceFree(t *testing.T) {
	x := newExtEnv(t)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			x.s.RegisterPreLogin(func(context.Context, string, string) error { return nil })
		}()
		go func() {
			defer wg.Done()
			x.login("10.0.0.1")
		}()
	}
	wg.Wait()
}

func TestGateErrorFailsClosed(t *testing.T) {
	x := newExtEnv(t)
	_ = x.s.RegisterLoginGate(errGate{})
	code, r := x.login("10.0.0.1")
	if code == 200 || r.Data["accessToken"] != nil {
		t.Fatalf("gate failure must not issue tokens: %d %+v", code, r)
	}
}

type errGate struct{}

func (errGate) Name() string { return "broken" }
func (errGate) Check(context.Context, GateInput) (*GateRequirement, error) {
	return nil, errors.New("backend down")
}
func (errGate) Verify(context.Context, GateChallenge, GateAnswer) error { return nil }
