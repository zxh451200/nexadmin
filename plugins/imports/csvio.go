package imports

// csvio.go 是 CSV/XLSX 编解码:模板生成(BOM + 表头 + 示例行)、上传解析(BOM 容忍、
// XLSX 支持、表头按 Label 识别)、错误报告生成。与 nexGrain 点表导入同口径:UTF-8 CSV,Excel 可直接打开。

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/unicode"
)

const utf8BOM = "\uFEFF"

// ErrTooManyRows 表示数据行超过异步导入的硬上限（由服务层转成明确的用户提示）。
var ErrTooManyRows = errors.New("imports: too many rows")

// templateCells 拆出模板的表头与示例行,CSV/XLSX 两种生成共用同一口径。
func templateCells(ctx context.Context, s Source) (header, example []string) {
	s = localize(ctx, s)
	suffix := requiredSuffix(ctx)
	header = make([]string, len(s.Columns))
	example = make([]string, len(s.Columns))
	for i, c := range s.Columns {
		label := c.Label
		if c.Required {
			label += suffix
		}
		header[i] = label
		example[i] = c.Example
	}
	return header, example
}

// BuildTemplateCSV 生成导入模板:UTF-8 BOM + 表头(Label)+ 示例行。
func BuildTemplateCSV(ctx context.Context, s Source) []byte {
	var buf bytes.Buffer
	buf.WriteString(utf8BOM)
	w := csv.NewWriter(&buf)
	header, example := templateCells(ctx, s)
	_ = w.Write(header)
	_ = w.Write(example)
	w.Flush()
	return buf.Bytes()
}

// BuildTemplateXLSX 生成 XLSX 导入模板:表头加粗 + 示例行,列宽按内容自适应(中文按两格计)。
func BuildTemplateXLSX(ctx context.Context, s Source) ([]byte, error) {
	header, example := templateCells(ctx, s)
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		return nil, fmt.Errorf("imports: write xlsx header: %w", err)
	}
	if err := f.SetSheetRow(sheet, "A2", &example); err != nil {
		return nil, fmt.Errorf("imports: write xlsx example: %w", err)
	}
	style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, fmt.Errorf("imports: style xlsx header: %w", err)
	}
	lastCol, err := excelize.ColumnNumberToName(len(s.Columns))
	if err != nil {
		return nil, fmt.Errorf("imports: xlsx column name: %w", err)
	}
	if err := f.SetCellStyle(sheet, "A1", lastCol+"1", style); err != nil {
		return nil, fmt.Errorf("imports: style xlsx header: %w", err)
	}
	for i, c := range s.Columns {
		col, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return nil, fmt.Errorf("imports: xlsx column name: %w", err)
		}
		width := displayWidth(header[i])
		if w := displayWidth(c.Example); w > width {
			width = w
		}
		if err := f.SetColWidth(sheet, col, col, float64(width)+4); err != nil {
			return nil, fmt.Errorf("imports: set xlsx column width: %w", err)
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("imports: write xlsx: %w", err)
	}
	return buf.Bytes(), nil
}

// displayWidth 估算单元格显示宽度(全角字符按两格),用于模板列宽。
func displayWidth(text string) int {
	w := 0
	for _, r := range text {
		if r >= 0x1100 && (r <= 0x115F || (r >= 0x2E80 && r <= 0xA4CF) ||
			(r >= 0xAC00 && r <= 0xD7A3) || (r >= 0xF900 && r <= 0xFAFF) || r >= 0xFE30) {
			w += 2
			continue
		}
		w++
	}
	return w
}

// ParseUpload 解析上传的 CSV 或 XLSX:容忍 BOM,表头按「去空白的 Label(剥掉"(必填)"后缀)」识别。
// 返回 key→值 的行映射(已去空白)与行号(1 起,不含表头)。
func ParseUpload(s Source, content []byte, fileName string) ([]map[string]string, error) {
	if strings.HasSuffix(strings.ToLower(fileName), ".xlsx") {
		return parseXLSX(s, content)
	}
	return parseCSV(s, content)
}

