package identity

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---- BER：恶意 / 畸形输入 ----

// decodeAll 把一段字节走一遍客户端会用到的所有解码入口；只要不 panic、不死循环、不超量分配即可。
func decodeAll(b []byte) {
	_, _, _, _ = parseTLV(b)
	_, _ = readTLV(bytes.NewReader(b), maxLDAPMessage)
	_, _ = parseEntry(b)
	_, _, _ = parseResult(b)
	_, _ = decodeInt(b)
	_, _, _ = decodeLen(b)
	_, _ = unescapeValue(string(b))
	_, _ = compileFilter(string(b))
}

func TestBERMalformedInputsNeverPanic(t *testing.T) {
	deep := bytes.Repeat([]byte{0x30, 0x80 | 0x01}, 100000) // 10 万层“嵌套”的头部，且长度字节被截断
	nested := []byte{}
	for i := 0; i < 5000; i++ {
		nested = enc(tagSeq, nested)
	}
	cases := map[string][]byte{
		"empty":                {},
		"one byte":             {0x30},
		"indefinite length":    {0x30, 0x80, 0x00, 0x00},
		"length of length 5":   {0x30, 0x85, 0xff, 0xff, 0xff, 0xff, 0xff},
		"length 4GB":           {0x30, 0x84, 0xff, 0xff, 0xff, 0xff},
		"length 2GB":           {0x30, 0x84, 0x80, 0x00, 0x00, 0x00},
		"length 0x7fffffff":    {0x30, 0x84, 0x7f, 0xff, 0xff, 0xff, 0x00},
		"length 8 bytes":       {0x30, 0x88, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		"length byte missing":  {0x30, 0x82, 0x01},
		"truncated value":      {0x30, 0x05, 0x02, 0x01},
		"high tag number":      {0x1f, 0x81, 0x01, 0x00},
		"negative-looking len": {0x04, 0x81, 0xff},
		"int too long":         {0x02, 0x05, 1, 2, 3, 4, 5},
		"int empty":            {0x02, 0x00},
		"nested headers":       deep,
		"5000 nested seqs":     nested,
		"entry garbage":        enc(tagSeq, enc(tagOctet, []byte("dn")), enc(tagSeq, enc(tagSeq, []byte{0x04}))),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) { decodeAll(in) })
	}
	// 合法长度超过上限：必须在分配 body 之前拒绝（reader 只给头部，若去读 body 会得到 EOF 而不是 too large）
	_, err := readTLV(bytes.NewReader([]byte{0x30, 0x84, 0x7f, 0xff, 0xff, 0xff}), maxLDAPMessage)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversize length must be rejected up front, got %v", err)
	}
	// 深层嵌套由固定的结构化解析限制：parseEntry 只下钻固定层数，不递归
	if _, err := parseEntry(nested); err == nil {
		t.Fatal("deeply nested garbage must not parse as an entry")
	}
}

func TestBERRandomMutationsNeverPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	seeds := [][]byte{
		ldapMsg(1, ldapResult(tagBindResp, 0, "ok")),
		ldapMsg(2, enc(tagSearchEnt, encStr(tagOctet, "uid=a"), enc(tagSeq, enc(tagSeq, encStr(tagOctet, "uid"), enc(tagSet, encStr(tagOctet, "a")))))),
	}
	for i := 0; i < 20000; i++ {
		b := append([]byte(nil), seeds[i%len(seeds)]...)
		for k := rng.Intn(4) + 1; k > 0; k-- {
			switch rng.Intn(3) {
			case 0:
				b[rng.Intn(len(b))] = byte(rng.Intn(256))
			case 1:
				b = b[:rng.Intn(len(b))+1]
			default:
				b = append(b, byte(rng.Intn(256)))
			}
		}
		decodeAll(b)
		if _, _, _, _, err := readMsgFrom(b); err == nil {
			continue
		}
	}
}

// readMsgFrom 复用 ldapConn.readMsg 的解码路径。
func readMsgFrom(b []byte) (int, byte, []byte, int, error) {
	c := &ldapConn{br: bufio.NewReader(bytes.NewReader(b))}
	id, tag, val, err := c.readMsg()
	return id, tag, val, c.rx, err
}

