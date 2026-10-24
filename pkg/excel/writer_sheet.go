package excel

import (
	"archive/zip"
	"errors"
	"fmt"
	"strconv"
	"time"
)

func reflectInt(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int8:
		return int64(t)
	case int16:
		return int64(t)
	case int32:
		return int64(t)
	}
	return v.(int64)
}

func reflectUint(v any) uint64 {
	switch t := v.(type) {
	case uint:
		return uint64(t)
	case uint8:
		return uint64(t)
	case uint16:
		return uint64(t)
	case uint32:
		return uint64(t)
	}
	return v.(uint64)
}

// WriteRow 追加一行。支持 string、[]byte、整数、浮点、bool、time.Time（含指针）；nil 与零值时间写空单元格，
// 其它类型按 fmt.Sprint 当文本。前 200 行会先缓冲用于估算列宽，之后逐行直接写出，内存不随行数增长。
func (s *Sheet) WriteRow(vals ...any) error {
	x := s.x
	if x.err != nil {
		return x.err
	}
	if s.done || x.cur != s {
		return errors.New("excel: sheet already finished")
	}
	if len(vals) > MaxSheetCols {
		return fmt.Errorf("%w: too many columns", ErrTooLarge)
	}
	cells := make([]cellv, 0, len(vals))
	widths := make([]float64, len(vals))
	for i, v := range vals {
		c, ok, err := x.convert(i, v)
		if err != nil {
			return err
		}
		if ok {
			cells = append(cells, c)
			widths[i] = x.cellWidth(c)
		}
	}
	return s.addRow(cells, widths)
}

func (s *Sheet) addRow(cells []cellv, widths []float64) error {
	if s.row+1 > MaxSheetRows {
		return fmt.Errorf("%w: too many rows", ErrTooLarge)
	}
	s.row++
	for i, w := range widths {
		for len(s.widths) <= i {
			s.widths = append(s.widths, 0)
		}
		s.widths[i] = max(s.widths[i], w)
	}
	if !s.started {
		s.pending = append(s.pending, cells)
		if len(s.pending) < sampleRows {
			return nil
		}
		return s.start()
	}
	return s.writeRow(s.row, cells)
}

func (s *Sheet) put(str string) {
	if _, err := s.bw.WriteString(str); err != nil {
		s.x.err = err
	}
}

// start 写出文件头（含按样本估算的列宽与冻结窗格）并冲刷缓冲行。
func (s *Sheet) start() error {
	s.started = true
	first := len(s.x.sheets) == 1
	s.put(xmlHeader + `<worksheet xmlns="` + nsMain + `"><sheetViews><sheetView workbookViewId="0"`)
	if first {
		s.put(` tabSelected="1"`)
	}
	s.put(`>`)
	if len(s.opts.Header) > 0 {
		s.put(`<pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>` +
			`<selection pane="bottomLeft" activeCell="A2" sqref="A2"/>`)
	}
	s.put(`</sheetView></sheetViews><sheetFormatPr defaultRowHeight="15"/>`)
	if len(s.widths) > 0 {
		s.put(`<cols>`)
		for i, w := range s.widths {
			w = min(max(w+2, minColWidth), maxColWidth)
			n := strconv.Itoa(i + 1)
			s.put(`<col min="` + n + `" max="` + n + `" width="` + strconv.FormatFloat(w, 'f', 1, 64) + `" customWidth="1"/>`)
		}
		s.put(`</cols>`)
	}
	s.put(`<sheetData>`)
	for i, cells := range s.pending {
		if err := s.writeRow(i+1, cells); err != nil {
			return err
		}
	}
	s.pending = nil
	return s.x.err
}

func (s *Sheet) writeRow(n int, cells []cellv) error {
	rn := strconv.Itoa(n)
	s.put(`<row r="` + rn + `">`)
	for _, c := range cells {
		ref := colName(c.col) + rn
		s.put(`<c r="` + ref + `"`)
		if c.style != styleDefault {
			s.put(` s="` + strconv.Itoa(c.style) + `"`)
		}
		switch c.kind {
		case 's':
			s.put(` t="s"><v>` + strconv.Itoa(c.idx) + `</v></c>`)
		case 'b':
			s.put(` t="b"><v>` + c.num + `</v></c>`)
		default:
			s.put(`><v>` + c.num + `</v></c>`)
		}
	}
	s.put(`</row>`)
	return s.x.err
}

func (s *Sheet) finish() error {
	if s.done {
		return s.x.err
	}
	s.done = true
	s.x.cur = nil
	if !s.started {
		if err := s.start(); err != nil {
			return err
		}
	}
	s.put(`</sheetData>`)
	if vs := s.validationsXML(); vs != "" {
		s.put(vs)
	}
	s.put(`<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/></worksheet>`)
	if err := s.bw.Flush(); err != nil && s.x.err == nil {
		s.x.err = err
	}
	return s.x.err
}

func (s *Sheet) validationsXML() string {
	var b []byte
	n := 0
	for _, v := range s.opts.Validations {
		list := ""
		for i, it := range v.List {
			if i > 0 {
				list += ","
			}
			list += cleanText(it)
		}
		if len(v.List) == 0 || len(list) > 255 || v.Col < 0 || v.Col >= MaxSheetCols || v.FirstRow < 1 {
			continue
		}
		last := v.LastRow
		if last < v.FirstRow {
			last = v.FirstRow + 999
		}
		col := colName(v.Col)
		b = append(b, `<dataValidation type="list" allowBlank="1" showErrorMessage="1" sqref="`+
			col+strconv.Itoa(v.FirstRow)+`:`+col+strconv.Itoa(min(last, MaxSheetRows))+
			`"><formula1>"`+escapeText(list)+`"</formula1></dataValidation>`...)
		n++
	}
	if n == 0 {
		return ""
	}
	return `<dataValidations count="` + strconv.Itoa(n) + `">` + string(b) + `</dataValidations>`
}

// Close 结束当前 sheet 并写出 sharedStrings、样式、工作簿等部件。不会关闭底层 io.Writer。
// 没有任何 sheet 时会补一个空的 Sheet1，保证文件合法。
func (x *Writer) Close() error {
	if x.closed {
		return x.err
	}
	x.closed = true
	if x.err != nil {
		_ = x.zw.Close()
		return x.err
	}
	if x.cur != nil {
		if err := x.cur.finish(); err != nil {
			return err
		}
	}
	if len(x.sheets) == 0 {
		x.closed = false
		s, err := x.NewSheet("Sheet1", SheetOptions{})
		x.closed = true
		if err != nil {
			return err
		}
		if err := s.finish(); err != nil {
			return err
		}
	}
	x.writeParts()
	if err := x.zw.Close(); err != nil && x.err == nil {
		x.err = err
	}
	return x.err
}

func (x *Writer) addFile(name, content string) {
	if x.err != nil {
		return
	}
	w, err := x.zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Unix(0, 0).UTC()})
	if err == nil {
		_, err = w.Write([]byte(content))
	}
	if err != nil {
		x.err = err
	}
}
