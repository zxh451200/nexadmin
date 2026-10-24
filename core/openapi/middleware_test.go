package openapi

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/middleware"
	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

type fakeStore struct {
	mu       sync.Mutex
	pats     map[string]*PAT
	owners   map[idgen.ID]*Owner
	apps     map[string]*App
	perms    []string
	patTouch int
	appTouch int
}

func newFakeStore() *fakeStore {
	return &fakeStore{pats: map[string]*PAT{}, owners: map[idgen.ID]*Owner{}, apps: map[string]*App{}}
}

func (f *fakeStore) PATByHash(_ context.Context, h string) (*PAT, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pats[h], nil
}
func (f *fakeStore) OwnerOf(_ context.Context, id idgen.ID) (*Owner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.owners[id], nil
}
func (f *fakeStore) AppByKey(_ context.Context, k string) (*App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.apps[k], nil
}
func (f *fakeStore) AppPerms(context.Context, *App) ([]string, error) { return f.perms, nil }
func (f *fakeStore) TouchPAT(context.Context, idgen.ID, time.Time) error {
	f.mu.Lock()
	f.patTouch++
	f.mu.Unlock()
	return nil
}
func (f *fakeStore) TouchApp(context.Context, idgen.ID, time.Time) error {
	f.mu.Lock()
	f.appTouch++
	f.mu.Unlock()
	return nil
}

const testSecret = "s3cr3t-value"

type harness struct {
	t     *testing.T
	st    *fakeStore
	cache cache.Cache
	eng   *gin.Engine
	now   time.Time
	lim   *middleware.Limiter
}

func newHarness(t *testing.T, limit int) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	key := bytes.Repeat([]byte{7}, 32)
	c, err := secret.New(key)
	if err != nil {
		t.Fatal(err)
	}
	secret.SetDefault(c)
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	h := &harness{t: t, st: newFakeStore(), cache: ch, now: time.Now()}
	if limit > 0 {
		h.lim = middleware.NewLimiter(ch, nil, "openapi-sign-test", limit, time.Minute)
	}
	h.eng = gin.New()
	_ = h.eng.SetTrustedProxies(nil)
	h.eng.Use(Authenticate(Options{Store: h.st, Cache: ch, Limiter: h.lim, Now: func() time.Time { return h.now }}))
	ok := func(c *gin.Context) {
		cl := auth.ClaimsFrom(c)
		if cl == nil {
			c.String(200, "anon")
			return
		}
		c.String(200, cl.Principal+"|"+cl.Username+"|"+strconv.FormatBool(cl.Super)+"|"+strings.Join(cl.Perms, ","))
	}
	h.eng.Any("/api/v1/*any", ok)
	return h
}

func (h *harness) do(method, target string, body []byte, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.9:1234"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.eng.ServeHTTP(w, req)
	return w
}

func (h *harness) addPAT(scope string, expires *time.Time) string {
	plain, hash, prefix := NewPATToken()
	p := &PAT{UserID: 11, Name: "t", Scope: scope, TokenHash: hash, Prefix: prefix, ExpiresAt: expires}
	p.ID, p.TenantID = 100, 1
	h.st.pats[hash] = p
	h.st.owners[11] = &Owner{ID: 11, TenantID: 1, Username: "alice"}
	return plain
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

func TestPATRecognizedAsOwner(t *testing.T) {
	h := newHarness(t, 0)
	tok := h.addPAT("write", nil)
	w := h.do("GET", "/api/v1/users", nil, bearer(tok))
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "pat|alice|false|") {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	if w := h.do("GET", "/api/v1/users", nil, bearer("nxp_"+strings.Repeat("a", 32))); w.Code != 401 {
		t.Fatalf("unknown token should be 401, got %d", w.Code)
	}
	if w := h.do("GET", "/api/v1/users", nil, bearer("nxp_short")); w.Code != 401 {
		t.Fatalf("malformed token should be 401, got %d", w.Code)
	}
}

func TestPATScopeRead(t *testing.T) {
	h := newHarness(t, 0)
	tok := h.addPAT("read", nil)
	for _, m := range []string{"GET", "HEAD"} {
		if w := h.do(m, "/api/v1/users", nil, bearer(tok)); w.Code != 200 {
			t.Fatalf("%s with read scope = %d", m, w.Code)
		}
	}
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if w := h.do(m, "/api/v1/users", nil, bearer(tok)); w.Code != 403 {
			t.Fatalf("%s with read scope = %d, want 403", m, w.Code)
		}
	}
	wtok := h.addPAT("write", nil)
	if w := h.do("POST", "/api/v1/users", nil, bearer(wtok)); w.Code != 200 {
		t.Fatalf("POST with write scope = %d", w.Code)
	}
}

