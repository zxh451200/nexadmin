package sendlog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/notify"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/mail"
)

func init() { gin.SetMode(gin.TestMode) }

func tctx(id idgen.ID) context.Context { return tenant.With(context.Background(), id) }

// memSink 收集 Service 落库的行（替换 insert 缝），可注入失败 / 阻塞。
type memSink struct {
	mu   sync.Mutex
	rows []SendLog
	err  error
	gate chan struct{} // 非 nil 时 insert 阻塞到被关闭
}

func (m *memSink) insert(_ context.Context, l *SendLog) error {
	if m.gate != nil {
		<-m.gate
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.rows = append(m.rows, *l)
	return nil
}

func (m *memSink) all() []SendLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]SendLog(nil), m.rows...)
}

func newSinkSvc(t *testing.T, sink *memSink) (*Service, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.DebugLevel)
	s := NewService(nil, "UTC", zap.New(core))
	s.insert = sink.insert
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

func TestRecordStoresMaskedAndSanitizedRow(t *testing.T) {
	sink := &memSink{}
	s, _ := newSinkSvc(t, sink)
	s.Record(tctx(7), notify.Record{
		Channel: notify.RecordSMS, Scene: notify.SceneVerifyCode, Recipient: "13812345678", Subject: "login",
		Status: notify.StatusFail, Duration: 1500 * time.Millisecond,
		Err: `aliyun: InvalidParameter phone=13812345678 code=123456 token=abcdef12345 Post "https://dysmsapi.aliyuncs.com/?Signature=XYZ"` + strings.Repeat(" 很长", 400),
	})
	waitFor(t, "row", func() bool { return len(sink.all()) == 1 })
	r := sink.all()[0]
	if r.TenantID != 7 || r.Channel != "sms" || r.Scene != "verify_code" || r.Status != "fail" || r.DurationMs != 1500 {
		t.Fatalf("row = %+v", r)
	}
	if r.Recipient != "138****5678" {
		t.Fatalf("recipient must be masked, got %q", r.Recipient)
	}
	for _, leak := range []string{"13812345678", "123456", "abcdef12345", "XYZ", "Signature"} {
		if strings.Contains(r.Error, leak) || strings.Contains(r.Recipient, leak) || strings.Contains(r.Subject, leak) {
			t.Fatalf("%q leaked into the stored row: %+v", leak, r)
		}
	}
	if n := len([]rune(r.Error)); n > MaxError {
		t.Fatalf("error not truncated: %d characters", n)
	}
}

func TestRecordNeverStoresVerificationCodeOrBody(t *testing.T) {
	sink := &memSink{}
	s, _ := newSinkSvc(t, sink)
	// 即使上游错误地把验证码、正文放进标题 / 错误，落库前也被清理。
	s.Record(tctx(1), notify.Record{Channel: notify.RecordEmail, Scene: notify.SceneVerifyCode, Recipient: "alice@example.com",
		Subject: "您的验证码 482913", Err: "smtp 550 rejected: Your code is 482913", Status: notify.StatusFail})
	waitFor(t, "row", func() bool { return len(sink.all()) == 1 })
	r := sink.all()[0]
	if strings.Contains(r.Subject+r.Error, "482913") || r.Recipient != "a***@example.com" {
		t.Fatalf("row = %+v", r)
	}
}

func TestRecordDropsWithoutTenantAndInvalidInput(t *testing.T) {
	sink := &memSink{}
	s, logs := newSinkSvc(t, sink)
	s.Record(context.Background(), notify.Record{Channel: "sms", Status: "success"}) // 没有租户
	s.Record(tctx(1), notify.Record{Channel: "carrier-pigeon", Status: "success"})   // 未知渠道
	s.Record(tctx(1), notify.Record{Channel: "sms", Status: "weird", Recipient: "13812345678"})
	waitFor(t, "one valid row", func() bool { return len(sink.all()) == 1 })
	if got := sink.all()[0].Status; got != "fail" {
		t.Fatalf("an unknown status must not be stored as-is, got %q", got)
	}
	if logs.FilterMessage("sendlog: no tenant in context, record dropped").Len() != 1 {
		t.Fatal("a record without tenant must be dropped with a warning, never written under another tenant")
	}
	var nilSvc *Service
	nilSvc.Record(tctx(1), notify.Record{Channel: "sms"}) // nil 安全
	nilSvc.Close()
}

