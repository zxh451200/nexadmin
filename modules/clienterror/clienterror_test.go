package clienterror

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/middleware"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func init() { gin.SetMode(gin.TestMode) }

// memStore 模拟 SQL 的 upsert 语义：(tenant, fingerprint) 相同则次数 +1。
type memStore struct {
	mu   sync.Mutex
	rows map[string]*ClientError
}

func newMemStore() *memStore { return &memStore{rows: map[string]*ClientError{}} }

func (m *memStore) Upsert(ctx context.Context, e *Event) error {
	tid, _ := tenant.From(ctx)
	key := tid.String() + "/" + e.Fingerprint
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[key]; ok {
		r.HitCount++
		r.LastSeenAt, r.Route, r.Username = e.At, e.Route, e.Username
		return nil
	}
	m.rows[key] = &ClientError{Route: e.Route, Message: e.Message, Stack: e.Stack, UA: e.UA, Version: e.Version,
		Username: e.Username, Fingerprint: e.Fingerprint, HitCount: 1, LastSeenAt: e.At}
	return nil
}

func (m *memStore) Purge(context.Context, time.Time) (int64, error) { return 0, nil }

func (m *memStore) all() []*ClientError {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*ClientError
	for _, r := range m.rows {
		out = append(out, r)
	}
	return out
}

func tctx(id idgen.ID) context.Context { return tenant.With(context.Background(), id) }

func TestScrubRedactsSecrets(t *testing.T) {
	secrets := map[string]string{
		"authorization header": "Request failed. Authorization: Bearer abcdef1234567890abcdef",
		"bearer alone":         "got Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl in fetch",
		"jwt alone":            "token was eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl",
		"password kv":          "login failed password=hunter2 for user",
		"password json":        `{"username":"bob","password":"hunter2"}`,
		"token in url":         "GET /api/x?access_token=SECRETVALUE&x=1 failed",
		"refresh json":         `{"refreshToken":"SECRETVALUE"}`,
		"api key":              "api_key: SECRETVALUE",
		"cookie":               "cookie=SECRETVALUE; other=1",
		"url userinfo":         "connect https://admin:SECRETVALUE@db.internal/x failed",
		"mixed case":           "PASSWORD=SECRETVALUE",
	}
	for name, in := range secrets {
		out := Scrub(in)
		for _, leak := range []string{"hunter2", "SECRETVALUE", "abcdef1234567890abcdef", "c2lnbmF0dXJl"} {
			if strings.Contains(out, leak) {
				t.Errorf("%s: leaked %q in %q", name, leak, out)
			}
		}
		if !strings.Contains(out, redacted) {
			t.Errorf("%s: no redaction marker in %q", name, out)
		}
	}
	// 普通错误信息不应被破坏
	for _, ok := range []string{
		"Unexpected token '<', \"<!doctype \"... is not valid JSON",
		"TypeError: Cannot read properties of undefined (reading 'map')",
		"at render (http://localhost/assets/index-AbCd1234.js:10:5)",
	} {
		if got := Scrub(ok); got != ok {
			t.Errorf("benign text changed:\n in: %q\nout: %q", ok, got)
		}
	}
	// 键名保留，便于排查
	if got := Scrub("password=hunter2"); got != "password="+redacted {
		t.Errorf("Scrub kept shape = %q", got)
	}
}

func TestSanitizeLimits(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "at fn"+strings.Repeat("x", 10)+" (file.js:1:1)")
	}
	r := Sanitize(Report{
		Route:   "/users/42?token=SECRET#frag",
		Message: strings.Repeat("错", 2000) + "\x00\x07",
		Stack:   strings.Join(lines, "\n"),
		UA:      strings.Repeat("u", 1000),
		Version: strings.Repeat("v", 100),
	})
	if r.Route != "/users/42" {
		t.Errorf("route = %q", r.Route)
	}
	if n := len([]rune(r.Message)); n != MaxMessage || strings.ContainsAny(r.Message, "\x00\x07") {
		t.Errorf("message runes = %d", n)
	}
	if n := strings.Count(r.Stack, "\n") + 1; n != MaxStackLines {
		t.Errorf("stack lines = %d, want %d", n, MaxStackLines)
	}
	if len(r.UA) != MaxUA || len(r.Version) != MaxVersion {
		t.Errorf("ua=%d version=%d", len(r.UA), len(r.Version))
	}
	huge := Sanitize(Report{Message: "m", Stack: strings.Repeat("a", 50000)})
	if len(huge.Stack) != MaxStack {
		t.Errorf("stack len = %d, want %d", len(huge.Stack), MaxStack)
	}
}

