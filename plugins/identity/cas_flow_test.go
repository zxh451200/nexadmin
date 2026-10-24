package identity

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"nexadmin/core/auth"
)

func TestCASEndToEndLoginUsesOnlyTrustedServiceURL(t *testing.T) {
	c := newCASEnv(t)
	// 请求头里塞各种“我是 evil.example.com”：service URL 必须仍然来自配置
	w := c.get("/api/v1/plugins/identity/cas/login?source="+c.srcID.String(), "", "", map[string]string{
		"Host": "evil.example.com", "X-Forwarded-Host": "evil.example.com", "X-Forwarded-Proto": "http", "Forwarded": "host=evil.example.com", "Origin": "https://evil.example.com", "Referer": "https://evil.example.com/",
	})
	if w.Code != http.StatusFound {
		t.Fatalf("code=%d", w.Code)
	}
	loc := w.Header().Get("Location")
	if strings.Contains(loc, "evil") || !strings.HasPrefix(loc, c.cas.URL+"/cas/login?service=") {
		t.Fatalf("Location = %s", loc)
	}
	state, u := c.start(t, "source="+c.srcID.String())
	want := "https://admin.example.com/api/v1/plugins/identity/cas/callback?source=" + c.srcID.String() + "&state=" + state
	if got := u.Query().Get("service"); got != want {
		t.Fatalf("service = %q\nwant      %q", got, want)
	}

	rec := c.callback(state, "ST-good", "", state)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"accessToken":"`) || !strings.Contains(body, `location.replace("/")`) {
		t.Fatalf("callback: %d %s", rec.Code, body)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "nonce-") || rec.Header().Get("Cache-Control") != "no-store" ||
		rec.Header().Get("Referrer-Policy") != "no-referrer" || strings.Contains(rec.Header().Get("Location"), "token") {
		t.Fatalf("unsafe headers: %v", rec.Header())
	}
	// CAS 服务器收到的 service 与发起时完全一致（ticket 与 service 绑定）
	c.mu.Lock()
	last := c.services[len(c.services)-1]
	c.mu.Unlock()
	if last != want {
		t.Fatalf("validated service = %q", last)
	}
	// 签发的是 carol 的真实令牌，登录日志记 cas 方式
	tok := between(body, `"accessToken":"`, `"`)
	cl, err := c.auth.ParseAccess(context.Background(), tok)
	if err != nil || cl.Username != "carol" || cl.TenantID != 1 {
		t.Fatalf("claims=%+v err=%v", cl, err)
	}
	if got := c.prov.loginLog(); got != "cas/" {
		t.Fatalf("login log = %q", got)
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		return s[:j]
	}
	return s
}

func TestCASCallbackRejections(t *testing.T) {
	c := newCASEnv(t)
	noTokens := func(t *testing.T, what string, rec interface{ String() string }, code int) {
		t.Helper()
		if code != http.StatusForbidden || strings.Contains(rec.String(), "accessToken") {
			t.Fatalf("%s: code=%d body=%s", what, code, rec.String())
		}
	}
	st, _ := c.start(t, "source="+c.srcID.String())
	before := c.hitCount()
	if w := c.callback(st, "ST-good", "", ""); true {
		noTokens(t, "no cookie (login CSRF)", w.Body, w.Code)
	}
	if w := c.callback(st, "ST-good", "", strings.Repeat("0", 64)); true {
		noTokens(t, "wrong cookie", w.Body, w.Code)
	}
	if w := c.callback(strings.Repeat("1", 64), "ST-good", "", strings.Repeat("1", 64)); true {
		noTokens(t, "unknown state", w.Body, w.Code)
	}
	if c.hitCount() != before {
		t.Fatal("CAS must not be contacted before state is proven")
	}
	// 票据字符非法：不会去问 CAS
	if w := c.callback(st, "ST-1&service=http://evil", "", st); true {
		noTokens(t, "malformed ticket", w.Body, w.Code)
	}
	// state 一次性：失败的回调也会消耗它
	if w := c.callback(st, "ST-good", "", st); true {
		noTokens(t, "state consumed by earlier attempt", w.Body, w.Code)
	}
	// 租户不一致：A 租户发起的流程不能在 B 租户落地
	st, _ = c.start(t, "source="+c.srcID.String())
	if w := c.callback(st, "ST-good", "2", st); true {
		noTokens(t, "tenant mismatch", w.Body, w.Code)
	}
	// 各种坏票据 / 坏响应
	for _, tk := range []string{"ST-bad", "ST-xxe", "ST-big", "ST-redir", "ST-dave"} {
		st, _ = c.start(t, "source="+c.srcID.String())
		w := c.callback(st, tk, "", st)
		noTokens(t, tk, w.Body, w.Code)
	}
	// 重放成功的回调
	st, _ = c.start(t, "source="+c.srcID.String())
	if w := c.callback(st, "ST-good", "", st); w.Code != http.StatusOK {
		t.Fatalf("good login: %d", w.Code)
	}
	if w := c.callback(st, "ST-good", "", st); true {
		noTokens(t, "replay", w.Body, w.Code)
	}
	// 失败回调的页面是固定文案，不回显任何请求内容
	st, _ = c.start(t, "source="+c.srcID.String())
	w := c.callback(st, "ST-bad", "", st)
	if strings.Contains(w.Body.String(), "ST-bad") || strings.Contains(w.Body.String(), st) {
		t.Fatalf("error page echoes request data: %s", w.Body.String())
	}
}

