package ticket

// 客户邮件通知的单元测试：文案、收件地址兜底、开关闸门、设置保存校验。
// 真库链路（建单 → 回复 → 发信）在 IT（it_mysql_test.go）覆盖；sqlmock 在这里只验逻辑分支。

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/notify"
	"nexadmin/pkg/mail"
)

// fakeMailer 记录 SendMail 调用（收件地址 + 模板数据），供各闸门用例断言。
type fakeMailer struct {
	mu    sync.Mutex
	calls []struct {
		to, tpl, lang string
		data          mail.Data
	}
}

func (f *fakeMailer) SendMail(_ context.Context, to, tpl, lang string, d mail.Data) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		to, tpl, lang string
		data          mail.Data
	}{to, tpl, lang, d})
	return nil
}

func (f *fakeMailer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeMailer) last() (string, string, mail.Data) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls[len(f.calls)-1]
	return c.to, c.tpl, c.data
}

func newMailerSvc(t *testing.T, enabled bool) (*Service, *fakeMailer, sqlmock.Sqlmock) {
	t.Helper()
	db, m := mockDB(t)
	svc := NewService(db, nopLogger{}).WithMailer(&fakeMailer{})
	// Settings 读缓存：注入一条开/关状态，不再按用例各自 mock。
	row := ticketSettingRow(enabled, "https://help.example.com")
	m.ExpectQuery("SELECT \\* FROM .plg_ticket_setting.").WillReturnRows(row)
	svc.Settings(testCtx())
	return svc, svc.mailer.(*fakeMailer), m
}

func ticketSettingRow(enabled bool, portal string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "email_notify_enabled", "portal_url", "created_at", "updated_at"}).
		AddRow(1, enabled, portal, testNow, testNow)
}

func TestReporterEmailOf(t *testing.T) {
	if got := reporterEmailOf(&Ticket{ReporterEmail: "a@b.com", ReporterID: "tok-1"}); got != "a@b.com" {
		t.Fatalf("reporterEmailOf = %q, want a@b.com", got)
	}
	// 存量邮件单：reporter_email 还没回填时，reporterId 是邮箱形态就兜底。
	if got := reporterEmailOf(&Ticket{ReporterID: "cust@example.org"}); got != "cust@example.org" {
		t.Fatalf("fallback = %q, want cust@example.org", got)
	}
	if got := reporterEmailOf(&Ticket{ReporterID: "tok-1"}); got != "" {
		t.Fatalf("non-email reporterId must not be used, got %q", got)
	}
	if got := reporterEmailOf(nil); got != "" {
		t.Fatalf("nil ticket = %q", got)
	}
}

func TestComposeReporterMail(t *testing.T) {
	cases := []struct {
		lang, kind string
		wantSub    string
	}{
		{"zh-CN", EmailCreated, "工单已受理：TK1"},
		{"zh-CN", EmailReplied, "工单有新回复：TK1"},
		{"zh-CN", EmailResolved, "工单已解决：TK1"},
		{"zh-CN", EmailClosed, "工单已关闭：TK1"},
		{"en-US", EmailCreated, "Ticket received: TK1"},
		{"en-US", EmailReplied, "New reply on ticket TK1"},
		{"en-US", EmailResolved, "Ticket resolved: TK1"},
		{"en-US", EmailClosed, "Ticket closed: TK1"},
	}
	for _, c := range cases {
		title, content := composeReporterMail(c.lang, c.kind, "TK1", "进度说明")
		if title != c.wantSub {
			t.Fatalf("compose(%s,%s) title = %q, want %q", c.lang, c.kind, title, c.wantSub)
		}
		if !strings.Contains(content, "进度说明") {
			t.Fatalf("compose(%s,%s) content missing preview: %q", c.lang, c.kind, content)
		}
	}
}

