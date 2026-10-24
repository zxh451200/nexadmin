package htmlsafe

import (
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
)

var boolAttrs = set("controls", "muted", "loop")

// 每个标签允许的属性。a 的 rel/target、style、class 另有专门处理。
var tagAttrs = map[string]map[string]bool{
	"a":      set("href", "title", "target"),
	"img":    set("src", "alt", "title", "width", "height"),
	"video":  set("src", "poster", "controls", "muted", "loop", "preload", "width", "height", "title"),
	"source": set("src", "type"),
	"td":     set("colspan", "rowspan"),
	"th":     set("colspan", "rowspan"),
	"col":    set("span"),
	"ol":     set("start"),
}

var (
	reDigits    = regexp.MustCompile(`^[0-9]{1,5}$`)
	reSize      = regexp.MustCompile(`^[0-9]{1,5}%?$`)
	reMIME      = regexp.MustCompile(`^[A-Za-z0-9.+-]+/[A-Za-z0-9.+-]+$`)
	reClass     = regexp.MustCompile(`^(ql|language)-[A-Za-z0-9_+-]+$`)
	reScheme    = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*):`)
	reDataImage = regexp.MustCompile(`^(?i:data:image/(png|jpe?g|gif|webp|bmp);base64,)[A-Za-z0-9+/]+={0,2}$`)
)

// cleanAttrs 按标签白名单过滤并规范属性；同名属性只取第一个（与浏览器一致）。
func cleanAttrs(tag string, attrs []xhtml.Attribute) []xhtml.Attribute {
	var out []xhtml.Attribute
	seen := map[string]bool{}
	allowed := tagAttrs[tag]
	for _, a := range attrs {
		key := strings.ToLower(a.Key)
		if a.Namespace != "" || seen[key] {
			continue
		}
		seen[key] = true
		val := a.Val
		switch {
		case key == "style":
			if s := cleanStyle(val); s != "" {
				out = append(out, xhtml.Attribute{Key: "style", Val: s})
			}
			continue
		case key == "class":
			if c := cleanClass(val); c != "" {
				out = append(out, xhtml.Attribute{Key: "class", Val: c})
			}
			continue
		case !allowed[key]:
			continue
		}
		switch key {
		case "href":
			if safeURL(val, "http", "https", "mailto") {
				out = append(out, xhtml.Attribute{Key: key, Val: strings.TrimSpace(val)})
			}
		case "src", "poster":
			if safeMediaURL(tag, key, val) {
				out = append(out, xhtml.Attribute{Key: key, Val: strings.TrimSpace(val)})
			}
		case "target":
			if val == "_blank" {
				out = append(out, xhtml.Attribute{Key: key, Val: "_blank"})
			}
		case "width", "height":
			if reSize.MatchString(strings.TrimSpace(val)) {
				out = append(out, xhtml.Attribute{Key: key, Val: strings.TrimSpace(val)})
			}
		case "colspan", "rowspan", "span", "start":
			if reDigits.MatchString(strings.TrimSpace(val)) {
				out = append(out, xhtml.Attribute{Key: key, Val: strings.TrimSpace(val)})
			}
		case "preload":
			if v := strings.ToLower(val); v == "none" || v == "metadata" || v == "auto" {
				out = append(out, xhtml.Attribute{Key: key, Val: v})
			}
		case "type":
			if reMIME.MatchString(val) {
				out = append(out, xhtml.Attribute{Key: key, Val: val})
			}
		case "controls", "muted", "loop":
			out = append(out, xhtml.Attribute{Key: key})
		default: // alt / title
			out = append(out, xhtml.Attribute{Key: key, Val: val})
		}
	}
	if tag == "a" {
		out = append(out, xhtml.Attribute{Key: "rel", Val: "noopener noreferrer"})
	}
	return out
}

// safeMediaURL：img 的 src 额外允许受控的 data:image（不含 svg）；video/source/poster 只允许 http(s) 和相对路径。
func safeMediaURL(tag, key, v string) bool {
	if tag == "img" && key == "src" && reDataImage.MatchString(strings.TrimSpace(v)) {
		return true
	}
	return safeURL(v, "http", "https")
}

// safeURL 只放行白名单 scheme 或相对路径。含控制字符的值一律拒绝（浏览器会忽略其中的 tab/换行，
// 形如 "java\tscript:" 的绕过因此无从谈起）；相对路径的第一段不能含冒号，否则会被当成 scheme。
func safeURL(v string, schemes ...string) bool {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 8192 || strings.ContainsRune(v, '\\') {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	if m := reScheme.FindStringSubmatch(v); m != nil {
		s := strings.ToLower(m[1])
		for _, ok := range schemes {
			if s == ok {
				return true
			}
		}
		return false
	}
	first := v
	if i := strings.IndexAny(v, "/?#"); i >= 0 {
		first = v[:i]
	}
	return !strings.Contains(first, ":")
}

func cleanClass(v string) string {
	var keep []string
	for _, c := range strings.Fields(v) {
		if reClass.MatchString(c) {
			keep = append(keep, c)
		}
	}
	return strings.Join(keep, " ")
}
