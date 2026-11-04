package website

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

//go:embed tpl/*.tmpl
var tplFS embed.FS

//go:embed static
var staticFS embed.FS

// assetVersion 返回内嵌静态资源的内容哈希（前 8 字节十六进制），模板拿它当 ?v=。
// 原来 ?v= 是手写的字面量，改完 CSS 忘了改它，浏览器就一直用缓存；
// 内容哈希让地址随文件内容自动变化，不需要手动硬刷新。
// 文件不存在时返回空串（地址退化成不带版本，至少不会 404）。
var assetCache sync.Map // name -> string

func assetVersion(name string) string {
	if v, ok := assetCache.Load(name); ok {
		return v.(string)
	}
	sum := ""
	if b, err := staticFS.ReadFile("static/" + name); err == nil {
		h := sha256.Sum256(b)
		sum = hex.EncodeToString(h[:8])
	}
	assetCache.Store(name, sum)
	return sum
}

// 图标集：特性卡片 Icon 字段引用这些名字；未知名渲染默认圆点。
// 24×24 线性风格（stroke），路径取自 Tabler Icons v3.36.0 outline（MIT，Copyright (c) 2020-2025 Paweł Kuna）。
// 键名会写进数据库，换图标只改路径不改键；库里没有的语义才手绘（见 defaultIcon）。
var iconPaths = map[string]string{
	"building": `<path d="M3 21l18 0" /><path d="M9 8l1 0" /><path d="M9 12l1 0" /><path d="M9 16l1 0" /><path d="M14 8l1 0" /><path d="M14 12l1 0" /><path d="M14 16l1 0" /><path d="M5 21v-16a2 2 0 0 1 2 -2h10a2 2 0 0 1 2 2v16" />`,
	"shield":   `<path d="M12 3a12 12 0 0 0 8.5 3a12 12 0 0 1 -8.5 15a12 12 0 0 1 -8.5 -15a12 12 0 0 0 8.5 -3" />`,
	"key":      `<path d="M16.555 3.843l3.602 3.602a2.877 2.877 0 0 1 0 4.069l-2.643 2.643a2.877 2.877 0 0 1 -4.069 0l-.301 -.301l-6.558 6.558a2 2 0 0 1 -1.239 .578l-.175 .008h-1.172a1 1 0 0 1 -.993 -.883l-.007 -.117v-1.172a2 2 0 0 1 .467 -1.284l.119 -.13l.414 -.414h2v-2h2v-2l2.144 -2.144l-.301 -.301a2.877 2.877 0 0 1 0 -4.069l2.643 -2.643a2.877 2.877 0 0 1 4.069 0z" /><path d="M15 9h.01" />`,
	"plug":     `<path d="M9.785 6l8.215 8.215l-2.054 2.054a5.81 5.81 0 1 1 -8.215 -8.215l2.054 -2.054z" /><path d="M4 20l3.5 -3.5" /><path d="M15 4l-3.5 3.5" /><path d="M20 9l-3.5 3.5" />`,
	"file":     `<path d="M14 3v4a1 1 0 0 0 1 1h4" /><path d="M17 21h-10a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h7l5 5v11a2 2 0 0 1 -2 2z" />`,
	"chart":    `<path d="M3 13a1 1 0 0 1 1 -1h4a1 1 0 0 1 1 1v6a1 1 0 0 1 -1 1h-4a1 1 0 0 1 -1 -1z" /><path d="M15 9a1 1 0 0 1 1 -1h4a1 1 0 0 1 1 1v10a1 1 0 0 1 -1 1h-4a1 1 0 0 1 -1 -1z" /><path d="M9 5a1 1 0 0 1 1 -1h4a1 1 0 0 1 1 1v14a1 1 0 0 1 -1 1h-4a1 1 0 0 1 -1 -1z" /><path d="M4 20h14" />`,
	"globe":    `<path d="M7 9a4 4 0 1 0 8 0a4 4 0 0 0 -8 0" /><path d="M5.75 15a8.015 8.015 0 1 0 9.25 -13" /><path d="M11 17v4" /><path d="M7 21h8" />`,
	"puzzle":   `<path d="M4 7h3a1 1 0 0 0 1 -1v-1a2 2 0 0 1 4 0v1a1 1 0 0 0 1 1h3a1 1 0 0 1 1 1v3a1 1 0 0 0 1 1h1a2 2 0 0 1 0 4h-1a1 1 0 0 0 -1 1v3a1 1 0 0 1 -1 1h-3a1 1 0 0 1 -1 -1v-1a2 2 0 0 0 -4 0v1a1 1 0 0 1 -1 1h-3a1 1 0 0 1 -1 -1v-3a1 1 0 0 1 1 -1h1a2 2 0 0 0 0 -4h-1a1 1 0 0 1 -1 -1v-3a1 1 0 0 1 1 -1" />`,
	"terminal": `<path d="M8 9l3 3l-3 3" /><path d="M13 15l3 0" /><path d="M3 4m0 2a2 2 0 0 1 2 -2h14a2 2 0 0 1 2 2v12a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2z" />`,
	"zap":      `<path d="M13 3l0 7l6 0l-8 11l0 -7l-6 0l8 -11" />`,
	"layers":   `<path d="M12 6l-8 4l8 4l8 -4l-8 -4" /><path d="M4 14l8 4l8 -4" />`,
	"box":      `<path d="M12 3l8 4.5l0 9l-8 4.5l-8 -4.5l0 -9l8 -4.5" /><path d="M12 12l8 -4.5" /><path d="M12 12l0 9" /><path d="M12 12l-8 -4.5" />`,
	"users":    `<path d="M9 7m-4 0a4 4 0 1 0 8 0a4 4 0 1 0 -8 0" /><path d="M3 21v-2a4 4 0 0 1 4 -4h4a4 4 0 0 1 4 4v2" /><path d="M16 3.13a4 4 0 0 1 0 7.75" /><path d="M21 21v-2a4 4 0 0 0 -3 -3.85" />`,
	"bell":     `<path d="M10 5a2 2 0 1 1 4 0a7 7 0 0 1 4 6v3a4 4 0 0 0 2 3h-16a4 4 0 0 0 2 -3v-3a7 7 0 0 1 4 -6" /><path d="M9 17v1a3 3 0 0 0 6 0v-1" />`,
	"lock":     `<path d="M5 13a2 2 0 0 1 2 -2h10a2 2 0 0 1 2 2v6a2 2 0 0 1 -2 2h-10a2 2 0 0 1 -2 -2v-6z" /><path d="M11 16a1 1 0 1 0 2 0a1 1 0 0 0 -2 0" /><path d="M8 11v-4a4 4 0 1 1 8 0v4" />`,
	"code":     `<path d="M7 8l-4 4l4 4" /><path d="M17 8l4 4l-4 4" /><path d="M14 4l-4 16" />`,
	"mail":     `<path d="M3 7a2 2 0 0 1 2 -2h14a2 2 0 0 1 2 2v10a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2v-10z" /><path d="M3 7l9 6l9 -6" />`,
	"phone":    `<path d="M5 4h4l2 5l-2.5 1.5a11 11 0 0 0 5 5l1.5 -2.5l5 2v4a2 2 0 0 1 -2 2a16 16 0 0 1 -15 -15a2 2 0 0 1 2 -2" />`,
}

