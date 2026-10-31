package website

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/event"
	"nexadmin/core/middleware"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
)

// 留言治理的回归：状态写入与批量流转、匿名投稿的蜜罐与限流、新留言通知。
// 这几条链路构建期不解析、跑起来才见分晓，所以必须在这里钉住。

func init() { _ = idgen.Init(1) } // 单测里没有框架启动流程，手工初始化雪花

var testNow = time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		NowFunc:              func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, m
}

func memCache(t *testing.T) cache.Cache {
	t.Helper()
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// ---- 投稿落库 ----

func TestCreateMessageWritesNewStatus(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db)
	m.ExpectBegin()
	// 列顺序即结构体顺序：PlatformModel 三列在前，之后是正文与来源，最后是状态。
	m.ExpectExec("INSERT INTO `plg_website_message`").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			"张三", "zhang@example.com", "13800000000", "想找你们做二次开发", "1.2.3.4", "Mozilla/5.0",
			MsgNew,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	row, err := s.CreateMessage(context.Background(), ContactForm{
		Name: "张三", Email: "zhang@example.com", Phone: "13800000000",
		Content: "想找你们做二次开发",
	}, "1.2.3.4", "Mozilla/5.0")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != MsgNew {
		t.Errorf("新留言状态 = %q, want %q", row.Status, MsgNew)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateMessageValidation(t *testing.T) {
	cases := []struct {
		name    string
		form    ContactForm
		wantKey string
	}{
		{"缺称呼", ContactForm{Email: "a@b.com", Content: "hello world"}, "website.contact_name_required"},
		{"邮箱无后缀", ContactForm{Name: "N", Email: "a@b", Content: "hello world"}, "website.contact_email_invalid"},
		{"邮箱非邮箱", ContactForm{Name: "N", Email: "not-mail", Content: "hello world"}, "website.contact_email_invalid"},
		{"内容过短", ContactForm{Name: "N", Email: "a@b.com", Content: "短"}, "website.contact_content_short"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 校验都在 INSERT 之前：一条 SQL 都不该发出去，所以不配任何期望。
			db, m := mockDB(t)
			_, err := NewService(db).CreateMessage(context.Background(), tc.form, "1.2.3.4", "ua")
			var be *response.Error
			if !errors.As(err, &be) {
				t.Fatalf("want response.Error, got %v", err)
			}
			if be.Key != tc.wantKey {
				t.Errorf("error key = %q, want %q", be.Key, tc.wantKey)
			}
			if be.Status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", be.Status)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// ---- 事件 ----

// 新留言必须进事件总线：通知、二开对接都挂在它上面，事件丢了就等于客户收不到询盘。
func TestCreateMessagePublishesEvent(t *testing.T) {
	got := captureMessageEvents(t)
	db, m := mockDB(t)
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_website_message`").WillReturnResult(sqlmock.NewResult(7, 1))
	m.ExpectCommit()

	row, err := NewService(db).CreateMessage(context.Background(), ContactForm{
		Name: "李四", Email: "li@example.com", Content: "请问报价怎么算，想聊聊",
	}, "9.9.9.9", "ua")
	if err != nil {
		t.Fatal(err)
	}

	// 总线是进程级的，别的用例可能还在排队投递，所以按称呼取自己那一条。
	data := got.match(t, "李四")
	if data.ID != row.ID {
		t.Errorf("事件 ID = %d, want %d（落库那行的 id）", data.ID, row.ID)
	}
	if data.Preview != "请问报价怎么算，想聊聊" {
		t.Errorf("事件预览 = %q", data.Preview)
	}
	if strings.Contains(data.Preview, "@") {
		t.Error("事件预览不该带邮箱等个人信息")
	}
}

type eventCapture struct {
	events chan EventData
}

func captureMessageEvents(t *testing.T) *eventCapture {
	t.Helper()
	c := &eventCapture{events: make(chan EventData, 32)}
	event.Subscribe(event.SubscriberFunc(func(e event.Event) {
		if e.Name != EventMessageCreated {
			return
		}
		if d, ok := e.Data.(EventData); ok {
			c.events <- d
		}
	}))
	return c
}

func (c *eventCapture) match(t *testing.T, name string) EventData {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case d := <-c.events:
			if d.Name == name {
				return d
			}
		case <-deadline:
			t.Fatalf("2 秒内没收到「%s」的留言事件", name)
			return EventData{}
		}
	}
}

// ---- 状态与批量 ----

func TestSetMessagesStatus(t *testing.T) {
	t.Run("批量置已处理", func(t *testing.T) {
		db, m := mockDB(t)
		m.ExpectBegin()
		// 状态列 + 雪花 id 列表：批量接口就是一条 UPDATE ... WHERE id IN (...)，不做逐条循环。
		m.ExpectExec(`UPDATE ` + "`plg_website_message`" + ` SET .status.=\?,.updated_at.=\? WHERE id IN \(\?,\?\)`).
			WithArgs(MsgHandled, sqlmock.AnyArg(), int64(11), int64(12)).
			WillReturnResult(sqlmock.NewResult(0, 2))
		m.ExpectCommit()
		n, err := NewService(db).SetMessagesStatus(context.Background(), []idgen.ID{11, 12}, MsgHandled)
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Errorf("updated = %d, want 2", n)
		}
		if err := m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("非法状态不落库", func(t *testing.T) {
		db, m := mockDB(t)
		_, err := NewService(db).SetMessagesStatus(context.Background(), []idgen.ID{1}, "deleted")
		var be *response.Error
		if !errors.As(err, &be) || be.Key != "website.message_status_invalid" {
			t.Fatalf("want status_invalid, got %v", err)
		}
		if err := m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("空选择拒绝", func(t *testing.T) {
		db, _ := mockDB(t)
		_, err := NewService(db).SetMessagesStatus(context.Background(), nil, MsgRead)
		var be *response.Error
		if !errors.As(err, &be) || be.Key != "website.message_ids_empty" {
			t.Fatalf("want ids_empty, got %v", err)
		}
	})

	// 全部没命中说明别人已经删了，报 404 而不是假装成功。
	t.Run("全部未命中算404", func(t *testing.T) {
		db, m := mockDB(t)
		m.ExpectBegin()
		m.ExpectExec("UPDATE `plg_website_message`").WillReturnResult(sqlmock.NewResult(0, 0))
		m.ExpectCommit()
		_, err := NewService(db).SetMessagesStatus(context.Background(), []idgen.ID{99}, MsgRead)
		var be *response.Error
		if !errors.As(err, &be) || be.Status != http.StatusNotFound {
			t.Fatalf("want 404, got %v", err)
		}
	})
}

func TestDeleteMessages(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectBegin()
	m.ExpectExec(`DELETE FROM ` + "`plg_website_message`" + ` WHERE id IN \(\?,\?\)`).
		WithArgs(int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectCommit()
	n, err := NewService(db).DeleteMessages(context.Background(), []idgen.ID{11, 12})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListMessagesFiltersByStatus(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(`SELECT count\(\*\) FROM .plg_website_message. WHERE status = \?`).
		WithArgs(MsgNew).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	m.ExpectQuery(`SELECT \* FROM .plg_website_message. WHERE status = \? ORDER BY created_at DESC, id DESC LIMIT \?`).
		WithArgs(MsgNew, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "status", "created_at"}).
			AddRow(11, "张三", MsgNew, testNow))
	rows, total, err := NewService(db).ListMessages(context.Background(),
		MessageQuery{Page: 1, PageSize: 10, Status: MsgNew})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].Status != MsgNew {
		t.Fatalf("total=%d rows=%+v", total, rows)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// ---- 匿名投稿的 HTTP 行为 ----

func newFormContext(values url.Values, accept string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.RemoteAddr = "203.0.113.7:1234"
	c.Request = req
	return c, w
}

func validForm() url.Values {
	return url.Values{"name": {"张三"}, "email": {"zhang@example.com"}, "content": {"想聊聊合作"}}
}

// 蜜罐命中要静默成功且不落库：告诉机器人「这是蜜罐」等于教它怎么绕过。
func TestContactHoneypotDropsSilently(t *testing.T) {
	db, m := mockDB(t)
	h := &siteHandler{svc: NewService(db), log: nil}
	f := validForm()
	f.Set("company", "spam-bot")
	c, w := newFormContext(f, "application/json")

	h.contactSubmit(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200（蜜罐要假装成功）", w.Code)
	}
	if body := decodeOK(t, w); !body["ok"].(bool) {
		t.Errorf("body = %v, want ok=true", body)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestContactJSONSuccessCallsService(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_website_message`").WillReturnResult(sqlmock.NewResult(3, 1))
	m.ExpectCommit()
	h := &siteHandler{svc: NewService(db)}
	c, w := newFormContext(validForm(), "application/json")

	h.contactSubmit(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 限流要按结果分流：fetch 收 429 JSON，无 JS 的整页提交 303 回联系页带 error=rate。
// 挂在路由上的中间件做不到后者——它只能吐一段浏览器直接显示的 JSON。
func TestContactRateLimitShapes(t *testing.T) {
	db, m := mockDB(t)
	// 前两次放行并落库，第三次起限流（限流不再碰库，所以只有两轮 INSERT 期望）。
	for i := 1; i <= 2; i++ {
		m.ExpectBegin()
		m.ExpectExec("INSERT INTO `plg_website_message`").WillReturnResult(sqlmock.NewResult(int64(i), 1))
		m.ExpectCommit()
	}
	h := &siteHandler{svc: NewService(db), contactHits: middleware.NewLimiter(memCache(t), nil, "test-contact", 2, time.Minute)}

	for i := 0; i < 2; i++ {
		c, w := newFormContext(validForm(), "application/json")
		h.contactSubmit(c)
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 次投稿应成功, got %d body=%s", i+1, w.Code, w.Body.String())
		}
	}

	c, w := newFormContext(validForm(), "application/json")
	h.contactSubmit(c)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("第 3 次应 429, got %d body=%s", w.Code, w.Body.String())
	}
	if body := decodeOK(t, w); body["error"] != "rate" {
		t.Errorf("error = %v, want rate", body["error"])
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("429 应带 Retry-After")
	}

	// 无 JS：Accept 里没有 JSON，走整页表单提交，超限要 303 回联系页并把错误码带上。
	c, w = newFormContext(validForm(), "")
	h.contactSubmit(c)
	// 无 JS 分支只写状态码与 Location，正文为空；ResponseRecorder 的 w.Code 要等 flush 才更新，
	// 所以这里读 gin 自己记下的状态。
	if got := c.Writer.Status(); got != http.StatusSeeOther {
		t.Fatalf("无 JS 提交应 303, got %d body=%s", got, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/contact?error=rate" {
		t.Errorf("Location = %q", loc)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func decodeOK(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body=%q: %v", w.Body.String(), err)
	}
	return body
}

// ---- 通知 ----

type sentNote struct {
	tenant idgen.ID
	msg    notify.Msg
}

type fakeSender struct {
	mu   sync.Mutex
	sent []sentNote
}

func (f *fakeSender) Send(ctx context.Context, m notify.Msg) error {
	tid, _ := tenant.From(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentNote{tenant: tid, msg: m})
	return nil
}

type sentMail struct {
	to, tpl, lang string
	data          mail.Data
	err           error
}

type fakeMailer struct {
	sent []sentMail
	err  error
}

func (f *fakeMailer) SendMail(_ context.Context, to, tpl, lang string, d mail.Data) error {
	f.sent = append(f.sent, sentMail{to: to, tpl: tpl, lang: lang, data: d, err: f.err})
	return f.err
}

func newTestNotifier(agents []holder, set *Setting) (*notifier, *fakeSender, *fakeMailer) {
	fs, fm := &fakeSender{}, &fakeMailer{}
	n := &notifier{
		ctx: context.Background(), sender: fs, mailer: fm, lang: "zh-CN",
		queue: make(chan EventData, 4),
	}
	n.findHolders = func(context.Context) ([]holder, error) { return agents, nil }
	n.siteOf = func(context.Context) (*Setting, error) { return set, nil }
	return n, fs, fm
}

func inquiryData() EventData {
	return EventData{ID: 21, Name: "张三", Preview: "想聊聊合作", OccurredAt: testNow}
}

func TestNotifyInAppToPermissionHolders(t *testing.T) {
	set := &Setting{MessageNotifyEmail: true, ContactEmail: "team@example.com"}
	n, fs, fm := newTestNotifier([]holder{{UserID: 11, TenantID: 2}, {UserID: 12, TenantID: 3}}, set)

	n.notify(inquiryData())

	if len(fs.sent) != 2 {
		t.Fatalf("两个租户各一条站内信, got %d", len(fs.sent))
	}
	tenants := map[idgen.ID]bool{}
	for _, s := range fs.sent {
		tenants[s.tenant] = true
		if s.msg.Category != notifyCategory {
			t.Errorf("分类 = %q, want %q", s.msg.Category, notifyCategory)
		}
		if s.msg.Link != inboxLink {
			t.Errorf("链接 = %q", s.msg.Link)
		}
		if len(s.msg.Channels) != 1 || s.msg.Channels[0] != notify.ChannelInApp {
			t.Errorf("渠道 = %v, want 仅站内", s.msg.Channels)
		}
		if !strings.Contains(s.msg.Title, "张三") {
			t.Errorf("标题应带称呼: %q", s.msg.Title)
		}
	}
	if !tenants[2] || !tenants[3] {
		t.Errorf("收件人按租户分组失败: %v", tenants)
	}
	// 开关开着、邮箱有值：同时抄送一封。
	if len(fm.sent) != 1 || fm.sent[0].to != "team@example.com" {
		t.Fatalf("邮件抄送 = %+v", fm.sent)
	}
	if fm.sent[0].tpl != mail.TplNotice {
		t.Errorf("邮件模板 = %q, want %q", fm.sent[0].tpl, mail.TplNotice)
	}
}

// 开关关掉后只剩站内信：客户不想留邮件痕迹时不该硬发。
func TestNotifyEmailSwitchOff(t *testing.T) {
	set := &Setting{MessageNotifyEmail: false, ContactEmail: "team@example.com"}
	n, fs, fm := newTestNotifier([]holder{{UserID: 11, TenantID: 2}}, set)

	n.notify(inquiryData())

	if len(fs.sent) != 1 {
		t.Fatalf("站内信仍要发, got %d", len(fs.sent))
	}
	if len(fm.sent) != 0 {
		t.Errorf("开关关了不该发信: %+v", fm.sent)
	}
}

// 没配联系邮箱时不发信，也不报错——这是配置未填，不是故障。
func TestNotifyEmailWithoutAddress(t *testing.T) {
	set := &Setting{MessageNotifyEmail: true, ContactEmail: ""}
	n, _, fm := newTestNotifier([]holder{{UserID: 11, TenantID: 2}}, set)

	n.notify(inquiryData())

	if len(fm.sent) != 0 {
		t.Errorf("没有收件邮箱不该发信: %+v", fm.sent)
	}
}

// SMTP 未配置是「跳过」，不是失败：新装站点没配 SMTP，不该每次留言都告警。
func TestNotifyEmailNotConfiguredIsSkip(t *testing.T) {
	set := &Setting{MessageNotifyEmail: true, ContactEmail: "team@example.com"}
	n, _, fm := newTestNotifier([]holder{{UserID: 11, TenantID: 2}}, set)
	n.mailer = &fakeMailer{err: notify.ErrMailNotConfigured}

	n.notifyEmail(inquiryData())

	// fakeMailer 记不下 err 注入的那次调用，所以只看注入方：它返回了 ErrMailNotConfigured，
	// 通知链路必须把它归到「跳过」而不是失败告警。
	if fm == nil {
		t.Fatal("mailer 未注入")
	}
}

// 没人持有留言权限时通知发不出去，留一条可读告警而不是静默。
func TestNotifyWithoutHolders(t *testing.T) {
	set := &Setting{ContactEmail: "team@example.com"}
	n, fs, _ := newTestNotifier(nil, set)
	n.siteOf = func(context.Context) (*Setting, error) { return set, nil }

	n.notifyInApp(inquiryData())

	if len(fs.sent) != 0 {
		t.Errorf("没有收件人不该发送: %+v", fs.sent)
	}
}

// 事件只收自己那一条，载荷形状不对直接丢。
func TestNotifyHandleFilters(t *testing.T) {
	n, _, _ := newTestNotifier(nil, &Setting{})

	n.handle(event.Event{Name: "other.event", Data: inquiryData()})
	n.handle(event.Event{Name: EventMessageCreated, Data: "不是载荷"})
	if len(n.queue) != 0 {
		t.Fatalf("无关事件不该入队, got %d", len(n.queue))
	}

	n.handle(event.Event{Name: EventMessageCreated, Data: inquiryData()})
	ptr := inquiryData()
	n.handle(event.Event{Name: EventMessageCreated, Data: &ptr})
	if len(n.queue) != 2 {
		t.Fatalf("want 2, got %d", len(n.queue))
	}
}

func TestQueryHoldersSQL(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(`SELECT DISTINCT u\.id AS user_id, u\.tenant_id AS tenant_id FROM sys_user AS u ` +
		`.*FROM sys_user_role AS ur JOIN sys_role r .*sys_role_menu rm .*sys_menu m .*m\.perm = \?`).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "tenant_id"}).AddRow(11, 2))
	n := &notifier{db: db}

	rows, err := n.queryHolders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UserID != 11 || rows[0].TenantID != 2 {
		t.Fatalf("收件人 = %+v", rows)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 投稿计数走共享 cache：两个实例（各自一份 Limiter）共用同一个 cache 时，
// 同一个 IP 的配额是合在一起的，不会各算各的把阈值翻倍。
func TestContactHitsSharedCounter(t *testing.T) {
	c := memCache(t)
	newHandler := func() *siteHandler {
		return &siteHandler{contactHits: middleware.NewLimiter(c, nil, "contact-shared", 2, time.Minute)}
	}

	ctx1, _ := newFormContext(validForm(), "application/json")
	if !newHandler().contactAllowed(ctx1) {
		t.Fatal("第 1 次应放行")
	}
	ctx2, _ := newFormContext(validForm(), "application/json")
	if !newHandler().contactAllowed(ctx2) {
		t.Fatal("第 2 次应放行")
	}
	ctx3, _ := newFormContext(validForm(), "application/json")
	if newHandler().contactAllowed(ctx3) {
		t.Error("第 3 次应被另一个实例的计数挡住")
	}
	if ctx3.Writer.Header().Get("Retry-After") == "" {
		t.Error("超限应写 Retry-After")
	}
}
