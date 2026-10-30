package website

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net/url"
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
// 24×24 线性风格（stroke），由本插件自行绘制。
var iconPaths = map[string]string{
	"building": `<rect x="4" y="3" width="16" height="18" rx="1.5"/><path d="M9 21v-4h6v4M9 7h2M13 7h2M9 11h2M13 11h2M9 15h2"/>`,
	"shield":   `<path d="M12 3l7 3v5c0 4.6-3 8.4-7 10-4-1.6-7-5.4-7-10V6z"/><path d="M9.5 12l2 2 3.5-4"/>`,
	"key":      `<circle cx="8" cy="14" r="4"/><path d="M11 11L20 2M17 5l3 3M14 8l2 2"/>`,
	"plug":     `<path d="M9 3v5M15 3v5M7 8h10v4a5 5 0 0 1-10 0zM12 17v4"/>`,
	"file":     `<path d="M13 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V9z"/><path d="M13 3v6h6M9 14h6M9 17h4"/>`,
	"chart":    `<path d="M4 20h16M7 20v-6M12 20V8M17 20v-9"/>`,
	"globe":    `<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3.5 3 14 0 18M12 3c-3 3.5-3 14 0 18"/>`,
	"puzzle":   `<path d="M10 3a2 2 0 0 1 4 0v2h3a1 1 0 0 1 1 1v3h2a2 2 0 0 1 0 4h-2v5a1 1 0 0 1-1 1h-5v-2a2 2 0 0 0-4 0v2H4a1 1 0 0 1-1-1v-5h2a2 2 0 0 0 0-4H3V6a1 1 0 0 1 1-1h6z"/>`,
	"terminal": `<path d="M4 4h16a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1z"/><path d="M7 9l3 3-3 3M12 15h5"/>`,
	"zap":      `<path d="M13 2L4 14h6l-1 8 9-12h-6z"/>`,
	"layers":   `<path d="M12 3l9 5-9 5-9-5zM3 13l9 5 9-5"/>`,
	"box":      `<path d="M12 3l8 4.5v9L12 21l-8-4.5v-9zM12 12l8-4.5M12 12L4 7.5M12 12v9"/>`,
	"users":    `<circle cx="9" cy="8" r="3.5"/><path d="M3 20c.8-3.2 3-5 6-5s5.2 1.8 6 5M16 4.5a3.5 3.5 0 0 1 0 7M18 20c-.4-1.9-1.2-3.4-2.3-4.4"/>`,
	"bell":     `<path d="M6 9a6 6 0 1 1 12 0c0 5 2 6 2 6H4s2-1 2-6M10 19a2 2 0 0 0 4 0"/>`,
	"lock":     `<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>`,
	"code":     `<path d="M8 6l-6 6 6 6M16 6l6 6-6 6M13 4l-2 16"/>`,
	"mail":     `<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3.5 7l8.5 6 8.5-6"/>`,
	"phone":    `<path d="M6.5 3h3l1.5 4-2 1.5a12 12 0 0 0 6.5 6.5l1.5-2 4 1.5v3a2.5 2.5 0 0 1-2.7 2.5A16.5 16.5 0 0 1 4 5.7 2.5 2.5 0 0 1 6.5 3z"/>`,
}

// socialIconPaths 是页脚社交图标：键集合必须与 service.go 的 socialPlatforms 一致（有测试兜底）。
var socialIconPaths = map[string]string{
	"github":   `<path d="M9 19c-4 1.5-4-2.5-6-3m12 6v-3.5c0-1 .1-1.4-.5-2 2.8-.3 5.5-1.4 5.5-6a4.6 4.6 0 0 0-1.3-3.2 4.2 4.2 0 0 0-.1-3.2s-1.1-.3-3.5 1.3a12 12 0 0 0-6.2 0C6.5 3.8 5.4 4.1 5.4 4.1a4.2 4.2 0 0 0-.1 3.2A4.6 4.6 0 0 0 4 10.5c0 4.6 2.7 5.7 5.5 6-.6.6-.6 1.2-.5 2V22"/>`,
	"x":        `<path d="M4 4l16 16M20 4L4 20"/>`,
	"linkedin": `<rect x="3" y="3" width="18" height="18" rx="3"/><path d="M7.5 10.5v6M7.5 7.6v.01M11.5 16.5v-3.4a2 2 0 0 1 4 0v3.4"/>`,
	"youtube":  `<rect x="2.5" y="5.5" width="19" height="13" rx="3.5"/><path d="M10.5 9.5l4.5 2.5-4.5 2.5z"/>`,
	"wechat":   `<path d="M9 7.5c-3.3 0-6 2.1-6 4.7 0 1.5.8 2.8 2.1 3.6L4.6 18l2.4-1.1c.4.1.9.1 1.3.1"/><path d="M15.5 10c3 0 5.5 1.9 5.5 4.3 0 1.3-.7 2.5-1.9 3.3l.4 2-2.2-1.1c-.5.1-1 .2-1.6.2-3 0-5.5-1.9-5.5-4.3S12.5 10 15.5 10z"/>`,
	"weibo":    `<circle cx="10.5" cy="15.5" r="5.5"/><path d="M16.5 8.5a4 4 0 0 1 4 4"/><path d="M18.5 4.5a8 8 0 0 1 4 8"/>`,
	"bilibili": `<rect x="3" y="7" width="18" height="12.5" rx="3"/><path d="M7.5 3l3 4M16.5 3l-3 4M9 11v3M15 11v3"/>`,
	"rss":      `<circle cx="6" cy="18" r="1.5"/><path d="M4 11a9 9 0 0 1 9 9M4 4a16 16 0 0 1 16 16"/>`,
	"mail":     `<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3.5 7l8.5 6 8.5-6"/>`,
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
		return template.HTML(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + p + `</svg>`)
	},
	"socialIcon": func(platform string) template.HTML {
		p, ok := socialIconPaths[platform]
		if !ok {
			p = socialIconPaths["rss"]
		}
		return template.HTML(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + p + `</svg>`)
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
	SiteName     string
	SiteDesc     string
	LogoURL      string
	HomeTitle    string
	HomeSubtitle string
	HomeCTALabel string
	HomeCTAHref  string
	ContactEmail string
	ContactPhone string
	QRCodeURL    string
	QRCodeCaption string
	Socials      []SocialLink
	FriendLinks  []FriendLink
	ICP          string
	FooterNote   string
	SeoKeywords  string
	BaseURL      string // 绝对地址，用于 canonical / og:url / og:image
	Path         string // 当前请求路径（含语言前缀），用于 canonical
	Route        string // 去掉语言前缀的站内路径，用于导航高亮与语言切换
	Query        string // 原始 query，语言切换时保留筛选
	Navs         []Nav
	Features     []Feature
	Title        string
	Desc         string
	OGImage      string // 绝对地址；空 = 用站点默认分享图
	Noindex      bool
	Posts        []Post
	Post         *Post
	Page         *Page
	Year         int
	// 联系页的提交结果：无 JS 的整页提交回跳后由模板渲染提示。
	ContactSent  bool
	ContactError string

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
		"contactSending", "contactSent", "contactFailed", "contactRate", "contactErrName", "contactErrEmail", "contactErrContent",
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