func TestSendReporterEmailGates(t *testing.T) {
	ticket := &Ticket{TicketNo: "TK1", ReporterEmail: "cust@example.org", Status: StatusProcessing}

	// 关闭开关：即使有邮箱也不发。
	svc, ml, _ := newMailerSvc(t, false)
	svc.sendReporterEmail(testCtx(), ticket, EmailReplied, "hi")
	if ml.count() != 0 {
		t.Fatalf("disabled switch must not send, got %d", ml.count())
	}

	// 开关打开 + 有邮箱：发一封，标题/模板/链接都对。
	svc, ml, _ = newMailerSvc(t, true)
	svc.sendReporterEmail(testCtx(), ticket, EmailReplied, "hello")
	if ml.count() != 1 {
		t.Fatalf("enabled switch should send once, got %d", ml.count())
	}
	to, tpl, data := ml.last()
	if to != "cust@example.org" || tpl != mail.TplNotice {
		t.Fatalf("unexpected mail to=%s tpl=%s", to, tpl)
	}
	if data.Title != "工单有新回复：TK1" {
		t.Fatalf("title = %q", data.Title)
	}
	if data.Link != "https://help.example.com/ticket/view?no=TK1" {
		t.Fatalf("link = %q", data.Link)
	}

	// 没有任何邮箱：静默跳过，不报错。
	svc, ml, _ = newMailerSvc(t, true)
	svc.sendReporterEmail(testCtx(), &Ticket{TicketNo: "TK2"}, EmailReplied, "")
	if ml.count() != 0 {
		t.Fatalf("no recipient must skip, got %d", ml.count())
	}

	// mailer 未注入（测试夹具常态）：空操作。
	gdb, _ := mockDB(t)
	plain := NewService(gdb, nopLogger{})
	plain.sendReporterEmail(testCtx(), ticket, EmailReplied, "x")
}

func TestSaveSettingsValidation(t *testing.T) {
	db, m := mockDB(t)
	svc := NewService(db, nopLogger{})
	oldResolver := portalResolver
	portalResolver = fakeResolver{}
	t.Cleanup(func() { portalResolver = oldResolver })

	// 非法 scheme / 私网地址：不碰库直接拒绝。
	for _, bad := range []string{"ftp://x.com", "http://127.0.0.1:8080", "http://192.168.1.1", "http://localhost", "http://user@x.com"} {
		if _, err := svc.SaveSettings(testCtx(), SettingsInput{PortalURL: bad}); err == nil {
			t.Fatalf("portal %q should be rejected", bad)
		}
	}
	// 合法公网地址：事务内先读后 upsert（Save 的 ON DUPLICATE 语义）。
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM .plg_ticket_setting.").WillReturnRows(sqlmock.NewRows([]string{"id", "email_notify_enabled", "portal_url", "created_at", "updated_at"}))
	m.ExpectExec("INSERT INTO .plg_ticket_setting.").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	row, err := svc.SaveSettings(testCtx(), SettingsInput{EmailNotifyEnabled: true, PortalURL: "https://help.example.com"})
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if !row.EmailNotifyEnabled || row.PortalURL != "https://help.example.com" {
		t.Fatalf("saved = %+v", row)
	}
}

type fakeResolver struct{}

func (fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
}

func TestSettingsCache(t *testing.T) {
	db, m := mockDB(t)
	svc := NewService(db, nopLogger{})
	m.ExpectQuery("SELECT \\* FROM .plg_ticket_setting.").WillReturnRows(ticketSettingRow(true, ""))
	if got := svc.Settings(testCtx()); !got.EmailNotifyEnabled {
		t.Fatal("first read should see enabled=true")
	}
	// 缓存期内不再查库：直接改缓存值也不该反映出来。
	svc.settingCache.Store(&cachedSetting{value: TicketSetting{}, at: time.Now()})
	if got := svc.Settings(testCtx()); got.EmailNotifyEnabled {
		t.Fatal("cached read must not hit db again")
	}
	// SettingsFresh 绕过缓存。
	m.ExpectQuery("SELECT \\* FROM .plg_ticket_setting.").WillReturnRows(ticketSettingRow(false, ""))
	if got := svc.SettingsFresh(testCtx()); got.EmailNotifyEnabled {
		t.Fatal("fresh read should see disabled")
	}
	// 查询失败按默认值兜底（开关关），不让业务被设置表连坐。
	m.ExpectQuery("SELECT \\* FROM .plg_ticket_setting.").WillReturnError(errors.New("boom"))
	if got := svc.Settings(testCtx()); got.EmailNotifyEnabled {
		t.Fatal("db failure must fall back to defaults")
	}
}

// 编译期守卫：core/notify 的 *Service 同时满足站内信与邮件两个最小面（与生产注入一致）。
var (
	_ msgSender  = (*notify.Service)(nil)
	_ mailSender = (*notify.Service)(nil)
)
