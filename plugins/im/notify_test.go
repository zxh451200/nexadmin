package im

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/event"
	"nexadmin/core/notify"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type sentMsg struct {
	tenant idgen.ID
	msg    notify.Msg
}

type fakeSender struct {
	mu   sync.Mutex
	sent []sentMsg
}

func (f *fakeSender) Send(ctx context.Context, m notify.Msg) error {
	tid, _ := tenant.From(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMsg{tenant: tid, msg: m})
	return nil
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

func newTestNotifier(t *testing.T, db *gorm.DB, c cache.Cache, agents []agentContact) (*notifier, *fakeSender) {
	t.Helper()
	fs := &fakeSender{}
	n := &notifier{
		ctx: context.Background(), db: db, cache: c, sender: fs,
		lang: "zh-CN", log: nopLogger{}, queue: make(chan EventData, 4),
	}
	n.findAgents = func(context.Context) ([]agentContact, error) { return agents, nil }
	return n, fs
}

func TestNotifyVisitorMessageWritesInApp(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(`FROM .plg_im_conversation. WHERE conv_no = \?`).
		WillReturnRows(convRow("IM1", StatusOpen, 3, 0))
	n, fs := newTestNotifier(t, db, memCache(t), []agentContact{{UserID: 11, TenantID: 2}})

	n.notify(EventData{ConvNo: "IM1", Seq: 3, SenderType: SenderVisitor, Preview: "你好，在吗"})

	if len(fs.sent) != 1 {
		t.Fatalf("want 1 notification, got %d", len(fs.sent))
	}
	got := fs.sent[0]
	if got.tenant != 2 {
		t.Fatalf("want tenant 2, got %d", got.tenant)
	}
	if len(got.msg.To) != 1 || got.msg.To[0] != 11 {
		t.Fatalf("unexpected recipients: %v", got.msg.To)
	}
	if got.msg.Category != notifyCategory {
		t.Fatalf("want category %q, got %q", notifyCategory, got.msg.Category)
	}
	if got.msg.Link != "/im/conversations?conv=IM1" {
		t.Fatalf("unexpected link: %q", got.msg.Link)
	}
	if len(got.msg.Channels) != 1 || got.msg.Channels[0] != notify.ChannelInApp {
		t.Fatalf("unexpected channels: %v", got.msg.Channels)
	}
	if !strings.Contains(got.msg.Title, "在线客服") || !strings.Contains(got.msg.Title, "visitor") {
		t.Fatalf("unexpected title: %q", got.msg.Title)
	}
	if got.msg.Content != "你好，在吗" {
		t.Fatalf("unexpected content: %q", got.msg.Content)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNotifySkipsMessageAlreadyRead(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(`FROM .plg_im_conversation. WHERE conv_no = \?`).
		WillReturnRows(convRow("IM1", StatusOpen, 5, 5))
	n, fs := newTestNotifier(t, db, memCache(t), []agentContact{{UserID: 11, TenantID: 2}})

	n.notify(EventData{ConvNo: "IM1", Seq: 5, SenderType: SenderVisitor})

	if len(fs.sent) != 0 {
		t.Fatalf("read message must not notify, got %d", len(fs.sent))
	}
}

func TestNotifySkipsClosedConversation(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(`FROM .plg_im_conversation. WHERE conv_no = \?`).
		WillReturnRows(convRow("IM1", StatusClosed, 3, 0))
	n, fs := newTestNotifier(t, db, memCache(t), []agentContact{{UserID: 11, TenantID: 2}})

	n.notify(EventData{ConvNo: "IM1", Seq: 3, SenderType: SenderVisitor})

	if len(fs.sent) != 0 {
		t.Fatalf("closed conversation must not notify, got %d", len(fs.sent))
	}
}

func TestNotifySkipsWhileAgentWatching(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(`FROM .plg_im_conversation. WHERE conv_no = \?`).
		WillReturnRows(convRow("IM1", StatusOpen, 3, 0))
	c := memCache(t)
	if err := c.Set(context.Background(), activeKey("IM1"), "1", agentActiveTTL); err != nil {
		t.Fatal(err)
	}
	n, fs := newTestNotifier(t, db, c, []agentContact{{UserID: 11, TenantID: 2}})

	n.notify(EventData{ConvNo: "IM1", Seq: 3, SenderType: SenderVisitor})

	if len(fs.sent) != 0 {
		t.Fatalf("agent is watching, must not notify, got %d", len(fs.sent))
	}
}

func TestNotifyCooldownPerConversation(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(`FROM .plg_im_conversation. WHERE conv_no = \?`).
		WillReturnRows(convRow("IM1", StatusOpen, 3, 0))
	m.ExpectQuery(`FROM .plg_im_conversation. WHERE conv_no = \?`).
		WillReturnRows(convRow("IM1", StatusOpen, 4, 0))
	n, fs := newTestNotifier(t, db, memCache(t), []agentContact{{UserID: 11, TenantID: 2}})

	n.notify(EventData{ConvNo: "IM1", Seq: 3, SenderType: SenderVisitor})
	n.notify(EventData{ConvNo: "IM1", Seq: 4, SenderType: SenderVisitor})

	if len(fs.sent) != 1 {
		t.Fatalf("cooldown must collapse burst into one notification, got %d", len(fs.sent))
	}
}

func TestHandleOnlyQueuesVisitorMessages(t *testing.T) {
	n, _ := newTestNotifier(t, nil, memCache(t), nil)

	n.handle(event.Event{Name: "other.event", Data: EventData{ConvNo: "IM1", SenderType: SenderVisitor}})
	n.handle(event.Event{Name: EventMessageCreated, Data: EventData{ConvNo: "IM1", Seq: 1, SenderType: SenderAgent}})
	if len(n.queue) != 0 {
		t.Fatalf("non-visitor events must not be queued, got %d", len(n.queue))
	}

	n.handle(event.Event{Name: EventMessageCreated, Data: EventData{ConvNo: "IM1", Seq: 2, SenderType: SenderVisitor}})
	n.handle(event.Event{Name: EventMessageCreated, Data: &EventData{ConvNo: "IM2", Seq: 1, SenderType: SenderVisitor}})
	if len(n.queue) != 2 {
		t.Fatalf("want 2 queued visitor events, got %d", len(n.queue))
	}
}

func TestWorkerSendsQueuedVisitorEvent(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(`FROM .plg_im_conversation. WHERE conv_no = \?`).
		WillReturnRows(convRow("IM1", StatusOpen, 2, 0))
	n, fs := newTestNotifier(t, db, memCache(t), []agentContact{{UserID: 11, TenantID: 2}})
	ctx, cancel := context.WithCancel(context.Background())
	n.ctx = ctx
	go n.run()
	defer cancel()

	n.handle(event.Event{Name: EventMessageCreated, Data: EventData{ConvNo: "IM1", Seq: 2, SenderType: SenderVisitor, Preview: "hi"}})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fs.mu.Lock()
		done := len(fs.sent) == 1
		fs.mu.Unlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("worker did not deliver the notification")
}

func TestQueryAgentsCoversPermissionSuperAndBuiltin(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(`SELECT DISTINCT u\.id AS user_id, u\.tenant_id AS tenant_id FROM sys_user AS u ` +
		`.*FROM sys_user_role AS ur JOIN sys_role r .*sys_role_menu rm .*sys_menu m .*m\.perm = \?.*r\.builtin = \?`).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "tenant_id"}).AddRow(11, 2).AddRow(12, 3))
	n := &notifier{db: db}

	got, err := n.queryAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].UserID != 11 || got[0].TenantID != 2 || got[1].TenantID != 3 {
		t.Fatalf("unexpected agents: %+v", got)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
