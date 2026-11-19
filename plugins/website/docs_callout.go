package website

import "strings"

// calloutType 是一种提示块的样式与标题文案。
type calloutType struct {
	key    string
	labels map[string]string
}

// 文档提示块：正文里写 GitHub 风格的告警块
//
//	> [!WARNING]
//	> 这段必须看
//
// 渲染后套一层 .docs-callout，交给 site.css 上色。
// 不引 goldmark 的 HTML 直通（WithUnsafe）——那等于让任何 md 都能塞脚本进页面，
// 而这里只需要几种语气，后处理渲染结果更收敛。
var calloutTypes = map[string]calloutType{
	"NOTE":      {"note", map[string]string{"zh": "注意", "en": "Note"}},
	"TIP":       {"tip", map[string]string{"zh": "提示", "en": "Tip"}},
	"IMPORTANT": {"important", map[string]string{"zh": "重要", "en": "Important"}},
	"WARNING":   {"warning", map[string]string{"zh": "警告", "en": "Warning"}},
	"CAUTION":   {"caution", map[string]string{"zh": "小心", "en": "Caution"}},
}

// docLangLabel 由文件路径判语言：译文放在 <code>/ 子目录下（en/xxx.md）。
func docLangLabel(file string) string {
	if i := strings.LastIndex(file, "/"); i > 0 && docLanguageCodeRe.MatchString(file[:i]) {
		if strings.HasPrefix(strings.ToLower(file), "zh") {
			return "zh"
		}
		return "en"
	}
	return "zh"
}

// applyCallouts 把 <blockquote><p>[!TYPE]</p>…</blockquote> 改写成提示块。
// 标记可与正文同段（> [!NOTE] 后不空行），也可独占一段。
func applyCallouts(rendered, lang string) string {
	var b strings.Builder
	for {
		start := strings.Index(rendered, "<blockquote>")
		if start < 0 {
			b.WriteString(rendered)
			break
		}
		rest := rendered[start:]
		typ, body, tail, ok := splitCallout(rest)
		if !ok {
			// 不是提示块：原样过去，从它的内容继续扫。
			b.WriteString(rendered[:start+len("<blockquote>")])
			rendered = rest[len("<blockquote>"):]
			continue
		}
		b.WriteString(rendered[:start])
		b.WriteString(`<div class="docs-callout docs-callout--` + typ.key + `"><p class="docs-callout-title">` + typ.labels[lang] + `</p>`)
		b.WriteString(body)
		b.WriteString(`</div>`)
		rendered = tail
	}
	return b.String()
}

// splitCallout 判定 s（以 <blockquote> 开头）是否为提示块。
// 是则返回类型、块内 HTML（已剥掉 [!TYPE] 标记）与闭合标签之后的内容。
func splitCallout(s string) (calloutType, string, string, bool) {
	head := "<blockquote>"
	rest := strings.TrimLeft(s[len(head):], " \t\r\n")
	if !strings.HasPrefix(rest, "<p>[!") {
		return calloutType{}, "", "", false
	}
	closing := strings.Index(rest, "]")
	if closing < 0 {
		return calloutType{}, "", "", false
	}
	typ, ok := calloutTypes[strings.ToUpper(rest[len("<p>[!"):closing])]
	if !ok {
		return calloutType{}, "", "", false
	}
	rest = rest[closing+1:]

	// 标记独占一段时后面紧跟 </p>，整段丢掉；同一段里还有正文则把被吃掉的 <p> 补回来。
	inline := true
	if trimmed := strings.TrimLeft(rest, " \t\r\n"); strings.HasPrefix(trimmed, "</p>") {
		rest, inline = trimmed[len("</p>"):], false
	}

	// 找配对的 </blockquote>，块内允许嵌套引用。
	depth, end := 1, -1
	for i := 0; i < len(rest); {
		switch {
		case strings.HasPrefix(rest[i:], "<blockquote>"):
			depth++
			i += len("<blockquote>")
		case strings.HasPrefix(rest[i:], "</blockquote>"):
			depth--
			i += len("</blockquote>")
			if depth == 0 {
				end = i - len("</blockquote>")
			}
		default:
			i++
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return calloutType{}, "", "", false
	}
	body := rest[:end]
	if inline {
		body = "<p>" + body
	}
	if strings.TrimSpace(htmlText(body)) == "" {
		return calloutType{}, "", "", false
	}
	return typ, body, rest[end+len("</blockquote>"):], true
}

// htmlText 粗去标签，只用来判断块内是否还有正文。
func htmlText(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}