func TestPATExpiredDeletedOwnerDisabled(t *testing.T) {
	h := newHarness(t, 0)
	past := h.now.Add(-time.Second)
	if w := h.do("GET", "/api/v1/x", nil, bearer(h.addPAT("write", &past))); w.Code != 401 {
		t.Fatalf("expired = %d", w.Code)
	}
	future := h.now.Add(time.Hour)
	tok := h.addPAT("write", &future)
	if w := h.do("GET", "/api/v1/x", nil, bearer(tok)); w.Code != 200 {
		t.Fatalf("not yet expired = %d", w.Code)
	}
	h.now = h.now.Add(2 * time.Hour)
	if w := h.do("GET", "/api/v1/x", nil, bearer(tok)); w.Code != 401 {
		t.Fatalf("after expiry = %d", w.Code)
	}
	// 删除
	tok2 := h.addPAT("write", nil)
	delete(h.st.pats, HashToken(tok2))
	if w := h.do("GET", "/api/v1/x", nil, bearer(tok2)); w.Code != 401 {
		t.Fatalf("deleted = %d", w.Code)
	}
	// 所有者被停用/删除（OwnerOf 返回 nil）
	tok3 := h.addPAT("write", nil)
	delete(h.st.owners, 11)
	if w := h.do("GET", "/api/v1/x", nil, bearer(tok3)); w.Code != 401 {
		t.Fatalf("owner disabled = %d", w.Code)
	}
}

func TestPATCannotManageTokensOrLogin(t *testing.T) {
	h := newHarness(t, 0)
	tok := h.addPAT("write", nil)
	for _, c := range []struct{ m, p string }{
		{"GET", "/api/v1/profile/tokens"}, {"POST", "/api/v1/profile/tokens"}, {"DELETE", "/api/v1/profile/tokens/123"},
		{"POST", "/api/v1/auth/logout"}, {"GET", "/api/v1/auth/me"}, {"POST", "/api/v1/oauth/authorize"},
		{"GET", "/api/v1//profile/tokens"}, {"GET", "/api/v1/profile/../profile/tokens"},
	} {
		if w := h.do(c.m, c.p, nil, bearer(tok)); w.Code != 403 {
			t.Fatalf("%s %s = %d, want 403", c.m, c.p, w.Code)
		}
	}
	if w := h.do("GET", "/api/v1/profile", nil, bearer(tok)); w.Code != 200 {
		t.Fatalf("other profile endpoints stay usable, got %d", w.Code)
	}
}

func TestPATLastUsedThrottled(t *testing.T) {
	h := newHarness(t, 0)
	tok := h.addPAT("write", nil)
	for i := 0; i < 5; i++ {
		h.do("GET", "/api/v1/x", nil, bearer(tok))
	}
	if h.st.patTouch != 1 {
		t.Fatalf("last_used_at written %d times within a minute, want 1", h.st.patTouch)
	}
}

