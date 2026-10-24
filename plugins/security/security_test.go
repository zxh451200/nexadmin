package security

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type env struct {
	s  *Service
	st *memStore
	ck *clock
}

func newEnv(t *testing.T) *env {
	t.Helper()
	key := []byte(strings.Repeat("k", 32))
	ci, err := secret.New(key)
	if err != nil {
		t.Fatal(err)
	}
	secret.SetDefault(ci)
	t.Cleanup(func() { secret.SetDefault(nil) })
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	st := newMemStore()
	ck := &clock{t: time.Now().Truncate(time.Second)}
	st.now = ck.Now
	s := newService(st, ch, nil)
	s.now = ck.Now
	s.loadBypass(func(string) string { return "" })                        // 测试不受宿主环境变量影响
	s.lookup = func(_ context.Context, uid idgen.ID) (*auth.User, error) { // 默认目标是普通用户
		return &auth.User{ID: uid, TenantID: 1, Username: "u"}, nil
	}
	s.reauth = func(_ context.Context, _ User, pw string) error {
		if pw != "pw" {
			return response.Unauthorized("auth.invalid_credentials")
		}
		return nil
	}
	return &env{s: s, st: st, ck: ck}
}

var alice = User{TID: 1, ID: 100, Name: "alice", IP: "203.0.113.9"}

func (e *env) code(t *testing.T, sec string) string {
	t.Helper()
	c, err := totpCode(sec, totpStep(e.ck.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// enable2FA 走完 setup + enable，返回密钥和恢复码；之后时间前进一步，避免启用时用掉的码影响后续用例。
func (e *env) enable2FA(t *testing.T, u User) (string, []string) {
	t.Helper()
	ctx := context.Background()
	res, err := e.s.Setup(ctx, u, "pw")
	if err != nil {
		t.Fatal(err)
	}
	codes, err := e.s.Enable(ctx, u, e.code(t, res.Secret))
	if err != nil {
		t.Fatal(err)
	}
	e.ck.Add(totpPeriod * time.Second)
	return res.Secret, codes
}

func statusOf(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

// RFC 6238 附录 B 的 SHA1 测试向量（8 位；密钥 ASCII "12345678901234567890"）。
func TestTOTPRFC6238Vectors(t *testing.T) {
	key := []byte("12345678901234567890")
	for _, v := range []struct {
		unix int64
		want string
	}{
		{59, "94287082"}, {1111111109, "07081804"}, {1111111111, "14050471"},
		{1234567890, "89005924"}, {2000000000, "69279037"}, {20000000000, "65353130"},
	} {
		if got := hotp(key, uint64(v.unix/30), 8); got != v.want {
			t.Errorf("T=%d: got %s want %s", v.unix, got, v.want)
		}
	}
	// 6 位码是同一个动态截断值取模 10^6，即 8 位向量的后 6 位。
	sec := b32.EncodeToString(key)
	if got, _ := totpCode(sec, 59/30); got != "287082" {
		t.Errorf("6-digit at T=59: %s", got)
	}
}

func TestMatchTOTPWindow(t *testing.T) {
	sec, _ := newSecret()
	now := time.Unix(1700000000, 0)
	cur := totpStep(now)
	for d, want := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		c, _ := totpCode(sec, cur+d)
		step, ok := matchTOTP(sec, c, now)
		if ok != want || (ok && step != cur+d) {
			t.Errorf("offset %d: ok=%v step=%d want ok=%v", d, ok, step, want)
		}
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef", "12 456"} {
		if _, ok := matchTOTP(sec, bad, now); ok {
			t.Errorf("%q must not match", bad)
		}
	}
}

func TestOtpauthURI(t *testing.T) {
	u := otpauthURI("ABC234", "a b@x")
	for _, w := range []string{"otpauth://totp/NexAdmin:a%20b@x?", "secret=ABC234", "issuer=NexAdmin", "algorithm=SHA1", "digits=6", "period=30"} {
		if !strings.Contains(u, w) {
			t.Errorf("uri %q lacks %q", u, w)
		}
	}
}

func TestSecretEncryptedAtRest(t *testing.T) {
	e := newEnv(t)
	res, err := e.s.Setup(context.Background(), alice, "pw")
	if err != nil {
		t.Fatal(err)
	}
	row, _ := e.st.GetTOTP(context.Background(), alice.TID, alice.ID)
	if !secret.IsEncrypted(row.SecretEnc) || strings.Contains(row.SecretEnc, res.Secret) {
		t.Fatalf("secret must be stored encrypted, got %q", row.SecretEnc)
	}
	// 没有主密钥时不能退化成明文存储
	secret.SetDefault(nil)
	if _, err := e.s.Setup(context.Background(), User{TID: 1, ID: 101, Name: "bob"}, "pw"); err == nil {
		t.Fatal("setup must fail without a cipher")
	}
}

func TestEnableRequiresValidCodeAndRecoveryCodes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	res, _ := e.s.Setup(ctx, alice, "pw")
	if _, err := e.s.Enable(ctx, alice, "000000"); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("wrong code must be 400, got %v", err)
	}
	codes, err := e.s.Enable(ctx, alice, e.code(t, res.Secret))
	if err != nil || len(codes) != 10 {
		t.Fatalf("enable: %v codes=%d", err, len(codes))
	}
	row, _ := e.st.GetTOTP(ctx, alice.TID, alice.ID)
	for _, h := range e.st.recovery[[2]idgen.ID{alice.TID, alice.ID}] {
		for _, c := range codes {
			if h.CodeHash == c || strings.Contains(h.CodeHash, strings.ReplaceAll(c, "-", "")) {
				t.Fatal("recovery codes must be stored hashed")
			}
		}
		if len(h.CodeHash) != 64 {
			t.Fatalf("unexpected hash %q", h.CodeHash)
		}
	}
	if !row.Enabled {
		t.Fatal("not enabled")
	}
	if _, err := e.s.Setup(ctx, alice, "pw"); statusOf(err) != http.StatusConflict {
		t.Fatalf("setup on enabled user must be 409, got %v", err)
	}
}

func TestReplayRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sec, _ := e.enable2FA(t, alice)
	c := e.code(t, sec)
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "", c); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "totp", c); err == nil {
		t.Fatal("same time-step code must be rejected the second time")
	}
	// 下一个时间步的新码可用
	e.ck.Add(totpPeriod * time.Second)
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "", e.code(t, sec)); err != nil {
		t.Fatalf("next step code: %v", err)
	}
}

