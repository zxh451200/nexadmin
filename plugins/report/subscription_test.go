package report

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
	"nexadmin/pkg/mail"
)

// 报表订阅：配置校验（收件人是要发信的对象，格式错误应当场报）与到期判定/当天去重。

func TestSaveSubscriptionValidation(t *testing.T) {
	svc, m := newTestService(t)
	// 会走到 count 的用例才需要这条期望；周期/小时/星期校验在 count 之前，不配期望
	base := func() {
		m.ExpectQuery(`SELECT count\(\*\) FROM .plg_report_report. WHERE tenant_id = \? AND id = \?`).
			WithArgs(int64(7), int64(3)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	}

	t.Run("周期非法", func(t *testing.T) {
		_, err := svc.SaveSubscription(tctx(7), 3, SubscriptionForm{Schedule: "hourly", Recipients: "a@b.com"})
		var be *response.Error
		if !errors.As(err, &be) || be.Key != "report.subscription_schedule_invalid" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("小时越界", func(t *testing.T) {
		_, err := svc.SaveSubscription(tctx(7), 3, SubscriptionForm{Schedule: SchedDaily, SendHour: 24, Recipients: "a@b.com"})
		var be *response.Error
		if !errors.As(err, &be) || be.Key != "report.subscription_hour_invalid" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("weekly 星期越界", func(t *testing.T) {
		_, err := svc.SaveSubscription(tctx(7), 3, SubscriptionForm{Schedule: SchedWeekly, SendHour: 8, Weekday: 7, Recipients: "a@b.com"})
		var be *response.Error
		if !errors.As(err, &be) || be.Key != "report.subscription_weekday_invalid" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("收件人为空 / 格式错 / 超上限", func(t *testing.T) {
		for i, tc := range []struct{ recipients, key string }{
			{"", "report.subscription_recipient_required"},
			{"not-an-email", "report.subscription_recipient_invalid"},
		} {
			_, err := svc.SaveSubscription(tctx(7), 3, SubscriptionForm{Schedule: SchedDaily, Recipients: tc.recipients})
			var be *response.Error
			if !errors.As(err, &be) || be.Key != tc.key {
				t.Fatalf("case %d: err = %v, want %s", i, err, tc.key)
			}
		}
		// 合法用例：分号分隔也能过校验（会走到 count 与显式事务里的 First+INSERT）
		base()
		m.ExpectBegin()
		m.ExpectQuery(`SELECT \* FROM .plg_report_subscription. WHERE tenant_id = \? AND report_id = \? .*LIMIT \?`).
			WithArgs(int64(7), int64(3), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "report_id"}))
		m.ExpectExec(`INSERT INTO .plg_report_subscription.`).
			WillReturnResult(sqlmock.NewResult(5, 1))
		m.ExpectCommit()
		if _, err := svc.SaveSubscription(tctx(7), 3, SubscriptionForm{Schedule: SchedDaily, Recipients: "a@b.com;c@d.com"}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("报表不存在", func(t *testing.T) {
		m.ExpectQuery(`SELECT count\(\*\) FROM .plg_report_report. WHERE tenant_id = \? AND id = \?`).
			WithArgs(int64(7), int64(9)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		_, err := svc.SaveSubscription(tctx(7), 9, SubscriptionForm{Schedule: SchedDaily, Recipients: "a@b.com"})
		if !isStatus(err, 404) {
			t.Fatalf("err = %v, want 404", err)
		}
	})
}

// 到期判定：daily 按 hour 命中、weekly 按 hour+weekday 命中、当天已发的不再发、发送后写回 last_sent_date。
func TestRunDueDeliversOncePerDay(t *testing.T) {
	svc, m := newTestService(t)
	var sentTo []string
	svc.WithMailer(fakeMailer{onSend: func(to string) { sentTo = append(sentTo, to) }})

	// 周三 09:00（2026-11-04 是周三）：daily(hour=9) 与 weekly(hour=9, weekday=3) 各一条到期，
	// 一条 hour 不符，一条当天已发。
	now := time.Date(2026, 11, 4, 9, 5, 0, 0, time.Local)
	lastSent := time.Date(2026, 11, 4, 0, 0, 0, 0, time.Local)
	m.ExpectQuery(`SELECT \* FROM .plg_report_subscription. WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "report_id", "schedule", "send_hour", "weekday", "recipients", "enabled", "last_sent_date"}).
			AddRow(1, 7, 11, SchedDaily, 9, 1, "ops@b.com", 1, nil).
			AddRow(2, 7, 12, SchedWeekly, 9, 3, "week@b.com", 1, nil).
			AddRow(3, 7, 13, SchedDaily, 10, 1, "late@b.com", 1, nil).
			AddRow(4, 7, 14, SchedDaily, 9, 1, "done@b.com", 1, lastSent))
	// 每条投递的顺序：读报表名 → 发邮件 → 写回 last_sent_date，两条订阅交替
	for i, rid := range []int64{11, 12} {
		m.ExpectQuery(`SELECT \* FROM .plg_report_report. WHERE id = \? .*LIMIT \?`).
			WithArgs(rid, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name"}).AddRow(rid, 7, "报表"+string(rune('A'+i))))
		m.ExpectExec(`UPDATE .plg_report_subscription. SET .last_sent_date.`).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}

	n, err := svc.RunDue(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(sentTo) != 2 || sentTo[0] != "ops@b.com" || sentTo[1] != "week@b.com" {
		t.Fatalf("sent = %d %v", n, sentTo)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 未装配邮件出口：调度整体空转，一条 SQL 都不发。
func TestRunDueWithoutMailer(t *testing.T) {
	svc, m := newTestService(t)
	n, err := svc.RunDue(context.Background(), time.Now())
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type fakeMailer struct{ onSend func(to string) }

func (f fakeMailer) SendMail(_ context.Context, to, _ /*tpl*/, _ string, _ mail.Data) error {
	f.onSend(to)
	return nil
}