func FuzzBERDecode(f *testing.F) {
	f.Add(ldapMsg(1, ldapResult(tagBindResp, 49, "bad")))
	f.Add([]byte{0x30, 0x84, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, b []byte) { decodeAll(b); _, _, _, _, _ = readMsgFrom(b) })
}

// ---- 空白口令 ----

func TestWhitespaceOnlyPasswordRejected(t *testing.T) {
	for _, pw := range []string{"", " ", "   ", "\t", " \u00a0 ", "\n"} {
		if validLoginInput("alice", pw) {
			t.Errorf("password %q must be rejected by validLoginInput", pw)
		}
	}
	if validLoginInput("  ", "x") {
		t.Error("blank username must be rejected")
	}
	srv := newTestLDAP(t, testUsers, nil, false)
	srv.unauthSuccess = true
	c, err := dialLDAP(context.Background(), baseCfg(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, pw := range []string{"", "  ", "\t "} {
		if err := c.Bind("uid=alice,ou=people,dc=example,dc=com", pw); !errors.Is(err, errEmptyPassword) {
			t.Errorf("Bind(%q) = %v, want errEmptyPassword", pw, err)
		}
	}
	if n := len(srv.bindLog()); n != 0 {
		t.Fatalf("blank-password bind reached the wire %d times", n)
	}
}

// ---- 明文 ldap:// 需显式确认 ----

func TestCleartextLDAPRequiresExplicitConfirmation(t *testing.T) {
	initTest(t)
	srv := newTestLDAP(t, testUsers, nil, false)
	cfg := baseCfg(srv)
	cfg.AllowPlaintext = false
	if c, err := dialLDAP(context.Background(), cfg); !errors.Is(err, errPlaintextNotAllowed) {
		if c != nil {
			_ = c.Close()
		}
		t.Fatalf("cleartext dial without confirmation: %v", err)
	}
	svc := NewService(newMemStore(), nil)
	save := func(c LDAPConfig) (*SourceView, error) {
		return svc.Save(tctx(1), 0, SourceForm{Type: TypeLDAP, Name: "n", Config: mustJSON(t, c)})
	}
	if _, err := save(cfg); err == nil {
		t.Fatal("saving ldap:// without startTls/allowPlaintext must be rejected")
	}
	cfg.AllowPlaintext = true
	if _, err := save(cfg); err != nil {
		t.Fatalf("explicit confirmation must be accepted: %v", err)
	}
	cfg.StartTLS, cfg.AllowPlaintext = true, true
	v, err := save(cfg)
	if err != nil || v.Config.(LDAPConfig).AllowPlaintext {
		t.Fatalf("allowPlaintext must be cleared when startTls is on: %+v %v", v, err)
	}
	cfg.URL, cfg.StartTLS, cfg.AllowPlaintext = "ldaps://dc.example.com", false, true
	if v, err = save(cfg); err != nil || v.Config.(LDAPConfig).AllowPlaintext {
		t.Fatalf("allowPlaintext must be cleared for ldaps: %+v %v", v, err)
	}
}

// ---- 恶意服务端：StartTLS 响应后夹带明文、引用洪水 ----

// rawServer 对每个连接调用 fn，fn 拿到原始 net.Conn 与读取器。
func rawServer(t *testing.T, fn func(net.Conn, *bufio.Reader)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				fn(c, bufio.NewReader(c))
			}()
		}
	}()
	return "ldap://" + ln.Addr().String()
}

func msgID(raw []byte) int {
	_, body, _, _ := parseTLV(raw)
	_, idv, _, _ := parseTLV(body)
	id, _ := decodeInt(idv)
	return id
}

