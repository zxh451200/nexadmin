package smscode

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/response"
)

// fakeClock + fakeCache：可手动拨时间的缓存，用来验证冷却、过期。
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

type item struct {
	v   string
	exp time.Time
}

type fakeCache struct {
	clk  *fakeClock
	mu   sync.Mutex
	data map[string]item
}

func (f *fakeCache) Get(_ context.Context, k string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.data[k]
	if !ok || (!it.exp.IsZero() && !f.clk.Now().Before(it.exp)) {
		return "", cache.ErrNotFound
	}
	return it.v, nil
}
func (f *fakeCache) Set(_ context.Context, k, v string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	it := item{v: v}
	if ttl > 0 {
		it.exp = f.clk.Now().Add(ttl)
	}
	f.data[k] = it
	return nil
}
func (f *fakeCache) Del(_ context.Context, ks ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range ks {
		delete(f.data, k)
	}
	return nil
}

// GetDel 实现 cache.AtomicGetDel：同一把锁内取出并删除。
func (f *fakeCache) GetDel(_ context.Context, k string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.data[k]
	delete(f.data, k)
	if !ok || (!it.exp.IsZero() && !f.clk.Now().Before(it.exp)) {
		return "", cache.ErrNotFound
	}
	return it.v, nil
}
func (f *fakeCache) Exists(ctx context.Context, k string) (bool, error) {
	_, err := f.Get(ctx, k)
	if errors.Is(err, cache.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
func (f *fakeCache) Close() error { return nil }

type recorder struct {
	phone, tpl, code string
	n                int
	err              error
}

func (r *recorder) Send(_ context.Context, phone, tpl string, p map[string]string) error {
	r.n++
	r.phone, r.tpl, r.code = phone, tpl, p["code"]
	return r.err
}

func newSvc() (*Service, *recorder, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)}
	rec := &recorder{}
	s := &Service{cache: &fakeCache{clk: clk, data: map[string]item{}}, log: zap.NewNop(), prov: rec, template: "login", now: clk.Now}
	return s, rec, clk
}

func isStatus(err error, status int) bool {
	var e *response.Error
	return errors.As(err, &e) && e.Status == status
}

const phone = "13800138000"

func TestSendAndVerifyOnce(t *testing.T) {
	s, rec, _ := newSvc()
	ctx := context.Background()
	if err := s.Send(ctx, phone, "1.1.1.1", true); err != nil {
		t.Fatal(err)
	}
	if rec.n != 1 || rec.phone != phone || rec.tpl != "login" || len(rec.code) != 6 {
		t.Fatalf("provider call: %+v", rec)
	}
	for _, v := range s.cache.(*fakeCache).data { // 缓存里只能是哈希，不能出现明文验证码
		if strings.Contains(v.v, rec.code) && len(v.v) < 20 {
			t.Fatal("plain code stored")
		}
	}
	if err := s.Verify(ctx, phone, rec.code); err != nil {
		t.Fatalf("correct code rejected: %v", err)
	}
	if err := s.Verify(ctx, phone, rec.code); err == nil {
		t.Fatal("code must be single use")
	}
}

func TestCodeExpiresAfterFiveMinutes(t *testing.T) {
	s, rec, clk := newSvc()
	ctx := context.Background()
	_ = s.Send(ctx, phone, "1.1.1.1", true)
	clk.Advance(CodeTTL + time.Second)
	if err := s.Verify(ctx, phone, rec.code); err == nil {
		t.Fatal("expired code accepted")
	}
}

