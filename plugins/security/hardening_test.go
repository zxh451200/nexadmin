package security

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nexadmin/core/auth"
	"nexadmin/core/cluster"
	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

// doH 同 do，但可以带自定义请求头（用来验证转发头伪造）。
func (x *authEnv) doH(method, path, remote string, hdr map[string]string, token string, body any) (int, reply) {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = net.JoinHostPort(remote, "4321")
	w := httptest.NewRecorder()
	x.r.ServeHTTP(w, req)
	var rp reply
	_ = json.Unmarshal(w.Body.Bytes(), &rp)
	return w.Code, rp
}

func (x *authEnv) loginH(user, remote string, hdr map[string]string) (int, reply) {
	return x.doH("POST", "/login", remote, hdr, "", map[string]string{"type": "password", "username": user, "password": "right"})
}

// allowOnly 写入“只允许 10.0.0.0/8”的白名单（操作者在名单内，不会触发防自锁）。
func (x *authEnv) allowOnly(t *testing.T, scope string) {
	t.Helper()
	op := User{TID: 1, ID: adminID, Name: "admin", IP: "10.0.0.1"}
	if _, err := x.s.SaveRule(context.Background(), op, 0, RuleForm{Action: "allow", CIDR: "10.0.0.0/8", Scope: scope, Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

func TestLoopbackExemptCannotBeSpoofedByForwardedHeaders(t *testing.T) {
	x := newAuthEnv(t)
	x.allowOnly(t, "all")
	if code, _ := x.loginH("carol", "203.0.113.9", nil); code != 403 {
		t.Fatalf("outside the allow list: %d, want 403", code)
	}
	// 直连本机:豁免
	if code, r := x.loginH("carol", "127.0.0.1", nil); code != 200 || !hasTokens(r) {
		t.Fatalf("direct loopback must be exempt: %d", code)
	}
	if code, _ := x.loginH("carol", "::1", nil); code != 200 {
		t.Fatalf("direct ipv6 loopback must be exempt: %d", code)
	}
	// 对端不是回环,伪造各种转发头:仍被规则限制
	for _, h := range []map[string]string{
		{"X-Forwarded-For": "127.0.0.1"},
		{"X-Real-IP": "127.0.0.1"},
		{"Forwarded": "for=127.0.0.1"},
		{"X-Forwarded-For": "127.0.0.1, 10.1.1.1"},
	} {
		if code, r := x.loginH("carol", "203.0.113.9", h); code != 403 || hasTokens(r) {
			t.Fatalf("spoofed %v from a non-loopback peer must stay restricted: %d", h, code)
		}
	}
	// 同机反向代理转发的远程请求(对端是回环但带转发头):不算本机直连
	if code, _ := x.loginH("carol", "127.0.0.1", map[string]string{"X-Forwarded-For": "203.0.113.9"}); code != 403 {
		t.Fatalf("a local proxy forwarding a remote client must not be exempt: %d", code)
	}
}

func TestLoopbackExemptCanBeTurnedOff(t *testing.T) {
	x := newAuthEnv(t)
	x.allowOnly(t, "all")
	x.s.loadBypass(func(k string) string {
		if k == envLoopbackExempt {
			return "0"
		}
		return ""
	})
	if code, _ := x.loginH("carol", "127.0.0.1", nil); code != 403 {
		t.Fatalf("with the exemption off, loopback is subject to rules: %d", code)
	}
}

func TestIPRulesDisabledSwitch(t *testing.T) {
	x := newAuthEnv(t)
	x.allowOnly(t, "all")
	x.allowOnly(t, "super")
	if code, _ := x.loginH("root", "203.0.113.9", nil); code == 200 {
		t.Fatal("precondition: rules must restrict before the switch")
	}
	x.s.loadBypass(func(k string) string {
		if k == envIPRulesDisabled {
			return "1"
		}
		return ""
	})
	for _, u := range []string{"carol", "root"} {
		if code, r := x.loginH(u, "203.0.113.9", nil); code != 200 || !hasTokens(r) {
			t.Fatalf("%s with rules disabled: %d", u, code)
		}
	}
	// 规则数据还在,去掉开关即恢复
	x.s.loadBypass(func(string) string { return "" })
	if code, _ := x.loginH("carol", "203.0.113.9", nil); code != 403 {
		t.Fatalf("rules must be enforced again once the switch is off: %d", code)
	}
}

func TestEnvBool(t *testing.T) {
	get := func(v string) func(string) string { return func(string) string { return v } }
	for v, want := range map[string]bool{"1": true, "TRUE": true, " on ": true, "0": false, "false": false, "": true, "junk": true} {
		if got := envBool(get(v), "X", true); got != want {
			t.Errorf("envBool(%q, def=true)=%v want %v", v, got, want)
		}
	}
	if envBool(get(""), "X", false) || envBool(get("junk"), "X", false) {
		t.Error("unset / unparsable must fall back to the default")
	}
}

func TestSuperScopeGateLoopbackExempt(t *testing.T) {
	x := newAuthEnv(t)
	x.allowOnly(t, "super")
	if code, _ := x.loginH("root", "203.0.113.9", nil); code == 200 {
		t.Fatal("super admin outside the list must be refused")
	}
	if code, r := x.loginH("root", "127.0.0.1", nil); code != 200 || !hasTokens(r) {
		t.Fatalf("super admin on the server itself must get in to repair the rules: %d", code)
	}
	if code, _ := x.loginH("root", "203.0.113.9", map[string]string{"X-Forwarded-For": "127.0.0.1"}); code == 200 {
		t.Fatal("forged forwarded header must not bypass the super-scope rule")
	}
}

func refreshOf(r reply) string { s, _ := r.Data["refreshToken"].(string); return s }

func TestRefreshRechecksIPRulesAndRevokes(t *testing.T) {
	x := newAuthEnv(t)
	code, r := x.loginH("carol", "10.0.0.5", nil)
	if code != 200 {
		t.Fatalf("login: %d", code)
	}
	rt := refreshOf(r)
	// 登录之后才新增的 deny 规则:刷新被拒,令牌作废
	op := User{TID: 1, ID: adminID, Name: "admin", IP: "192.0.2.1"}
	if _, err := x.s.SaveRule(context.Background(), op, 0, RuleForm{Action: "deny", CIDR: "10.0.0.0/8", Scope: "all", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if code, _ = x.doH("POST", "/refresh", "10.0.0.5", nil, "", map[string]string{"refreshToken": rt}); code != 403 {
		t.Fatalf("refresh from a now-denied ip must be 403, got %d", code)
	}
	if code, _ = x.doH("POST", "/refresh", "198.51.100.1", nil, "", map[string]string{"refreshToken": rt}); code != 401 {
		t.Fatalf("the refresh token must be revoked after a denied refresh, got %d", code)
	}
}

func TestRefreshLoopbackExemptAndNoSpoof(t *testing.T) {
	x := newAuthEnv(t)
	code, r := x.loginH("carol", "10.0.0.5", nil)
	if code != 200 {
		t.Fatalf("login: %d", code)
	}
	op := User{TID: 1, ID: adminID, Name: "admin", IP: "192.0.2.1"}
	if _, err := x.s.SaveRule(context.Background(), op, 0, RuleForm{Action: "allow", CIDR: "192.0.2.0/24", Scope: "all", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// 白名单只含 192.0.2.0/24。对端 10.0.0.5 伪造 XFF=127.0.0.1(测试引擎信任所有代理,所以 ClientIP 被改成 127.0.0.1):
	// ClientIP 看起来是回环,但对端不是回环且带转发头,不能享受豁免(该令牌随之作废,所以后面重新登录拿新的)
	if code, _ = x.doH("POST", "/refresh", "10.0.0.5", map[string]string{"X-Forwarded-For": "127.0.0.1"}, "", map[string]string{"refreshToken": refreshOf(r)}); code != 403 {
		t.Fatalf("forged XFF must not exempt a refresh: %d", code)
	}
	_, r = x.loginH("carol", "127.0.0.1", nil)
	if code, r2 := x.doH("POST", "/refresh", "127.0.0.1", nil, "", map[string]string{"refreshToken": refreshOf(r)}); code != 200 || refreshOf(r2) == "" {
		t.Fatalf("direct loopback refresh is exempt: %d", code)
	}
}

func TestConfirmLockoutOverHTTP(t *testing.T) {
	x := newAuthEnv(t)
	pair, _ := x.svc.IssuePair(&auth.User{ID: adminID, TenantID: 1, Username: "admin"})
	body := map[string]any{"action": "allow", "cidr": "192.0.2.0/24", "scope": "all", "enabled": true}
	url := "/api/v1/plugins/security/ip-rules"
	if code, _ := x.do("POST", url, "10.0.0.1", pair.AccessToken, body); code != http.StatusConflict {
		t.Fatalf("a rule that locks the operator out must need confirmation: %d", code)
	}
	body["confirmLockout"] = true
	code, r := x.do("POST", url, "10.0.0.1", pair.AccessToken, body)
	id, _ := r.Data["id"].(string)
	if code != 200 || id == "" {
		t.Fatalf("confirmed save: %d %+v", code, r)
	}
	logs, _, _ := x.st.ListLogs(context.Background(), 1, 1, 20)
	found := false
	for _, l := range logs {
		found = found || (l.Action == "create_ip_rule" && strings.Contains(l.Detail, "confirm_lockout"))
	}
	if !found {
		t.Fatal("a confirmed lockout must be recorded in the audit log")
	}
	// 现在名单已把自己排除在外,删掉它只会放开,不算锁定,不需要确认
	if code, _ = x.do("DELETE", url+"/"+id, "10.0.0.1", pair.AccessToken, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
}

// ---- 2FA 绑定 / 重置的重新认证 ----

func TestSetupRequiresPasswordAndBindsSession(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	u := User{TID: 1, ID: 300, Name: "dave", IP: "203.0.113.9", Session: "sess-A"}
	if _, err := e.s.Setup(ctx, u, "wrong"); statusOf(err) != http.StatusUnauthorized {
		t.Fatalf("wrong password: %v", err)
	}
	if row, _ := e.st.GetTOTP(ctx, 1, 300); row != nil {
		t.Fatal("a failed re-authentication must not leave a pending secret")
	}
	e.s.reauth = nil
	if _, err := e.s.Setup(ctx, u, "pw"); statusOf(err) != http.StatusServiceUnavailable {
		t.Fatalf("no verifier: must fail closed, got %v", err)
	}
	e.s.reauth = func(context.Context, User, string) error { return nil }
	res, err := e.s.Setup(ctx, u, "x")
	if err != nil {
		t.Fatal(err)
	}
	other := u
	other.Session = "sess-B"
	if _, err := e.s.Enable(ctx, other, e.code(t, res.Secret)); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("enable from another session must be refused: %v", err)
	}
	if row, _ := e.st.GetTOTP(ctx, 1, 300); row == nil || row.Enabled {
		t.Fatal("the pending secret must stay pending")
	}
	if _, err := e.s.Enable(ctx, u, e.code(t, res.Secret)); err != nil {
		t.Fatalf("enable in the same session: %v", err)
	}
}

func TestEnableWithoutSetupBindingRefused(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	enc, _ := encryptForTest("JBSWY3DPEHPK3PXP")
	if err := e.st.SavePending(ctx, 1, 301, "erin", enc); err != nil { // 绕过 Setup 直接写入待确认密钥
		t.Fatal(err)
	}
	u := User{TID: 1, ID: 301, Name: "erin", Session: "s"}
	c, _ := totpCode("JBSWY3DPEHPK3PXP", totpStep(e.ck.Now()))
	if _, err := e.s.Enable(ctx, u, c); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("enable without a session-bound setup must be refused: %v", err)
	}
}

func TestTokenPrincipalCannotChange2FA(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	pat := User{TID: 1, ID: 302, Name: "frank", Principal: auth.PrincipalPAT, Session: "pat:1"}
	if _, err := e.s.Setup(ctx, pat, "pw"); statusOf(err) != http.StatusForbidden {
		t.Fatalf("setup via PAT: %v", err)
	}
	if _, err := e.s.Enable(ctx, pat, "000000"); statusOf(err) != http.StatusForbidden {
		t.Fatalf("enable via PAT: %v", err)
	}
	if err := e.s.Disable(ctx, pat, "", "000000"); statusOf(err) != http.StatusForbidden {
		t.Fatalf("disable via PAT: %v", err)
	}
	if _, err := e.s.RegenerateCodes(ctx, pat, "000000"); statusOf(err) != http.StatusForbidden {
		t.Fatalf("regenerate via PAT: %v", err)
	}
}

func TestResetUserProtectsSuperAdmins(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	super := User{TID: 1, ID: 1, Name: "root", Super: true}
	admin := User{TID: 1, ID: 9, Name: "admin"}
	boss := User{TID: 1, ID: 1000, Name: "boss"}
	e.enable2FA(t, boss)
	e.s.lookup = func(_ context.Context, uid idgen.ID) (*auth.User, error) {
		return &auth.User{ID: uid, TenantID: 1, Username: "boss", Super: uid == 1000}, nil
	}
	if err := e.s.ResetUser(ctx, admin, 1000); statusOf(err) != http.StatusForbidden {
		t.Fatalf("a non-super admin must not reset a super admin: %v", err)
	}
	if row, _ := e.st.GetTOTP(ctx, 1, 1000); row == nil || !row.Enabled {
		t.Fatal("the super admin's 2FA must be untouched")
	}
	logs, _, _ := e.st.ListLogs(ctx, 1, 1, 50)
	denied := false
	for _, l := range logs {
		denied = denied || l.Action == "reset_2fa_denied"
	}
	if !denied {
		t.Fatal("the refusal must be audited")
	}
	// lookup 不可用:非超管失败即关闭
	e.s.lookup = nil
	if err := e.s.ResetUser(ctx, admin, 1000); err == nil {
		t.Fatal("unknown target identity must fail closed")
	}
	// 超管操作者可以重置(且不需要 lookup)
	if err := e.s.ResetUser(ctx, super, 1000); err != nil {
		t.Fatalf("a super admin may reset: %v", err)
	}
}

// ---- 失败计数:原子、按 IP 与用户两个维度 ----

func encryptForTest(plain string) (string, error) { return secret.Encrypt(plain) }

func setTestSecret(t *testing.T, key []byte) {
	t.Helper()
	c, err := secret.New(key)
	if err != nil {
		t.Fatal(err)
	}
	secret.SetDefault(c)
}

func withIP(ip string) context.Context { return withAttemptIP(context.Background(), ip) }

func TestFailureLockIsPerIPNotGlobal(t *testing.T) {
	e := newEnv(t)
	sec, _ := e.enable2FA(t, alice)
	attacker, victim := withIP("198.51.100.66"), withIP("203.0.113.9")
	var last error
	for i := 0; i < e.s.maxFails; i++ {
		last = e.s.verifyAny(attacker, alice.TID, alice.ID, "", "000000")
	}
	if statusOf(last) != 423 {
		t.Fatalf("the attacking ip must be locked after maxFails: %v", last)
	}
	if err := e.s.verifyAny(attacker, alice.TID, alice.ID, "", e.code(t, sec)); statusOf(err) != 423 {
		t.Fatalf("a locked ip is refused even with a right code: %v", err)
	}
	e.ck.Add(31 * time.Second)
	if err := e.s.verifyAny(victim, alice.TID, alice.ID, "", e.code(t, sec)); err != nil {
		t.Fatalf("the real user from another ip must not be locked out by the attacker: %v", err)
	}
}

func TestUserLevelCooldownAllowsRecoveryBypass(t *testing.T) {
	e := newEnv(t)
	sec, codes := e.enable2FA(t, alice)
	// 分布式猜测:每个 IP 只试一两次,累计到用户级阈值
	n := e.s.maxFails * userFailFactor
	for i := 0; i < n; i++ {
		ip := "198.51.100." + idgen.ID(i+1).String()
		if err := e.s.verifyAny(withIP(ip), alice.TID, alice.ID, "", "000000"); statusOf(err) == 423 && i < n-1 {
			t.Fatalf("attempt %d must not be locked yet: %v", i, err)
		}
	}
	real := withIP("203.0.113.9")
	if err := e.s.verifyAny(real, alice.TID, alice.ID, "", e.code(t, sec)); statusOf(err) != 423 {
		t.Fatalf("user-level cooldown must block TOTP: %v", err)
	}
	if err := e.s.verifyAny(real, alice.TID, alice.ID, "recovery", codes[0]); err != nil {
		t.Fatalf("a recovery code must bypass the cooldown: %v", err)
	}
	if err := e.s.verifyAny(real, alice.TID, alice.ID, "", e.code(t, sec)); err != nil {
		t.Fatalf("a successful recovery clears the cooldown: %v", err)
	}
}

func TestFailureCountingIsAtomicUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	e.enable2FA(t, alice)
	ctx := withIP("198.51.100.77")
	const n = 60
	var invalid, locked, other atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			switch err := e.s.verifyAny(ctx, alice.TID, alice.ID, "", "000000"); {
			case err == errInvalidCode:
				invalid.Add(1)
			case statusOf(err) == 423:
				locked.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	// 恰好 maxFails 个名额进入校验:前 maxFails-1 次是普通失败,第 maxFails 次触发锁定,其余都被挡在校验之外
	if int(invalid.Load()) != e.s.maxFails-1 || int(locked.Load()) != n-(e.s.maxFails-1) || other.Load() != 0 {
		t.Fatalf("invalid=%d locked=%d other=%d, want %d/%d/0", invalid.Load(), locked.Load(), other.Load(), e.s.maxFails-1, n-(e.s.maxFails-1))
	}
}

// ---- 恢复码哈希 ----

func TestRecoveryCodesUseKeyedHashAndLegacyStillWorks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, codes := e.enable2FA(t, alice)
	// 新生成的码按 HMAC 存储,不等于旧的无密钥 SHA256
	want, err := e.s.hashRecovery(alice.TID, alice.ID, codes[0])
	if err != nil {
		t.Fatal(err)
	}
	if want == hashRecoveryLegacy(alice.TID, alice.ID, codes[0]) {
		t.Fatal("the new hash must differ from the legacy hash")
	}
	if n, _ := e.st.CountRecovery(ctx, alice.TID, alice.ID); n != int64(recoveryCount) {
		t.Fatalf("recovery rows: %d", n)
	}
	if ok, _ := e.st.ConsumeRecovery(ctx, alice.TID, alice.ID, hashRecoveryLegacy(alice.TID, alice.ID, codes[0])); ok {
		t.Fatal("a fresh code must not be consumable through the legacy hash")
	}
	// 换主密钥后哈希不同(pepper 来自主密钥)
	s2 := newService(e.st, e.s.cache, nil)
	k2 := []byte(strings.Repeat("z", 32))
	setTestSecret(t, k2)
	got2, err := s2.hashRecovery(alice.TID, alice.ID, codes[0])
	if err != nil || got2 == want {
		t.Fatalf("the hash must depend on the server secret: %v", err)
	}
	setTestSecret(t, []byte(strings.Repeat("k", 32)))

	// 存量:库里是旧格式哈希的恢复码仍可使用一次
	legacy := []string{"abcde-fghjk", "mnpqr-stuvw"}
	hs := []string{hashRecoveryLegacy(alice.TID, alice.ID, legacy[0]), hashRecoveryLegacy(alice.TID, alice.ID, legacy[1])}
	if err := e.st.ReplaceRecovery(ctx, alice.TID, alice.ID, hs); err != nil {
		t.Fatal(err)
	}
	if err := e.s.verifyAny(withIP("203.0.113.9"), alice.TID, alice.ID, "recovery", "ABCDE-FGHJK"); err != nil {
		t.Fatalf("legacy code must still work: %v", err)
	}
	if err := e.s.verifyAny(withIP("203.0.113.9"), alice.TID, alice.ID, "recovery", "abcdefghjk"); err == nil {
		t.Fatal("a legacy code is single-use as well")
	}
	if n, _ := e.st.CountRecovery(ctx, alice.TID, alice.ID); n != 1 {
		t.Fatalf("one legacy code must be left, got %d", n)
	}
}

// ---- 规则缓存跨实例失效 ----

func TestRuleCacheInvalidatedAcrossInstancesViaBus(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	b1 := cluster.NewWithID(e.s.cache, nil, "inst-1")
	b2 := cluster.NewWithID(e.s.cache, nil, "inst-2")
	t.Cleanup(func() { b1.Close(); b2.Close() })
	s2 := newService(e.st, e.s.cache, nil)
	s2.now = e.ck.Now
	s2.attachBus(b2)
	e.s.attachBus(b1)

	if v, err := s2.rulesFor(ctx, 1); err != nil || len(v) != 0 { // 预热实例 2 的缓存
		t.Fatalf("warm: %v %v", v, err)
	}
	op := User{TID: 1, ID: 1, Name: "admin", IP: "10.0.0.1"}
	if _, err := e.s.SaveRule(ctx, op, 0, RuleForm{Action: "deny", CIDR: "198.51.100.0/24", Scope: "all", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		v, _ := s2.rulesFor(ctx, 1)
		if len(v) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("instance 2 still serves the stale rule cache; the bus broadcast did not invalidate it")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRuleCacheFallsBackToTTLWithoutBus(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	s2 := newService(e.st, e.s.cache, nil)
	clk := &clock{t: time.Now()}
	s2.now = clk.Now
	s2.attachBus(nil) // 没有总线
	_, _ = s2.rulesFor(ctx, 1)
	op := User{TID: 1, ID: 1, Name: "admin", IP: "10.0.0.1"}
	if _, err := e.s.SaveRule(ctx, op, 0, RuleForm{Action: "deny", CIDR: "198.51.100.0/24", Scope: "all", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s2.rulesFor(ctx, 1); len(v) != 0 {
		t.Fatal("within the TTL the other instance may still be stale")
	}
	clk.Add(ruleCacheTTL + time.Second)
	if v, _ := s2.rulesFor(ctx, 1); len(v) != 1 {
		t.Fatal("after the 5s TTL the rule must be visible")
	}
}