func TestRecordsCarryTheirOwnTenant(t *testing.T) {
	sink := &memSink{}
	s, _ := newSinkSvc(t, sink)
	s.Record(tctx(1), notify.Record{Channel: "sms", Recipient: "13800000001", Status: "success"})
	s.Record(tctx(2), notify.Record{Channel: "sms", Recipient: "13800000002", Status: "success"})
	waitFor(t, "rows", func() bool { return len(sink.all()) == 2 })
	got := map[string]idgen.ID{}
	for _, r := range sink.all() {
		got[r.Recipient] = r.TenantID
	}
	if got["138****0001"] != 1 || got["138****0002"] != 2 {
		t.Fatalf("tenant mix-up: %+v", got)
	}
}

func TestRecordDoesNotBlockWhenWriterIsStuck(t *testing.T) {
	sink := &memSink{gate: make(chan struct{})}
	s, logs := newSinkSvc(t, sink)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < queueSize+workers+50; i++ { // 写入全部卡住，队列塞满后继续记也必须立刻返回
			s.Record(tctx(1), notify.Record{Channel: "email", Recipient: "a@example.com", Status: "success"})
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Record blocked while the writer was stuck")
	}
	if logs.FilterMessage("sendlog: queue full, record dropped").Len() == 0 {
		t.Fatal("dropped records must be logged")
	}
	close(sink.gate)
}

func TestWriteFailureOnlyLogsAndDoesNotAffectDelivery(t *testing.T) {
	sink := &memSink{err: errors.New("db down")}
	s, logs := newSinkSvc(t, sink)

	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	nf := notify.New(nil, c, zap.NewNop())
	t.Cleanup(nf.Close)
	nf.SetRecorder(s)
	nf.SetSettings(func(context.Context) (notify.Settings, error) {
		return notify.Settings{Lang: "en-US", Mail: notify.MailSettings{Host: "h", Port: 25, FromAddr: "a@example.org"}}, nil
	})
	sender := &capSender{}
	nf.SetMailer(func(notify.MailSettings) (notify.MailSender, error) { return sender, nil })

	if err := nf.SendMail(tctx(1), "bob@example.org", mail.TplNotice, "", mail.Data{Title: "hello"}); err != nil {
		t.Fatalf("SendMail must succeed even though the log table is unwritable: %v", err)
	}
	waitFor(t, "mail delivered", func() bool { return sender.n() == 1 })
	waitFor(t, "write failure logged", func() bool { return logs.FilterMessage("sendlog: write failed, record dropped").Len() == 1 })
	if len(sink.all()) != 0 {
		t.Fatal("nothing should have been stored")
	}
}

type capSender struct {
	mu sync.Mutex
	n_ int
}

func (c *capSender) Send(context.Context, mail.Message) error {
	c.mu.Lock()
	c.n_++
	c.mu.Unlock()
	return nil
}
func (c *capSender) n() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n_ }

