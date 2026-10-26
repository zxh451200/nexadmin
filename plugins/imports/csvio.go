package imports

// csvio.go 是 CSV/XLSX 编解码:模板生成(BOM + 表头 + 示例行)、上传解析(BOM 容忍、
// XLSX 支持、表头按 Label 识别)、错误报告生成。与 nexGrain 点表导入同口径:UTF-8 CSV,Excel 可直接打开。

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/unicode"
)

const utf8BOM = "\uFEFF"

// BuildTemplateCSV 生成导入模板:UTF-8 BOM + 表头(Label)+ 示例行。
func BuildTemplateCSV(s Source) []byte {
	var buf bytes.Buffer
	buf.WriteString(utf8BOM)
	w := csv.NewWriter(&buf)
	header := make([]string, len(s.Columns))
	example := make([]string, len(s.Columns))
	for i, c := range s.Columns {
		label := c.Label
		if c.Required {
			label += "(必填)"
		}
		header[i] = label
		example[i] = c.Example
	}
	_ = w.Write(header)
	_ = w.Write(example)
	w.Flush()
	return buf.Bytes()
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
	f, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("imports: open xlsx: %w", err)
	}
	defer func() { _ = f.Close() }()
	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil || len(rows) < 1 {
		return nil, fmt.Errorf("imports: empty xlsx")
	}
	return mapHeaderToRows(s, rows)
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
	r := csv.NewReader(strings.NewReader(string(content)))
	r.FieldsPerRecord = -1 // 列数不齐的行交给逐列校验报错,而不是整体失败
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("imports: parse csv: %w", err)
	}
	if len(records) < 1 {
		return nil, fmt.Errorf("imports: empty csv")
	}
	return mapHeaderToRows(s, records)
}

// mapHeaderToRows 把「第一行表头 + 后续数据行」映射为 key→值 切片(公共逻辑)。
func mapHeaderToRows(s Source, records [][]string) ([]map[string]string, error) {
	labelToKey := map[string]string{}
	for _, c := range s.Columns {
		labelToKey[strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(c.Label, "(必填)"), "(必填)"))] = c.Key
	}
	header := records[0]
	keys := make([]string, len(header))
	for i, h := range header {
		h = strings.TrimPrefix(strings.TrimSpace(h), utf8BOM)
		if k, ok := labelToKey[h]; ok {
			keys[i] = k
		} else if k, ok := labelToKey[strings.TrimSuffix(h, "(必填)")]; ok {
			keys[i] = k
		}
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
