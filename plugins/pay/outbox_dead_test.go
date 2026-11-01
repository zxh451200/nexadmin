package pay

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

// 出站箱的两条终局此前没有闸：
//   - 「连续投不出去 → 超上限转死信」在真实进程里要打满 core/event 缓冲才触发，
//     打满会把同包其它用例一起卡死，等于没法定这条；Service.WithPublisher 就是为它开的注入点。
//   - 「投出去了 → 标 sent 并清掉旧的失败原因」写错只影响台账，没人会去看。
//
// 断言看的是 GORM 真正提交的写入内容（dest map），不是 SQL 文本：先试过用 sqlmock 的
// WithArgs，结果把 outbox.go 里 last_error 清零那行删掉用例照样绿——那是空转的闸。

type capturedWrite struct {
	table string
	dest  map[string]any
	id    int64
}

// captureOutboxWrites 记录每个 UPDATE 的表名、写入内容与目标行主键。
func captureOutboxWrites(db *gorm.DB, recs *[]capturedWrite) {
	db.Callback().Update().After("gorm:update").Register("test:captureOutboxUpdate", func(d *gorm.DB) {
		m, ok := d.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		cp := make(map[string]any, len(m))
		for k, v := range m {
			cp[k] = v
		}
		var id int64
		for _, v := range d.Statement.Vars {
			if iv, isInt := v.(int64); isInt {
				id = iv
				break
			}
		}
		*recs = append(*recs, capturedWrite{table: d.Statement.Table, dest: cp, id: id})
	})
}

// writesFor 取这一行的终局/退避写。租约抢占只写 next_retry_at 一个键，按键名滤掉：
// 它既不代表投递结论，也不该被算成一次「写结论」。
func writesFor(recs []capturedWrite, id int64) []map[string]any {
	var out []map[string]any
	for _, r := range recs {
		if r.table != "plg_pay_outbox" || r.id != id {
			continue
		}
		_, hasStatus := r.dest["status"]
		_, hasAttempts := r.dest["attempts"]
		if !hasStatus && !hasAttempts {
			continue
		}
		out = append(out, r.dest)
	}
	return out
}

func outboxRows(id int64, eventID string, payload string, attempts int) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "tenant_id", "event_id", "event_name", "payload", "status", "attempts", "next_retry_at"}).
		AddRow(id, 7, eventID, EventSucceeded, payload, OutboxPending, attempts, time.Now().UTC().Add(-time.Minute))
}

