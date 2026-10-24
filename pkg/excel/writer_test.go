package excel

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func part(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, _ := f.Open()
			defer rc.Close()
			b, _ := io.ReadAll(rc)
			return string(b)
		}
	}
	t.Fatalf("part %s not found", name)
	return ""
}

func readAll(t *testing.T, data []byte, opt ReadOptions) [][]string {
	t.Helper()
	out, err := ReadAll(bytes.NewReader(data), int64(len(data)), opt)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	x := NewWriter(&buf)
	s, err := x.NewSheet("用户", SheetOptions{Header: []string{"账号", "姓名", "年龄", "启用", "创建时间", "生日", "备注"}})
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("长文本abc😀\n", 3000)
	when := time.Date(2024, 1, 15, 13, 4, 5, 0, time.UTC)
	rows := [][]any{
		{"alice", "爱丽丝 😀", 30, true, when, time.Date(1990, 5, 6, 0, 0, 0, 0, time.UTC), long},
		{"bob", nil, 3.5, false, nil, nil, ""},
		{"<a&b>\"'", "line1\r\nline2\ttab", int64(-7), true, &when, nil, "  lead space"},
	}
	for _, r := range rows {
		if err := s.WriteRow(r...); err != nil {
			t.Fatal(err)
		}
	}
	s2, _ := x.NewSheet("第二页:[*]/?", SheetOptions{})
	_ = s2.WriteRow("x", 1)
	if err := x.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()

	got := readAll(t, data, ReadOptions{})
	if len(got) != 4 {
		t.Fatalf("rows=%d", len(got))
	}
	if got[0][0] != "账号" || got[1][1] != "爱丽丝 😀" || got[1][2] != "30" || got[1][3] != "TRUE" ||
		got[1][4] != "2024-01-15 13:04:05" || got[1][5] != "1990-05-06" || got[1][6] != strings.ReplaceAll(long, "\r", "\r") {
		t.Fatalf("row1 mismatch: %q", got[1][:6])
	}
	if got[2][2] != "3.5" || got[2][3] != "FALSE" || got[2][1] != "" || len(got[2]) != 4 {
		t.Fatalf("row2 mismatch: %q", got[2])
	}
	if got[3][0] != "<a&b>\"'" || got[3][1] != "line1\r\nline2\ttab" || got[3][2] != "-7" || got[3][4] != "2024-01-15 13:04:05" || got[3][6] != "  lead space" {
		t.Fatalf("row3 mismatch: %q", got[3])
	}

	// 类型：读回数字是 Number、日期是 Date、布尔是 Bool
	rd, err := Open(bytes.NewReader(data), int64(len(data)), ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if names := rd.SheetNames(); len(names) != 2 || names[0] != "用户" || names[1] != "第二页______" {
		t.Fatalf("sheet names %v", names)
	}
	_ = rd.ReadRows(func(row int, cells []Cell) error {
		if row == 2 && (cells[2].Kind != KindNumber || cells[3].Kind != KindBool || cells[4].Kind != KindDate || !cells[4].Time.Equal(when)) {
			t.Errorf("types wrong: %+v", cells)
		}
		return nil
	})
	// 第二个 sheet
	if g := readAll(t, data, ReadOptions{SheetIndex: 1}); len(g) != 1 || g[0][0] != "x" {
		t.Fatalf("sheet2 %v", g)
	}
	if g := readAll(t, data, ReadOptions{Sheet: "用户"}); len(g) != 4 {
		t.Fatal("by name")
	}

	// 结构：冻结首行、表头加粗样式、日期样式、列宽
	sh := part(t, data, "xl/worksheets/sheet1.xml")
	for _, want := range []string{`state="frozen"`, `ySplit="1"`, `<c r="A1" s="1" t="s">`, `s="2"><v>32999</v>`, `<cols>`} {
		if !strings.Contains(sh, want) {
			t.Errorf("sheet xml lacks %s", want)
		}
	}
	if !strings.Contains(part(t, data, "xl/styles.xml"), "<b/>") {
		t.Error("no bold font")
	}
}

func TestSerial(t *testing.T) {
	if v, _ := timeToSerial(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)); v != 45306 {
		t.Fatalf("serial=%v", v)
	}
	if v, _ := timeToSerial(time.Date(1970, 1, 1, 12, 0, 0, 0, time.UTC)); v != 25569.5 {
		t.Fatalf("serial=%v", v)
	}
	if _, ok := timeToSerial(time.Date(1899, 1, 1, 0, 0, 0, 0, time.UTC)); ok {
		t.Fatal("pre-1900 should be unsupported")
	}
	tm, _ := serialToTime(45306.5, false)
	if !tm.Equal(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %v", tm)
	}
}

