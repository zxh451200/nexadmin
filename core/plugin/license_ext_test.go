package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMultiKeyAndKID(t *testing.T) {
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, privB, _ := ed25519.GenerateKey(rand.Reader)
	keys := []Key{{KID: KeyID(pubA), Pub: pubA}, {KID: KeyID(pubB), Pub: pubB}}
	future := now.Add(24 * time.Hour)

	// sign 写入 kid，且等于公钥指纹。
	rawB := signed(t, privB, "cron", future)
	var env envelope
	if err := json.Unmarshal(rawB, &env); err != nil || env.KID != KeyID(pubB) {
		t.Fatalf("kid = %q, err %v", env.KID, err)
	}
	// 多公钥：两把钥签的授权都能通过。
	for _, raw := range [][]byte{signed(t, privA, "cron", future), rawB} {
		if _, err := VerifyKeys(raw, "cron", now, keys); err != nil {
			t.Fatal(err)
		}
	}
	// kid 指向的钥不在集合里：invalid。
	if _, err := VerifyKeys(rawB, "cron", now, keys[:1]); status(err) != StatusInvalid {
		t.Fatalf("want invalid, got %v", err)
	}
	// 旧文件（无 kid）：依次尝试全部公钥。
	env.KID = ""
	legacy, _ := json.Marshal(env)
	if _, err := VerifyKeys(legacy, "cron", now, keys); err != nil {
		t.Fatalf("legacy file without kid should fall back to all keys: %v", err)
	}
	if _, err := VerifyKeys(legacy, "cron", now, keys[:1]); status(err) != StatusInvalid {
		t.Fatalf("want invalid, got %v", err)
	}
	// kid 对但签名不是该钥签的：invalid（不会回退去试别的钥）。
	var forged envelope
	_ = json.Unmarshal(signed(t, privA, "cron", future), &forged)
	forged.KID = KeyID(pubB)
	raw, _ := json.Marshal(forged)
	if _, err := VerifyKeys(raw, "cron", now, keys); status(err) != StatusInvalid {
		t.Fatalf("want invalid, got %v", err)
	}
}

func TestPublicKeysFile(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# 发行方追加的公钥\n\n" + base64Std(pub) + "  # 2027 新钥\nnot-a-key\n" + base64Std(pub) + "\n"
	if err := os.WriteFile(filepath.Join(root, "config", "license-keys.pub"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	keys := PublicKeys(root)
	if len(keys) != len(BuiltinKeys())+1 {
		t.Fatalf("want builtin+1 keys (dedup, comments, junk ignored), got %d", len(keys))
	}
	if last := keys[len(keys)-1]; !last.Pub.Equal(pub) || last.KID != KeyID(pub) {
		t.Fatalf("appended key mismatch: %+v", last)
	}
	if got := PublicKeys(t.TempDir()); len(got) != len(BuiltinKeys()) {
		t.Fatalf("missing file should yield builtin keys only, got %d", len(got))
	}
}

func TestIsDevKeyIsSetSemantics(t *testing.T) {
	if !IsDevKey() {
		t.Fatal("dev key is in the builtin set, IsDevKey must be true")
	}
	old := builtinPublicKeysB64
	defer func() { builtinPublicKeysB64 = old }()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	builtinPublicKeysB64 = []string{base64Std(pub)}
	if IsDevKey() {
		t.Fatal("set without dev key must not report dev key")
	}
	builtinPublicKeysB64 = []string{base64Std(pub), devPublicKeyB64}
	if !IsDevKey() {
		t.Fatal("set containing dev key must report dev key")
	}
}

func TestMatchDomain(t *testing.T) {
	cases := []struct {
		domains []string
		host    string
		want    bool
	}{
		{[]string{"a.com"}, "a.com", true},
		{[]string{"a.com"}, "A.com:8080", true},
		{[]string{"a.com"}, "b.a.com", false},
		{[]string{"a.com"}, "evil-a.com", false},
		{[]string{"*.example.com"}, "x.example.com", true},
		{[]string{"*.example.com"}, "x.y.example.com", true},
		{[]string{"*.example.com"}, "example.com", false},
		{[]string{"*.example.com"}, "badexample.com", false},
		{[]string{"a.com", "*.b.com"}, "z.b.com", true},
		{[]string{"a.com"}, "", false},
		{[]string{"a.com"}, "localhost", false},
	}
	for _, c := range cases {
		if got := MatchDomain(c.domains, c.host); got != c.want {
			t.Errorf("MatchDomain(%v, %q) = %v, want %v", c.domains, c.host, got, c.want)
		}
	}
}

// licenseRoot 建一个带 pro 插件 cron 和授权文件的项目根。
func licenseRoot(t *testing.T, priv ed25519.PrivateKey, domains []string, expires time.Time) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "cron")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	y := "id: cron\nname: cron\nversion: 1.0.0\nedition: pro\n"
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := Sign(priv, License{Plugin: "cron", Customer: "acme", IssuedAt: now.Format(time.RFC3339),
		ExpiresAt: expires.Format(time.RFC3339), Domains: domains})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLicense(root, "cron", raw); err != nil {
		t.Fatal(err)
	}
	return root
}