func deadPayload() string {
	raw, err := json.Marshal(EventData{EventID: "EV-DEAD-1", EventName: EventSucceeded})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// expectSweepRound 布上一轮 flushOutbox 的期望：扫表 → 抢租约 → 一次终局写。
func expectSweepRound(m sqlmock.Sqlmock, rows *sqlmock.Rows) {
	m.ExpectQuery("SELECT \\* FROM `plg_pay_outbox`").WillReturnRows(rows)
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
}

// 达到上限必须转 dead，并留下 dead_at 与可读原因——静默标成 sent
// 等于把「事件丢了」伪装成「事件投出去了」。
func TestOutboxFlipsToDeadAtMaxAttempts(t *testing.T) {
	db, m := mockDB(t)
	var recs []capturedWrite
	captureOutboxWrites(db, &recs)
	s := NewService(db, nopLog()).WithPublisher(func(context.Context, string, EventData) bool { return false })

	expectSweepRound(m, outboxRows(501, "EV-DEAD-1", deadPayload(), outboxMaxAttempt-1))
	if err := s.flushOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	finals := writesFor(recs, 501)
	if len(finals) != 1 {
		t.Fatalf("这一行的写入次数 = %d，want 1（没落到这一行就是空转）", len(finals))
	}
	last := finals[0]
	if last["status"] != OutboxDead {
		t.Fatalf("status = %v, want %s（超上限必须转死信）", last["status"], OutboxDead)
	}
	if _, ok := last["dead_at"]; !ok {
		t.Fatalf("死信要留 dead_at，实际写入 %v", last)
	}
	if reason, _ := last["last_error"].(string); reason == "" {
		t.Fatal("死信必须留原因，否则运维只能猜")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 未达上限只能累计次数并退避重投：一次总线抖动不该把资金事件判死。
func TestOutboxKeepsRetryingBelowMax(t *testing.T) {
	db, m := mockDB(t)
	var recs []capturedWrite
	captureOutboxWrites(db, &recs)
	s := NewService(db, nopLog()).WithPublisher(func(context.Context, string, EventData) bool { return false })

	expectSweepRound(m, outboxRows(502, "EV-RETRY-1", deadPayload(), 2))
	if err := s.flushOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	finals := writesFor(recs, 502)
	if len(finals) != 1 {
		t.Fatalf("结论写次数 = %d, want 1", len(finals))
	}
	last := finals[0]
	if last["status"] == OutboxDead || last["status"] == OutboxSent {
		t.Fatalf("未达上限却写了终局状态 %v", last["status"])
	}
	if got, ok := last["attempts"].(int); !ok || got != 3 {
		t.Fatalf("attempts = %v, want 3", last["attempts"])
	}
	if _, ok := last["next_retry_at"]; !ok {
		t.Fatal("必须安排下一次重试时间")
	}
}

// 投出去了：写 sent、写 sent_at、清掉上一次的 last_error——
// 旧的失败原因挂在已投递行上，运维会以为这条还没投出去。
func TestOutboxSentClearsLastError(t *testing.T) {
	db, m := mockDB(t)
	var recs []capturedWrite
	captureOutboxWrites(db, &recs)
	s := NewService(db, nopLog()).WithPublisher(func(context.Context, string, EventData) bool { return true })

	expectSweepRound(m, outboxRows(503, "EV-OK-1", deadPayload(), 3))
	if err := s.flushOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	finals := writesFor(recs, 503)
	if len(finals) != 1 {
		t.Fatalf("结论写次数 = %d, want 1", len(finals))
	}
	last := finals[0]
	if last["status"] != OutboxSent {
		t.Fatalf("status = %v, want %s", last["status"], OutboxSent)
	}
	if _, ok := last["sent_at"]; !ok {
		t.Fatal("必须记录投递时间")
	}
	if v, present := last["last_error"]; !present || v != "" {
		t.Fatalf("已投递的行必须清空 last_error，实际 %v (present=%v)", v, present)
	}
}

// 载荷不可解析＝毒丸：转死信而不是标 sent，并留下可诊断的原因。
func TestOutboxPoisonGoesToDead(t *testing.T) {
	db, m := mockDB(t)
	var recs []capturedWrite
	captureOutboxWrites(db, &recs)
	s := NewService(db, nopLog())

	rows := sqlmock.NewRows([]string{"id", "tenant_id", "event_id", "event_name", "payload", "status", "attempts", "next_retry_at"}).
		AddRow(504, 7, "EV-POISON", EventSucceeded, `{not json`, OutboxPending, 1, time.Now().UTC().Add(-time.Minute))
	m.ExpectQuery("SELECT \\* FROM `plg_pay_outbox`").WillReturnRows(rows)
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_pay_outbox`").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	if err := s.flushOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	finals := writesFor(recs, 504)
	if len(finals) != 1 {
		t.Fatalf("结论写次数 = %d, want 1", len(finals))
	}
	if finals[0]["status"] != OutboxDead {
		t.Fatalf("毒丸必须转死信，实际 %v", finals[0]["status"])
	}
	if reason, _ := finals[0]["last_error"].(string); reason == "" {
		t.Fatal("毒丸死信要留解析错误")
	}
}

// 默认口径不许被注入点带跑：没注入时仍走进程内总线，一轮恰好一次终局写
// （多写=重复投递资金事件，少写=事件静默消失）。
func TestOutboxDefaultPublisherWritesExactlyOnce(t *testing.T) {
	db, m := mockDB(t)
	var recs []capturedWrite
	captureOutboxWrites(db, &recs)
	s := NewService(db, nopLog())
	if s.publisher != nil {
		t.Fatal("默认必须是 nil（走 core/event 总线）")
	}

	expectSweepRound(m, outboxRows(505, "EV-BUS-1", deadPayload(), 0))
	if err := s.flushOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(writesFor(recs, 505)); n != 1 {
		t.Fatalf("终局写次数 = %d, want 1", n)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