func TestReplayConcurrentOnlyOne(t *testing.T) {
	e := newEnv(t)
	sec, _ := e.enable2FA(t, alice)
	c := e.code(t, sec)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.s.verifyAny(context.Background(), alice.TID, alice.ID, "", c) == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("exactly one concurrent use may succeed, got %d", ok)
	}
}

func TestRecoveryCodeOneTime(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, codes := e.enable2FA(t, alice)
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "recovery", strings.ToUpper(codes[0])); err != nil {
		t.Fatalf("recovery code (case-insensitive): %v", err)
	}
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "recovery", codes[0]); err == nil {
		t.Fatal("recovery code must be one-time")
	}
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "recovery", strings.ReplaceAll(codes[1], "-", "")); err != nil {
		t.Fatalf("dashes are optional: %v", err)
	}
	st, _ := e.s.Status(ctx, alice)
	if st.RecoveryLeft != 8 {
		t.Fatalf("recovery left = %d, want 8", st.RecoveryLeft)
	}
	// 别的用户的恢复码无效（哈希绑定租户+用户）
	other := User{TID: 1, ID: 200, Name: "bob"}
	_, ocodes := e.enable2FA(t, other)
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "recovery", ocodes[0]); err == nil {
		t.Fatal("another user's recovery code must not work")
	}
}

func TestRecoveryConcurrentOnlyOne(t *testing.T) {
	e := newEnv(t)
	_, codes := e.enable2FA(t, alice)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.s.verifyAny(context.Background(), alice.TID, alice.ID, "recovery", codes[3]) == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("exactly one concurrent recovery use may succeed, got %d", ok)
	}
}

func TestFailureLimitLocksOut(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sec, _ := e.enable2FA(t, alice)
	for i := 0; i < e.s.maxFails-1; i++ {
		if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "", "000000"); !errors.Is(err, errInvalidCode) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "", "000000"); statusOf(err) != 423 {
		t.Fatalf("the failure that reaches the limit must lock (423), got %v", err)
	}
	// 锁定期间连正确的码也不行，也不会消耗该码
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "", e.code(t, sec)); statusOf(err) != 423 {
		t.Fatalf("locked user must get 423, got %v", err)
	}
	// 管理员重置后（清除锁定）才能重新验证；这里直接模拟清除
	e.s.clearFails(ctx, alice.TID, alice.ID)
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "", e.code(t, sec)); err != nil {
		t.Fatalf("after unlock: %v", err)
	}
}

func TestFailureLimitIsPerUserAndSuccessResets(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sec, _ := e.enable2FA(t, alice)
	for i := 0; i < e.s.maxFails-1; i++ {
		_ = e.s.verifyAny(ctx, alice.TID, alice.ID, "", "000000")
	}
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "", e.code(t, sec)); err != nil {
		t.Fatalf("success below the limit: %v", err)
	}
	e.ck.Add(totpPeriod * time.Second)
	for i := 0; i < e.s.maxFails-1; i++ { // 成功后计数清零：又能失败 maxFails-1 次而不锁
		if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "", "000000"); !errors.Is(err, errInvalidCode) {
			t.Fatalf("counter must reset on success: %v", err)
		}
	}
}

func TestDisableNeedsValidCode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sec, _ := e.enable2FA(t, alice)
	if err := e.s.Disable(ctx, alice, "", "123456"); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("wrong code: %v", err)
	}
	if err := e.s.Disable(ctx, alice, "", e.code(t, sec)); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if row, _ := e.st.GetTOTP(ctx, alice.TID, alice.ID); row != nil {
		t.Fatal("row must be gone")
	}
	if n := len(e.st.recovery[[2]idgen.ID{alice.TID, alice.ID}]); n != 0 {
		t.Fatalf("recovery codes must be cleared, %d left", n)
	}
}

func TestRegenerateCodesInvalidatesOld(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sec, old := e.enable2FA(t, alice)
	fresh, err := e.s.RegenerateCodes(ctx, alice, e.code(t, sec))
	if err != nil || len(fresh) != 10 {
		t.Fatalf("regen: %v", err)
	}
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "recovery", old[0]); err == nil {
		t.Fatal("old recovery codes must be invalid after regeneration")
	}
	if err := e.s.verifyAny(ctx, alice.TID, alice.ID, "recovery", fresh[0]); err != nil {
		t.Fatalf("new recovery code: %v", err)
	}
}

func TestTenantIsolation(t *testing.T) {
	e := newEnv(t)
	e.enable2FA(t, alice)
	if row, _ := e.st.GetTOTP(context.Background(), 2, alice.ID); row != nil {
		t.Fatal("same user id in another tenant must not see the binding")
	}
}
