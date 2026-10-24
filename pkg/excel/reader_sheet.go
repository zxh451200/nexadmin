package excel

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// isDateFormat 判断数字格式是否为日期/时间格式。
func isDateFormat(id int, code string) bool {
	switch {
	case id >= 14 && id <= 22, id >= 27 && id <= 36, id >= 45 && id <= 47, id >= 50 && id <= 58:
		return true
	case id < 164 || code == "":
		return false
	}
	if l := strings.ToLower(code); l == "general" || l == "standard" {
		return false
	}
	rs := []rune(code)
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; r {
		case '"': // 引号内是字面文本
			for i++; i < len(rs) && rs[i] != '"'; i++ {
			}
		case '\\', '_', '*':
			i++
		case '[': // [Red] [$-409] 等条件/区域设置；[h] [mm] [ss] 是经过时间
			j := i + 1
			for j < len(rs) && rs[j] != ']' {
				j++
			}
			if body := strings.ToLower(string(rs[i+1 : min(j, len(rs))])); body != "" && strings.Trim(body, "hms") == "" {
				return true
			}
			i = j
		case 'y', 'Y', 'd', 'D', 'h', 'H', 's', 'S', 'm', 'M':
			return true
		}
	}
	return false
}

// loadStyles 返回每个 cellXfs 下标是否为日期样式。
func (rd *Reader) loadStyles() ([]bool, error) {
	part := rd.relPart("/styles")
	if part == "" {
		part = "xl/styles.xml"
	}
	t, c, err := rd.tokens(part)
	if err != nil || t == nil {
		return nil, err
	}
	defer c.Close()
	codes := map[int]string{}
	var xfs []bool
	inXfs := false
	for {
		tok, err := t.next()
		if err == io.EOF {
			return xfs, nil
		}
		if err != nil {
			return nil, err
		}
		switch v := tok.(type) {
		case xml.StartElement:
			switch v.Name.Local {
			case "numFmt":
				if id, err := strconv.Atoi(attr(v, "numFmtId")); err == nil {
					codes[id] = attr(v, "formatCode")
				}
			case "cellXfs":
				inXfs = true
			case "xf":
				if inXfs {
					if len(xfs) >= 100000 {
						return nil, fmt.Errorf("%w: too many cell styles", ErrTooLarge)
					}
					id, _ := strconv.Atoi(attr(v, "numFmtId"))
					xfs = append(xfs, isDateFormat(id, codes[id]))
				}
			}
		case xml.EndElement:
			if v.Name.Local == "cellXfs" {
				inXfs = false
			}
		}
	}
}

// loadSharedStrings 读取 sharedStrings；富文本拼接各 <r><t>，忽略拼音注释 <rPh>。
func (rd *Reader) loadSharedStrings() ([]string, error) {
	part := rd.relPart("/sharedStrings")
	if part == "" {
		part = "xl/sharedStrings.xml"
	}
	t, c, err := rd.tokens(part)
	if err != nil || t == nil {
		return nil, err
	}
	defer c.Close()
	var out []string
	for {
		tok, err := t.next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "si" {
			if len(out) >= rd.opt.MaxStrings {
				return nil, fmt.Errorf("%w: more than %d shared strings", ErrTooLarge, rd.opt.MaxStrings)
			}
			s, err := t.text()
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
	}
}

func (rd *Reader) pickSheet() (sheetRef, error) {
	if rd.opt.Sheet != "" {
		for _, s := range rd.sheets {
			if s.name == rd.opt.Sheet {
				return s, nil
			}
		}
		return sheetRef{}, fmt.Errorf("excel: sheet %q not found", rd.opt.Sheet)
	}
	if rd.opt.SheetIndex < 0 || rd.opt.SheetIndex >= len(rd.sheets) {
		return sheetRef{}, fmt.Errorf("excel: sheet index %d out of range", rd.opt.SheetIndex)
	}
	return rd.sheets[rd.opt.SheetIndex], nil
}

// ReadRows 按文档顺序回调选定 sheet 中有内容的行。row 为真实行号（1 起）；cells 的下标是列号（0 起），
// 行内缺失的单元格补 KindEmpty；完全没有内容的行不回调。公式只取缓存值，合并单元格只有左上角有值。
func (rd *Reader) ReadRows(fn func(row int, cells []Cell) error) (err error) {
	defer recoverTo(&err)
	sh, err := rd.pickSheet()
	if err != nil {
		return err
	}
	styles, err := rd.loadStyles()
	if err != nil {
		return err
	}
	sst, err := rd.loadSharedStrings()
	if err != nil {
		return err
	}
	t, c, err := rd.tokens(sh.part)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("%w: worksheet part %s missing", ErrNotXLSX, sh.part)
	}
	defer c.Close()
	p := &sheetParser{rd: rd, t: t, styles: styles, sst: sst}
	for {
		tok, err := t.next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "row" {
			if err := p.row(se, fn); err != nil {
				return err
			}
		}
	}
}

