package excel

import (
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"
)

// tokenizer 包装 xml.Decoder：拒绝 DTD/实体声明、限制嵌套深度。
// encoding/xml 本身不解析外部实体，也不会展开自定义实体（未知实体直接报错），所以不存在实体膨胀；
// 这里仍然显式拒绝 <!DOCTYPE>，让带 DTD 的文件整体失败而不是被悄悄接受。
type tokenizer struct {
	d     *xml.Decoder
	depth int
	max   int
}

func newTokenizer(r io.Reader, maxDepth int) *tokenizer {
	d := xml.NewDecoder(r)
	d.Strict = true
	return &tokenizer{d: d, max: maxDepth}
}

func (t *tokenizer) next() (xml.Token, error) {
	tok, err := t.d.Token()
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, mapErr(err)
	}
	switch tok.(type) {
	case xml.Directive:
		return nil, fmt.Errorf("%w: DTD / entity declarations are not allowed", ErrMalformed)
	case xml.StartElement:
		if t.depth++; t.depth > t.max {
			return nil, fmt.Errorf("%w: xml nesting deeper than %d", ErrTooLarge, t.max)
		}
	case xml.EndElement:
		t.depth--
	}
	return tok, nil
}

// skip 跳过当前已读入的开始标签对应的整个子树。
func (t *tokenizer) skip() error {
	for target := t.depth - 1; t.depth > target; {
		if _, err := t.next(); err != nil {
			return unexpectedEOF(err)
		}
	}
	return nil
}

func unexpectedEOF(err error) error {
	if err == io.EOF {
		return fmt.Errorf("%w: unexpected end of xml", ErrMalformed)
	}
	return err
}

// text 读取当前元素的全部文本内容（含子元素文本，跳过 rPh 拼音注释），读到该元素结束为止。
func (t *tokenizer) text() (string, error) {
	var b strings.Builder
	for target := t.depth - 1; t.depth > target; {
		tok, err := t.next()
		if err != nil {
			return "", unexpectedEOF(err)
		}
		switch v := tok.(type) {
		case xml.CharData:
			b.Write(v)
		case xml.StartElement:
			if v.Name.Local == "rPh" {
				if err := t.skip(); err != nil {
					return "", err
				}
			}
		}
	}
	return b.String(), nil
}

func attr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local && (a.Name.Space == "" || local != "id") {
			return a.Value
		}
	}
	return ""
}

// relAttr 取 r:id（带命名空间的 id 属性）。
func relAttr(se xml.StartElement) string {
	for _, a := range se.Attr {
		if a.Name.Local == "id" && a.Name.Space != "" {
			return a.Value
		}
	}
	return ""
}

func (rd *Reader) tokens(part string) (*tokenizer, io.Closer, error) {
	rc, err := rd.openPart(part)
	if err != nil || rc == nil {
		return nil, nil, err
	}
	return newTokenizer(rc, rd.opt.MaxDepth), rc, nil
}

// loadRels 读取 .rels，返回 Id -> (Type, Target)。
func (rd *Reader) loadRels(part string) (map[string][2]string, error) {
	t, c, err := rd.tokens(part)
	if err != nil || t == nil {
		return nil, err
	}
	defer c.Close()
	out := map[string][2]string{}
	for {
		tok, err := t.next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
			out[attr(se, "Id")] = [2]string{attr(se, "Type"), attr(se, "Target")}
		}
	}
}

func resolveTarget(base, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	return path.Join(path.Dir(base), target)
}

// loadWorkbook 解析 workbook.xml 与其 rels，得到 sheet 名称 -> 部件路径。
func (rd *Reader) loadWorkbook() error {
	wbPart := "xl/workbook.xml"
	if root, err := rd.loadRels("_rels/.rels"); err != nil {
		return err
	} else {
		for _, r := range root {
			if strings.HasSuffix(r[0], "/officeDocument") {
				wbPart = resolveTarget("x", r[1])
			}
		}
	}
	rels, err := rd.loadRels(path.Join(path.Dir(wbPart), "_rels", path.Base(wbPart)+".rels"))
	if err != nil {
		return err
	}
	t, c, err := rd.tokens(wbPart)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("%w: workbook part missing", ErrNotXLSX)
	}
	defer c.Close()
	rd.rels = map[string]string{}
	for {
		tok, err := t.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "workbookPr":
			rd.date = attr(se, "date1904") == "1" || attr(se, "date1904") == "true"
		case "sheet":
			rel, ok := rels[relAttr(se)]
			if !ok {
				continue
			}
			if len(rd.sheets) >= 1000 {
				return fmt.Errorf("%w: too many sheets", ErrTooLarge)
			}
			rd.sheets = append(rd.sheets, sheetRef{name: attr(se, "name"), part: resolveTarget(wbPart, rel[1])})
		}
	}
	if len(rd.sheets) == 0 {
		return fmt.Errorf("%w: no worksheets", ErrNotXLSX)
	}
	for id, r := range rels {
		rd.rels[id] = r[0] + "|" + resolveTarget(wbPart, r[1])
	}
	return nil
}

// relPart 返回工作簿中指定类型（Type 后缀）关系的目标部件。
func (rd *Reader) relPart(suffix string) string {
	for _, v := range rd.rels {
		if i := strings.IndexByte(v, '|'); i > 0 && strings.HasSuffix(v[:i], suffix) {
			return v[i+1:]
		}
	}
	return ""
}