func TestFailedPATsAreRateLimitedByIP(t *testing.T) {
	h := newHarness(t, 3)
	bad := "nxp_" + strings.Repeat("b", 32)
	for i := 0; i < 3; i++ {
		if w := h.do("GET", "/api/v1/x", nil, bearer(bad)); w.Code != 401 {
			t.Fatalf("attempt %d = %d", i, w.Code)
		}
	}
	if w := h.do("GET", "/api/v1/x", nil, bearer(bad)); w.Code != 429 {
		t.Fatalf("after limit = %d, want 429", w.Code)
	}
}

// ---- 应用签名 ----

func (h *harness) addApp(ip []string) *App {
	enc, err := secret.Encrypt(testSecret)
	if err != nil {
		h.t.Fatal(err)
	}
	a := &App{Name: "svc", AppKey: "nxapp_test", SecretEnc: enc, Enabled: true, RoleIDs: EncodeList([]string{"5"}), IPAllow: EncodeList(ip)}
	a.ID, a.TenantID = 200, 1
	h.st.apps[a.AppKey] = a
	h.st.perms = []string{"user:list"}
	return a
}

func (h *harness) signed(method, target string, body []byte, nonce string, ts time.Time, mutate func(map[string]string)) *httptest.ResponseRecorder {
	u := httptest.NewRequest(method, target, nil).URL
	tsStr := strconv.FormatInt(ts.Unix(), 10)
	sig, err := Sign(testSecret, method, u.EscapedPath(), u.RawQuery, body, tsStr, nonce)
	if err != nil {
		h.t.Fatal(err)
	}
	hdr := map[string]string{"X-App-Key": "nxapp_test", "X-Timestamp": tsStr, "X-Nonce": nonce, "X-Signature": sig}
	if mutate != nil {
		mutate(hdr)
	}
	return h.do(method, target, body, hdr)
}

func TestAppSignatureValid(t *testing.T) {
	h := newHarness(t, 0)
	h.addApp(nil)
	w := h.signed("POST", "/api/v1/users?b=2&a=1&a=0", []byte(`{"x":1}`), "n-1", h.now, nil)
	if w.Code != 200 || w.Body.String() != "app|app:nxapp_test|false|user:list" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	// 服务端按规范化 query 校验：客户端把参数顺序换一下，签名同样有效
	u := "/api/v1/users?a=0&a=1&b=2"
	sig, _ := Sign(testSecret, "GET", "/api/v1/users", "b=2&a=1&a=0", nil, strconv.FormatInt(h.now.Unix(), 10), "n-2")
	w = h.do("GET", u, nil, map[string]string{"X-App-Key": "nxapp_test", "X-Timestamp": strconv.FormatInt(h.now.Unix(), 10), "X-Nonce": "n-2", "X-Signature": sig})
	if w.Code != 200 {
		t.Fatalf("query order must not matter, got %d", w.Code)
	}
}

func TestAppSignatureWrong(t *testing.T) {
	h := newHarness(t, 0)
	h.addApp(nil)
	cases := map[string]func(map[string]string){
		"bad signature": func(m map[string]string) { m["X-Signature"] = strings.Repeat("0", 64) },
		"non-hex":       func(m map[string]string) { m["X-Signature"] = "zzzz" },
		"wrong key":     func(m map[string]string) { m["X-App-Key"] = "nope" },
		"nonce swapped": func(m map[string]string) { m["X-Nonce"] = "other" },
		"missing sig":   func(m map[string]string) { delete(m, "X-Signature") },
	}
	i := 0
	for name, mut := range cases {
		i++
		if w := h.signed("GET", "/api/v1/users", nil, "nonce-"+strconv.Itoa(i), h.now, mut); w.Code != 401 {
			t.Fatalf("%s: got %d, want 401", name, w.Code)
		}
	}
	// 篡改请求体
	ts := strconv.FormatInt(h.now.Unix(), 10)
	sig, _ := Sign(testSecret, "POST", "/api/v1/users", "", []byte(`{"a":1}`), ts, "nb")
	w := h.do("POST", "/api/v1/users", []byte(`{"a":2}`), map[string]string{"X-App-Key": "nxapp_test", "X-Timestamp": ts, "X-Nonce": "nb", "X-Signature": sig})
	if w.Code != 401 {
		t.Fatalf("tampered body = %d", w.Code)
	}
	// 篡改 method / path
	sig, _ = Sign(testSecret, "GET", "/api/v1/users", "", nil, ts, "nc")
	w = h.do("DELETE", "/api/v1/users", nil, map[string]string{"X-App-Key": "nxapp_test", "X-Timestamp": ts, "X-Nonce": "nc", "X-Signature": sig})
	if w.Code != 401 {
		t.Fatalf("tampered method = %d", w.Code)
	}
}

