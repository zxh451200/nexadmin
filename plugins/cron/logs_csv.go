package cron

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"time"
	"unicode/utf8"
)

// logCellTruncateAt 与 logListColumns 里 SUBSTR(...,1,500) 的 500 同一个数：
// 导出里的 output / error 是日志的前 500 个字符，不是全文（全文用 GET /logs/:id）。
const logCellTruncateAt = 500

// BuildLogsCSV 把执行日志拼成 CSV（UTF-8 with BOM，Excel 双击打开不乱码）。
// 列与列表页一致：开始时间 / 任务 / 触发方式 / 状态 / 尝试次数 / 耗时 ms / 输出 / 错误。
func BuildLogsCSV(logs []Log) []byte {
	var buf bytes.Buffer
	// BOM 是本仓库所有 CSV 的口径（见 pkg/excel、plugins/imports、modules/system/user/import.go）：
	// 没有它，Excel 会把 UTF-8 当本地代码页读，中文任务名直接乱码。
	buf.WriteString("\ufeff")
	w := csv.NewWriter(&buf)
	w.UseCRLF = true
	header := []string{"started_at", "job", "handler", "trigger", "status", "attempts", "cost_ms", "output", "error"}
	rows := make([][]string, 0, len(logs)+1)
	rows = append(rows, header)
	for _, l := range logs {
		rows = append(rows, []string{
			l.StartedAt.Format(time.RFC3339),
			csvCell(l.JobName),
			csvCell(l.Handler),
			csvCell(l.TriggerType),
			csvCell(l.Status),
			fmt.Sprintf("%d", l.Attempts),
			fmt.Sprintf("%d", l.CostMs),
			csvCell(head(l.Output)),
			csvCell(head(l.Error)),
		})
	}
	// 写的是内存 buffer，正常不会失败；真失败了也不能交出一份缺行的文件，所以直接抛——
	// 导出路径上没有「部分成功」这个状态。
	if err := w.WriteAll(rows); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// head 给被 SUBSTR 截过断的单元格补一个省略号：导出文件里没有「这格被截了」的元信息，
// 不加标记读日志的人会以为输出就是这么长。恰好 500 字符的完整输出会被误标，宁可多标不可漏标。
func head(s string) string {
	if utf8.RuneCountInString(s) >= logCellTruncateAt {
		return s + "…"
	}
	return s
}

// csvCell 处理一个文本单元格：先挡公式注入，引号转义交给 encoding/csv。
// 注入面按 pkg/excel 的口径来（= + - @ 之外还有制表符与回车——Excel 会把 "\t=cmd" 也当公式）。
func csvCell(v string) string {
	if v == "" {
		return ""
	}
	if c := v[0]; c == '=' || c == '+' || c == '-' || c == '@' || c == '\t' || c == '\r' {
		v = "'" + v
	}
	return v
}
