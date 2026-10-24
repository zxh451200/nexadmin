package excel

import (
	"bytes"
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"
)

func openErr(data []byte, opt ReadOptions) error {
	_, err := ReadAll(bytes.NewReader(data), int64(len(data)), opt)
	return err
}

func TestLimitsAndAttacks(t *testing.T) {
	ns := `<worksheet xmlns="` + nsM + `"><sheetData>`
	cell := func(i int) string {
		return `<row r="` + itoa(i) + `"><c r="A` + itoa(i) + `" t="inlineStr"><is><t>x</t></is></c></row>`
	}
	rows := func(n int) string {
		var b strings.Builder
		for i := 1; i <= n; i++ {
			b.WriteString(cell(i))
		}
		return hdr + ns + b.String() + `</sheetData></worksheet>`
	}
	ok := func(sheet string) map[string]string {
		return minimal(sheet, hdr+`<sst xmlns="`+nsM+`"/>`, "")
	}
	one := func(inner string) []byte { return buildZip(t, ok(hdr+ns+inner+`</sheetData></worksheet>`)) }
	withJunk := ok(rows(1))
	for i := 0; i < 20; i++ {
		withJunk["junk/"+itoa(i)+".txt"] = "x"
	}
	cases := []struct {
		name string
		data []byte
		opt  ReadOptions
		want error
	}{
		{"too many rows", buildZip(t, ok(rows(30))), ReadOptions{MaxRows: 10}, ErrTooLarge},
		{"huge row number", one(cell(900000)), ReadOptions{MaxRows: 100}, ErrTooLarge},
		{"too many cols", one(`<row r="1"><c r="ZZ1" t="inlineStr"><is><t>x</t></is></c></row>`), ReadOptions{MaxCols: 50}, ErrTooLarge},
		{"too many files", buildZip(t, withJunk), ReadOptions{MaxFiles: 10}, ErrTooLarge},
		{"zip bomb part", one(strings.Repeat(" ", 8<<20)), ReadOptions{MaxPartBytes: 1 << 20}, ErrTooLarge},
		{"zip bomb total", one(strings.Repeat(" ", 3<<20)), ReadOptions{MaxTotalBytes: 1 << 20}, ErrTooLarge},
		{"archive too big", buildZip(t, ok(rows(1))), ReadOptions{MaxArchiveBytes: 100}, ErrTooLarge},
		{"deep nesting", one(strings.Repeat("<a>", 100) + strings.Repeat("</a>", 100)), ReadOptions{MaxDepth: 20}, ErrTooLarge},
		{"billion laughs", buildZip(t, ok(hdr+`<!DOCTYPE lolz [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;">]>`+ns+
			`<row r="1"><c r="A1" t="inlineStr"><is><t>&b;</t></is></c></row></sheetData></worksheet>`)), ReadOptions{}, ErrMalformed},
		{"external entity", buildZip(t, ok(hdr+`<!DOCTYPE x [<!ENTITY e SYSTEM "file:///etc/passwd">]>`+ns+
			`<row r="1"><c r="A1" t="inlineStr"><is><t>&e;</t></is></c></row></sheetData></worksheet>`)), ReadOptions{}, ErrMalformed},
		{"undefined entity", one(`<row r="1"><c r="A1" t="inlineStr"><is><t>&e;</t></is></c></row>`), ReadOptions{}, ErrMalformed},
		{"bad sst index", one(`<row r="1"><c r="A1" t="s"><v>99</v></c></row>`), ReadOptions{}, ErrMalformed},
		{"truncated xml", buildZip(t, ok(hdr+ns+`<row r="1"><c r="A1"`)), ReadOptions{}, ErrMalformed},
		{"not a zip", []byte("hello world, definitely not a zip"), ReadOptions{}, ErrNotXLSX},
		{"empty", nil, ReadOptions{}, ErrNotXLSX},
		{"zip without workbook", buildZip(t, map[string]string{"a.txt": "x"}), ReadOptions{}, ErrNotXLSX},
	}
	for _, c := range cases {
		if err := openErr(c.data, c.opt); !errors.Is(err, c.want) {
			t.Errorf("%s: err=%v want %v", c.name, err, c.want)
		}
	}
	if err := openErr(buildZip(t, ok(rows(10))), ReadOptions{MaxRows: 10}); err != nil {
		t.Errorf("exactly at limit should pass: %v", err)
	}
}

func TestCorruptNeverPanics(t *testing.T) {
	var buf bytes.Buffer
	x := NewWriter(&buf)
	s, _ := x.NewSheet("s", SheetOptions{Header: []string{"a", "b"}})
	for i := 0; i < 50; i++ {
		_ = s.WriteRow("v", i, time.Now(), true)
	}
	_ = x.Close()
	orig := buf.Bytes()
	rng := rand.New(rand.NewSource(1))
	try := func(b []byte) {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("panic: %v", p)
			}
		}()
		_, _ = ReadAll(bytes.NewReader(b), int64(len(b)), ReadOptions{})
	}
	for n := 0; n < len(orig); n += 7 { // 各种位置截断
		try(orig[:n])
	}
	for i := 0; i < 400; i++ { // 随机翻转字节
		b := append([]byte(nil), orig...)
		for k := 0; k < 1+rng.Intn(8); k++ {
			b[rng.Intn(len(b))] ^= byte(1 + rng.Intn(255))
		}
		try(b)
	}
	if err := openErr(orig[:len(orig)/2], ReadOptions{}); err == nil {
		t.Error("truncated file should error")
	}
}