type sheetParser struct {
	rd      *Reader
	t       *tokenizer
	styles  []bool
	sst     []string
	prevRow int
	emitted int
}

func (p *sheetParser) row(se xml.StartElement, fn func(int, []Cell) error) error {
	rn := p.prevRow + 1
	if v, err := strconv.Atoi(attr(se, "r")); err == nil && v > 0 {
		rn = v
	}
	p.prevRow = rn
	var cells []Cell
	next := 0 // 无 r 属性的单元格按顺序排列
	for target := p.t.depth - 1; p.t.depth > target; {
		tok, err := p.t.next()
		if err != nil {
			return unexpectedEOF(err)
		}
		cs, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if cs.Name.Local != "c" {
			if err := p.t.skip(); err != nil {
				return err
			}
			continue
		}
		col := next
		if ref := attr(cs, "r"); ref != "" {
			if cc, _, ok := parseRef(ref); ok {
				col = cc
			}
		}
		next = col + 1
		cell, err := p.cell(cs)
		if err != nil {
			return err
		}
		if cell.Kind == KindEmpty {
			continue
		}
		if col >= p.rd.opt.MaxCols {
			return fmt.Errorf("%w: more than %d columns", ErrTooLarge, p.rd.opt.MaxCols)
		}
		for len(cells) <= col {
			cells = append(cells, Cell{})
		}
		cells[col] = cell
	}
	if len(cells) == 0 {
		return nil
	}
	if p.emitted++; rn > p.rd.opt.MaxRows || p.emitted > p.rd.opt.MaxRows {
		return fmt.Errorf("%w: more than %d rows", ErrTooLarge, p.rd.opt.MaxRows)
	}
	return fn(rn, cells)
}

var isoLayouts = []string{"2006-01-02T15:04:05Z", "2006-01-02T15:04:05", "2006-01-02T15:04:05.999999999Z", "2006-01-02"}

func (p *sheetParser) cell(cs xml.StartElement) (Cell, error) {
	typ := attr(cs, "t")
	style, _ := strconv.Atoi(attr(cs, "s"))
	isDate := style >= 0 && style < len(p.styles) && p.styles[style]
	var v, inline string
	hasInline := false
	for target := p.t.depth - 1; p.t.depth > target; {
		tok, err := p.t.next()
		if err != nil {
			return Cell{}, unexpectedEOF(err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "v":
			if v, err = p.t.text(); err != nil {
				return Cell{}, err
			}
		case "is":
			hasInline = true
			if inline, err = p.t.text(); err != nil {
				return Cell{}, err
			}
		default: // <f> 公式等：不执行，只用缓存值 <v>
			if err := p.t.skip(); err != nil {
				return Cell{}, err
			}
		}
	}
	switch typ {
	case "inlineStr":
		if !hasInline {
			inline = v
		}
		return strCell(inline), nil
	case "s":
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || i < 0 || i >= len(p.sst) {
			return Cell{}, fmt.Errorf("%w: invalid shared string index %q", ErrMalformed, v)
		}
		return strCell(p.sst[i]), nil
	case "str", "e":
		return strCell(v), nil
	case "b":
		s := strings.TrimSpace(v)
		if s == "" {
			return Cell{}, nil
		}
		return Cell{Kind: KindBool, Bool: s == "1" || strings.EqualFold(s, "true")}, nil
	case "d":
		for _, l := range isoLayouts {
			if tm, err := time.Parse(l, strings.TrimSpace(v)); err == nil {
				n, _ := timeToSerial(tm)
				return Cell{Kind: KindDate, Time: tm.UTC(), Num: n}, nil
			}
		}
		return strCell(v), nil
	}
	if hasInline && v == "" { // 缺省类型但写了 <is>，宽容处理
		return strCell(inline), nil
	}
	raw := strings.TrimSpace(v)
	if raw == "" {
		return Cell{}, nil
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return strCell(v), nil
	}
	if isDate {
		if tm, ok := serialToTime(f, p.rd.date); ok {
			return Cell{Kind: KindDate, Time: tm, Num: f}, nil
		}
	}
	return Cell{Kind: KindNumber, Str: raw, Num: f}, nil
}

func strCell(s string) Cell {
	if s == "" {
		return Cell{}
	}
	return Cell{Kind: KindString, Str: s}
}
