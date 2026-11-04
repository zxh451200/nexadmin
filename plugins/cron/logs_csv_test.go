package cron

import (
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
)

func sampleLogs() []Log {
	t0 := time.Date(2026, 11, 3, 3, 0, 0, 0, time.UTC)
	return []Log{
		{JobName: "清理 30 天前的日志", Handler: "clean_logs", TriggerType: "schedule", Status: "success",
			Attempts: 1, CostMs: 420, StartedAt: t0, Output: "deleted 128 rows", Error: ""},
		{JobName: "带, 逗号与\"引号\"的任务", Handler: "sync_region", TriggerType: "manual", Status: "failed",
			Attempts: 3, CostMs: 90, StartedAt: t0.Add(time.Minute), Output: "", Error: "boom\n第二行"},
	}
}

// 导出文件必须能被 CSV 解析器原样读回：列数、行数、每格内容都要对得上。
// 这条同时钉住 BOM 与 CRLF——没有 BOM 时 Excel 按本地代码页读，中文任务名乱码。
func TestBuildLogsCSVRoundTrip(t *testing.T) {
	out := BuildLogsCSV(sampleLogs())
	if !strings.HasPrefix(string(out), "\ufeff") {
		t.Error("缺少 UTF-8 BOM：Excel 打开会把中文任务名读成乱码（仓库里其它 CSV 都带）")
	}
	if !strings.Contains(string(out), "started_at,job,") || !strings.Contains(string(out), "\r\n") {
		t.Errorf("表头/换行不符 CSV 口径：%q", string(out)[:min(64, len(out))])
	}

	r := csv.NewReader(strings.NewReader(string(out)))
	r.FieldsPerRecord = 9
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("导出文件不是合法 CSV：%v", err)
	}
	if len(records) != 3 {
		t.Fatalf("记录数 = %d，想要 3（表头 + 2 行）", len(records))
	}
	if records[1][1] != "清理 30 天前的日志" || records[1][6] != "420" {
		t.Errorf("第一行数据不符：%q", records[1])
	}
	// 含逗号与引号的单元格读回来必须还是原样（引号转义交给 encoding/csv）。
	if records[2][1] != "带, 逗号与\"引号\"的任务" {
		t.Errorf("带引号的任务名读回成 %q", records[2][1])
	}
	if records[2][8] != "boom\n第二行" {
		t.Errorf("含换行的错误信息读回成 %q", records[2][8])
	}
}

// 公式注入：以 = + - @ 或制表符/回车开头的单元格会被 Excel 当公式执行。
// 前缀单引号是仓库统一口径（pkg/excel/writer.go 还包含 \t 与 \r，这里对齐）。
func TestBuildLogsCSVGuardsFormulaCells(t *testing.T) {
	for _, prefix := range []string{"=", "+", "-", "@", "\t", "\r"} {
		payload := prefix + "cmd|' /C calc'!A0"
		out := string(BuildLogsCSV([]Log{{JobName: payload, Handler: payload, StartedAt: time.Now().UTC()}}))
		r := csv.NewReader(strings.NewReader(out))
		r.FieldsPerRecord = 9
		recs, err := r.ReadAll()
		if err != nil {
			t.Fatalf("前缀 %q：导出文件不是合法 CSV：%v", prefix, err)
		}
		cell := recs[1][1]
		if !strings.HasPrefix(cell, "'") {
			t.Errorf("前缀 %q 的单元格没有被前置单引号：%q", prefix, cell)
		}
	}

	// 正控制：普通文本不该被加上前缀引号（否则每个任务名都多个撇号）。
	out := string(BuildLogsCSV([]Log{{JobName: "sync_region", StartedAt: time.Now().UTC()}}))
	if !strings.Contains(out, "sync_region") || strings.Contains(out, "'sync_region") {
		t.Errorf("普通单元格被误加前缀引号：%q", out)
	}
}

// output / error 在 SQL 里被 SUBSTR 到 500 字，导出必须把这层截断显示出来：
// 否则读日志的人以为输出就这么长。
func TestBuildLogsCSVMarksTruncatedCells(t *testing.T) {
	full := strings.Repeat("x", 499)
	edge := strings.Repeat("y", logCellTruncateAt)
	out := string(BuildLogsCSV([]Log{
		{JobName: "j", Output: full, StartedAt: time.Now().UTC()},
		{JobName: "j2", Output: edge, StartedAt: time.Now().UTC()},
	}))
	// 追加而不是替换：前 500 字要原样保留，499 字的不该被标记。
	if strings.Contains(out, full+"…") {
		t.Errorf("未截断的输出（499 字）被加了省略号标记")
	}
	if !strings.Contains(out, full) {
		t.Errorf("未截断的输出在文件里缺失")
	}
	if !strings.Contains(out, edge+"…") {
		t.Errorf("恰好 %d 字符的输出没有截断标记（读的人会以为这就是全文）", logCellTruncateAt)
	}
}

// 0 行也要出表头：界面点导出不能拿到空文件（那和请求失败看起来一样）。
func TestBuildLogsCSVHeaderOnly(t *testing.T) {
	out := string(BuildLogsCSV(nil))
	r := csv.NewReader(strings.NewReader(out))
	r.FieldsPerRecord = 9
	recs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || strings.TrimPrefix(recs[0][0], "\ufeff") != "started_at" {
		t.Errorf("空结果的导出 = %q，想要只剩表头", out)
	}
	if utf8.RuneCountInString(out) == 0 {
		t.Error("空导出不能是零字节")
	}
}

// 行数超上限时必须在写响应头之前拒绝，并把「命中多少行 / 上限多少」说清楚：
// 静默截断成 20000 行是使用者最难发现的那种错。
func TestExportLogsRejectsOverLimit(t *testing.T) {
	svc, mock := mockDB(t)
	max := int(logExportMax)
	cases := []struct {
		count   int64
		wantErr bool
	}{{int64(max - 1), false}, {int64(max), false}, {int64(max) + 1, true}}
	for _, tc := range cases {
		mock.ExpectQuery("SELECT count\\(\\*\\)").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.count))
		if !tc.wantErr {
			// 未超限才会继续取数据；这里给一行足够跑通。
			mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		}
		rows, err := svc.ExportLogs(t.Context(), LogQuery{})
		if tc.wantErr {
			if err == nil {
				t.Fatalf("命中 %d 行应当拒绝，实际放行 %d 行", tc.count, len(rows))
			}
			var re *response.Error
			if !errors.As(err, &re) {
				t.Fatalf("超限错误 = %v，想要 *response.Error", err)
			}
			if re.Key != "cron.export_too_many_rows" {
				t.Errorf("超限错误键 = %q，想要 cron.export_too_many_rows", re.Key)
			}
			// 必须把「命中多少行 / 上限多少」带给界面，否则用户只知道失败。
			if len(re.Args) != 2 || re.Args[0] != tc.count || re.Args[1] != max {
				t.Errorf("超限错误的参数 = %v，想要 [%d %d]", re.Args, tc.count, max)
			}
			continue
		}
		if err != nil {
			t.Errorf("命中 %d 行（未超上限）被拒：%v", tc.count, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock 期望未满足：%v", err)
	}
}
