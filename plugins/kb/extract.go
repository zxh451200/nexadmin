package kb

// 文档文本抽取：txt / md 直接读；docx 用标准库解 zip 走 XML 流式抽取（零额外依赖）；
// pdf 用纯 Go 的 ledongthuc/pdf（rsc.io/pdf 系，BSD）做文本抽取。
// 抽不出文本（扫描件 / 纯图片 PDF）一律明确报错引导转换，绝不静默入库空文档。

import (
	"archive/zip"
	"encoding/xml"
	"strings"
	"unicode/utf8"

	docpdf "github.com/ledongthuc/pdf"

	"nexadmin/core/response"
)

// maxTextBytes 是抽取后的文本上限：防住压缩炸弹把内存打爆。
const maxTextBytes = 4 << 20

// extractText 按扩展名分流抽取正文。
func extractText(ext, raw string) (string, error) {
	switch ext {
	case ".txt", ".md", ".markdown":
		return extractPlainText(raw)
	case ".pdf":
		return extractPdf(raw)
	case ".docx":
		return extractDocx(raw)
	default:
		return "", responseBadRequest("kb.document_type")
	}
}

func responseBadRequest(key string) error {
	return response.BadRequest(key)
}

func extractPlainText(raw string) (string, error) {
	// 二进制防线：UTF-8 校验不过、含 NUL 都按「不是文本」拒收。
	text := strings.TrimPrefix(raw, "\uFEFF")
	if !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
		return "", responseBadRequest("kb.document_type")
	}
	return text, nil
}

// extractPdf 抽取 PDF 文本层。逐页抽取：个别页字体/CMap 解不开时跳过该页继续，
// 全部失败（典型是扫描件）报 kb.document_no_text。
func extractPdf(raw string) (string, error) {
	r, err := docpdf.NewReader(strings.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", responseBadRequest("kb.document_type")
	}
	var b strings.Builder
	for i := 1; i <= r.NumPage() && b.Len() <= maxTextBytes; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		// 页面字体表：GetPlainText 需要调用方把本页字体收集成 map。
		fonts := make(map[string]*docpdf.Font)
		for _, name := range page.Fonts() {
			font := page.Font(name)
			fonts[name] = &font
		}
		text, perr := page.GetPlainText(fonts)
		if perr != nil {
			continue // 单页解不出：跳过，不让整篇失败
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	text := strings.TrimSpace(limitText(b.String()))
	if text == "" {
		// 常见原因：扫描件 / 纯图片 PDF，没有文本层。
		return "", responseBadRequest("kb.document_no_text")
	}
	return text, nil
}

// extractDocx 抽取 .docx（Office Open XML）正文：word/document.xml 里的 w:t 文本运行，
// w:p 段落转空行（知识块自然分界），w:tab / w:br 转空白；页眉页脚、脚注、批注等其它部件不收。
func extractDocx(raw string) (string, error) {
	zr, err := zip.NewReader(strings.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", responseBadRequest("kb.document_type") // 不是 zip：多半是老 .doc 改了后缀
	}
	var entry *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			entry = f
			break
		}
	}
	if entry == nil {
		return "", responseBadRequest("kb.document_type")
	}
	rc, err := entry.Open()
	if err != nil {
		return "", responseBadRequest("kb.document_type")
	}
	defer func() { _ = rc.Close() }()

	var b strings.Builder
	write := func(s string) {
		if b.Len() <= maxTextBytes {
			b.WriteString(s)
		}
	}
	depthParagraph := 0
	inText := false
	dec := xml.NewDecoder(rc)
	dec.Strict = false // 容忍个别坏实体：拿到多少用多少
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				depthParagraph++
			case "t":
				inText = true
			case "tab":
				write(" ")
			case "br":
				write("\n")
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				if depthParagraph > 0 {
					depthParagraph--
					if depthParagraph == 0 {
						write("\n\n")
					}
				}
			case "t":
				inText = false
			}
		case xml.CharData:
			if inText {
				write(string(t))
			}
		}
	}
	text := strings.TrimSpace(limitText(b.String()))
	if text == "" {
		return "", responseBadRequest("kb.document_empty")
	}
	if !utf8.ValidString(text) {
		return "", responseBadRequest("kb.document_type")
	}
	return text, nil
}

// limitText 截断超长抽取文本（按 UTF-8 边界）。
func limitText(s string) string {
	if len(s) <= maxTextBytes {
		return s
	}
	cut := s[:maxTextBytes]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return cut
}