// parseXLSX 解析 XLSX 第一张工作表(第一行为表头)。
func parseXLSX(s Source, content []byte) ([]map[string]string, error) {
	// 压缩包解压与工作表 XML 都要有上限：输入 20MB 的 xlsx 解压后可以膨胀到 GB 级。
	f, err := excelize.OpenReader(bytes.NewReader(content), excelize.Options{
		UnzipSizeLimit:    256 << 20,
		UnzipXMLSizeLimit: 64 << 20,
	})
	if err != nil {
		return nil, fmt.Errorf("imports: open xlsx: %w", err)
	}
	defer func() { _ = f.Close() }()
	iter, err := f.Rows(f.GetSheetName(0))
	if err != nil {
		return nil, fmt.Errorf("imports: open sheet: %w", err)
	}
	defer func() { _ = iter.Close() }()
	// 多读一行用于判断是否超限，不把整张表读进内存。
	records := make([][]string, 0, 64)
	for len(records) <= AsyncMaxRows && iter.Next() {
		cols, cerr := iter.Columns()
		if cerr != nil {
			return nil, fmt.Errorf("imports: read xlsx: %w", cerr)
		}
		records = append(records, cols)
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("imports: read xlsx: %w", err)
	}
	if len(records) < 1 {
		return nil, fmt.Errorf("imports: empty xlsx")
	}
	if len(records) > AsyncMaxRows {
		return nil, ErrTooManyRows
	}
	return mapHeaderToRows(s, records)
}

// parseCSV 解析 CSV:容忍 BOM,表头按 Label 识别。
func parseCSV(s Source, content []byte) ([]map[string]string, error) {
	content = bytes.TrimPrefix(content, []byte(utf8BOM))
	// UTF-16(Excel 另存的 Unicode CSV)容忍:检测到 BOM 就转 UTF-8。
	if len(content) >= 2 && ((content[0] == 0xFF && content[1] == 0xFE) || (content[0] == 0xFE && content[1] == 0xFF)) {
		decoded, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(content)
		if err != nil {
			return nil, fmt.Errorf("imports: decode UTF-16: %w", err)
		}
		content = bytes.TrimPrefix(decoded, []byte(utf8BOM))
	}
	r := csv.NewReader(bytes.NewReader(content))
	r.FieldsPerRecord = -1 // 列数不齐的行交给逐列校验报错,而不是整体失败
	// 逐行读并在硬上限处停下，避免超大 CSV 先把全部记录读进内存。
	records := make([][]string, 0, 64)
	for len(records) <= AsyncMaxRows {
		rec, rerr := r.Read()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, fmt.Errorf("imports: parse csv: %w", rerr)
		}
		records = append(records, rec)
	}
	if len(records) > AsyncMaxRows {
		return nil, ErrTooManyRows
	}
	if len(records) < 1 {
		return nil, fmt.Errorf("imports: empty csv")
	}
	return mapHeaderToRows(s, records)
}

// mapHeaderToRows 把「第一行表头 + 后续数据行」映射为 key→值 切片(公共逻辑)。
func mapHeaderToRows(s Source, records [][]string) ([]map[string]string, error) {
	labelToKey := labelIndex(s)
	header := records[0]
	keys := make([]string, len(header))
	for i, h := range header {
		h = strings.TrimPrefix(strings.TrimSpace(h), utf8BOM)
		keys[i] = labelToKey[h]
	}
	rows := make([]map[string]string, 0, len(records)-1)
	for i, rec := range records[1:] {
		empty := true
		for _, cell := range rec {
			if strings.TrimSpace(cell) != "" {
				empty = false
				break
			}
		}
		if empty {
			continue // 跳过整行空白
		}
		row := map[string]string{}
		for j, cell := range rec {
			if j < len(keys) && keys[j] != "" {
				row[keys[j]] = strings.TrimSpace(cell)
			}
		}
		rows = append(rows, row)
		_ = i
	}
	return rows, nil
}

// BuildErrorsCSV 生成错误报告:行号 / 列 / 原因。
func BuildErrorsCSV(errs []RowError) []byte {
	var buf bytes.Buffer
	buf.WriteString(utf8BOM)
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"row", "key", "message"})
	for _, e := range errs {
		_ = w.Write([]string{fmt.Sprintf("%d", e.Row), e.Key, e.Message})
	}
	w.Flush()
	return buf.Bytes()
}
