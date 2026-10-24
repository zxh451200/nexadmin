package excel

import (
	"archive/zip"
	"bytes"
	"strconv"
	"strings"
	"testing"
)

// buildZip 现场构造 zip。
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func itoa(i int) string { return strconv.Itoa(i) }

const (
	hdr  = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	nsM  = `http://schemas.openxmlformats.org/spreadsheetml/2006/main`
	nsR  = `http://schemas.openxmlformats.org/officeDocument/2006/relationships`
	nsPR = `http://schemas.openxmlformats.org/package/2006/relationships`
)

// minimal 返回结构仿照 Excel 导出的最小工作簿（绝对路径 Target、命名空间前缀属性、rels 齐全）。
func minimal(sheet, sst, styles string) map[string]string {
	return map[string]string{
		"[Content_Types].xml": hdr + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels":         hdr + `<Relationships xmlns="` + nsPR + `"><Relationship Id="rId1" Type="` + nsR + `/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml": hdr + `<workbook xmlns="` + nsM + `" xmlns:r="` + nsR + `"><workbookPr defaultThemeVersion="124226"/>` +
			`<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/><sheet name="Other" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": hdr + `<Relationships xmlns="` + nsPR + `">` +
			`<Relationship Id="rId1" Type="` + nsR + `/worksheet" Target="/xl/worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId2" Type="` + nsR + `/worksheet" Target="worksheets/sheet2.xml"/>` +
			`<Relationship Id="rId3" Type="` + nsR + `/styles" Target="styles.xml"/>` +
			`<Relationship Id="rId4" Type="` + nsR + `/sharedStrings" Target="sharedStrings.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": sheet,
		"xl/worksheets/sheet2.xml": hdr + `<worksheet xmlns="` + nsM + `"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>other</t></is></c></row></sheetData></worksheet>`,
		"xl/sharedStrings.xml":     sst,
		"xl/styles.xml":            styles,
	}
}

