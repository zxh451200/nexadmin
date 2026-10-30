package user

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
)

type fakeStore struct {
	mu     sync.Mutex
	users  map[string]*forgotUser // byEmail 按 key 查
	phones map[string]*forgotUser // byPhone 按归一化手机号查
	hashes map[idgen.ID]string
	sets   int
}

func (f *fakeStore) byEmail(_ context.Context, e string) (*forgotUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[e], nil
}

func (f *fakeStore) byPhone(_ context.Context, p string) (*forgotUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.phones == nil {
		return nil, nil
	}
	return f.phones[p], nil
}

func (f *fakeStore) setPassword(_ context.Context, id idgen.ID, h string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hashes[id] = h
	f.sets++
	return nil
}

type mailbox struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (m *mailbox) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	m.sent = append(m.sent, msg)
	m.mu.Unlock()
	return nil
}

func (m *mailbox) all() []mail.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mail.Message(nil), m.sent...)
}

type fgEnv struct {
	f  *forgotService
	st *fakeStore
	mb *mailbox
	ns *notify.Service
}

func newFgEnv(t *testing.T, configured bool) *fgEnv {
	t.Helper()
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	ns := notify.New(nil, c, zap.NewNop())
	t.Cleanup(ns.Close)
	mb := &mailbox{}
	ns.SetMailer(func(notify.MailSettings) (notify.MailSender, error) { return mb, nil })
	ns.SetSettings(func(context.Context) (notify.Settings, error) {
		st := notify.Settings{SiteName: "Nex", Lang: "en-US"}
		if configured {
			st.Mail = notify.MailSettings{Host: "smtp.example.org", Port: 25, FromAddr: "noreply@example.org"}
		}
		return st, nil
	})
	st := &fakeStore{users: map[string]*forgotUser{"alice@example.org": {ID: 1, Username: "alice", Name: "Alice"}}, hashes: map[idgen.ID]string{}}
	return &fgEnv{f: newForgotService(st, c, ns, nil, zap.NewNop()), st: st, mb: mb, ns: ns}
}

var codeRe = regexp.MustCompile(`\b(\d{6})\b`)

// codeFor 发送验证码并从（假）邮箱里取出它。
func (e *fgEnv) codeFor(t *testing.T, email string) string {
	t.Helper()
	before := len(e.mb.all())
	if err := e.f.sendCode(context.Background(), email, "1.1.1.1", "en"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400; i++ {
		if m := e.mb.all(); len(m) > before {
			return codeRe.FindString(m[len(m)-1].Text)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("verification mail was not sent")
	return ""
}

func status(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

func TestForgotNoEnumeration(t *testing.T) {
	e := newFgEnv(t, true)
	ctx := context.Background()
	if err := e.f.sendCode(ctx, "ghost@example.org", "1.1.1.1", ""); err != nil {
		t.Fatalf("unknown account must look like success, got %v", err)
	}
	if err := e.f.sendCode(ctx, "Alice@Example.org", "1.1.1.1", ""); err != nil {
		t.Fatal(err)
	}
	// 冷却期内两种账号的失败响应也一致。
	for _, a := range []string{"ghost@example.org", "alice@example.org"} {
		if err := e.f.sendCode(ctx, a, "1.1.1.1", ""); status(err) != 429 {
			t.Fatalf("%s: second send should be 429, got %v", a, err)
		}
	}
	e.ns.Close() // 排空队列
	sent := e.mb.all()
	if len(sent) != 1 || sent[0].To[0] != "alice@example.org" {
		t.Fatalf("only the existing account gets mail, got %+v", sent)
	}
	if err := e.f.reset(ctx, "ghost@example.org", "123456", "Passw0rd!!", "1.1.1.1", ""); status(err) != 400 || err.Error() != badCode().Error() {
		t.Fatalf("unknown account reset must fail like a wrong code, got %v", err)
	}
}

func TestForgotCodeIsSingleUse(t *testing.T) {
	e := newFgEnv(t, true)
	ctx := context.Background()
	code := e.codeFor(t, "alice@example.org")
	if err := e.f.reset(ctx, "alice@example.org", code, "", "1.1.1.1", ""); status(err) != 400 {
		t.Fatalf("empty password: %v", err)
	}
	if err := e.f.reset(ctx, "alice@example.org", code, strings.Repeat("a", 73), "1.1.1.1", ""); status(err) != 400 {
		t.Fatalf("over-72-byte password: %v", err)
	}
	if err := e.f.reset(ctx, "alice@example.org", code, "abc", "1.1.1.1", "en"); err != nil { // 无强度规则：短密码可用
		t.Fatalf("first use (code must survive the rejected attempts): %v", err)
	}
	if e.st.sets != 1 || e.st.hashes[1] == "" || e.st.hashes[1] == "abc" {
		t.Fatalf("password must be stored hashed once: %+v", e.st.hashes)
	}
	if err := e.f.reset(ctx, "alice@example.org", code, "An0ther-Passw0rd", "1.1.1.1", ""); err == nil {
		t.Fatal("code must not be reusable")
	}
	if e.st.sets != 1 {
		t.Fatal("second reset must not touch the password")
	}
	e.ns.Close()
	if n := len(e.mb.all()); n != 2 { // 验证码邮件 + 安全告警邮件
		t.Fatalf("mails = %d, want 2", n)
	}
}

func TestForgotVoidedAfterFiveWrongTries(t *testing.T) {
	e := newFgEnv(t, true)
	ctx := context.Background()
	code := e.codeFor(t, "alice@example.org")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < 5; i++ {
		if err := e.f.reset(ctx, "alice@example.org", wrong, "N3w-Passw0rd", "1.1.1.1", ""); err == nil {
			t.Fatal("wrong code accepted")
		}
	}
	if err := e.f.reset(ctx, "alice@example.org", code, "N3w-Passw0rd", "1.1.1.1", ""); err == nil {
		t.Fatal("code must be void after 5 wrong attempts, even the right one")
	}
	if e.st.sets != 0 {
		t.Fatal("password changed despite void code")
	}
}

func TestForgotExpiryUnavailableAndIPLimit(t *testing.T) {
	e := newFgEnv(t, true)
	ctx := context.Background()
	code := e.codeFor(t, "alice@example.org")
	e.f.now = func() time.Time { return time.Now().Add(forgotTTL + time.Second) }
	if err := e.f.reset(ctx, "alice@example.org", code, "N3w-Passw0rd", "1.1.1.1", ""); err == nil {
		t.Fatal("expired code accepted")
	}
	e2 := newFgEnv(t, false) // SMTP 未配置
	if err := e2.f.sendCode(ctx, "alice@example.org", "1.1.1.1", ""); status(err) != 503 {
		t.Fatalf("unconfigured mail must be 503, got %v", err)
	}
	e3 := newFgEnv(t, true)
	var last error
	for i := 0; i < 11; i++ {
		last = e3.f.sendCode(ctx, fmt.Sprintf("u%d@example.org", i), "9.9.9.9", "")
	}
	if status(last) != 429 {
		t.Fatalf("11th request from one IP should be limited, got %v", last)
	}
}
