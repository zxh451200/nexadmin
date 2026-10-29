package user

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"nexadmin/core/response"
)

// fakeSms 是 smsSender 的假实现：记录发送的手机号与 deliver，Verify 只认 goodCode。
type fakeSms struct {
	mu       sync.Mutex
	enabled  bool
	goodCode string
	sent     []string // "phone:deliver" 列表
	verifies int
}

func (s *fakeSms) Enabled() bool { return s != nil && s.enabled }

func (s *fakeSms) Send(_ context.Context, phone, _ string, deliver bool) error {
	if !s.enabled {
		return response.Unavailable("auth.provider_missing").WithMsg("短信服务未启用")
	}
	s.mu.Lock()
	s.sent = append(s.sent, phone+":"+strconv.FormatBool(deliver))
	s.mu.Unlock()
	return nil
}

func (s *fakeSms) Verify(_ context.Context, _, code string) error {
	s.mu.Lock()
	s.verifies++
	s.mu.Unlock()
	if code == s.goodCode {
		return nil
	}
	return badCode()
}

func newPhoneEnv(t *testing.T, smsEnabled, mailConfigured bool) (*fgEnv, *fakeSms) {
	t.Helper()
	e := newFgEnv(t, mailConfigured)
	sms := &fakeSms{enabled: smsEnabled, goodCode: "246810"}
	e.f.sms = sms
	e.st.phones = map[string]*forgotUser{
		"13800138000": {ID: 7, Username: "bob", Name: "Bob", Email: "bob@example.org"},
	}
	return e, sms
}

func errStatus(t *testing.T, err error) int {
	t.Helper()
	var re *response.Error
	if !errors.As(err, &re) {
		t.Fatalf("unexpected error %v", err)
	}
	return re.Status
}

// 绑定了手机号的账号才真正发送；未绑定的响应一致但不发送（防枚举）。
func TestForgotPhoneSendDeliversOnlyForBound(t *testing.T) {
	e, sms := newPhoneEnv(t, true, false)
	ctx := context.Background()
	if err := e.f.sendCode(ctx, "13800138000", "1.1.1.1", "en"); err != nil {
		t.Fatal(err)
	}
	if err := e.f.sendCode(ctx, "13900139000", "1.1.1.1", "en"); err != nil {
		t.Fatal(err)
	}
	if len(sms.sent) != 2 || sms.sent[0] != "13800138000:true" || sms.sent[1] != "13900139000:false" {
		t.Fatalf("sent = %v", sms.sent)
	}
}

// 短信未配置时手机号找回直接 503；格式错误 400。
func TestForgotPhoneSendGuards(t *testing.T) {
	e, _ := newPhoneEnv(t, false, false)
	ctx := context.Background()
	if s := errStatus(t, e.f.sendCode(ctx, "13800138000", "1.1.1.1", "en")); s != 503 {
		t.Fatalf("status = %d", s)
	}
	e.f.sms.(*fakeSms).enabled = true
	if s := errStatus(t, e.f.sendCode(ctx, "12345", "1.1.1.1", "en")); s != 400 {
		t.Fatalf("bad phone status = %d", s)
	}
}

// 手机号 + 验证码重置成功：写新密码、清登录锁、给已知邮箱发安全告警。
func TestForgotPhoneResetSuccess(t *testing.T) {
	e, _ := newPhoneEnv(t, true, true)
	ctx := context.Background()
	before := len(e.mb.all())
	err := e.f.reset(ctx, "13800138000", "246810", "NewPass123", "1.1.1.1", "en")
	if err != nil {
		t.Fatal(err)
	}
	if e.st.sets != 1 || e.st.hashes[7] == "" {
		t.Fatalf("password not set: sets=%d", e.st.sets)
	}
	for i := 0; i < 400; i++ { // 告警邮件异步投递，轮询等它到
		if len(e.mb.all()) > before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("security alert not sent, mails=%d", len(e.mb.all()))
}

// 错误验证码 / 验证码正确但手机号没绑定：都不改密码。
func TestForgotPhoneResetRejects(t *testing.T) {
	e, _ := newPhoneEnv(t, true, false)
	ctx := context.Background()
	if s := errStatus(t, e.f.reset(ctx, "13800138000", "000000", "NewPass123", "1.1.1.1", "en")); s != 400 {
		t.Fatalf("wrong code status = %d", s)
	}
	if s := errStatus(t, e.f.reset(ctx, "13900139000", "246810", "NewPass123", "1.1.1.1", "en")); s != 400 {
		t.Fatalf("unbound phone status = %d", s)
	}
	if e.st.sets != 0 {
		t.Fatalf("password must not be set, sets=%d", e.st.sets)
	}
}

// 没绑定邮箱的账号重置成功后不发告警邮件（尽力而为，不报错）。
func TestForgotPhoneResetWithoutEmailNoAlert(t *testing.T) {
	e, _ := newPhoneEnv(t, true, true)
	e.st.phones["13800138000"].Email = ""
	before := len(e.mb.all())
	if err := e.f.reset(context.Background(), "13800138000", "246810", "NewPass123", "1.1.1.1", "en"); err != nil {
		t.Fatal(err)
	}
	if len(e.mb.all()) != before {
		t.Fatalf("unexpected alert mail, mails=%d", len(e.mb.all()))
	}
}
