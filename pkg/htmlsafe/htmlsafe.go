// Package htmlsafe 是服务端富文本白名单净化：基于 golang.org/x/net/html 的 tokenizer 逐 token 重新生成 HTML，
// 只输出白名单里的标签和属性，所有文本与属性值都重新转义，所以实体编码、大小写混淆、属性注入等绕过手法
// 都不会原样透传。未闭合的标签会被补全，多余的结束标签会被丢弃。
//
// 它不是浏览器级解析器：不在白名单里的标签被“解包”（标签丢弃、文字保留），
// 危险标签（script/style/iframe/object/embed/form/svg/math…）连同内容整体丢弃。
package htmlsafe

import (
	"html"
	"strings"

	xhtml "golang.org/x/net/html"
)

// maxDepth 限制输出的嵌套深度，防止病态输入拖垮浏览器或后续处理。
const maxDepth = 64

// dropWithContent 的标签连同内容整体丢弃（含未闭合时直到输入结尾）。
var dropWithContent = set("script", "style", "frame", "frameset", "object", "embed", "applet", "form",
	"svg", "math", "noscript", "template", "textarea", "title", "head", "xmp", "plaintext", "noembed", "noframes",
	"select", "button", "canvas", "audio", "link", "meta", "base")

// voidTags 是允许输出的空标签。
var voidTags = set("br", "hr", "img", "source", "col")

// allowedTags 是允许输出的标签（含空标签）。
var allowedTags = set(
	"p", "div", "span", "br", "hr", "h1", "h2", "h3", "h4", "h5", "h6",
	"ul", "ol", "li", "blockquote", "pre", "code",
	"table", "thead", "tbody", "tfoot", "tr", "th", "td", "caption", "colgroup", "col",
	"strong", "b", "em", "i", "u", "s", "del", "ins", "sub", "sup", "mark", "small",
	"a", "img", "video", "source", "figure", "figcaption", "iframe",
)

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// Sanitize 返回净化后的 HTML。空输入返回空串。
func Sanitize(in string) string {
	if in == "" {
		return ""
	}
	var (
		out   strings.Builder
		stack []string // 已输出且尚未闭合的标签
		z     = xhtml.NewTokenizer(strings.NewReader(in))
	)
	out.Grow(len(in))
	closeTop := func() {
		out.WriteString("</" + stack[len(stack)-1] + ">")
		stack = stack[:len(stack)-1]
	}
	for {
		tt := z.Next()
		switch tt {
		case xhtml.ErrorToken:
			for len(stack) > 0 { // 补全未闭合标签
				closeTop()
			}
			return out.String()
		case xhtml.TextToken:
			out.WriteString(html.EscapeString(string(z.Text())))
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			t := z.Token()
			name := t.Data
			if dropWithContent[name] {
				if tt == xhtml.StartTagToken {
					skipElement(z, name)
				}
				continue
			}
			if !allowedTags[name] {
				continue // 解包：丢标签，留内容
			}
			// iframe 只服务视频嵌入：src 不在平台白名单里的整块丢弃，避免留下空框或任意站外页面。
			if name == "iframe" {
				src := ""
				for _, a := range t.Attr {
					if strings.EqualFold(a.Key, "src") {
						src = a.Val
					}
				}
				if !safeEmbedURL(src) {
					if tt == xhtml.StartTagToken {
						skipElement(z, name)
					}
					continue
				}
			}
			if voidTags[name] {
				writeStart(&out, name, t.Attr, true)
				continue
			}
			if len(stack) >= maxDepth {
				continue
			}
			writeStart(&out, name, t.Attr, false)
			if tt == xhtml.SelfClosingTagToken {
				out.WriteString("</" + name + ">")
			} else {
				stack = append(stack, name)
			}
		case xhtml.EndTagToken:
			name := z.Token().Data
			idx := -1
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i] == name {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue // 没有对应的开标签（或标签不在白名单）
			}
			for len(stack) > idx {
				closeTop()
			}
		default:
			// 注释、doctype、CDATA 一律丢弃
		}
	}
}

// skipElement 跳过 name 元素的全部内容，处理同名嵌套；找不到结束标签就跳到输入结尾。
func skipElement(z *xhtml.Tokenizer, name string) {
	depth := 1
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			return
		case xhtml.StartTagToken:
			if n, _ := z.TagName(); string(n) == name {
				depth++
			}
		case xhtml.EndTagToken:
			if n, _ := z.TagName(); string(n) == name {
				if depth--; depth == 0 {
					return
				}
			}
		}
	}
}

func writeStart(out *strings.Builder, name string, attrs []xhtml.Attribute, void bool) {
	out.WriteString("<" + name)
	for _, a := range cleanAttrs(name, attrs) {
		out.WriteString(" " + a.Key)
		if a.Val != "" || !boolAttrs[a.Key] {
			out.WriteString(`="` + html.EscapeString(a.Val) + `"`)
		}
	}
	out.WriteString(">")
}