// socialIconPaths 是页脚社交图标：键集合必须与 service.go 的 socialPlatforms 一致（有测试兜底）。
// 路径取自 Tabler Icons v3.36.0 的 outline 系列（MIT，Copyright (c) 2020-2025 Paweł Kuna，https://tabler.io/icons），
// 已去掉每个图标开头的透明底板；品牌标识的商标权仍归各自所有，这里只是几何描摹。
// 刷新时按同名文件覆盖即可，注意 wrapper 的 stroke-width 要保持 Tabler 自己的 2。
var socialIconPaths = map[string]string{
	"github":    `<path d="M9 19c-4.3 1.4 -4.3 -2.5 -6 -3m12 5v-3.5c0 -1 .1 -1.4 -.5 -2c2.8 -.3 5.5 -1.4 5.5 -6a4.6 4.6 0 0 0 -1.3 -3.2a4.2 4.2 0 0 0 -.1 -3.2s-1.1 -.3 -3.5 1.3a12.3 12.3 0 0 0 -6.2 0c-2.4 -1.6 -3.5 -1.3 -3.5 -1.3a4.2 4.2 0 0 0 -.1 3.2a4.6 4.6 0 0 0 -1.3 3.2c0 4.6 2.7 5.7 5.5 6c-.6 .6 -.6 1.2 -.5 2v3.5"/>`,
	"x":         `<path d="M4 4l11.733 16h4.267l-11.733 -16z"/><path d="M4 20l6.768 -6.768m2.46 -2.46l6.772 -6.772"/>`,
	"linkedin":  `<path d="M8 11v5"/><path d="M8 8v.01"/><path d="M12 16v-5"/><path d="M16 16v-3a2 2 0 1 0 -4 0"/><path d="M3 7a4 4 0 0 1 4 -4h10a4 4 0 0 1 4 4v10a4 4 0 0 1 -4 4h-10a4 4 0 0 1 -4 -4z"/>`,
	"youtube":   `<path d="M2 8a4 4 0 0 1 4 -4h12a4 4 0 0 1 4 4v8a4 4 0 0 1 -4 4h-12a4 4 0 0 1 -4 -4v-8z"/><path d="M10 9l5 3l-5 3z"/>`,
	"instagram": `<path d="M4 8a4 4 0 0 1 4 -4h8a4 4 0 0 1 4 4v8a4 4 0 0 1 -4 4h-8a4 4 0 0 1 -4 -4z"/><path d="M9 12a3 3 0 1 0 6 0a3 3 0 0 0 -6 0"/><path d="M16.5 7.5v.01"/>`,
	"facebook":  `<path d="M7 10v4h3v7h4v-7h3l1 -4h-4v-2a1 1 0 0 1 1 -1h3v-4h-3a5 5 0 0 0 -5 5v2h-3"/>`,
	"discord":   `<path d="M8 12a1 1 0 1 0 2 0a1 1 0 0 0 -2 0"/><path d="M14 12a1 1 0 1 0 2 0a1 1 0 0 0 -2 0"/><path d="M15.5 17c0 1 1.5 3 2 3c1.5 0 2.833 -1.667 3.5 -3c.667 -1.667 .5 -5.833 -1.5 -11.5c-1.457 -1.015 -3 -1.34 -4.5 -1.5l-.972 1.923a11.913 11.913 0 0 0 -4.053 0l-.975 -1.923c-1.5 .16 -3.043 .485 -4.5 1.5c-2 5.667 -2.167 9.833 -1.5 11.5c.667 1.333 2 3 3.5 3c.5 0 2 -2 2 -3"/><path d="M7 16.5c3.5 1 6.5 1 10 0"/>`,
	"telegram":  `<path d="M15 10l-4 4l6 6l4 -16l-18 7l4 2l2 6l3 -4"/>`,
	"whatsapp":  `<path d="M3 21l1.65 -3.8a9 9 0 1 1 3.4 2.9l-5.05 .9"/><path d="M9 10a.5 .5 0 0 0 1 0v-1a.5 .5 0 0 0 -1 0v1a5 5 0 0 0 5 5h1a.5 .5 0 0 0 0 -1h-1a.5 .5 0 0 0 0 1"/>`,
	"reddit":    `<path d="M12 8c2.648 0 5.028 .826 6.675 2.14a2.5 2.5 0 0 1 2.326 4.36c0 3.59 -4.03 6.5 -9 6.5c-4.875 0 -8.845 -2.8 -9 -6.294l-1 -.206a2.5 2.5 0 0 1 2.326 -4.36c1.646 -1.313 4.026 -2.14 6.674 -2.14z"/><path d="M12 8l1 -5l6 1"/><path d="M19 4m-1 0a1 1 0 1 0 2 0a1 1 0 1 0 -2 0"/><circle cx="9" cy="13" r=".5" fill="currentColor"/><circle cx="15" cy="13" r=".5" fill="currentColor"/><path d="M10 17c.667 .333 1.333 .5 2 .5s1.333 -.167 2 -.5"/>`,
	"tiktok":    `<path d="M21 7.917v4.034a9.948 9.948 0 0 1 -5 -1.951v4.5a6.5 6.5 0 1 1 -8 -6.326v4.326a2.5 2.5 0 1 0 4 2v-11.5h4.083a6.005 6.005 0 0 0 4.917 4.917z"/>`,
	"twitch":    `<path d="M4 5v11a1 1 0 0 0 1 1h2v4l4 -4h5.584c.266 0 .52 -.105 .707 -.293l2.415 -2.414c.187 -.188 .293 -.442 .293 -.708v-8.585a1 1 0 0 0 -1 -1h-14a1 1 0 0 0 -1 1z"/><path d="M16 8l0 4"/><path d="M12 8l0 4"/>`,
	"mastodon":  `<path d="M18.648 15.254c-1.816 1.763 -6.648 1.626 -6.648 1.626a18.262 18.262 0 0 1 -3.288 -.256c1.127 1.985 4.12 2.81 8.982 2.475c-1.945 2.013 -13.598 5.257 -13.668 -7.636l-.026 -1.154c0 -3.036 .023 -4.115 1.352 -5.633c1.671 -1.91 6.648 -1.666 6.648 -1.666s4.977 -.243 6.648 1.667c1.329 1.518 1.352 2.597 1.352 5.633s-.456 4.074 -1.352 4.944z"/><path d="M12 11.204v-2.926c0 -1.258 -.895 -2.278 -2 -2.278s-2 1.02 -2 2.278v4.722m4 -4.722c0 -1.258 .895 -2.278 2 -2.278s2 1.02 2 2.278v4.722"/>`,
	"wechat":    `<path d="M16.5 10c3.038 0 5.5 2.015 5.5 4.5c0 1.397 -.778 2.645 -2 3.47l0 2.03l-1.964 -1.178a6.649 6.649 0 0 1 -1.536 .178c-3.038 0 -5.5 -2.015 -5.5 -4.5s2.462 -4.5 5.5 -4.5z"/><path d="M11.197 15.698c-.69 .196 -1.43 .302 -2.197 .302a8.008 8.008 0 0 1 -2.612 -.432l-2.388 1.432v-2.801c-1.237 -1.082 -2 -2.564 -2 -4.199c0 -3.314 3.134 -6 7 -6c3.782 0 6.863 2.57 7 5.785l0 .233"/><path d="M10 8h.01"/><path d="M7 8h.01"/><path d="M15 14h.01"/><path d="M18 14h.01"/>`,
	"weibo":     `<path d="M19 14.127c0 3.073 -3.502 5.873 -8 5.873c-4.126 0 -8 -2.224 -8 -5.565c0 -1.78 .984 -3.737 2.7 -5.567c2.362 -2.51 5.193 -3.687 6.551 -2.238c.415 .44 .752 1.39 .749 2.062c2 -1.615 4.308 .387 3.5 2.693c1.26 .557 2.5 .538 2.5 2.742z"/><path d="M15 4h1a5 5 0 0 1 5 5v1"/>`,
	"bilibili":  `<path d="M3 10a4 4 0 0 1 4 -4h10a4 4 0 0 1 4 4v6a4 4 0 0 1 -4 4h-10a4 4 0 0 1 -4 -4v-6z"/><path d="M8 3l2 3"/><path d="M16 3l-2 3"/><path d="M9 13v-2"/><path d="M15 11v2"/>`,
	"rss":       `<path d="M5 19m-1 0a1 1 0 1 0 2 0a1 1 0 1 0 -2 0"/><path d="M4 4a16 16 0 0 1 16 16"/><path d="M4 11a9 9 0 0 1 9 9"/>`,
	"mail":      `<path d="M3 7a2 2 0 0 1 2 -2h14a2 2 0 0 1 2 2v10a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2v-10z"/><path d="M3 7l9 6l9 -6"/>`,
}