func TestStartTLSRejectsInjectedPlaintext(t *testing.T) {
	u := rawServer(t, func(c net.Conn, br *bufio.Reader) {
		raw, err := readTLV(br, 1<<20)
		if err != nil {
			return
		}
		// 成功响应 + 紧跟的伪造明文报文（若客户端不检查缓冲区，会把它当作 TLS 后的第一条消息）
		out := append(ldapMsg(msgID(raw), ldapResult(tagExtResp, 0, "")), ldapMsg(99, ldapResult(tagBindResp, 0, "forged"))...)
		_, _ = c.Write(out)
		_, _ = io.Copy(io.Discard, br)
	})
	cfg := LDAPConfig{URL: u, StartTLS: true, TimeoutSec: 3}
	c, err := dialLDAP(context.Background(), cfg)
	if err == nil {
		_ = c.Close()
		t.Fatal("data injected after the StartTLS response must abort the connection")
	}
	if !strings.Contains(err.Error(), "unexpected data after StartTLS") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchReferralFloodIsBounded(t *testing.T) {
	u := rawServer(t, func(c net.Conn, br *bufio.Reader) {
		raw, err := readTLV(br, 1<<20)
		if err != nil {
			return
		}
		id := msgID(raw)
		for i := 0; i < 5000; i++ { // 永不发 SearchResultDone
			if _, err := c.Write(ldapMsg(id, enc(tagSearchRef, encStr(tagOctet, "ldap://evil/")))); err != nil {
				return
			}
		}
	})
	c, err := dialLDAP(context.Background(), LDAPConfig{URL: u, AllowPlaintext: true, TimeoutSec: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	// 引用一律 fail-closed（不跟随、也不把引用当结果），所以洪水在第一条就被切断；
	// 这里只要求“报错 + 及时”，不要求特定的错误文案，避免把限流阈值写死进断言。
	_, err = c.Search("dc=x", scopeSub, "(uid=a)", nil, 2)
	if err == nil || !strings.Contains(err.Error(), "referral") {
		t.Fatalf("referral flood must be cut off, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("flood handling was not prompt")
	}
}

func TestSearchByteBudgetIsBounded(t *testing.T) {
	big := enc(tagSearchEnt, encStr(tagOctet, "uid=a"), enc(tagSeq, enc(tagSeq, encStr(tagOctet, "description"),
		enc(tagSet, encStr(tagOctet, strings.Repeat("A", 900<<10))))))
	u := rawServer(t, func(c net.Conn, br *bufio.Reader) {
		raw, err := readTLV(br, 1<<20)
		if err != nil {
			return
		}
		for i := 0; i < 40; i++ {
			if _, err := c.Write(ldapMsg(msgID(raw), big)); err != nil {
				return
			}
		}
	})
	c, err := dialLDAP(context.Background(), LDAPConfig{URL: u, AllowPlaintext: true, TimeoutSec: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Search("dc=x", scopeSub, "(uid=a)", nil, 2); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("cumulative search response must be bounded, got %v", err)
	}
}

// ---- 绑定口令不能被“换个地址”带走 ----

func TestStoredBindPasswordNotReusableForNewDestination(t *testing.T) {
	initTest(t)
	svc := NewService(newMemStore(), nil)
	cfg := LDAPConfig{URL: "ldaps://dc.example.com", BindDN: "cn=svc,dc=x", BindPassword: "pw", BaseDN: "dc=x", UserFilter: "(uid={username})"}
	v, err := svc.Save(tctx(1), 0, SourceForm{Type: TypeLDAP, Name: "n", Config: mustJSON(t, cfg)})
	if err != nil {
		t.Fatal(err)
	}
	upd := func(mut func(*LDAPConfig)) error {
		c := cfg
		c.BindPassword = "" // 沿用
		mut(&c)
		_, err := svc.Save(tctx(1), v.ID, SourceForm{Type: TypeLDAP, Name: "n", Config: mustJSON(t, c)})
		return err
	}
	if err := upd(func(c *LDAPConfig) { c.BaseDN = "dc=y" }); err != nil {
		t.Fatalf("unrelated edit must keep the stored password: %v", err)
	}
	for name, mut := range map[string]func(*LDAPConfig){
		"url":      func(c *LDAPConfig) { c.URL = "ldaps://attacker.example.net" },
		"bind dn":  func(c *LDAPConfig) { c.BindDN = "cn=other,dc=x" },
		"ca":       func(c *LDAPConfig) { c.CACert = ""; c.URL = "ldaps://dc.example.com:6360" },
		"starttls": func(c *LDAPConfig) { c.URL, c.StartTLS = "ldap://dc.example.com:389", true },
	} {
		if err := upd(mut); err == nil {
			t.Errorf("changing %s while reusing the stored bind password must be rejected", name)
		}
	}
	if err := upd(func(c *LDAPConfig) { c.URL, c.BindPassword = "ldaps://new.example.com", "new-pw" }); err != nil {
		t.Fatalf("new destination with a new password must work: %v", err)
	}
}

// ---- 测试接口：不回显内部细节 + 限频 ----

func TestTestEndpointsDoNotLeakNetworkDetailsAndAreRateLimited(t *testing.T) {
	e := newLDAPEnv(t, testUsers)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	_ = ln.Close()
	cfg := baseCfg(e.srv)
	cfg.URL = "ldap://" + dead
	src := addLDAP(t, e.svc, 5, cfg, true)

	r, err := e.svc.TestConnection(tctx(5), src.ID)
	if err != nil || r.OK {
		t.Fatalf("dead address: %+v %v", r, err)
	}
	host, port, _ := net.SplitHostPort(dead)
	for _, bad := range []string{host, port, "dial", "tcp", "refused"} {
		if strings.Contains(r.Message, bad) {
			t.Fatalf("message leaks network detail %q: %q", bad, r.Message)
		}
	}
	// 服务端诊断文本不回显
	msg := e.svc.safeMessage("bind", &ldapError{Code: 49, Msg: "internal host ad-prod-07.corp.local said no"})
	if strings.Contains(msg, "ad-prod") || !strings.Contains(msg, "49") {
		t.Fatalf("directory diagnostic leaked or code lost: %q", msg)
	}
	// 限频：每租户+用户每分钟 testRateMax 次（含 test-login），租户之间互不影响，窗口过后恢复
	now := time.Now()
	e.svc.now = func() time.Time { return now }
	e.svc.testHits = nil
	var limited bool
	for i := 0; i < testRateMax+3; i++ {
		if _, err := e.svc.TestConnection(tctx(5), src.ID); err != nil {
			limited = true
			if i < testRateMax {
				t.Fatalf("limited too early at call %d", i)
			}
		}
	}
	if !limited {
		t.Fatal("test-connection must be rate limited")
	}
	if _, err := e.svc.TestLogin(tctx(5), src.ID, "alice", "alice-pw"); err == nil {
		t.Fatal("test-login shares the same budget")
	}
	if !e.svc.allowTest(6, 0) {
		t.Fatal("another tenant must have its own budget")
	}
	// 维度是 租户+用户：同租户的另一个用户有自己的额度
	for i := 0; i < testRateMax; i++ {
		e.svc.allowTest(7, 1)
	}
	if e.svc.allowTest(7, 1) || !e.svc.allowTest(7, 2) {
		t.Fatal("budget must be keyed by tenant and user")
	}
	now = now.Add(testRateWindow + time.Second)
	if _, err := e.svc.TestConnection(tctx(5), src.ID); err != nil {
		t.Fatalf("budget must recover after the window: %v", err)
	}
}

func TestTestCASDoesNotEchoInternalResponses(t *testing.T) {
	initTest(t)
	hit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("INTERNAL-ADMIN-CONSOLE secret-banner"))
	}))
	defer hit.Close()
	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><secret-internal-tag>"))
	}))
	defer garbage.Close()
	svc := NewService(newMemStore(), nil)
	for _, u := range []string{hit.URL, garbage.URL} {
		r := svc.testCAS(context.Background(), CASConfig{ServerURL: u, ServiceBaseURL: "https://admin.example.com", Version: "3", TimeoutSec: 3})
		if r.OK {
			t.Fatal("must not be OK")
		}
		host := strings.TrimPrefix(u, "http://")
		for _, bad := range []string{"INTERNAL", "secret", host, "500", "127.0.0.1"} {
			if strings.Contains(r.Message, bad) {
				t.Fatalf("testCAS leaks %q: %q", bad, r.Message)
			}
		}
	}
}

func TestCASServerURLMustBeHTTPSExceptLoopback(t *testing.T) {
	for u, ok := range map[string]bool{
		"https://cas.example.com/cas": true, "http://cas.example.com/cas": false, "http://10.0.0.5/cas": false,
		"http://127.0.0.1:8080/cas": true, "http://localhost/cas": true, "http://[::1]:8080/cas": true,
	} {
		c := CASConfig{ServerURL: u, ServiceBaseURL: "https://admin.example.com"}
		if err := c.normalize(); (err == nil) != ok {
			t.Errorf("serverUrl %s: err=%v, want ok=%v", u, err, ok)
		}
	}
}