func cronInfo(m *Manager) Info { return m.List()[0] }

func TestDomainValidation(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	future := now.Add(90 * 24 * time.Hour)
	mk := func(domains []string, o Options) *Manager {
		o.Root = licenseRoot(t, priv, domains, future)
		o.PublicKey = pub
		o.Now = func() time.Time { return now }
		return newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}}, o)
	}

	t.Run("exact via config hosts", func(t *testing.T) {
		if i := cronInfo(mk([]string{"admin.acme.com"}, Options{Hosts: []string{"admin.acme.com"}})); i.Status != StatusActive {
			t.Fatalf("got %+v", i)
		}
	})
	t.Run("wildcard via config hosts", func(t *testing.T) {
		if i := cronInfo(mk([]string{"*.acme.com"}, Options{Hosts: []string{"https://admin.acme.com:8443"}})); i.Status != StatusActive {
			t.Fatalf("got %+v", i)
		}
	})
	t.Run("mismatch", func(t *testing.T) {
		i := cronInfo(mk([]string{"*.acme.com"}, Options{Hosts: []string{"other.com"}}))
		if i.Status != StatusInvalid || !strings.Contains(i.Reason, "授权域名不匹配") {
			t.Fatalf("got %+v", i)
		}
	})
	t.Run("localhost not exempt", func(t *testing.T) {
		i := cronInfo(mk([]string{"a.com"}, Options{Hosts: []string{"localhost"}}))
		if i.Status != StatusInvalid {
			t.Fatalf("got %+v", i)
		}
	})
	t.Run("allow_dev_hosts", func(t *testing.T) {
		i := cronInfo(mk([]string{"a.com"}, Options{Hosts: []string{"127.0.0.1"}, AllowDevHosts: true}))
		if i.Status != StatusActive {
			t.Fatalf("got %+v", i)
		}
	})
	t.Run("no domains in license means unrestricted", func(t *testing.T) {
		if i := cronInfo(mk(nil, Options{Hosts: []string{"whatever.com"}})); i.Status != StatusActive {
			t.Fatalf("got %+v", i)
		}
	})
	t.Run("config from yaml", func(t *testing.T) {
		root := licenseRoot(t, priv, []string{"*.acme.com"}, future)
		if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
			t.Fatal(err)
		}
		y := "sso:\n  issuer: https://sso.acme.com/api/v1/oauth\n"
		if err := os.WriteFile(filepath.Join(root, "config", "default.yaml"), []byte(y), 0o644); err != nil {
			t.Fatal(err)
		}
		m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}},
			Options{Root: root, PublicKey: pub, Now: func() time.Time { return now }})
		if i := cronInfo(m); i.Status != StatusActive {
			t.Fatalf("issuer host should be used: %+v", i)
		}
	})

	t.Run("pending until first request host", func(t *testing.T) {
		m := mk([]string{"*.acme.com"}, Options{})
		if i := cronInfo(m); i.Status != StatusActive {
			t.Fatalf("pending should stay active until host known: %+v", i)
		}
		m.ObserveHost("evil.com:80")
		if i := cronInfo(m); i.Status != StatusInvalid || !strings.Contains(i.Reason, "授权域名不匹配") {
			t.Fatalf("got %+v", i)
		}
		m.ObserveHost("admin.acme.com") // 只有第一次生效
		if i := cronInfo(m); i.Status != StatusInvalid {
			t.Fatalf("host must be decided once: %+v", i)
		}

		m2 := mk([]string{"*.acme.com"}, Options{})
		m2.ObserveHost("admin.acme.com")
		if i := cronInfo(m2); i.Status != StatusActive {
			t.Fatalf("got %+v", i)
		}
	})

	t.Run("gate returns 403 on mismatch via first request", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		m := mk([]string{"*.acme.com"}, Options{})
		r := gin.New()
		r.Group("", m.gate(m.entries["cron"])).GET("/x", func(c *gin.Context) { c.String(200, "ok") })
		req := httptest.NewRequest("GET", "/x", nil)
		req.Host = "evil.com"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d", w.Code)
		}
	})

	t.Run("import rejects domain mismatch", func(t *testing.T) {
		m := mk([]string{"*.acme.com"}, Options{Hosts: []string{"acme.com"}}) // 通配不含根域
		raw, _ := Sign(priv, License{Plugin: "cron", Customer: "acme", ExpiresAt: future.Format(time.RFC3339), Domains: []string{"*.acme.com"}})
		if _, err := m.ImportLicense("cron", string(raw)); status(err) != StatusInvalid {
			t.Fatalf("want invalid, got %v", err)
		}
	})
}

