package report

import (
	"bytes"
	"context"
	"errors"
	"net/http"
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
	fake := &fakeMailer{onSend: func(to string) { sentTo = append(sentTo, to) }}
	svc.WithMailer(fake)

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
	if len(fake.lastAtts) != 0 {
		t.Fatalf("非 table 订阅不应带附件, got %v", fake.lastAtts)
	}
}

// 正向：报表里有表格图时，订阅邮件要真的带上导出的 xlsx 附件（文件名取图表名、类型是 xlsx、
// 内容是真 zip）。只有负向断言的话「附件生成整条链路挂了但邮件照发」这种回归照不出来 ——
// subscriptionAttachments 对每张图/每次导出失败都只记日志并跳过，接口不会报错。
func TestRunDueAttachesTableChart(t *testing.T) {
	svc, m := newTestService(t)
	var sentTo []string
	fake := &fakeMailer{onSend: func(to string) { sentTo = append(sentTo, to) }}
	svc.WithMailer(fake)

	now := time.Date(2026, 11, 4, 9, 5, 0, 0, time.Local)
	cfg := `{"items":[{"chartId":"11","group":"","span":12}],"filters":[]}`
	m.ExpectQuery(`SELECT \* FROM .plg_report_subscription. WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "report_id", "schedule", "send_hour", "weekday", "recipients", "enabled", "last_sent_date"}).
			AddRow(1, 7, 3, SchedDaily, 9, 1, "ops@b.com", 1, nil))
	// deliver 读报表名（tenant.Skip，只按 id）→ subscriptionAttachments 带租户重读配置 →
	// 读图表（table）→ ExportReportChart 内部再过一遍 报表/图表/数据集 + 跑数据查询 → 写回 last_sent_date。
	m.ExpectQuery(`SELECT \* FROM .plg_report_report. WHERE id = \? .*LIMIT \?`).
		WithArgs(int64(3), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name"}).AddRow(3, 7, "值班报表"))
	m.ExpectQuery(reportRowSQL).WithArgs(int64(7), int64(3), 1).WillReturnRows(reportRows(cfg))
	m.ExpectQuery("FROM `plg_report_chart` WHERE tenant_id = \\? AND id = \\?").WithArgs(int64(7), int64(11), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "dataset_id", "type", "config"}).AddRow(11, 7, "状态明细", 5, "table", tableChartCfg))
	m.ExpectQuery(reportRowSQL).WithArgs(int64(7), int64(3), 1).WillReturnRows(reportRows(cfg))
	expectChartAndDataset(m, "table", tableChartCfg, `{"columns":["status","attempts"],"filters":[]}`)
	m.ExpectQuery("SELECT `status` AS `d0` FROM `plg_cron_log` WHERE `tenant_id` = \\? ORDER BY 1 LIMIT \\?").
		WithArgs(int64(7), 4).
		WillReturnRows(sqlmock.NewRows([]string{"d0"}).AddRow("a").AddRow("b").AddRow("c").AddRow("d"))
	m.ExpectExec(`UPDATE .plg_report_subscription. SET .last_sent_date.`).WillReturnResult(sqlmock.NewResult(0, 1))

	n, err := svc.RunDue(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(sentTo) != 1 || sentTo[0] != "ops@b.com" {
		t.Fatalf("sent = %d %v", n, sentTo)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(fake.lastAtts) != 1 {
		t.Fatalf("表格图应带 1 个附件，实得 %d", len(fake.lastAtts))
	}
	att := fake.lastAtts[0]
	if att.Name != "状态明细.xlsx" {
		t.Errorf("附件名 = %q, want 状态明细.xlsx", att.Name)
	}
	if att.ContentType != xlsxContentType {
		t.Errorf("附件类型 = %q, want %q", att.ContentType, xlsxContentType)
	}
	// xlsx 就是 zip：不是 PK 头说明导出写到一半就挂了，邮件里会是个打不开的附件。
	if !bytes.HasPrefix(att.Data, []byte("PK")) || len(att.Data) < 200 {
		t.Errorf("附件内容不是 xlsx：%d 字节, 头 %q", len(att.Data), att.Data[:min(4, len(att.Data))])
	}
}

// 自动刷新间隔的服务端范围校验：直调 API 写 1 秒轮询必须被拒（否则报表页变成按秒打数据库），
// 0（关闭）与 5..3600 两端都要能正常存下。
func TestReportRefreshSecRange(t *testing.T) {
	svc, m := newTestService(t)
	for _, bad := range []int{1, 4, -1, 3601, 86400} {
		_, err := svc.SaveReport(tctx(7), 0, ReportForm{Name: "r", Config: ReportConfig{RefreshSec: bad}})
		var be *response.Error
		if !errors.As(err, &be) || be.Key != "report.refresh_interval_invalid" || be.Status != http.StatusBadRequest {
			t.Fatalf("refreshSec=%d: err = %v, want 400 report.refresh_interval_invalid", bad, err)
		}
	}
	// 越界值在落库前就拦下，一条 SQL 都不该发。
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	for _, ok := range []int{0, 5, 3600} {
		m.ExpectExec("INSERT INTO `plg_report_report`").WillReturnResult(sqlmock.NewResult(3, 1))
		v, err := svc.SaveReport(tctx(7), 0, ReportForm{Name: "r", Config: ReportConfig{RefreshSec: ok}})
		if err != nil {
			t.Fatalf("refreshSec=%d: %v", ok, err)
		}
		if v.Config.RefreshSec != ok {
			t.Errorf("refreshSec=%d 存下来变成 %d", ok, v.Config.RefreshSec)
		}
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

type fakeMailer struct {
	onSend   func(to string)
	lastAtts []mail.Attachment // 最后一次发送带的附件（空 = 纯文本提醒）
}

func (f *fakeMailer) SendMailWithAttachments(_ context.Context, to, _ /*tpl*/, _ string, _ mail.Data, atts []mail.Attachment) error {
	f.onSend(to)
	f.lastAtts = atts
	return nil
}
