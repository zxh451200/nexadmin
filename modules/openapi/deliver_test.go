package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/event"
	"nexadmin/core/secret"
	"nexadmin/pkg/idgen"
)

type memStore struct {
	mu            sync.Mutex
	hooks         map[int64]*Webhook
	ds            map[idgen.ID]*Delivery
	seq           idgen.ID
	cleanupBefore time.Time
}

func newMemStore() *memStore {
	return &memStore{hooks: map[int64]*Webhook{}, ds: map[idgen.ID]*Delivery{}}
}

func (m *memStore) ActiveHooks(_ context.Context, tenantID int64) ([]Webhook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Webhook
	for _, h := range m.hooks {
		if int64(h.TenantID) == tenantID && h.Enabled {
			out = append(out, *h)
		}
	}
	return out, nil
}

func (m *memStore) Enqueue(_ context.Context, ds []Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range ds {
		m.seq++
		d.ID = 9000 + m.seq
		cp := d
		m.ds[d.ID] = &cp
	}
	return nil
}

func (m *memStore) ClaimDue(_ context.Context, now time.Time, lease time.Duration, limit int) ([]Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Delivery
	for _, d := range m.ds {
		if d.Status == StatusPending && !d.NextAttemptAt.After(now) && len(out) < limit {
			d.NextAttemptAt = now.Add(lease)
			out = append(out, *d)
		}
	}
	return out, nil
}

func (m *memStore) Webhook(_ context.Context, id int64) (*Webhook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hooks[id], nil
}

func (m *memStore) Finish(_ context.Context, d *Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *d
	m.ds[d.ID] = &cp
	return nil
}

func (m *memStore) Cleanup(_ context.Context, before time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupBefore = before
	return 0, nil
}

func (m *memStore) get(id idgen.ID) Delivery {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.ds[id]
}

const hookSecret = "whsec_unit_test"

func setupSecret(t *testing.T) {
	t.Helper()
	c, err := secret.New(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	secret.SetDefault(c)
}

func newTestHook(t *testing.T, st *memStore, url string, events ...string) *Webhook {
	t.Helper()
	enc, err := secret.Encrypt(hookSecret)
	if err != nil {
		t.Fatal(err)
	}
	h := &Webhook{Name: "h", URL: url, Events: encodeList(events), SecretEnc: enc, Enabled: true}
	h.ID, h.TenantID = 77, 1
	st.hooks[77] = h
	return h
}

func newTestDelivery(st *memStore, now time.Time) idgen.ID {
	body := `{"id":"evt_1","event":"user.created","createdAt":"2026-01-01T00:00:00Z","tenantId":"1","data":{"id":"5"}}`
	_ = st.Enqueue(context.Background(), []Delivery{{WebhookID: 77, EventID: "evt_1", Event: "user.created", Payload: body, Status: StatusPending, NextAttemptAt: now}})
	return 9001
}

// plainSender 允许连回环地址的接收端（httptest），只用于测投递逻辑；SSRF 另有专门的用例。
func plainSender(now func() time.Time) *sender {
	return &sender{client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, now: now}
}

func TestNextBackoff(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	for i, w := range want {
		got, ok := nextBackoff(i + 1)
		if !ok || got != w {
			t.Fatalf("attempt %d: got %v %v, want %v", i+1, got, ok, w)
		}
	}
	for _, n := range []int{0, 5, 6} {
		if _, ok := nextBackoff(n); ok {
			t.Fatalf("attempt %d must not be retried", n)
		}
	}
}

func TestDeliverySignatureAndEnvelope(t *testing.T) {
	setupSecret(t)
	var got struct {
		hdr  http.Header
		body []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.hdr = r.Header.Clone()
		got.body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(strings.Repeat("x", 5000))) // 超长响应体
	}))
	defer srv.Close()

	now := time.Unix(1_700_000_000, 0).UTC()
	st := newMemStore()
	newTestHook(t, st, srv.URL, "user.created")
	id := newTestDelivery(st, now)
	w := newWorker(st, plainSender(func() time.Time { return now }), nil)
	w.now = func() time.Time { return now }
	if n := w.tick(context.Background()); n != 1 {
		t.Fatalf("tick processed %d", n)
	}

	d := st.get(id)
	if d.Status != StatusSuccess || d.Attempts != 1 || d.ResponseStatus != 200 || d.DeliveredAt == nil {
		t.Fatalf("delivery = %+v", d)
	}
	if len(d.ResponseBody) != 2048 {
		t.Fatalf("response body must be truncated to 2048 bytes, got %d", len(d.ResponseBody))
	}
	ts := got.hdr.Get("X-Nex-Timestamp")
	if ts != "1700000000" || got.hdr.Get("X-Nex-Event") != "user.created" || got.hdr.Get("X-Nex-Delivery") != id.String() {
		t.Fatalf("headers = %v", got.hdr)
	}
	if want := signPayload(hookSecret, ts, got.body); got.hdr.Get("X-Nex-Signature") != want || len(want) != 64 {
		t.Fatalf("signature = %q, want %q", got.hdr.Get("X-Nex-Signature"), want)
	}
	if signPayload("other-secret", ts, got.body) == got.hdr.Get("X-Nex-Signature") {
		t.Fatal("signature must depend on the secret")
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(got.body, &env); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "event", "createdAt", "tenantId", "data"} {
		if _, ok := env[k]; !ok {
			t.Fatalf("envelope missing %q: %s", k, got.body)
		}
	}
	if strings.Contains(d.Payload, hookSecret) || strings.Contains(d.ResponseBody, hookSecret) {
		t.Fatal("secret must never be stored in the delivery record")
	}
}