func TestAppTimestampWindow(t *testing.T) {
	h := newHarness(t, 0)
	h.addApp(nil)
	if w := h.signed("GET", "/api/v1/users", nil, "t1", h.now.Add(-4*time.Minute), nil); w.Code != 200 {
		t.Fatalf("-4m should pass, got %d", w.Code)
	}
	if w := h.signed("GET", "/api/v1/users", nil, "t2", h.now.Add(4*time.Minute), nil); w.Code != 200 {
		t.Fatalf("+4m should pass, got %d", w.Code)
	}
	if w := h.signed("GET", "/api/v1/users", nil, "t3", h.now.Add(-6*time.Minute), nil); w.Code != 401 {
		t.Fatalf("-6m should be rejected, got %d", w.Code)
	}
	if w := h.signed("GET", "/api/v1/users", nil, "t4", h.now.Add(6*time.Minute), nil); w.Code != 401 {
		t.Fatalf("+6m should be rejected, got %d", w.Code)
	}
}

func TestAppReplayRejected(t *testing.T) {
	h := newHarness(t, 0)
	h.addApp(nil)
	ts := strconv.FormatInt(h.now.Unix(), 10)
	sig, _ := Sign(testSecret, "GET", "/api/v1/users", "", nil, ts, "same")
	hdr := map[string]string{"X-App-Key": "nxapp_test", "X-Timestamp": ts, "X-Nonce": "same", "X-Signature": sig}
	if w := h.do("GET", "/api/v1/users", nil, hdr); w.Code != 200 {
		t.Fatalf("first = %d", w.Code)
	}
	if w := h.do("GET", "/api/v1/users", nil, hdr); w.Code != 401 {
		t.Fatalf("replay = %d, want 401", w.Code)
	}
}

func TestAppIPAllowAndDisabled(t *testing.T) {
	h := newHarness(t, 0)
	a := h.addApp([]string{"198.51.100.0/24", "192.0.2.7"})
	if w := h.signed("GET", "/api/v1/users", nil, "ip1", h.now, nil); w.Code != 401 {
		t.Fatalf("ip outside allow list = %d", w.Code)
	}
	a.IPAllow = EncodeList([]string{"203.0.113.0/24"})
	if w := h.signed("GET", "/api/v1/users", nil, "ip2", h.now, nil); w.Code != 200 {
		t.Fatalf("ip inside CIDR = %d", w.Code)
	}
	a.Enabled = false
	if w := h.signed("GET", "/api/v1/users", nil, "ip3", h.now, nil); w.Code != 401 {
		t.Fatalf("disabled app = %d", w.Code)
	}
}

func TestAppCannotUseLoginOrAppAdminEndpoints(t *testing.T) {
	h := newHarness(t, 0)
	h.addApp(nil)
	for i, p := range []string{"/api/v1/auth/me", "/api/v1/auth/logout", "/api/v1/profile", "/api/v1/profile/tokens", "/api/v1/openapi/apps", "/api/v1/openapi/apps/1/reset-secret", "/api/v1/oauth/authorize"} {
		if w := h.signed("POST", p, nil, "f"+strconv.Itoa(i), h.now, nil); w.Code != 403 {
			t.Fatalf("%s = %d, want 403", p, w.Code)
		}
	}
	if w := h.signed("GET", "/api/v1/openapi/webhooks", nil, "ok1", h.now, nil); w.Code != 200 {
		t.Fatalf("other openapi endpoints stay usable, got %d", w.Code)
	}
}

func TestAppPermEnforcedByPermMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	run := func(perms []string, need string) int {
		e := gin.New()
		e.Use(func(c *gin.Context) {
			auth.SetClaims(c, &auth.Claims{UserID: 9, Username: "app:k", Principal: auth.PrincipalApp, Perms: perms})
		})
		e.GET("/x", middleware.Perm(need), func(c *gin.Context) { c.Status(204) })
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
		return w.Code
	}
	if c := run([]string{"user:list"}, "user:list"); c != 204 {
		t.Fatalf("granted perm = %d", c)
	}
	if c := run([]string{"user:*"}, "user:list"); c != 204 {
		t.Fatalf("wildcard perm = %d", c)
	}
	if c := run([]string{"user:list"}, "user:delete"); c != 403 {
		t.Fatalf("missing perm = %d", c)
	}
	if c := run(nil, "user:list"); c != 403 {
		t.Fatalf("no perms = %d", c)
	}
}

func TestFailedSignaturesRateLimited(t *testing.T) {
	h := newHarness(t, 2)
	h.addApp(nil)
	bad := func(m map[string]string) { m["X-Signature"] = strings.Repeat("1", 64) }
	for i := 0; i < 2; i++ {
		if w := h.signed("GET", "/api/v1/users", nil, "r"+strconv.Itoa(i), h.now, bad); w.Code != 401 {
			t.Fatalf("attempt %d = %d", i, w.Code)
		}
	}
	// 达到上限后连正确签名也先 429（按 IP 限，防止暴力猜测）
	if w := h.signed("GET", "/api/v1/users", nil, "good", h.now, nil); w.Code != 429 {
		t.Fatalf("after limit = %d, want 429", w.Code)
	}
}

func TestSessionClaimsLeftAlone(t *testing.T) {
	h := newHarness(t, 0)
	e := gin.New()
	called := false
	e.Use(func(c *gin.Context) { auth.SetClaims(c, &auth.Claims{UserID: 1, Username: "jwt-user"}) },
		Authenticate(Options{Store: h.st, Cache: h.cache}))
	e.GET("/x", func(c *gin.Context) { called = true; c.String(200, auth.ClaimsFrom(c).Username) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer nxp_"+strings.Repeat("c", 32))
	e.ServeHTTP(w, req)
	if !called || w.Body.String() != "jwt-user" {
		t.Fatalf("existing session must win, got %d %q", w.Code, w.Body.String())
	}
}

func TestIPAllowed(t *testing.T) {
	if !IPAllowed(nil, "1.2.3.4") {
		t.Fatal("empty list allows all")
	}
	l := []string{"10.0.0.0/8", "2001:db8::1", "192.0.2.5"}
	for ip, want := range map[string]bool{"10.1.2.3": true, "192.0.2.5": true, "192.0.2.6": false, "2001:db8::1": true, "2001:db8::2": false, "::ffff:10.0.0.9": true, "garbage": false} {
		if got := IPAllowed(l, ip); got != want {
			t.Fatalf("IPAllowed(%q) = %v, want %v", ip, got, want)
		}
	}
	if _, ok := ParseAllowEntry("999.1.1.1"); ok {
		t.Fatal("invalid entry accepted")
	}
}

func TestTokenFormat(t *testing.T) {
	plain, hash, prefix := NewPATToken()
	if !IsPATFormat(plain) || len(plain) != 36 || prefix != plain[:8] || hash != HashToken(plain) || len(hash) != 64 {
		t.Fatalf("bad token %q %q %q", plain, hash, prefix)
	}
	if plain == func() string { p, _, _ := NewPATToken(); return p }() {
		t.Fatal("tokens must be random")
	}
}
