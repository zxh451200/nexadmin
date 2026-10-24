package event

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func TestPublishDoesNotBlockAndDropsWhenFull(t *testing.T) {
	b := NewBus(2)
	defer b.Close()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	b.Subscribe(SubscriberFunc(func(Event) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release // 订阅者卡住，队列会被填满
	}))

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish(context.Background(), UserCreated, UserData{ID: 1})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked while subscriber was stuck")
	}
	close(release)
	if b.Dropped() == 0 {
		t.Fatal("expected dropped events when queue is full")
	}
}

func TestDeliveredToSubscribersWithTenant(t *testing.T) {
	b := NewBus(8)
	defer b.Close()
	var mu sync.Mutex
	var got []Event
	ch := make(chan struct{}, 4)
	b.Subscribe(SubscriberFunc(func(e Event) { mu.Lock(); got = append(got, e); mu.Unlock(); ch <- struct{}{} }))
	b.Subscribe(SubscriberFunc(func(Event) { panic("boom") })) // 订阅者 panic 不影响其他订阅者

	b.Publish(tenant.With(context.Background(), 7), DeptCreated, DeptData{ID: 1, TenantID: 9})
	b.Publish(tenant.Skip(context.Background()), RoleCreated, RoleData{ID: 2, TenantID: 9}) // ctx 无租户 -> 取载荷租户
	for i := 0; i < 2; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("event not delivered")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if got[0].TenantID != 7 || got[1].TenantID != 9 {
		t.Fatalf("tenant ids = %v, %v", got[0].TenantID, got[1].TenantID)
	}
	if !strings.HasPrefix(got[0].ID, "evt_") || got[0].ID == got[1].ID {
		t.Fatalf("bad event ids %q %q", got[0].ID, got[1].ID)
	}
}

func TestCatalog(t *testing.T) {
	want := []string{"user.created", "user.updated", "user.deleted", "dept.created", "dept.updated", "dept.deleted",
		"role.created", "role.updated", "role.deleted", "notice.published"}
	c := Catalog()
	if len(c) != len(want) {
		t.Fatalf("catalog size %d", len(c))
	}
	for i, w := range want {
		if c[i].Name != w || c[i].Description == "" || !Known(w) {
			t.Fatalf("catalog[%d] = %+v", i, c[i])
		}
	}
	if Known("user.exploded") {
		t.Fatal("unknown event reported as known")
	}
}

func TestPayloadsHaveNoSensitiveFields(t *testing.T) {
	now := time.Now()
	pid := idgen.ID(5)
	payloads := []any{
		UserData{ID: 1, TenantID: 2, Username: "a", Nickname: "b", Status: "1", DeptID: 3},
		DeptData{ID: 1, TenantID: 2, Name: "d", ParentID: &pid, Status: "1"},
		RoleData{ID: 1, TenantID: 2, Name: "r", Code: "c", Status: "1"},
		NoticeData{ID: 1, TenantID: 2, Title: "t", Type: "x", Level: "y", Scope: "all", PublishedAt: &now},
	}
	for _, p := range payloads {
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		for k := range m {
			if isSensitive(k) {
				t.Fatalf("%T exposes sensitive key %q", p, k)
			}
		}
		if _, ok := m["id"]; !ok {
			t.Fatalf("%T has no id", p)
		}
		if _, ok := m["tenantId"]; !ok {
			t.Fatalf("%T has no tenantId", p)
		}
	}
}

func TestScrubRemovesSensitiveKeysRecursively(t *testing.T) {
	in := map[string]any{
		"id": idgen.ID(1850000000000000001), "username": "u", "Password": "x", "passwordHash": "h",
		"nested": map[string]any{"appSecret": "s", "ok": 1, "list": []any{map[string]any{"accessToken": "t", "name": "n"}}},
	}
	raw, err := Scrub(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, bad := range []string{"Password", "passwordHash", "appSecret", "accessToken", `"h"`, `"s"`} {
		if strings.Contains(s, bad) {
			t.Fatalf("scrubbed payload still contains %s: %s", bad, s)
		}
	}
	if !strings.Contains(s, `"1850000000000000001"`) || !strings.Contains(s, `"username":"u"`) || !strings.Contains(s, `"ok":1`) {
		t.Fatalf("scrub lost legitimate fields: %s", s)
	}
}