func TestNotifyToSendlogEndToEnd(t *testing.T) {
	sink := &memSink{}
	s, _ := newSinkSvc(t, sink)
	c, _ := cache.New(config.Cache{Driver: "memory"})
	nf := notify.New(nil, c, zap.NewNop())
	t.Cleanup(nf.Close)
	nf.SetRecorder(s)
	nf.SetSettings(func(context.Context) (notify.Settings, error) {
		return notify.Settings{Mail: notify.MailSettings{Host: "h", Port: 25, FromAddr: "a@example.org"}}, nil
	})
	nf.SetMailer(func(notify.MailSettings) (notify.MailSender, error) { return &capSender{}, nil })
	if err := nf.SendMail(tctx(9), "bob@example.org", mail.TplForgotCode, "", mail.Data{Code: "246810"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "row", func() bool { return len(sink.all()) == 1 })
	r := sink.all()[0]
	if r.TenantID != 9 || r.Channel != "email" || r.Scene != "verify_code" || r.Subject != mail.TplForgotCode ||
		r.Status != "success" || r.Recipient != "b***@example.org" {
		t.Fatalf("row = %+v", r)
	}
	if strings.Contains(r.Subject+r.Error+r.Recipient, "246810") {
		t.Fatal("verification code stored")
	}
}

func TestCloseDrainsQueueAndLateRecordsAreWrittenSynchronously(t *testing.T) {
	sink := &memSink{}
	s, _ := newSinkSvc(t, sink)
	for i := 0; i < 20; i++ {
		s.Record(tctx(1), notify.Record{Channel: "sms", Recipient: "13800000000", Status: "success"})
	}
	s.Close()
	if len(sink.all()) != 20 {
		t.Fatalf("Close must drain the queue, got %d", len(sink.all()))
	}
	s.Record(tctx(1), notify.Record{Channel: "sms", Recipient: "13800000000", Status: "success"}) // notify 排空自己的队列时才到
	if len(sink.all()) != 21 {
		t.Fatal("records arriving after Close must still be written")
	}
}

// ---- 租户隔离（走真实 SQL 构造，sqlmock 校验 tenant_id 条件与参数） ----

func newDBSvc(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(db, "UTC", zap.NewNop())
	t.Cleanup(s.Close)
	return s, mock
}

func TestQueriesAreScopedToTenant(t *testing.T) {
	s, mock := newDBSvc(t)
	ctx := tctx(7)
	q := Query{Channel: "sms", Status: "fail", Recipient: "138", StartDate: "2025-01-01", EndDate: "2025-01-31", Page: 1, PageSize: 10}

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_send_log` WHERE tenant_id = \\? AND channel = \\? AND status = \\? AND recipient LIKE \\?").
		WithArgs(int64(7), "sms", "fail", "%138%", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	mock.ExpectQuery("FROM `sys_send_log` WHERE tenant_id = \\? AND channel = \\?").
		WithArgs(int64(7), "sms", "fail", "%138%", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, _, err := s.List(ctx, q); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("FROM `sys_send_log` WHERE tenant_id = \\? AND id = \\?").
		WithArgs(int64(7), int64(99), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"})) // 别的租户的 id：当前租户查不到
	if _, err := s.Get(ctx, 99); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("a row of another tenant must look like it does not exist, got %v", err)
	}

	mock.ExpectExec("DELETE FROM `sys_send_log` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(7), int64(1), int64(2)).WillReturnResult(sqlmock.NewResult(0, 2))
	if n, err := s.Delete(ctx, []idgen.ID{1, 2}); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}

	mock.ExpectExec("DELETE FROM `sys_send_log` WHERE tenant_id = \\? AND created_at < \\?").
		WithArgs(int64(7), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 5))
	if n, err := s.Clean(ctx, 30); err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestQueriesWithoutTenantFailClosedAndSendNoSQL(t *testing.T) {
	s, mock := newDBSvc(t) // sqlmock 对任何未预期的语句都会报错
	ctx := context.Background()
	if _, _, err := s.List(ctx, Query{Page: 1, PageSize: 10}); !errors.Is(err, database.ErrNoTenant) {
		t.Fatalf("List err = %v", err)
	}
	if _, err := s.Get(ctx, 1); !errors.Is(err, database.ErrNoTenant) {
		t.Fatalf("Get err = %v", err)
	}
	if _, err := s.Delete(ctx, []idgen.ID{1}); !errors.Is(err, database.ErrNoTenant) {
		t.Fatalf("Delete err = %v", err)
	}
	if _, err := s.Clean(ctx, 1); !errors.Is(err, database.ErrNoTenant) {
		t.Fatalf("Clean err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ---- HTTP 层 ----

func newRouter(s *Service, tid idgen.ID) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), tid))
	})
	h := &Handler{svc: s}
	e.GET("/sendlogs", h.List)
	e.GET("/sendlogs/:id", h.Get)
	e.DELETE("/sendlogs", h.Delete)
	e.POST("/sendlogs/clean", h.Clean)
	return e
}

func do(e *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func TestHandlerRejectsBadInputWithoutTouchingDB(t *testing.T) {
	s, mock := newDBSvc(t)
	e := newRouter(s, 7)
	for name, w := range map[string]*httptest.ResponseRecorder{
		"bad channel":  do(e, "GET", "/sendlogs?channel=pigeon", ""),
		"bad status":   do(e, "GET", "/sendlogs?status=maybe", ""),
		"bad date":     do(e, "GET", "/sendlogs?startDate=yesterday", ""),
		"delete empty": do(e, "DELETE", "/sendlogs", ""),
		"delete junk":  do(e, "DELETE", "/sendlogs?ids=1,abc", ""),
		"clean empty":  do(e, "POST", "/sendlogs/clean", `{}`),
		"clean zero":   do(e, "POST", "/sendlogs/clean", `{"days":0}`),
		"clean huge":   do(e, "POST", "/sendlogs/clean", `{"days":99999}`),
	} {
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (%s)", name, w.Code, w.Body.String())
		}
	}
	if w := do(e, "GET", "/sendlogs/not-a-number", ""); w.Code != http.StatusNotFound {
		t.Errorf("bad id: status = %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestHandlerGetOtherTenantsRowIs404(t *testing.T) {
	s, mock := newDBSvc(t)
	mock.ExpectQuery("FROM `sys_send_log` WHERE tenant_id = \\? AND id = \\?").
		WithArgs(int64(8), int64(5), 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if w := do(newRouter(s, 8), "GET", "/sendlogs/5", ""); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