// uiIconPaths 是官网自己的界面小图标（导航箭头、主题切换、客服挂件、搜索框）：
// 与 socialIconPaths 同源 Tabler Icons v3.36.0 outline（MIT），统一走 stroke-width 2；
// 模板里写死一份会让笔画口径各走各的，改由 uiIcon 函数出图，键名与模板的引用由测试兜底。
var uiIconPaths = map[string]string{
	"chevron-down": `<path d="M6 9l6 6l6 -6" />`,
	"sun":          `<path d="M12 12m-4 0a4 4 0 1 0 8 0a4 4 0 1 0 -8 0" /><path d="M3 12h1m8 -9v1m8 8h1m-9 8v1m-6.4 -15.4l.7 .7m12.1 -.7l-.7 .7m0 11.4l.7 .7m-12.1 -.7l-.7 .7" />`,
	"moon":         `<path d="M12 3c.132 0 .263 0 .393 0a7.5 7.5 0 0 0 7.92 12.446a9 9 0 1 1 -8.313 -12.454z" />`,
	"message":      `<path d="M3 20l1.3 -3.9c-2.324 -3.437 -1.426 -7.872 2.1 -10.374c3.526 -2.501 8.59 -2.296 11.845 .48c3.255 2.777 3.695 7.266 1.029 10.501c-2.666 3.235 -7.615 4.215 -11.574 2.293l-4.7 1" />`,
	"close":        `<path d="M18 6l-12 12" /><path d="M6 6l12 12" />`,
	"paperclip":    `<path d="M15 7l-6.5 6.5a1.5 1.5 0 0 0 3 3l6.5 -6.5a3 3 0 0 0 -6 -6l-6.5 6.5a4.5 4.5 0 0 0 9 9l6.5 -6.5" />`,
	"send":         `<path d="M10 14l11 -11" /><path d="M21 3l-6.5 18a.55 .55 0 0 1 -1 0l-3.5 -7l-7 -3.5a.55 .55 0 0 1 0 -1l18 -6.5" />`,
	"search":       `<path d="M10 10m-7 0a7 7 0 1 0 14 0a7 7 0 1 0 -14 0" /><path d="M21 21l-6 -6" />`,
}

