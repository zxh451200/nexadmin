package website

import (
	"fmt"
	"html/template"
	"regexp"
	"strconv"
	"strings"
)

// 品牌强调色：只覆盖内置 default 主题的三个语义 token（--accent / --accent-2 /
// --accent-soft），不动布局、字号与其它颜色。独立品牌主题有自己的配色体系，
// 不消费这两个设置项（见 ThemeCapBrandColor）。
//
// 深浅皮肤分开存：深色皮肤为空时跟随浅色值，两者都空时模板不输出 style，
// 完全沿用主题内置色，存量站点升级后视觉零变化。

var brandColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{3}(?:[0-9a-fA-F]{3})?$`)

// normalizeBrandColor 校色并把 #abc 扩成 #aabbcc、统一小写；非法值返回 ""。
// 调用方据此区分「管理员没填」（空串）与「填了但不合法」（保存时报错）。
func normalizeBrandColor(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", true
	}
	if !brandColorRe.MatchString(s) {
		return "", false
	}
	s = strings.ToLower(s)
	if len(s) == 4 {
		s = fmt.Sprintf("#%c%c%c%c%c%c", s[1], s[1], s[2], s[2], s[3], s[3])
	}
	return s, true
}

func brandRGB(color string) (int, int, int) {
	ch := func(i int) int {
		v, _ := strconv.ParseUint(color[i:i+2], 16, 8)
		return int(v)
	}
	return ch(1), ch(3), ch(5)
}

// brandLighten 把主色向白色提亮一档，用作 --accent-2（深色皮肤上的次级强调色）。
// 22% 接近内置 default  deepening 后 #5e6ad2 → #8a8ff0 的观感差。
func brandLighten(r, g, b int) string {
	mix := func(v int) int { return v + (255-v)*22/100 }
	return fmt.Sprintf("#%02x%02x%02x", mix(r), mix(g), mix(b))
}

// brandOverrideCSS 生成注入 <head> 的覆盖样式。返回 template.CSS：内容仅由
// 校验过的 hex 与整数拼成，不存在脚本注入面。
func brandOverrideCSS(light, dark string) template.CSS {
	light, okL := normalizeBrandColor(light)
	dark, okD := normalizeBrandColor(dark)
	if !okL || !okD {
		return ""
	}
	if light == "" && dark == "" {
		return ""
	}
	var buf strings.Builder
	if dark != "" {
		r, g, b := brandRGB(dark)
		fmt.Fprintf(&buf, `html:not([data-scheme="light"]){--accent:%s;--accent-2:%s;--accent-soft:rgba(%d,%d,%d,.14)}`,
			dark, brandLighten(r, g, b), r, g, b)
	}
	if light != "" {
		r, g, b := brandRGB(light)
		// 内置亮色皮肤里 --accent-2 与 --accent 同值，保持一致。
		fmt.Fprintf(&buf, `html[data-scheme="light"]{--accent:%s;--accent-2:%s;--accent-soft:rgba(%d,%d,%d,.10)}`,
			light, light, r, g, b)
	}
	return template.CSS(buf.String())
}
