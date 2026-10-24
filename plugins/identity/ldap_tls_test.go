package identity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLDAPSAndStartTLSVerifyCertificates(t *testing.T) {
	caPEM, tcfg := testCerts(t)
	otherCA, _ := testCerts(t) // 另一张不相关的 CA
	for _, mode := range []string{"ldaps", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			srv := newTestLDAP(t, testUsers, tcfg, mode == "ldaps")
			cfg := baseCfg(srv)
			cfg.StartTLS = mode == "starttls"

			// 默认校验：自签证书必须被拒绝（没有“跳过校验”的开关）
			if c, err := dialLDAP(context.Background(), cfg); err == nil {
				_ = c.Close()
				t.Fatal("self-signed certificate must be rejected by default")
			} else if !strings.Contains(err.Error(), "certificate") && !strings.Contains(err.Error(), "x509") {
				t.Fatalf("unexpected error: %v", err)
			}
			// 配了不相关的 CA：仍然拒绝
			cfg.CACert = otherCA
			if c, err := dialLDAP(context.Background(), cfg); err == nil {
				_ = c.Close()
				t.Fatal("certificate signed by a different CA must be rejected")
			}
			// 配了签发它的 CA：通过，且能完成 bind
			cfg.CACert = caPEM
			c, err := dialLDAP(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if err := c.Bind(cfg.BindDN, "svc-secret"); err != nil {
				t.Fatalf("bind over TLS: %v", err)
			}
		})
	}
}

func TestStartTLSRefusedFailsClosed(t *testing.T) {
	srv := newTestLDAP(t, testUsers, nil, false) // 服务端不支持 StartTLS
	cfg := baseCfg(srv)
	cfg.StartTLS = true
	if c, err := dialLDAP(context.Background(), cfg); err == nil {
		_ = c.Close()
		t.Fatal("StartTLS refusal must not silently fall back to plaintext")
	}
}

func TestNewTLSConfigRejectsBadPEMAndHasNoSkipVerify(t *testing.T) {
	if _, err := newTLSConfig("x", "not a pem"); err == nil {
		t.Fatal("garbage CA must be rejected")
	}
	c, _ := newTLSConfig("ldap.example.com", "")
	if c.InsecureSkipVerify || c.ServerName != "ldap.example.com" || c.MinVersion < 0x0303 {
		t.Fatalf("unsafe TLS defaults: %+v", c)
	}
}

func TestServiceLDAPConfigNeverEchoesPassword(t *testing.T) {
	e := newLDAPEnv(t, testUsers)
	src := mustFirstSource(t, e)
	// 存库的是密文，不是明文
	if strings.Contains(src.Config, "svc-secret") || !strings.Contains(src.Config, "enc:v1:") {
		t.Fatalf("bind password must be stored encrypted, config = %s", src.Config)
	}
	list, err := e.svc.List(tctx(1))
	if err != nil {
		t.Fatal(err)
	}
	one, err := e.svc.Get(tctx(1), src.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]any{"list": list, "get": one} {
		out, _ := json.Marshal(v)
		s := string(out)
		if strings.Contains(s, "svc-secret") || strings.Contains(s, "enc:v1:") || strings.Contains(s, `"bindPassword"`) {
			t.Fatalf("%s response leaks the bind password: %s", name, s)
		}
		if !strings.Contains(s, `"hasBindPassword":true`) {
			t.Fatalf("%s response must say a password is set: %s", name, s)
		}
	}
	// 更新时留空 = 沿用旧口令；传新值 = 重新加密
	cfg := baseCfgOf(t, e)
	cfg.BindPassword = ""
	if _, err := e.svc.Save(tctx(1), src.ID, SourceForm{Name: "renamed", Enabled: true, Config: mustJSON(t, cfg)}); err != nil {
		t.Fatal(err)
	}
	after, _ := e.st.get(context.Background(), 1, src.ID)
	if after.Config != src.Config && !strings.Contains(after.Config, "enc:v1:") {
		t.Fatal("password lost on update")
	}
	if pw, err := plainBindPassword(baseCfgOf(t, e).BindPassword); err != nil || pw != "svc-secret" {
		t.Fatalf("kept password = %q, %v", pw, err)
	}
	cfg.BindPassword = "new-secret"
	if _, err := e.svc.Save(tctx(1), src.ID, SourceForm{Name: "renamed", Enabled: true, Config: mustJSON(t, cfg)}); err != nil {
		t.Fatal(err)
	}
	if pw, _ := plainBindPassword(baseCfgOf(t, e).BindPassword); pw != "new-secret" {
		t.Fatalf("rotated password = %q", pw)
	}
}

