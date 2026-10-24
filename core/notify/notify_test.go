package notify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
)

func newSvc(t *testing.T) (*Service, *observer.ObservedLogs) {
	t.Helper()
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	core, logs := observer.New(zap.DebugLevel)
	s := New(nil, c, zap.New(core))
	s.backoff = func(int) time.Duration { return 0 }
	t.Cleanup(s.Close)
	return s, logs
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type recorder struct {
	mu    sync.Mutex
	calls [][]idgen.ID
}

func (r *recorder) ch(failFirst map[idgen.ID]bool) Channel {
	return ChannelFunc(func(_ context.Context, _ string, _ Msg, to []Recipient) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		ids := make([]idgen.ID, len(to))
		var failed []Recipient
		for i, x := range to {
			ids[i] = x.ID
			if failFirst[x.ID] {
				delete(failFirst, x.ID)
				failed = append(failed, x)
			}
		}
		r.calls = append(r.calls, ids)
		if len(failed) > 0 {
			return &PartialError{Failed: failed, Err: errors.New("boom")}
		}
		return nil
	})
}

func (r *recorder) n() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.calls) }

func TestInAppDeliveredByDefault(t *testing.T) {
	s, _ := newSvc(t)
	rec := &recorder{}
	s.Register(ChannelInApp, rec.ch(nil))
	if err := s.Send(context.Background(), Msg{To: []idgen.ID{1, 2, 2}, Title: "hi"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "in-app delivery", func() bool { return rec.n() == 1 })
	if got := rec.calls[0]; len(got) != 2 {
		t.Fatalf("recipients should be de-duplicated, got %v", got)
	}
	if err := s.Send(context.Background(), Msg{To: []idgen.ID{1}}); err == nil {
		t.Fatal("empty title must be rejected")
	}
}

func TestInAppDisabledIsNoopAndLogged(t *testing.T) {
	s, logs := newSvc(t) // 没有注册站内渠道 = message 模块被停用
	if err := s.Send(context.Background(), Msg{To: []idgen.ID{1}, Title: "hi"}); err != nil {
		t.Fatalf("Send must not fail for business code: %v", err)
	}
	waitFor(t, "skip log", func() bool {
		for _, e := range logs.All() {
			if strings.Contains(e.ContextMap()["error"].(string), "in-app channel unavailable") {
				return true
			}
		}
		return false
	})
	var nilSvc *Service // Deps.Notify 为 nil（手工构造的 Deps）同样安全
	if err := nilSvc.Send(context.Background(), Msg{To: []idgen.ID{1}, Title: "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestRetryTwiceThenGiveUp(t *testing.T) {
	s, logs := newSvc(t)
	var mu sync.Mutex
	n := 0
	s.Register("flaky", ChannelFunc(func(context.Context, string, Msg, []Recipient) error {
		mu.Lock()
		defer mu.Unlock()
		n++
		return errors.New("down")
	}))
	_ = s.Send(context.Background(), Msg{To: []idgen.ID{1}, Title: "x", Channels: []string{"flaky"}})
	waitFor(t, "error log", func() bool { return logs.FilterMessage("notify: delivery failed after retries").Len() == 1 })
	mu.Lock()
	defer mu.Unlock()
	if n != 3 {
		t.Fatalf("attempts = %d, want 3 (1 + 2 retries)", n)
	}
}

func TestPartialFailureRetriesOnlyFailed(t *testing.T) {
	s, _ := newSvc(t)
	rec := &recorder{}
	s.Register(ChannelInApp, rec.ch(map[idgen.ID]bool{2: true}))
	_ = s.Send(context.Background(), Msg{To: []idgen.ID{1, 2}, Title: "x"})
	waitFor(t, "retry", func() bool { return rec.n() == 2 })
	if len(rec.calls[1]) != 1 || rec.calls[1][0] != 2 {
		t.Fatalf("second attempt should only target user 2, got %v", rec.calls[1])
	}
}

func TestPerUserChannelRateLimit(t *testing.T) {
	s, logs := newSvc(t)
	s.limits[ChannelInApp] = limit{2, time.Minute}
	rec := &recorder{}
	s.Register(ChannelInApp, rec.ch(nil))
	for i := 0; i < 3; i++ {
		_ = s.Send(context.Background(), Msg{To: []idgen.ID{7}, Title: "x"})
	}
	waitFor(t, "rate limit log", func() bool { return logs.FilterMessage("notify: recipient rate limited").Len() == 1 })
	waitFor(t, "two deliveries", func() bool { return rec.n() >= 2 })
	_ = s.Send(context.Background(), Msg{To: []idgen.ID{8}, Title: "x"}) // 另一个用户不受影响
	waitFor(t, "other user", func() bool { return rec.n() == 3 })
}

type dir map[idgen.ID]Contact

func (d dir) Contacts(_ context.Context, ids []idgen.ID) (map[idgen.ID]Contact, error) { return d, nil }

type capMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (c *capMailer) Send(_ context.Context, m mail.Message) error {
	c.mu.Lock()
	c.sent = append(c.sent, m)
	c.mu.Unlock()
	return nil
}

func TestEmailUsesProfileAddressEscapesAndSkips(t *testing.T) {
	s, logs := newSvc(t)
	cm := &capMailer{}
	s.SetMailer(func(MailSettings) (MailSender, error) { return cm, nil })
	s.SetDirectory(dir{1: {Name: "<b>Bob</b>", Email: "bob@example.org"}, 2: {Name: "NoMail"}})
	s.SetSettings(func(context.Context) (Settings, error) {
		return Settings{SiteName: "Nex", Lang: "en-US", Mail: MailSettings{Host: "h", Port: 25, FromAddr: "a@example.org"}}, nil
	})
	_ = s.Send(context.Background(), Msg{To: []idgen.ID{1, 2}, Title: "<script>alert(1)</script>", Content: "a & b", Channels: []string{ChannelEmail}})
	waitFor(t, "mail", func() bool { cm.mu.Lock(); defer cm.mu.Unlock(); return len(cm.sent) == 1 })
	m := cm.sent[0]
	if m.To[0] != "bob@example.org" || strings.Contains(m.HTML, "<script>") || !strings.Contains(m.HTML, "&lt;script&gt;") || !strings.Contains(m.HTML, "a &amp; b") {
		t.Fatalf("html not escaped: %+v", m)
	}
	if logs.FilterMessage("notify: recipient skipped, no email on profile").Len() != 1 {
		t.Fatal("user without email must be skipped with a log line")
	}
}

func TestMailNotConfigured(t *testing.T) {
	s, _ := newSvc(t)
	if s.MailConfigured(context.Background()) {
		t.Fatal("unset settings must mean not configured")
	}
	if err := s.SendMail(context.Background(), "a@example.org", mail.TplForgotCode, "", mail.Data{}); !errors.Is(err, ErrMailNotConfigured) {
		t.Fatalf("err = %v", err)
	}
}
