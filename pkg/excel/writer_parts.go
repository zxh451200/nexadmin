package excel

import (
	"strconv"
	"strings"
)

const (
	xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	nsMain    = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsRel     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPkgRel  = "http://schemas.openxmlformats.org/package/2006/relationships"
)

// stylesXML：cellXfs 顺序对应 styleDefault / styleHeader / styleDate / styleDateTime / styleQuote。
// 自定义数字格式 164 = yyyy-mm-dd，165 = yyyy-mm-dd hh:mm:ss；49 是内置的文本格式 "@"。
const stylesXML = xmlHeader + `<styleSheet xmlns="` + nsMain + `">` +
	`<numFmts count="2"><numFmt numFmtId="164" formatCode="yyyy\-mm\-dd"/><numFmt numFmtId="165" formatCode="yyyy\-mm\-dd\ hh:mm:ss"/></numFmts>` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="5">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="49" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" quotePrefix="1"/>` +
	`</cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`

func (x *Writer) writeParts() {
	var b strings.Builder
	b.WriteString(xmlHeader + `<sst xmlns="` + nsMain + `" count="` + strconv.Itoa(len(x.sstList)) +
		`" uniqueCount="` + strconv.Itoa(len(x.sstList)) + `">`)
	for _, s := range x.sstList {
		b.WriteString(`<si><t xml:space="preserve">` + escapeText(s) + `</t></si>`)
	}
	b.WriteString(`</sst>`)

	var ct, wb, rels strings.Builder
	ct.WriteString(xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
		`<Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>`)
	wb.WriteString(xmlHeader + `<workbook xmlns="` + nsMain + `" xmlns:r="` + nsRel + `"><bookViews><workbookView/></bookViews><sheets>`)
	rels.WriteString(xmlHeader + `<Relationships xmlns="` + nsPkgRel + `">`)
	for i, name := range x.sheets {
		n := strconv.Itoa(i + 1)
		ct.WriteString(`<Override PartName="/xl/worksheets/sheet` + n + `.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`)
		wb.WriteString(`<sheet name="` + escapeText(name) + `" sheetId="` + n + `" r:id="rId` + n + `"/>`)
		rels.WriteString(`<Relationship Id="rId` + n + `" Type="` + nsRel + `/worksheet" Target="worksheets/sheet` + n + `.xml"/>`)
	}
	k := strconv.Itoa(len(x.sheets) + 1)
	k2 := strconv.Itoa(len(x.sheets) + 2)
	rels.WriteString(`<Relationship Id="rId` + k + `" Type="` + nsRel + `/styles" Target="styles.xml"/>` +
		`<Relationship Id="rId` + k2 + `" Type="` + nsRel + `/sharedStrings" Target="sharedStrings.xml"/></Relationships>`)
	ct.WriteString(`</Types>`)
	wb.WriteString(`</sheets></workbook>`)

	x.addFile("[Content_Types].xml", ct.String())
	x.addFile("_rels/.rels", xmlHeader+`<Relationships xmlns="`+nsPkgRel+`"><Relationship Id="rId1" Type="`+nsRel+
		`/officeDocument" Target="xl/workbook.xml"/></Relationships>`)
	x.addFile("xl/workbook.xml", wb.String())
	x.addFile("xl/_rels/workbook.xml.rels", rels.String())
	x.addFile("xl/styles.xml", stylesXML)
	x.addFile("xl/sharedStrings.xml", b.String())
}