func mustFirstSource(t *testing.T, e *ldapEnv) *Source {
	t.Helper()
	srcs, _ := e.st.list(context.Background(), 1)
	if len(srcs) == 0 {
		t.Fatal("no source")
	}
	return &srcs[0]
}

func TestServiceRefusesPlaintextStorageAndBadConfig(t *testing.T) {
	initTest(t)
	svc := NewService(newMemStore(), nil)
	good := LDAPConfig{URL: "ldaps://dc.example.com", BindDN: "cn=svc,dc=x", BindPassword: "pw", BaseDN: "dc=x", UserFilter: "(uid={username})"}
	save := func(c LDAPConfig) error {
		_, err := svc.Save(tctx(1), 0, SourceForm{Type: TypeLDAP, Name: "n", Config: mustJSON(t, c)})
		return err
	}
	if err := save(good); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*LDAPConfig){
		"no placeholder":  func(c *LDAPConfig) { c.UserFilter = "(uid=admin)" },
		"broken filter":   func(c *LDAPConfig) { c.UserFilter = "(uid={username}" },
		"http url":        func(c *LDAPConfig) { c.URL = "http://x" },
		"url with path":   func(c *LDAPConfig) { c.URL = "ldap://x/dc=y" },
		"no base":         func(c *LDAPConfig) { c.BaseDN = "" },
		"dn without pass": func(c *LDAPConfig) { c.BindPassword = "" },
		"bad attr":        func(c *LDAPConfig) { c.AttrEmail = "mail)(x" },
		"bad ca":          func(c *LDAPConfig) { c.CACert = "junk" },
		"timeout":         func(c *LDAPConfig) { c.TimeoutSec = 600 },
	}
	for name, mut := range bad {
		c := good
		mut(&c)
		if err := save(c); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
	// 主密钥未初始化：宁可拒绝保存，也不落明文
	initTestNoKey()
	if err := save(good); err == nil {
		t.Fatal("must refuse to store a bind password without an encryption key")
	}
	if _, err := plainBindPassword("plaintext"); err == nil {
		t.Fatal("a plaintext stored password must not be accepted")
	}
}

func TestTestConnectionAndTestLogin(t *testing.T) {
	e := newLDAPEnv(t, testUsers)
	id := mustFirstSource(t, e).ID
	r, err := e.svc.TestConnection(tctx(1), id)
	if err != nil || !r.OK {
		t.Fatalf("connection: %+v %v", r, err)
	}
	l, err := e.svc.TestLogin(tctx(1), id, "alice", "alice-pw")
	if err != nil || !l.OK || l.DN != "uid=alice,ou=people,dc=example,dc=com" || l.Email != "alice@example.com" {
		t.Fatalf("login: %+v %v", l, err)
	}
	if l, _ = e.svc.TestLogin(tctx(1), id, "alice", "nope"); l.OK || l.Stage != "login" {
		t.Fatalf("wrong password: %+v", l)
	}
	if l, _ = e.svc.TestLogin(tctx(1), id, "ghost", "x"); l.OK || l.Stage != "search" {
		t.Fatalf("unknown user: %+v", l)
	}
	if _, err := e.svc.TestLogin(tctx(1), id, "alice", ""); err == nil {
		t.Fatal("empty password must be rejected")
	}
	// 其他租户既看不到也测不了这个源
	if _, err := e.svc.TestConnection(tctx(2), id); err == nil {
		t.Fatal("tenant 2 must not reach tenant 1's source")
	}
	if _, err := e.svc.Get(tctx(2), id); err == nil {
		t.Fatal("tenant 2 must not read tenant 1's source")
	}
	if err := e.svc.Delete(tctx(2), id); err == nil {
		t.Fatal("tenant 2 must not delete tenant 1's source")
	}
	out, _ := json.Marshal([]any{r, l})
	if strings.Contains(string(out), "alice-pw") || strings.Contains(string(out), "svc-secret") {
		t.Fatalf("test results leak a password: %s", out)
	}
}