func TestFormulaGuard(t *testing.T) {
	vals := []string{"=1+1", "+8613800000000", "-cmd", "@SUM(A1)", "\t=x", "safe=1"}
	for _, mode := range []FormulaGuard{GuardTextStyle, GuardPrefix} {
		var buf bytes.Buffer
		x := NewWriter(&buf, Options{FormulaGuard: mode})
		s, _ := x.NewSheet("s", SheetOptions{Header: []string{"=HYPERLINK(\"http://evil\")"}})
		for _, v := range vals {
			_ = s.WriteRow(v)
		}
		if err := x.Close(); err != nil {
			t.Fatal(err)
		}
		sh := part(t, buf.Bytes(), "xl/worksheets/sheet1.xml")
		if strings.Contains(sh, "<f>") || strings.Contains(sh, "<f ") {
			t.Fatal("formula element emitted")
		}
		got := readAll(t, buf.Bytes(), ReadOptions{})
		if mode == GuardTextStyle {
			// 值无损，但危险文本全部套 quotePrefix 样式(s=4)，且始终是 t="s"
			if n := strings.Count(sh, ` s="4" t="s"`); n != 5 {
				t.Errorf("quote style count wrong: %d", n)
			}
			if got[1][0] != "=1+1" || got[5][0] != "\t=x" {
				t.Errorf("lossless expected: %q", got)
			}
			if !strings.Contains(part(t, buf.Bytes(), "xl/styles.xml"), `quotePrefix="1"`) {
				t.Error("quotePrefix style missing")
			}
		} else if got[1][0] != "'=1+1" || got[2][0] != "'+8613800000000" || got[6][0] != "safe=1" {
			t.Errorf("prefix mode: %q", got)
		}
	}
}

func TestControlCharsFiltered(t *testing.T) {
	var buf bytes.Buffer
	x := NewWriter(&buf)
	s, _ := x.NewSheet("s", SheetOptions{})
	_ = s.WriteRow("a\x00b\x01c\x08d\x0be\x0cf\x1bg\x7fh\ufffei\xffj\tk\nl", "\ufeffbom")
	if err := x.Close(); err != nil {
		t.Fatal(err)
	}
	got := readAll(t, buf.Bytes(), ReadOptions{})
	// 0x7f 在 XML 1.0 中合法，保留；非法控制字符、U+FFFE 与坏 UTF-8 被去掉
	if want := "abcdefg\x7fhij\tk\nl"; got[0][0] != want {
		t.Fatalf("got %q", got[0][0])
	}
	for _, p := range []string{"xl/sharedStrings.xml", "xl/worksheets/sheet1.xml"} {
		for _, r := range part(t, buf.Bytes(), p) {
			if !validXMLRune(r) {
				t.Fatalf("illegal rune %U in %s", r, p)
			}
		}
	}
}

func TestEmptyAndManyRows(t *testing.T) {
	var buf bytes.Buffer
	x := NewWriter(&buf)
	if err := x.Close(); err != nil {
		t.Fatal(err)
	}
	if g := readAll(t, buf.Bytes(), ReadOptions{}); len(g) != 0 {
		t.Fatalf("%v", g)
	}
	buf.Reset()
	x = NewWriter(&buf)
	s, _ := x.NewSheet("big", SheetOptions{Header: []string{"n", "s"}, Validations: []Validation{{Col: 1, List: []string{"男", "女"}, FirstRow: 2}}})
	for i := 0; i < 30000; i++ {
		_ = s.WriteRow(i, "v"+strings.Repeat("x", i%7))
	}
	if err := x.Close(); err != nil {
		t.Fatal(err)
	}
	g := readAll(t, buf.Bytes(), ReadOptions{})
	if len(g) != 30001 || g[30000][0] != "29999" {
		t.Fatalf("rows=%d", len(g))
	}
	if sh := part(t, buf.Bytes(), "xl/worksheets/sheet1.xml"); !strings.Contains(sh, `<formula1>"男,女"</formula1>`) {
		t.Error("validation missing")
	}
}