func TestCooldownSixtySeconds(t *testing.T) {
	s, rec, clk := newSvc()
	ctx := context.Background()
	if err := s.Send(ctx, phone, "1.1.1.1", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(ctx, phone, "2.2.2.2", true); !isStatus(err, 429) || rec.n != 1 {
		t.Fatalf("second send within 60s must be 429, err=%v n=%d", err, rec.n)
	}
	clk.Advance(Cooldown + time.Second)
	if err := s.Send(ctx, phone, "2.2.2.2", true); err != nil {
		t.Fatalf("send after cooldown: %v", err)
	}
}

func TestDailyLimitPerPhone(t *testing.T) {
	s, _, clk := newSvc()
	ctx := context.Background()
	for i := 0; i < DailyLimit; i++ {
		if err := s.Send(ctx, phone, "ip-"+string(rune('a'+i)), true); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		clk.Advance(Cooldown + time.Second)
	}
	if err := s.Send(ctx, phone, "ip-z", true); !isStatus(err, 429) {
		t.Fatalf("over daily limit must be 429, got %v", err)
	}
}

func TestIPHourlyLimit(t *testing.T) {
	s, _, clk := newSvc()
	ctx := context.Background()
	for i := 0; i < IPHourLimit; i++ {
		ph := "1380013800" + string(rune('0'+i))
		if err := s.Send(ctx, ph, "9.9.9.9", true); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		clk.Advance(time.Second)
	}
	if err := s.Send(ctx, "13900000000", "9.9.9.9", true); !isStatus(err, 429) {
		t.Fatalf("same IP over limit must be 429, got %v", err)
	}
	if err := s.Send(ctx, "13900000000", "8.8.8.8", true); err != nil {
		t.Fatalf("other IP must not be limited: %v", err)
	}
}

func TestFiveWrongAttemptsInvalidateCode(t *testing.T) {
	s, rec, _ := newSvc()
	ctx := context.Background()
	_ = s.Send(ctx, phone, "1.1.1.1", true)
	wrong := "000000"
	if rec.code == wrong {
		wrong = "111111"
	}
	for i := 0; i < MaxAttempts; i++ {
		if err := s.Verify(ctx, phone, wrong); err == nil {
			t.Fatalf("wrong code accepted at attempt %d", i)
		}
	}
	if err := s.Verify(ctx, phone, rec.code); err == nil {
		t.Fatal("code must be invalidated after 5 wrong attempts, even if the right one is entered")
	}
}

func TestFourWrongThenCorrectStillWorks(t *testing.T) {
	s, rec, _ := newSvc()
	ctx := context.Background()
	_ = s.Send(ctx, phone, "1.1.1.1", true)
	wrong := "000000"
	if rec.code == wrong {
		wrong = "111111"
	}
	for i := 0; i < MaxAttempts-1; i++ {
		_ = s.Verify(ctx, phone, wrong)
	}
	if err := s.Verify(ctx, phone, rec.code); err != nil {
		t.Fatalf("4 wrong attempts must not invalidate: %v", err)
	}
}

func TestUndeliveredSendLooksIdentical(t *testing.T) {
	s, rec, _ := newSvc()
	ctx := context.Background()
	if err := s.Send(ctx, phone, "1.1.1.1", false); err != nil {
		t.Fatal(err)
	}
	if rec.n != 0 {
		t.Fatal("deliver=false must not call the provider")
	}
	if err := s.Verify(ctx, phone, "123456"); err == nil {
		t.Fatal("no code was issued, verify must fail")
	}
	if err := s.Send(ctx, phone, "1.1.1.1", false); !isStatus(err, 429) {
		t.Fatalf("cooldown must apply to undelivered sends too, got %v", err)
	}
}

func TestProviderFailureDropsCode(t *testing.T) {
	s, rec, _ := newSvc()
	rec.err = errors.New("gateway down")
	if err := s.Send(context.Background(), phone, "1.1.1.1", true); err == nil {
		t.Fatal("want error")
	}
	if err := s.Verify(context.Background(), phone, rec.code); err == nil {
		t.Fatal("code of a failed send must not be verifiable")
	}
}

func TestDisabled(t *testing.T) {
	s := &Service{}
	if s.Enabled() {
		t.Fatal("zero service must be disabled")
	}
	if err := s.Send(context.Background(), phone, "ip", true); !isStatus(err, 503) {
		t.Fatalf("want 503, got %v", err)
	}
}