func TestCASSourceSelectionAndNext(t *testing.T) {
	c := newCASEnv(t)
	// next 只允许站内相对路径
	for next, want := range map[string]string{"//evil.com": "/", "https://evil.com": "/", `/\evil.com`: "/", "/login?x": "/", "/system/user": "/system/user"} {
		st, _ := c.start(t, "source="+c.srcID.String()+"&next="+urlQ(next))
		w := c.callback(st, "ST-good", "", st)
		if !strings.Contains(w.Body.String(), `location.replace("`+want+`")`) {
			t.Fatalf("next=%q body=%s", next, w.Body.String())
		}
	}
	// 省略 source：恰好一个启用源时可用
	if w := c.get("/api/v1/plugins/identity/cas/login", "", "", nil); w.Code != http.StatusFound {
		t.Fatalf("single source default: %d", w.Code)
	}
	// 其他租户看不到这个源
	if w := c.get("/api/v1/plugins/identity/cas/login?source="+c.srcID.String(), "2", "", nil); w.Code != http.StatusNotFound {
		t.Fatalf("other tenant: %d", w.Code)
	}
	if w := c.get("/api/v1/plugins/identity/cas/login?source=abc", "", "", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("bad source: %d", w.Code)
	}
	// 停用后不可用
	if _, err := c.svc.SetEnabled(tctx(1), c.srcID, false); err != nil {
		t.Fatal(err)
	}
	if w := c.get("/api/v1/plugins/identity/cas/login?source="+c.srcID.String(), "", "", nil); w.Code != http.StatusNotFound {
		t.Fatalf("disabled: %d", w.Code)
	}
}

type needSecondFactor struct{}

func (needSecondFactor) Name() string { return "2fa" }
func (needSecondFactor) Check(context.Context, auth.GateInput) (*auth.GateRequirement, error) {
	return &auth.GateRequirement{Methods: []string{"totp"}}, nil
}
func (needSecondFactor) Verify(context.Context, auth.GateChallenge, auth.GateAnswer) error {
	return nil
}

func TestCASSecondFactorFailsClosedAndLogout(t *testing.T) {
	c := newCASEnv(t)
	if err := c.auth.RegisterLoginGate(needSecondFactor{}); err != nil {
		t.Fatal(err)
	}
	st, _ := c.start(t, "source="+c.srcID.String())
	w := c.callback(st, "ST-good", "", st)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "accessToken") {
		t.Fatalf("second factor required must not issue tokens: %d %s", w.Code, w.Body.String())
	}
	w = c.get("/api/v1/plugins/identity/cas/logout?source="+c.srcID.String(), "", "", map[string]string{"Host": "evil.example.com"})
	if w.Code != http.StatusFound || w.Header().Get("Location") != c.cas.URL+"/cas/logout?service=https%3A%2F%2Fadmin.example.com%2F" {
		t.Fatalf("logout: %d %s", w.Code, w.Header().Get("Location"))
	}
}

func urlQ(s string) string {
	return strings.NewReplacer("&", "%26", "?", "%3F", "#", "%23", " ", "%20").Replace(s)
}
