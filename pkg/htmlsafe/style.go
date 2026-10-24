package htmlsafe

import (
	"regexp"
	"strings"
)

var (
	reAlign = regexp.MustCompile(`^(?i:left|right|center|justify|start|end)$`)
	reColor = regexp.MustCompile(`^(?i:#[0-9a-f]{3,4}|#[0-9a-f]{6}|#[0-9a-f]{8}|(?:rgb|hsl)a?\([0-9., %/]{1,40}\)|[a-z]{3,20})$`)
)

// cleanStyle 只保留 text-align / color / background-color 三个属性，且值必须匹配安全模式；
// 含反斜杠（CSS 转义）、注释、括号函数（rgb/hsl 之外）、url() 等的声明一律丢弃。
func cleanStyle(v string) string {
	var parts []string
	for _, decl := range strings.Split(v, ";") {
		k, val, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		k, val = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(val)
		switch k {
		case "text-align":
			if reAlign.MatchString(val) {
				parts = append(parts, k+": "+strings.ToLower(val))
			}
		case "color", "background-color":
			if reColor.MatchString(val) {
				parts = append(parts, k+": "+val)
			}
		}
	}
	return strings.Join(parts, "; ")
}