func TestExcelLikeFixture(t *testing.T) {
	styles := hdr + `<styleSheet xmlns="` + nsM + `"><numFmts count="2">` +
		`<numFmt numFmtId="164" formatCode="yyyy/m/d\ h:mm"/><numFmt numFmtId="165" formatCode="0.00&quot;d&quot;"/></numFmts>` +
		`<cellStyleXfs count="1"><xf numFmtId="14"/></cellStyleXfs>` +
		`<cellXfs count="4"><xf numFmtId="0"/><xf numFmtId="14" applyNumberFormat="1"/><xf numFmtId="164"/><xf numFmtId="165"/></cellXfs></styleSheet>`
	sst := hdr + `<sst xmlns="` + nsM + `" count="4" uniqueCount="3">` +
		`<si><t>账号</t></si><si><r><rPr><b/></rPr><t>富</t></r><r><t xml:space="preserve"> 文本</t></r><rPh sb="0" eb="1"><t>ふ</t></rPh></si>` +
		`<si><t>&lt;x&gt; &amp; &#x1F600;</t></si></sst>`
	sheet := hdr + `<worksheet xmlns="` + nsM + `" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:x14ac="http://schemas.microsoft.com/office/spreadsheetml/2009/9/ac" mc:Ignorable="x14ac">` +
		`<dimension ref="A1:D6"/><sheetViews><sheetView tabSelected="1" workbookViewId="0"/></sheetViews><sheetFormatPr defaultRowHeight="15" x14ac:dyDescent="0.25"/>` +
		`<cols><col min="1" max="1" width="20" customWidth="1"/></cols><sheetData>` +
		`<row r="1" spans="1:4" x14ac:dyDescent="0.25"><c r="A1" t="s"><v>0</v></c><c r="C1" t="s"><v>1</v></c><c r="D1" t="s"><v>2</v></c></row>` +
		`<row r="3" spans="1:4"><c r="B3" s="1"><v>45306</v></c><c r="C3" s="2"><v>45306.5</v></c><c r="D3" s="3"><v>1.5</v></c><c r="A3"><v>13800000000</v></c></row>` +
		`<row r="4"><c r="A4" t="b"><v>1</v></c><c r="B4" t="b"><v>0</v></c><c r="C4" t="str"><f>A1&amp;"x"</f><v>公式结果</v></c><c r="D4" t="e"><v>#DIV/0!</v></c></row>` +
		`<row r="5"><c r="A5" t="inlineStr"><is><r><t>in</t></r><r><t>line</t></r></is></c><c r="B5"/><c r="C5" s="1"/></row>` +
		`<row r="6"><c r="A6" t="s"><v>0</v></c><c r="B6"/></row>` +
		`</sheetData><mergeCells count="1"><mergeCell ref="A6:B7"/></mergeCells><pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/></worksheet>`
	got := readAll(t, buildZip(t, minimal(sheet, sst, styles)), ReadOptions{})
	if len(got) != 6 || got[1] != nil {
		t.Fatalf("sparse rows wrong: %q", got)
	}
	eq := func(r, c int, want string) {
		t.Helper()
		g := ""
		if c < len(got[r]) {
			g = got[r][c]
		}
		if g != want {
			t.Errorf("[%d][%d]=%q want %q", r, c, g, want)
		}
	}
	eq(0, 0, "账号")
	eq(0, 1, "") // 稀疏列补空
	eq(0, 2, "富 文本")
	eq(0, 3, "<x> & 😀")
	eq(2, 0, "13800000000")
	eq(2, 1, "2024-01-15")
	eq(2, 2, "2024-01-15 12:00:00")
	eq(2, 3, "1.5") // 自定义数字格式 0.00"d" 不是日期
	eq(3, 0, "TRUE")
	eq(3, 1, "FALSE")
	eq(3, 2, "公式结果") // 公式取缓存值
	eq(3, 3, "#DIV/0!")
	eq(4, 0, "inline")
	eq(5, 0, "账号") // 合并区域只有左上角有值
	if len(got[5]) != 1 {
		t.Errorf("merged row %q", got[5])
	}
	if g := readAll(t, buildZip(t, minimal(sheet, sst, styles)), ReadOptions{Sheet: "Other"}); len(g) != 1 || g[0][0] != "other" {
		t.Fatalf("other sheet %v", g)
	}
}

func TestDate1904AndTimeOnly(t *testing.T) {
	files := minimal(hdr+`<worksheet xmlns="`+nsM+`"><sheetData><row r="1"><c r="A1" s="1"><v>43844</v></c><c r="B1" s="2"><v>0.75</v></c><c r="C1" t="d"><v>2024-03-04T05:06:07Z</v></c></row></sheetData></worksheet>`,
		hdr+`<sst xmlns="`+nsM+`"/>`,
		hdr+`<styleSheet xmlns="`+nsM+`"><cellXfs count="3"><xf numFmtId="0"/><xf numFmtId="14"/><xf numFmtId="21"/></cellXfs></styleSheet>`)
	files["xl/workbook.xml"] = strings.Replace(files["xl/workbook.xml"], `<workbookPr defaultThemeVersion="124226"/>`, `<workbookPr date1904="1"/>`, 1)
	got := readAll(t, buildZip(t, files), ReadOptions{})
	// 1904 系统：43844 + 1462 = 45306 -> 2024-01-15
	if got[0][0] != "2024-01-15" || got[0][1] != "18:00:00" || got[0][2] != "2024-03-04 05:06:07" {
		t.Fatalf("got %q", got[0])
	}
}

func TestInlineStrAndSharedStrings(t *testing.T) {
	files := minimal(hdr+`<worksheet xmlns="`+nsM+`"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>hi</t></is></c><c r="B1" t="s"><v>0</v></c></row></sheetData></worksheet>`,
		hdr+`<sst xmlns="`+nsM+`"><si><t>shared</t></si></sst>`, "")
	delete(files, "xl/styles.xml") // 没有 styles 部件也能读
	if got := readAll(t, buildZip(t, files), ReadOptions{}); got[0][0] != "hi" || got[0][1] != "shared" {
		t.Fatalf("got %q", got)
	}
}