var defaultIcon = `<circle cx="12" cy="12" r="3"/>`

// featureIconOrder 是管理端图标下拉的展示顺序；键集合必须与 iconPaths 完全一致（有测试兜底），
// 下拉的预览由 /plugins/website/feature-icons 直接读 iconPaths，避免前端另存一份路径后与官网走样。
var featureIconOrder = []string{
	"building", "shield", "key", "plug", "file", "chart", "globe", "puzzle",
	"terminal", "zap", "layers", "box", "users", "bell", "lock", "code", "mail", "phone",
}

var tplFuncs = template.FuncMap{
	// assetV 给静态资源做内容哈希缓存键（见 assetVersion）。
	"assetV": assetVersion,
	// safeHTML 只用于落库前已经过 htmlsafe.Sanitize 的富文本内容。
	"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	"icon": func(name string) template.HTML {
		p, ok := iconPaths[name]
		if !ok {
			p = defaultIcon
		}
		return template.HTML(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + p + `</svg>`)
	},
	"socialIcon": func(platform string) template.HTML {
		p, ok := socialIconPaths[platform]
		if !ok {
			p = socialIconPaths["rss"]
		}
		return template.HTML(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + p + `</svg>`)
	},
	// uiIcon 出界面小图标；第二个参数把 CSS 需要的 class 留在 svg 上（.ico-sun/.ico-moon 靠它切换显隐）。
	"uiIcon": func(name string, class ...string) template.HTML {
		cls := ""
		if len(class) > 0 && class[0] != "" {
			cls = ` class="` + template.HTMLEscapeString(class[0]) + `"`
		}
		p, ok := uiIconPaths[name]
		if !ok {
			p = defaultIcon
		}
		return template.HTML(`<svg` + cls + ` viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + p + `</svg>`)
	},
	"fmtDate":   func(l Language, v any) string { return formatDate(l, v) },
	"hasPrefix": strings.HasPrefix,
	"titleLines": func(s string) []string {
		return strings.Split(strings.TrimSpace(s), "\n")
	},
	"initial": func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return "N"
		}
		r := []rune(s)
		return string(unicode.ToUpper(r[0]))
	},
}

// layout 是所有页面共用的骨架；各页面模板定义自己的 "content"。
var layoutTmpl = template.Must(template.New("layout").Funcs(tplFuncs).ParseFS(tplFS, "tpl/layout.tmpl"))

func mustPage(name string) *template.Template {
	return template.Must(template.Must(layoutTmpl.Clone()).ParseFS(tplFS, "tpl/"+name+".tmpl"))
}

var pageTmpl = map[string]*template.Template{
	"docs":       mustPage("docs"),
	"docs-index": mustPage("docs-index"),
	"home":       mustPage("home"),
	"plugins":    mustPage("plugins"),
	"plugin":     mustPage("plugin"),
	"news":       mustPage("news"),
	"post":       mustPage("post"),
	"page":       mustPage("page"),
	"contact":    mustPage("contact"),
	"notfound":   mustPage("notfound"),
	"error":      mustPage("error"),
}

// siteView 是模板数据。公共字段来自站点设置与导航；Title/Desc 用于 <title> 与 meta。
type siteView struct {
	SiteName string
	SiteDesc string
	LogoURL  string
	// FaviconURL / OgImageURL 是站点设置里的资产：浏览器标签图标与默认分享图（og:image 兜底），
	// 空 = 模板回退内置 favicon.svg / og.png。
	FaviconURL string
	OgImageURL string
	// HeadCode 是站点设置里的自定义 <head> 代码（统计脚本等）：管理员可信输入，经
	// template.HTML 显式标记后原样注入；空 = 不渲染任何内容。
	HeadCode template.HTML
	// TitleSuffix 拼在 <title> 的「{标题}」之后；空 = 「 - {站点名}」。由 composeTitle 消费。
	TitleSuffix   string
	HomeTitle     string
	HomeSubtitle  string
	HomeCTALabel  string
	HomeCTAHref   string
	ContactEmail  string
	ContactPhone  string
	QRCodeURL     string
	QRCodeCaption string
	Socials       []SocialLink
	FriendLinks   []FriendLink
	ICP           string
	FooterNote    string
	SeoKeywords   string
	BaseURL       string // 绝对地址，用于 canonical / og:url / og:image
	Path          string // 当前请求路径（含语言前缀），用于 canonical
	Route         string // 去掉语言前缀的站内路径，用于导航高亮与语言切换
	Query         string // 原始 query，语言切换时保留筛选
	Navs          []Nav
	Features      []Feature
	Title         string
	Desc          string
	// SeoTitle 是内容级的完整 meta 标题覆盖：空 = 用 Title（「标题 - 站点名」的组合交给装配方）。
	SeoTitle string
	OGImage  string // 绝对地址；空 = 用站点默认分享图
	Noindex  bool
	Posts    []Post
	// NewsPage / NewsTotalPages 是公开新闻列表的分页状态（news.tmpl 渲染翻页链接）。
	NewsPage       int
	NewsTotalPages int
	// NewsCategories 是新闻页的栏目导航（含当前高亮那一项）。
	NewsCategories []termLink
	// NewsKind/NewsTermName 说明这一页是栏目还是标签归档：空 = 全部新闻。
	NewsKind     string
	NewsTermName string
	// NewsTermQuery 是当前归档的筛选查询串（category=launch / tag=性能优化）：
	// canonical、og:url 与翻页都要带上它，空 = 全部新闻。
	NewsTermQuery string
	Post          *Post
	Page          *Page
	Year          int
	// 联系页的提交结果：无 JS 的整页提交回跳后由模板渲染提示。
	ContactSent  bool
	ContactError string

	// Consent / ConsentURL：站点开启隐私同意勾选时的勾选项与政策地址。
	Consent    bool
	ConsentURL string

	Lang      Language
	Languages []Language
	// Suggest/SuggestLang：无语言 cookie 的默认语言首页，对浏览器偏好其他语言的访客给出提示。
	Suggest     bool
	SuggestLang Language

	// 插件市场（目录来自 config/plugin-market.yaml，与后台插件市场同源）
	MarketEnabled    bool
	MarketAvailable  bool
	PluginCount      int
	MarketFiltered   int
	MarketQuery      string
	MarketCategory   string
	MarketEdition    string
	MarketCategories []marketChip
	MarketEditions   []marketChip
	Plugins          []marketCard
	Plugin           *marketCard
	Related          []marketCard
	FeaturedPlugins  []marketCard
	MarketError      string
	InstallCommand   string
	AdminInstallURL  string
	MarketSourceURL  string
	DownloadURL      string
	DownloadFilename string
	PluginLD         template.JS
	MyScore          int
	Signed           bool
	SignatureURL     string
	PublicKeyURL     string

	// 文档区（/docs，渲染 nexadmin/docs 内嵌的框架使用文档）
	DocsEnabled bool
	DocNav      []DocGroup
	Doc         *DocView
	DocCards    []DocCard
	DocPrev     *DocLink
	DocNext     *DocLink
}

// T 取静态文案；语言缺键时回退英文/默认语言，最后回退 key 本身。
func (v *siteView) T(key string) string { return tr(v.Lang.Code, key) }

// SocialLabel 是社交图标的读屏/悬停名称。
func (v *siteView) SocialLabel(platform string) string { return v.T("social." + platform) }

// TF 是带参数的文案（字典里用 %s / %d）。
func (v *siteView) TF(key string, args ...any) string { return trf(v.Lang.Code, key, args...) }

// TN 是按数量选单复数的文案：n==1 用 <key>.one，其余用 <key>.other。
// 中文两种写法相同；新增有单复数规则的语言时按该语言字典补齐即可。
func (v *siteView) TN(n any, key string, args ...any) string {
	one := false
	switch x := n.(type) {
	case int:
		one = x == 1
	case int64:
		one = x == 1
	case float64:
		one = x == 1
	}
	l := languageOr(v.Lang.Code)
	k := key + ".other"
	if one {
		k = key + ".one"
	}
	if d := dicts[l.Code]; d != nil {
		if _, ok := d[k]; !ok {
			k = key
		}
	}
	return trf(l.Code, k, args...)
}

// Home 返回当前语言的首页链接。
// 默认语言的首页是无前缀的 "/"，它同时是语言中立入口：服务端会按 cookie 把访客送回
// 上次选择的语言（langNegotiate）。所以站内链接必须显式带 ?lang=——否则访客正在读
// 中文深链、浏览器却存着旧的英文 cookie 时，点「首页」会被送去英文站。
func (v *siteView) Home() string {
	if v.Lang.Default {
		return "/?lang=" + url.QueryEscape(v.Lang.Code)
	}
	return v.Lang.Home()
}

// URL 给站内页面链接加语言前缀；锚点回到当前语言首页，外部链接与下载/后台端点保持原样。
func (v *siteView) URL(href string) string {
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "#") {
		// 已在首页时锚点保持同页跳转，不必绕一次语言入口。
		if v.Route == "/" {
			return href
		}
		return v.Home() + href
	}
	if !strings.HasPrefix(href, "/") || isNonLocalizedPath(href) {
		return href
	}
	// 根路径对默认语言是语言中立入口，必须显式声明语言（见 Home）。
	if p := v.Lang.Path(href); p != "/" {
		return p
	}
	return v.Home()
}

// Alternate 返回同一页面在另一语言下的规范地址（不含 query，供 hreflang / og:locale:alternate）。
func (v *siteView) Alternate(l Language) string {
	route := v.Route
	if route == "" {
		route = "/"
	}
	return l.Path(route)
}

// Canonical 是本页的规范地址：归档筛选必须一起进 canonical，否则 /news?category=x 会被
// 搜索引擎当成 /news 的重复页。整串在 Go 里拼好、模板只放一个动作——
// html/template 对「文本 + 动作」拼接的 URL 会把动作里的 `=` 编码成 %3d，链接就废了。
func (v *siteView) Canonical() string {
	if v.NewsTermQuery == "" {
		return v.BaseURL + v.Path
	}
	return v.BaseURL + v.Path + "?" + v.NewsTermQuery
}

// NewsPrevURL / NewsNextURL 返回公开新闻列表的上一页/下一页链接；没有对应页返回空串。
// 第 1 页就是规范地址 /news，翻页链接不带 ?page=1。
func (v *siteView) NewsPrevURL() string {
	if v.NewsPage <= 1 {
		return ""
	}
	return v.newsURL(v.NewsPage - 1)
}

func (v *siteView) NewsNextURL() string {
	if v.NewsPage < 1 || v.NewsPage >= v.NewsTotalPages {
		return ""
	}
	return v.newsURL(v.NewsPage + 1)
}

// newsURL 拼公开新闻列表的链接：栏目/标签筛选必须跟着翻页一起走，
// 否则在归档页第 2 页会掉回「全部动态」。
func (v *siteView) newsURL(page int) string {
	vals, _ := url.ParseQuery(v.NewsTermQuery)
	if page > 1 {
		vals.Set("page", strconv.Itoa(page))
	}
	base := v.URL("/news")
	if enc := vals.Encode(); enc != "" {
		return base + "?" + enc
	}
	return base
}

// Switch 是语言切换菜单的链接：切到默认语言时带 ?lang= 覆盖已记住的语言 cookie，
// 其余情况保留当前筛选参数。
func (v *siteView) Switch(l Language) string {
	route := v.Route
	if route == "" {
		route = "/"
	}
	vals, _ := url.ParseQuery(v.Query)
	vals.Del("lang")
	if l.Default {
		vals.Set("lang", l.Code)
	}
	if enc := vals.Encode(); enc != "" {
		return l.Path(route) + "?" + enc
	}
	return l.Path(route)
}

// JSStrings 把前端脚本用到的文案以 JSON 交给 site.js（无内联脚本时也能工作）。
func (v *siteView) JSStrings() template.JS {
	m := map[string]string{}
	for _, k := range []string{
		"packaging", "packagingHint", "packFailed", "copied", "copyManual", "menuOpen", "menuClose", "emailCopy", "emailCopied",
		"contactSending", "contactSent", "contactFailed", "contactRate", "contactErrName", "contactErrEmail", "contactErrContent", "contactErrConsent",
		"chatErrSend", "chatErrLoad", "chatClosed", "chatGuest", "chatAgent",
		"chatClosedTitle", "chatClosedAuto", "chatNewNote", "chatRateFailed", "chatUploadFailed", "chatInviteDefault",
	} {
		m[k] = v.T("site.js." + k)
	}
	b, _ := json.Marshal(m)
	return template.JS(b)
}

// ensureLang 给手工构造的 siteView（测试、降级渲染）补齐语言字段。
func (v *siteView) ensureLang() {
	if v.Lang.Code == "" {
		v.Lang = defaultLang
	}
	if len(v.Languages) == 0 {
		v.Languages = Languages()
	}
}