func TestExpiryOffline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cur := now
	exp := now.Add(10 * 24 * time.Hour)
	root := licenseRoot(t, priv, nil, exp)
	m := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}},
		Options{Root: root, PublicKey: pub, Now: func() time.Time { return cur }})
	e := m.entries["cron"]

	r := gin.New()
	r.Group("", m.gate(e)).GET("/x", func(c *gin.Context) { c.String(200, "ok") })
	get := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
		return w.Code
	}

	// 10 天后到期：active 且 expiresSoon。
	if i := cronInfo(m); i.Status != StatusActive || !i.ExpiresSoon {
		t.Fatalf("got %+v", i)
	}
	if get() != 200 {
		t.Fatal("active plugin must be reachable")
	}
	// 定时检查：未到期不变化；到期后标 expired，路由 403。
	m.checkExpiry()
	if cronInfo(m).Status != StatusActive {
		t.Fatal("must stay active before expiry")
	}
	cur = exp.Add(time.Second)
	m.checkExpiry()
	i := cronInfo(m)
	if i.Status != StatusExpired || i.ExpiresSoon || !strings.Contains(i.Reason, "到期") {
		t.Fatalf("got %+v", i)
	}
	if get() != http.StatusForbidden {
		t.Fatal("expired plugin routes must return 403")
	}

	// 不依赖定时器：时间越过到期点后，请求本身就会被拦。
	cur = now
	m2 := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}},
		Options{Root: root, PublicKey: pub, Now: func() time.Time { return cur }})
	r2 := gin.New()
	r2.Group("", m2.gate(m2.entries["cron"])).GET("/x", func(c *gin.Context) { c.String(200, "ok") })
	cur = exp.Add(time.Hour)
	w := httptest.NewRecorder()
	r2.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != http.StatusForbidden || cronInfo(m2).Status != StatusExpired {
		t.Fatalf("got %d %+v", w.Code, cronInfo(m2))
	}

	// 离到期超过 30 天：不提示。
	far := licenseRoot(t, priv, nil, now.Add(60*24*time.Hour))
	m3 := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}},
		Options{Root: far, PublicKey: pub, Now: func() time.Time { return now }})
	if i := cronInfo(m3); i.Status != StatusActive || i.ExpiresSoon {
		t.Fatalf("got %+v", i)
	}
}

func TestLicenseErrorMessageCarriesAction(t *testing.T) {
	e := &LicenseError{Status: StatusExpired, Reason: "授权已于 2026-01-02 到期", ActionHint: "请联系发行方续期"}
	if e.Message() != e.Reason+"；"+e.ActionHint || !strings.Contains(e.Message(), "续期") {
		t.Fatalf("expired message: %q", e.Message())
	}
	if r := revokedError(RevokedEntry{Serial: "abc123", Reason: "退款"}); r.ActionHint == "" || !strings.Contains(r.Message(), "重新签发") {
		t.Fatalf("revoked message must carry an action: %q", r.Message())
	}
	plain := &LicenseError{Status: StatusInvalid, Reason: "x"}
	if plain.Message() != "x" {
		t.Fatalf("empty hint must not add punctuation: %q", plain.Message())
	}
}
