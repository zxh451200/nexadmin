package ticket

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/config"
	corei18n "nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// useBuiltinLocale 装载出厂词条目录（和运行期同一份 yaml），这样断言的是真文案：
// 少一个 key 会退回存值（如 open），测试立刻红，而不是悄悄用测试自己造的字典。
func useBuiltinLocale(t *testing.T) {
	t.Helper()
	b, err := corei18n.New([]config.Language{{Code: "zh-CN", Default: true}, {Code: "en-US"}}, "zh-CN", "none")
	if err != nil {
		t.Fatal(err)
	}
	prev := corei18n.Default()
	corei18n.SetDefault(b)
	t.Cleanup(func() { corei18n.SetDefault(prev) })
}

func ctxLang(lang string) context.Context {
	return corei18n.WithLang(context.Background(), lang)
}

// cell 取第 i 列文本；空白单元格在读回时可能整格缺席，缺席即空串。
func cell(row []string, i int) string {
	if i >= len(row) {
		return ""
	}
	return row[i]
}

func exportTicketRow() Ticket {
	return Ticket{
		TicketNo: "TK1001", Title: "订单没发货", Status: StatusPending, Priority: PriorityUrgent,
		GroupName: "售后组", AssigneeName: "kf-1", ReporterName: "visitor-9", CreatorName: "agent-1",
		BizType: "order", BizNo: "O-77", TagList: []string{"vip", "售后"},
		Overdue: true,
	}
}

func TestExportTicketsWritesLocalizedRow(t *testing.T) {
	useBuiltinLocale(t)
	// 应用时区 Asia/Shanghai：UTC 存值要按操作者看到的墙上时间落进单元。
	s := NewService(nil, nopLogger{}).WithTimezone("Asia/Shanghai")
	rating := 5
	tk := exportTicketRow()
	tk.CreatedAt = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	tk.Rating = &rating
	first := time.Date(2025, 6, 1, 1, 30, 0, 0, time.UTC)
	resolved := time.Date(2025, 6, 1, 3, 0, 0, 0, time.UTC)
	closed := time.Date(2025, 6, 2, 2, 0, 0, 0, time.UTC)
	last := time.Date(2025, 6, 1, 9, 15, 0, 0, time.UTC)
	tk.FirstReplyAt, tk.ResolvedAt, tk.ClosedAt, tk.LastReplyAt = &first, &resolved, &closed, &last

	var buf bytes.Buffer
	if err := s.WriteTicketsXLSX(ctxLang("zh-CN"), &buf, []Ticket{tk}); err != nil {
		t.Fatal(err)
	}
	rows, err := excel.ReadAll(bytes.NewReader(buf.Bytes()), int64(buf.Len()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("行数 = %d, want 2（表头 + 1 条数据）", len(rows))
	}
	head, data := rows[0], rows[1]
	if len(head) != len(exportColumns) {
		t.Fatalf("表头列数 = %d, want %d", len(head), len(exportColumns))
	}
	for i, want := range []string{"工单号", "标题", "状态", "优先级", "分组", "处理人", "报告人", "创建人",
		"业务对象", "标签", "满意度", "响应超时", "解决超时", "提交时间", "首次响应", "解决时间", "关闭时间", "最后回复"} {
		if head[i] != want {
			t.Errorf("表头第 %d 列 = %q, want %q", i, head[i], want)
		}
	}
	wantData := []string{"TK1001", "订单没发货", "待客户回复", "紧急", "售后组", "kf-1", "visitor-9", "agent-1",
		"order: O-77", "vip,售后", "5", "TRUE", "FALSE",
		"2025-06-01 08:00:00", "2025-06-01 09:30:00", "2025-06-01 11:00:00", "2025-06-02 10:00:00", "2025-06-01 17:15:00"}
	for i, want := range wantData {
		if got := cell(data, i); got != want {
			t.Errorf("数据第 %d 列 = %q, want %q", i, got, want)
		}
	}
}

func TestExportTicketsEnglishLabelsAndBlanks(t *testing.T) {
	useBuiltinLocale(t)
	s := NewService(nil, nopLogger{})
	tk := exportTicketRow() // 无评价、无时间列：这些格必须留空，不能写成 0 或纪元时间

	var buf bytes.Buffer
	if err := s.WriteTicketsXLSX(ctxLang("en-US"), &buf, []Ticket{tk}); err != nil {
		t.Fatal(err)
	}
	rows, err := excel.ReadAll(bytes.NewReader(buf.Bytes()), int64(buf.Len()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := cell(rows[0], 0); got != "Ticket no" {
		t.Errorf("英文表头首列 = %q", got)
	}
	if got := cell(rows[0], 12); got != "Resolution overdue" {
		t.Errorf("英文表头第 13 列 = %q", got)
	}
	if got := cell(rows[1], 2); got != "Waiting on customer" || cell(rows[1], 3) != "Urgent" {
		t.Errorf("英文状态/优先级 = %q / %q", cell(rows[1], 2), cell(rows[1], 3))
	}
	for _, i := range []int{10, 13, 14, 15, 16, 17} {
		if got := cell(rows[1], i); got != "" {
			t.Errorf("第 %d 列应为空，得到 %q", i, got)
		}
	}
}

func TestExportTicketsRejectsOverLimit(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery("SELECT count(.*) FROM `plg_ticket`").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5001))

	_, err := s.ExportTickets(context.Background(), TicketQuery{Status: StatusOpen}, exportLimit)
	var e *response.Error
	if !errors.As(err, &e) {
		t.Fatalf("超限应返回 response.Error，得到 %v", err)
	}
	if e.Reason != ExportTooManyReason || e.Status != 400 {
		t.Errorf("err = status %d reason %q", e.Status, e.Reason)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestExportTicketsUsesListFilters(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	args := []driver.Value{"resolved", true, int64(7), "%,vip,%"}
	m.ExpectQuery("SELECT count(.*) FROM `plg_ticket`").
		WithArgs(args...).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	m.ExpectQuery("SELECT .* FROM `plg_ticket`").
		WithArgs(append(args, int64(exportLimit))...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ticket_no", "status"}).AddRow(1001, "TK1001", StatusResolved))

	rows, err := s.ExportTickets(context.Background(), TicketQuery{
		Status: StatusResolved, Overdue: "1", AssigneeID: idgen.ID(7), Tag: "vip",
	}, exportLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TicketNo != "TK1001" {
		t.Fatalf("rows = %v", rows)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