func TestRetryBackoffThenFailed(t *testing.T) {
	setupSecret(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	now := time.Unix(1_700_000_000, 0).UTC()
	st := newMemStore()
	newTestHook(t, st, srv.URL, "user.created")
	id := newTestDelivery(st, now)
	w := newWorker(st, plainSender(func() time.Time { return now }), nil)
	w.now = func() time.Time { return now }

	wantDelay := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	for i, delay := range wantDelay {
		if n := w.tick(context.Background()); n != 1 {
			t.Fatalf("attempt %d: tick processed %d", i+1, n)
		}
		d := st.get(id)
		if d.Status != StatusPending || d.Attempts != i+1 || !d.NextAttemptAt.Equal(now.Add(delay)) || d.ResponseStatus != 500 {
			t.Fatalf("after attempt %d: %+v (want next=%v)", i+1, d, now.Add(delay))
		}
		if n := w.tick(context.Background()); n != 0 { // 退避期内不会被再次取走
			t.Fatalf("attempt %d: delivery picked up before its backoff elapsed", i+1)
		}
		now = now.Add(delay) // 时间走到下次重试点
	}
	if n := w.tick(context.Background()); n != 1 {
		t.Fatalf("final attempt not run")
	}
	d := st.get(id)
	if d.Status != StatusFailed || d.Attempts != 5 || hits.Load() != 5 {
		t.Fatalf("after 5 failures: %+v, hits=%d", d, hits.Load())
	}
	if n := w.tick(context.Background()); n != 0 {
		t.Fatal("failed delivery must not be picked up again")
	}
}

func TestDisabledOrMissingWebhookFailsWithoutSending(t *testing.T) {
	setupSecret(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	now := time.Now()
	st := newMemStore()
	h := newTestHook(t, st, srv.URL, "user.created")
	h.Enabled = false
	id := newTestDelivery(st, now)
	w := newWorker(st, plainSender(time.Now), nil)
	w.tick(context.Background())
	if d := st.get(id); d.Status != StatusFailed || d.Attempts != 0 || hits.Load() != 0 {
		t.Fatalf("disabled webhook: %+v hits=%d", d, hits.Load())
	}
}

func TestDispatcherEnqueuesMatchingHooksWithScrubbedPayload(t *testing.T) {
	setupSecret(t)
	st := newMemStore()
	h1 := newTestHook(t, st, "https://a.example/hook", "user.created", "user.updated")
	h2 := &Webhook{Name: "other-event", URL: "https://b.example/hook", Events: encodeList([]string{"dept.created"}), Enabled: true}
	h2.ID, h2.TenantID = 78, 1
	h3 := &Webhook{Name: "other-tenant", URL: "https://c.example/hook", Events: encodeList([]string{"user.created"}), Enabled: true}
	h3.ID, h3.TenantID = 79, 2
	h4 := &Webhook{Name: "disabled", URL: "https://d.example/hook", Events: encodeList([]string{"user.created"}), Enabled: false}
	h4.ID, h4.TenantID = 80, 1
	st.hooks[78], st.hooks[79], st.hooks[80] = h2, h3, h4

	woken := 0
	d := &dispatcher{st: st, log: zap.NewNop(), wake: func() { woken++ }, now: time.Now}
	// 故意塞进敏感字段，验证外发前被剔除
	payload := map[string]any{"id": idgen.ID(5), "tenantId": idgen.ID(1), "username": "bob", "password": "$2a$10$hash", "passwordHash": "x", "apiSecret": "y"}
	d.Handle(event.Event{ID: "evt_abc", Name: event.UserCreated, TenantID: 1, CreatedAt: time.Now(), Data: payload})

	if len(st.ds) != 1 || woken != 1 {
		t.Fatalf("deliveries=%d woken=%d, want 1/1", len(st.ds), woken)
	}
	dl := st.get(9001)
	if dl.WebhookID != h1.ID || dl.Event != "user.created" || dl.Status != StatusPending || dl.TenantID != 1 {
		t.Fatalf("delivery = %+v", dl)
	}
	for _, bad := range []string{"password", "$2a$10$hash", "apiSecret", "passwordHash"} {
		if strings.Contains(dl.Payload, bad) {
			t.Fatalf("payload leaks %q: %s", bad, dl.Payload)
		}
	}
	var env envelope
	if err := json.Unmarshal([]byte(dl.Payload), &env); err != nil || env.ID != "evt_abc" || env.Event != "user.created" || env.TenantID != 1 || !strings.Contains(string(env.Data), `"username":"bob"`) {
		t.Fatalf("envelope = %+v err=%v", env, err)
	}

	// 无租户的事件不外发
	before := len(st.ds)
	d.Handle(event.Event{ID: "evt_x", Name: event.UserCreated, TenantID: 0, Data: payload})
	if len(st.ds) != before {
		t.Fatal("tenant-less event must not be delivered")
	}
}

func TestCleanupUsesThirtyDayRetention(t *testing.T) {
	st := newMemStore()
	w := newWorker(st, plainSender(time.Now), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.run(ctx) // 首轮会清理一次，然后因 ctx 已取消返回
	want := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if d := st.cleanupBefore.Sub(want); d < -5*time.Second || d > 5*time.Second {
		t.Fatalf("cleanup cutoff %v, want about %v", st.cleanupBefore, want)
	}
}