func TestFingerprintStability(t *testing.T) {
	a := Sanitize(Report{Message: "Failed to load user 1234", Stack: "Error: x\n at load (http://h/assets/index-AbCd1234.js:10:5)\n at go (http://h/assets/vendor-Zz99Yy88.js:1:2)"})
	b := Sanitize(Report{Message: "Failed to load user 98765", Stack: "Error: x\n at load (http://h/assets/index-QqWwEeRr.js:77:9)\n at go (http://h/assets/vendor-Aa11Bb22.js:3:4)", Route: "/other"})
	c := Sanitize(Report{Message: "Different failure", Stack: a.Stack})
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("same bug (different ids / line numbers / chunk hashes / route) must share a fingerprint")
	}
	if Fingerprint(a) == Fingerprint(c) {
		t.Error("different messages must not share a fingerprint")
	}
	if len(Fingerprint(a)) != 32 {
		t.Errorf("fingerprint length = %d", len(Fingerprint(a)))
	}
}

func TestIngestAggregatesByFingerprint(t *testing.T) {
	st := newMemStore()
	s := newService(st, nil)
	ctx := tctx(1)
	for i := 0; i < 50; i++ {
		rep := Report{Message: "Cannot read x of undefined (item " + strings.Repeat("9", i%5+1) + ")", Stack: "Error\n at a (f.js:1:1)", Route: "/r"}
		if err := s.Ingest(ctx, 7, "bob", rep, "UA/1"); err != nil {
			t.Fatal(err)
		}
	}
	rows := st.all()
	if len(rows) != 1 || rows[0].HitCount != 50 {
		t.Fatalf("rows=%d, hit=%v; want 1 row with 50 hits", len(rows), rows)
	}
	if rows[0].UA != "UA/1" {
		t.Errorf("header UA fallback = %q", rows[0].UA)
	}
	// 另一个租户相同错误：独立一行
	if err := s.Ingest(tctx(2), 0, "", Report{Message: "Cannot read x of undefined (item 9)", Stack: "Error\n at a (f.js:1:1)"}, ""); err != nil {
		t.Fatal(err)
	}
	if len(st.all()) != 2 {
		t.Errorf("tenants must not share rows, got %d", len(st.all()))
	}
	if err := s.Ingest(ctx, 0, "", Report{Message: "   "}, ""); err != ErrEmpty {
		t.Errorf("empty message err = %v", err)
	}
}

func TestIngestNeverStoresSecrets(t *testing.T) {
	st := newMemStore()
	s := newService(st, nil)
	err := s.Ingest(tctx(1), 1, "bob", Report{
		Message: "request failed Authorization: Bearer abcdef1234567890abcdef", Route: "/a?token=SECRETVALUE",
		Stack: "Error\n at f (http://h/?password=SECRETVALUE)",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(st.all())
	if bytes.Contains(b, []byte("SECRETVALUE")) || bytes.Contains(b, []byte("abcdef1234567890abcdef")) {
		t.Errorf("secret persisted: %s", b)
	}
}

func TestServerThrottle(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := newService(newMemStore(), nil)
	s.throttle = newThrottle(3, time.Minute)
	s.throttle.now = func() time.Time { return now }
	ctx := tctx(1)
	rep := Report{Message: "boom"}
	for i := 0; i < 3; i++ {
		if err := s.Ingest(ctx, 0, "", rep, ""); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if err := s.Ingest(ctx, 0, "", rep, ""); err != ErrThrottled {
		t.Errorf("4th call err = %v, want ErrThrottled", err)
	}
	if err := s.Ingest(tctx(2), 0, "", rep, ""); err != nil {
		t.Errorf("other tenant should not be throttled: %v", err)
	}
	now = now.Add(61 * time.Second)
	if err := s.Ingest(ctx, 0, "", rep, ""); err != nil {
		t.Errorf("after window: %v", err)
	}
}

// newTestEngine 按真实装配方式挂路由：限流规则 client_error + Report handler，租户由测试中间件写入。
func newTestEngine(t *testing.T, st Store, limit int) *gin.Engine {
	t.Helper()
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	cfg := config.RateLimit{Enabled: true, ClientError: config.Rule{Limit: limit, Window: time.Minute}}
	e := gin.New()
	_ = e.SetTrustedProxies(nil)
	e.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 1))
		c.Next()
	})
	h := &Handler{svc: newService(st, nil)}
	e.POST("/api/v1/client-errors", middleware.RateLimitRule(ch, nil, cfg, "client_error", middleware.ByIP()), h.Report)
	return e
}

