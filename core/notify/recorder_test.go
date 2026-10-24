package notify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
)

type capRec struct {
	mu   sync.Mutex
	recs []Record
}

func (c *capRec) Record(_ context.Context, r Record) {
	c.mu.Lock()
	c.recs = append(c.recs, r)
	c.mu.Unlock()
}

func (c *capRec) all() []Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Record(nil), c.recs...)
}

func (c *capRec) n() int { return len(c.all()) }

func mailSettings() func(context.Context) (Settings, error) {
	return func(context.Context) (Settings, error) {
		return Settings{SiteName: "Nex", Lang: "en-US", Mail: MailSettings{Host: "h", Port: 25, FromAddr: "a@example.org", Password: "s3cr3tPass"}}, nil
	}
}

type failMailer struct{ err error }

func (f failMailer) Send(context.Context, mail.Message) error { return f.err }

func TestNoRecorderIsNoop(t *testing.T) {
	s, _ := newSvc(t)
	cm := &capMailer{}
	s.SetMailer(func(MailSettings) (MailSender, error) { return cm, nil })
	s.SetSettings(mailSettings())
	if err := s.SendMail(context.Background(), "bob@example.org", mail.TplNotice, "", mail.Data{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "mail", func() bool { cm.mu.Lock(); defer cm.mu.Unlock(); return len(cm.sent) == 1 })

	s.SetRecorder(nil) // 显式传 nil 也是空操作
	var nilSvc *Service
	nilSvc.SetRecorder(&capRec{})
	var h RecorderHolder
	h.Emit(context.Background(), nil, Record{Channel: RecordSMS})
	if h.Active() {
		t.Fatal("zero holder must be inactive")
	}
	var nilHolder *RecorderHolder
	nilHolder.Set(&capRec{})
	nilHolder.Emit(context.Background(), nil, Record{})
}

func TestSendMailRecordsResultWithoutSecrets(t *testing.T) {
	s, _ := newSvc(t)
	rec := &capRec{}
	s.SetRecorder(rec)
	s.SetSettings(mailSettings())

	cm := &capMailer{}
	s.SetMailer(func(MailSettings) (MailSender, error) { return cm, nil })
	if err := s.SendMail(context.Background(), "bob@example.org", mail.TplForgotCode, "", mail.Data{Code: "123456"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "success record", func() bool { return rec.n() == 1 })
	r := rec.all()[0]
	if r.Channel != RecordEmail || r.Scene != SceneVerifyCode || r.Subject != mail.TplForgotCode || r.Status != StatusSuccess || r.Recipient != "bob@example.org" {
		t.Fatalf("unexpected record: %+v", r)
	}
	if strings.Contains(r.Subject+r.Err+r.Recipient, "123456") {
		t.Fatalf("verification code leaked into record: %+v", r)
	}

	// 失败：重试 3 次只记一条；错误里的 SMTP 密码被抹掉。
	s.SetMailer(func(MailSettings) (MailSender, error) {
		return failMailer{errors.New("535 auth failed for password s3cr3tPass")}, nil
	})
	_ = s.SendMail(context.Background(), "bob@example.org", mail.TplSecurityAlert, "", mail.Data{})
	waitFor(t, "fail record", func() bool { return rec.n() == 2 })
	time.Sleep(30 * time.Millisecond)
	if rec.n() != 2 {
		t.Fatalf("retries must not produce extra records, got %d", rec.n())
	}
	f := rec.all()[1]
	if f.Status != StatusFail || f.Scene != SceneSecurity || strings.Contains(f.Err, "s3cr3tPass") || !strings.Contains(f.Err, "[REDACTED]") {
		t.Fatalf("unexpected failure record: %+v", f)
	}
}

func TestSendMailNotConfiguredRecordsSkipped(t *testing.T) {
	s, _ := newSvc(t)
	rec := &capRec{}
	s.SetRecorder(rec)
	if err := s.SendMail(context.Background(), "bob@example.org", mail.TplForgotCode, "", mail.Data{}); !errors.Is(err, ErrMailNotConfigured) {
		t.Fatalf("err = %v", err)
	}
	got := rec.all()
	if len(got) != 1 || got[0].Status != StatusSkipped || got[0].Channel != RecordEmail {
		t.Fatalf("records = %+v", got)
	}
	if err := s.SendMail(context.Background(), "not-an-address", mail.TplForgotCode, "", mail.Data{}); !errors.Is(err, ErrMailNotConfigured) || rec.n() != 1 {
		t.Fatal("an unparsable address must not be recorded")
	}
}

func TestChannelDeliveryRecordsPerRecipient(t *testing.T) {
	s, _ := newSvc(t)
	rec := &capRec{}
	s.SetRecorder(rec)
	s.SetDirectory(dir{
		1: {Phone: "13812345678"},
		2: {Phone: "13900001111"},
		3: {}, // 没有手机号
	})
	// 用户 2 一直失败：重试只重发 2，最终 1 成功、2 失败，3 合并成一条 skipped。
	s.Register(ChannelSMS, ChannelFunc(func(_ context.Context, _ string, _ Msg, to []Recipient) error {
		var failed []Recipient
		for _, r := range to {
			if r.ID == 2 {
				failed = append(failed, r)
			}
		}
		if len(failed) > 0 {
			return &PartialError{Failed: failed, Err: errors.New("provider rejected 13900001111")}
		}
		return nil
	}))
	if err := s.Send(context.Background(), Msg{To: []idgen.ID{1, 2, 3}, Title: "Hello", Content: "SECRET-BODY", Channels: []string{ChannelSMS}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "records", func() bool { return rec.n() == 3 })
	time.Sleep(30 * time.Millisecond)
	by := map[string]Record{}
	for _, r := range rec.all() {
		by[r.Status+"/"+r.Recipient] = r
		if r.Channel != RecordSMS || r.Scene != SceneNotice || r.Subject != "Hello" {
			t.Fatalf("unexpected record: %+v", r)
		}
		if strings.Contains(r.Subject+r.Err, "SECRET-BODY") {
			t.Fatalf("message body leaked: %+v", r)
		}
	}
	if len(by) != 3 || by["success/13812345678"].Status == "" || by["fail/13900001111"].Err == "" || by["skipped/"].Err == "" {
		t.Fatalf("records = %+v", rec.all())
	}
}

func TestChannelUnavailableRecordsSkipped(t *testing.T) {
	s, _ := newSvc(t)
	rec := &capRec{}
	s.SetRecorder(rec)
	s.SetDirectory(dir{1: {Phone: "13812345678"}})
	_ = s.Send(context.Background(), Msg{To: []idgen.ID{1}, Title: "x", Channels: []string{ChannelSMS}})
	waitFor(t, "skipped record", func() bool { return rec.n() == 1 })
	if r := rec.all()[0]; r.Status != StatusSkipped || r.Recipient != "13812345678" {
		t.Fatalf("record = %+v", r)
	}
	// 站内渠道不记。
	_ = s.Send(context.Background(), Msg{To: []idgen.ID{1}, Title: "x"})
	time.Sleep(50 * time.Millisecond)
	if rec.n() != 1 {
		t.Fatal("in-app messages must not be recorded")
	}
}

type fakeRobot struct{ err error }

func (f fakeRobot) Send(context.Context, string, string) error { return f.err }

func TestRobotRecordUsesChannelNameAndScrubsWebhook(t *testing.T) {
	s, _ := newSvc(t)
	rec := &capRec{}
	s.SetRecorder(rec)
	const hook = "https://hooks.example.com/services/T000/B000/XXXXTOKEN"
	s.SetSettings(func(context.Context) (Settings, error) {
		return Settings{Robots: []RobotSettings{{Name: "ops", Type: "slack", URL: hook, Secret: "topsecret"}}}, nil
	})
	s.robots = func(RobotSettings) (RobotSender, error) {
		return fakeRobot{errors.New(`Post "` + hook + `": signed with topsecret: connection refused`)}, nil
	}
	_ = s.Send(context.Background(), Msg{Title: "deploy", Channels: []string{"robot:ops"}})
	waitFor(t, "robot record", func() bool { return rec.n() == 1 })
	r := rec.all()[0]
	if r.Channel != RecordRobot || r.Recipient != "robot:ops" || r.Status != StatusFail {
		t.Fatalf("record = %+v", r)
	}
	if strings.Contains(r.Err, "XXXXTOKEN") || strings.Contains(r.Err, "topsecret") {
		t.Fatalf("webhook token or secret leaked: %q", r.Err)
	}
}

func TestTestMailAndTestRobotRecordTestScene(t *testing.T) {
	s, _ := newSvc(t)
	rec := &capRec{}
	s.SetRecorder(rec)
	cm := &capMailer{}
	s.SetMailer(func(MailSettings) (MailSender, error) { return cm, nil })
	ms := MailSettings{Host: "h", Port: 25, FromAddr: "a@example.org"}
	if err := s.TestMail(context.Background(), ms, "bob@example.org", "en-US", "Nex"); err != nil {
		t.Fatal(err)
	}
	s.robots = func(RobotSettings) (RobotSender, error) { return fakeRobot{errors.New("boom")}, nil }
	if err := s.TestRobot(context.Background(), RobotSettings{Name: "ops"}); err == nil {
		t.Fatal("robot error must still be returned to the caller")
	}
	got := rec.all()
	if len(got) != 2 || got[0].Scene != SceneTest || got[1].Scene != SceneTest || got[0].Status != StatusSuccess || got[1].Status != StatusFail {
		t.Fatalf("records = %+v", got)
	}
}

type panicRec struct{}

func (panicRec) Record(context.Context, Record) { panic("recorder exploded") }

func TestRecorderPanicDoesNotAffectDelivery(t *testing.T) {
	s, logs := newSvc(t)
	s.SetRecorder(panicRec{})
	cm := &capMailer{}
	s.SetMailer(func(MailSettings) (MailSender, error) { return cm, nil })
	s.SetSettings(mailSettings())
	if err := s.SendMail(context.Background(), "bob@example.org", mail.TplNotice, "", mail.Data{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "mail delivered", func() bool { cm.mu.Lock(); defer cm.mu.Unlock(); return len(cm.sent) == 1 })
	waitFor(t, "warn log", func() bool { return logs.FilterMessage("notify: send-log recorder panicked").Len() == 1 })
}

// 阻塞的 Recorder 只会拖住 worker 自己；Recorder 接口的契约是“不阻塞”，这里验证不注入时 worker 不受任何影响，
// 并且投递的最终结果与 Recorder 是否存在无关。
func TestDeliveryResultIndependentOfRecorder(t *testing.T) {
	for _, withRec := range []bool{false, true} {
		s, _ := newSvc(t)
		if withRec {
			s.SetRecorder(&capRec{})
		}
		cm := &capMailer{}
		s.SetMailer(func(MailSettings) (MailSender, error) { return cm, nil })
		s.SetSettings(mailSettings())
		if err := s.SendMail(context.Background(), "bob@example.org", mail.TplNotice, "", mail.Data{Title: "t"}); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "mail", func() bool { cm.mu.Lock(); defer cm.mu.Unlock(); return len(cm.sent) == 1 })
	}
}