func post(e *gin.Engine, body []byte, contentLength int64) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/client-errors", bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = contentLength
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func TestReportRejectsOversizedPayload(t *testing.T) {
	st := newMemStore()
	e := newTestEngine(t, st, 100)
	big, _ := json.Marshal(Report{Message: "x", Stack: strings.Repeat("a", 20*1024)})

	// 声明长度超限：直接 413
	if w := post(e, big, int64(len(big))); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("declared oversize = %d, want 413", w.Code)
	}
	// 谎报长度 / 分块（-1）：读取到上限时 413
	if w := post(e, big, -1); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("undeclared oversize = %d, want 413", w.Code)
	}
	if w := post(e, big, 10); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("lying content-length = %d, want 413", w.Code)
	}
	if n := len(st.all()); n != 0 {
		t.Errorf("oversized payload stored %d rows", n)
	}
	// 刚好在上限内的正常请求可以通过
	ok, _ := json.Marshal(Report{Message: "fine", Stack: strings.Repeat("a", 3000)})
	if len(ok) > MaxBody {
		t.Fatal("test payload too large")
	}
	if w := post(e, ok, int64(len(ok))); w.Code != http.StatusOK {
		t.Errorf("normal payload = %d: %s", w.Code, w.Body.String())
	}
	if n := len(st.all()); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestReportBadInput(t *testing.T) {
	e := newTestEngine(t, newMemStore(), 100)
	if w := post(e, []byte("{not json"), 9); w.Code != http.StatusBadRequest {
		t.Errorf("bad json = %d", w.Code)
	}
	b, _ := json.Marshal(Report{Message: ""})
	if w := post(e, b, int64(len(b))); w.Code != http.StatusBadRequest {
		t.Errorf("empty message = %d", w.Code)
	}
}

func TestReportRateLimitedPerIP(t *testing.T) {
	st := newMemStore()
	e := newTestEngine(t, st, 3)
	b, _ := json.Marshal(Report{Message: "boom"})
	codes := make([]int, 0, 5)
	for i := 0; i < 5; i++ {
		codes = append(codes, post(e, b, int64(len(b))).Code)
	}
	want := []int{200, 200, 200, http.StatusTooManyRequests, http.StatusTooManyRequests}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("codes = %v, want %v", codes, want)
		}
	}
	if rows := st.all(); len(rows) != 1 || rows[0].HitCount != 3 {
		t.Errorf("rate-limited requests must not reach the store: %+v", rows)
	}
	// 另一个 IP 不受影响
	req := httptest.NewRequest(http.MethodPost, "/api/v1/client-errors", bytes.NewReader(b))
	req.RemoteAddr = "198.51.100.7:1"
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("other ip = %d", w.Code)
	}
}

func TestDefaultRuleIsConfiguredAs30PerMinute(t *testing.T) {
	// default.yaml 里 client_error 规则需要被 RateLimit.Rule 认出来（未知规则名返回零值 = 不限流）。
	rl := config.RateLimit{Enabled: true, ClientError: config.Rule{Limit: 30, Window: time.Minute}}
	if r := rl.Rule("client_error"); r.Limit != 30 || r.Window != time.Minute {
		t.Errorf("Rule(client_error) = %+v", r)
	}
	if r := (config.RateLimit{ClientError: config.Rule{Limit: 30}}).Rule("client_error"); r.Limit != 0 {
		t.Errorf("master switch off must disable the rule, got %+v", r)
	}
}
