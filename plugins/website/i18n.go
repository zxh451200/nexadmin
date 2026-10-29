package website

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	pi18n "nexadmin/pkg/i18n"
)

// 官网多语言：语言元数据 + 静态文案字典 + 数据库内容翻译都在这里。
// 新增语言只需两步：在 locales/languages.json 加一条、放一份 locales/<code>.json；
// 公开页面会自动获得 /<prefix> 路由、语言切换、hreflang 与 sitemap 条目。

//go:embed locales/*
var localeFS embed.FS

// Language 是一种官网语言。Prefix 为空的语言走根路径（默认语言）。
type Language struct {
	Code       string `json:"code"`
	Label      string `json:"label"`
	Short      string `json:"short"`
	HTMLLang   string `json:"htmlLang"`
	OGLocale   string `json:"ogLocale"`
	Prefix     string `json:"prefix"`
	DateFormat string `json:"dateFormat"`
	Default    bool   `json:"default"`
}

var (
	languages     []Language
	defaultLang   Language
	dicts         = map[string]map[string]string{}
	langByCode    = map[string]Language{}
	langByAlias   = map[string]Language{}
	localeLoaded  bool
	displayLocale = "en-US" // 兜底语言（字典缺失时）
)

func init() { mustLoadLocales() }

func mustLoadLocales() {
	raw, err := localeFS.ReadFile("locales/languages.json")
	if err != nil {
		panic("website: 缺少 locales/languages.json: " + err.Error())
	}
	var fileLangs []Language
	if err := json.Unmarshal(raw, &fileLangs); err != nil {
		panic("website: locales/languages.json 解析失败: " + err.Error())
	}
	if err := applyLanguages(fileLangs); err != nil {
		panic("website: " + err.Error())
	}
}

// applyLanguages 用给定清单重建全局语言表与字典。
// 清单已由 core 校验并补全（见 core.NormalizeLanguages / sys_language），
// 官网不再重复实现一套规则——两份校验规则正是过去两边清单能各自跑偏的原因。
// 缺 locales/<code>.json 的语言回退默认语言字典：界面文案显示默认语言，内容翻译仍可单独录入。
func applyLanguages(list []Language) error {
	var def Language
	for _, l := range list {
		if l.Default {
			def = l
		}
	}
	if def.Code == "" {
		return fmt.Errorf("语言清单里没有默认语言")
	}
	defaultDict, err := readDict(def.Code)
	if err != nil {
		return fmt.Errorf("默认语言字典缺失: %v", err)
	}

	nextDicts := make(map[string]map[string]string, len(list))
	nextByCode := make(map[string]Language, len(list))
	nextByAlias := make(map[string]Language, len(list))
	for _, l := range list {
		d, err := readDict(l.Code)
		if err != nil {
			d = defaultDict
		}
		nextDicts[l.Code] = d
		nextByCode[l.Code] = l
		nextByAlias[strings.ToLower(l.Code)] = l
		if l.Prefix != "" {
			nextByAlias[strings.ToLower(l.Prefix)] = l
		}
		nextByAlias[strings.ToLower(l.HTMLLang)] = l
		if base, _, ok := strings.Cut(l.Code, "-"); ok {
			if _, exists := nextByAlias[strings.ToLower(base)]; !exists {
				nextByAlias[strings.ToLower(base)] = l
			}
		}
	}

	languages = list
	defaultLang = def
	dicts = nextDicts
	langByCode = nextByCode
	langByAlias = nextByAlias
	localeLoaded = true
	return nil
}

func readDict(code string) (map[string]string, error) {
	raw, err := localeFS.ReadFile("locales/" + code + ".json")
	if err != nil {
		return nil, err
	}
	var d map[string]string
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return d, nil
}

// HasDict 报告内置字典是否存在（后台据���提示「界面文案将回退默认语言」）。
func HasDict(code string) bool {
	_, err := localeFS.ReadFile("locales/" + code + ".json")
	return err == nil
}

// Languages 返回全部语言（副本，调用方可安全排序/修改）。
func Languages() []Language {
	out := make([]Language, len(languages))
	copy(out, languages)
	return out
}

// DefaultLanguage 返回默认语言（根路径）。
func DefaultLanguage() Language { return defaultLang }

// LanguageByCode 精确或大小写不敏感匹配语言 code。
func LanguageByCode(code string) (Language, bool) {
	if l, ok := langByCode[code]; ok {
		return l, true
	}
	key := strings.ToLower(strings.TrimSpace(code))
	if l, ok := langByAlias[key]; ok {
		return l, true
	}
	// en-GB 这类未配置的变体退回语言基码（en → en-US）。
	if base, _, found := strings.Cut(key, "-"); found {
		l, ok := langByAlias[base]
		return l, ok
	}
	return Language{}, false
}

func languageOr(code string) Language {
	if l, ok := LanguageByCode(code); ok {
		return l
	}
	return defaultLang
}

func tr(code, key string) string {
	if code == "" {
		code = defaultLang.Code
	}
	if localeLoaded {
		if d := dicts[code]; d != nil {
			if v, ok := d[key]; ok {
				return v
			}
		}
		// 语言变体缺键时退回语言基码（en-GB → en-US 之类由 alias 处理）。
		if l, ok := LanguageByCode(code); ok {
			if base, _, found := strings.Cut(l.Code, "-"); found {
				if d := dicts[base]; d != nil {
					if v, ok := d[key]; ok {
						return v
					}
				}
			}
		}
		if d := dicts[displayLocale]; d != nil {
			if v, ok := d[key]; ok {
				return v
			}
		}
		if d := dicts[defaultLang.Code]; d != nil {
			if v, ok := d[key]; ok {
				return v
			}
		}
	}
	return key
}

func trf(code, key string, args ...any) string {
	return fmt.Sprintf(tr(code, key), args...)
}

// ---- 请求语言 ----

const langCookie = "nex_site_lang"

// langContextKey 存当前请求语言 code；命中 /en 分组或协商出的语言。
const langContextKey = "website_lang"

// LanguageMiddleware 给带语言前缀的路由组设置语言，并记住访客选择。
func LanguageMiddleware(l Language) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(langContextKey, l.Code)
		// 覆盖框架按 Accept-Language 协商出的 Content-Language：官网语言由 URL 决定。
		c.Header("Content-Language", l.HTMLLang)
		setLangCookie(c, l.Code)
		c.Next()
	}
}

// LanguageContext 只固定当前请求的语言，不改写 cookie（默认语言分组用）。
func LanguageContext(l Language) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(langContextKey, l.Code)
		c.Header("Content-Language", l.HTMLLang)
		c.Next()
	}
}

// langOf 取路由组固定的语言；未固定时用默认语言。
// 注意：不能把 cookie 当成页面语言——/news 这类无前缀 URL 必须稳定渲染默认语言，
// 否则同一 URL 会随 cookie 返回不同语言，canonical 也会漂移。
func langOf(c *gin.Context) Language {
	if v, ok := c.Get(langContextKey); ok {
		if code, ok := v.(string); ok {
			return languageOr(code)
		}
	}
	return defaultLang
}

// preferredLang 用于语言中立的端点（下载失败页、评分回跳）：优先访客记住的语言。
func preferredLang(c *gin.Context) Language {
	if ck, err := c.Cookie(langCookie); err == nil {
		if l, ok := LanguageByCode(ck); ok {
			return l
		}
	}
	return langOf(c)
}

func setLangCookie(c *gin.Context, code string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: langCookie, Value: code, Path: "/",
		MaxAge: 365 * 24 * 3600, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: requestHTTPS(c),
	})
}

func requestHTTPS(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

// langNegotiate 处理 ?lang= 切换与“记住的语言”回跳；只挂在默认（无前缀）分组上。
func langNegotiate(c *gin.Context) {
	if q := strings.TrimSpace(c.Query("lang")); q != "" {
		if l, ok := LanguageByCode(q); ok {
			setLangCookie(c, l.Code)
			route := stripLangPrefix(c.Request.URL.Path)
			// 默认语言的无前缀路径就地渲染，不经 302 回跳：回跳要把语言交给 cookie，
			// 而 cookie 写不进去或被并发覆盖时，下一条 "/" 会按旧 cookie 把访客送去
			// /en——点了中文「首页」却看到英文正是这条路。URL 已点明语言就别再问 cookie。
			if l.Default && route == c.Request.URL.Path {
				c.Next()
				return
			}
			vals := c.Request.URL.Query()
			vals.Del("lang")
			target := l.Path(route)
			if enc := vals.Encode(); enc != "" {
				target += "?" + enc
			}
			c.Redirect(http.StatusFound, target)
			c.Abort()
			return
		}
	}
	// 只对首页做 cookie 回跳：深链保持访客点进来的那个 URL。
	if c.Request.URL.Path == "/" {
		if ck, err := c.Cookie(langCookie); err == nil {
			if l, ok := LanguageByCode(ck); ok && !l.Default {
				c.Redirect(http.StatusFound, l.Home())
				c.Abort()
				return
			}
		}
	}
	c.Next()
}

// negotiateAcceptLanguage 按 q 值挑出浏览器最想要的语言（包含默认语言）。
// 返回的 ok=true 但 Default=true 时，表示访客本来就偏好默认语言，不该提示切换。
func negotiateAcceptLanguage(header string) (Language, bool) {
	best := Language{}
	bestQ := -1.0
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		tag := strings.ToLower(strings.TrimSpace(fields[0]))
		if tag == "" || tag == "*" {
			continue
		}
		q := 1.0
		for _, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if v, ok := strings.CutPrefix(f, "q="); ok {
				if parsed, err := strconv.ParseFloat(v, 64); err == nil {
					q = parsed
				}
			}
		}
		if q <= 0 {
			continue
		}
		l, ok := LanguageByCode(tag)
		if !ok {
			continue
		}
		// 同 q 时保留靠前的条目（浏览器把更偏好的语言写在前面）。
		if q > bestQ {
			best, bestQ = l, q
		}
	}
	return best, best.Code != ""
}

// suggestLanguage 判断默认语言首页是否该给访客一个切换提示：
// 已显式选过语言（有 cookie）或浏览器本来就偏好默认语言时不提示。
func suggestLanguage(c *gin.Context) (Language, bool) {
	if c.Request.URL.Path != "/" {
		return Language{}, false
	}
	if _, err := c.Cookie(langCookie); err == nil {
		return Language{}, false
	}
	l, ok := negotiateAcceptLanguage(c.GetHeader("Accept-Language"))
	if !ok || l.Default || l.Code == defaultLang.Code {
		return Language{}, false
	}
	return l, true
}

// stripLangPrefix 去掉路径里的语言前缀（/en/plugins → /plugins，/en → /）。
func stripLangPrefix(p string) string {
	for _, l := range languages {
		if l.Prefix == "" {
			continue
		}
		if p == "/"+l.Prefix {
			return "/"
		}
		if strings.HasPrefix(p, "/"+l.Prefix+"/") {
			return p[len(l.Prefix)+1:]
		}
	}
	return p
}

// Home 返回该语言首页路径。
func (l Language) Home() string {
	if l.Prefix == "" {
		return "/"
	}
	return "/" + l.Prefix
}

// Path 给站内绝对路径加语言前缀（保留 query；"" / "/" 归一到首页）。
func (l Language) Path(p string) string {
	if p == "" {
		p = "/"
	}
	if !strings.HasPrefix(p, "/") {
		return p
	}
	path, query, hasQuery := strings.Cut(p, "?")
	if path == "" {
		path = "/"
	}
	if l.Prefix != "" {
		if path == "/" {
			path = "/" + l.Prefix
		} else {
			path = "/" + l.Prefix + path
		}
	}
	if hasQuery {
		return path + "?" + query
	}
	return path
}

// nonLocalizedPaths 是不做语言前缀的站内路径：静态资源、后台 SPA、协议端点与包下载。
func isNonLocalizedPath(p string) bool {
	path, _, _ := strings.Cut(p, "?")
	for _, pre := range []string{"/website-assets/", "/api/", "/admin"} {
		if strings.HasPrefix(path, pre) {
			return true
		}
	}
	switch path {
	case "/healthz", "/sitemap.xml", "/robots.txt", "/plugins/index.json", "/plugins/signing-key.pub":
		return true
	}
	if strings.HasPrefix(path, "/plugins/") {
		for _, suffix := range []string{"/download", "/download.sha256", "/download.sig"} {
			if strings.HasSuffix(path, suffix) {
				return true
			}
		}
	}
	return false
}

// ---- 日期 ----

func formatDate(l Language, v any) string {
	var t time.Time
	switch x := v.(type) {
	case time.Time:
		t = x
	case *time.Time:
		if x == nil {
			return ""
		}
		t = *x
	default:
		return ""
	}
	layout := l.DateFormat
	if layout == "" {
		layout = "2006-01-02"
	}
	return t.Local().Format(layout)
}

// ---- 数据库内容翻译 ----
//
// 译文文档、字段白名单、完成度与按语言发布的算法都在 pkg/i18n；
// 这里只剩官网自己的两件事：把本站语言表适配成 pkg 要的 Locales，
// 以及每类内容「哪些字段可翻译」的清单。

// siteLocales 把官网运行期语言表适配成 pkg/i18n 的 Locales 视图。
type siteLocales struct{}

func (siteLocales) All() []pi18n.Language {
	all := Languages()
	out := make([]pi18n.Language, 0, len(all))
	for _, l := range all {
		out = append(out, pi18n.Language{Code: l.Code, Default: l.Default})
	}
	return out
}

func (siteLocales) Resolve(code string) (pi18n.Language, bool) {
	l, ok := LanguageByCode(code)
	if !ok {
		return pi18n.Language{}, false
	}
	return pi18n.Language{Code: l.Code, Default: l.Default}, true
}

// translations 取某语言的字段译文（含语基回退）；nil 表示整篇回退原文。
func translations(raw, code string) map[string]string {
	return pi18n.ParseDoc(raw).For(code)
}

func (set *Setting) localize(code string) {
	tr := translations(set.I18nJSON, code)
	set.SiteDesc = pi18n.Pick(tr, "siteDesc", set.SiteDesc)
	set.HomeTitle = pi18n.Pick(tr, "homeTitle", set.HomeTitle)
	set.HomeSubtitle = pi18n.Pick(tr, "homeSubtitle", set.HomeSubtitle)
	set.HomeCTALabel = pi18n.Pick(tr, "homeCtaLabel", set.HomeCTALabel)
	set.SeoKeywords = pi18n.Pick(tr, "seoKeywords", set.SeoKeywords)
	set.FooterNote = pi18n.Pick(tr, "footerNote", set.FooterNote)
}

func (n *Nav) localize(code string) {
	n.Label = pi18n.Pick(translations(n.I18nJSON, code), "label", n.Label)
}

func (f *Feature) localize(code string) {
	tr := translations(f.I18nJSON, code)
	f.Title = pi18n.Pick(tr, "title", f.Title)
	f.Description = pi18n.Pick(tr, "description", f.Description)
}

func (p *Post) localize(code string) {
	tr := translations(p.I18nJSON, code)
	p.Title = pi18n.Pick(tr, "title", p.Title)
	p.Summary = pi18n.Pick(tr, "summary", p.Summary)
	p.SeoDesc = pi18n.Pick(tr, "seoDesc", p.SeoDesc)
	p.Content = pi18n.Pick(tr, "content", p.Content)
}

func (p *Page) localize(code string) {
	tr := translations(p.I18nJSON, code)
	p.Title = pi18n.Pick(tr, "title", p.Title)
	p.SeoDesc = pi18n.Pick(tr, "seoDesc", p.SeoDesc)
	p.Content = pi18n.Pick(tr, "content", p.Content)
}

// 各内容的可翻译字段白名单：Max=0 表示不限长度，Rich 表示按富文本清洗后入库。
// 这是官网的内容模型，所以留在插件里；算法在 pkg/i18n。
var (
	settingI18nFields = pi18n.Fields{
		"siteDesc": {Max: 300}, "homeTitle": {Max: 200}, "homeSubtitle": {Max: 500},
		"homeCtaLabel": {Max: 64}, "seoKeywords": {Max: 300}, "footerNote": {Max: 300},
	}
	navI18nFields     = pi18n.Fields{"label": {Max: 64}}
	featureI18nFields = pi18n.Fields{
		"title": {Max: 128}, "description": {Max: 500},
	}
	pageI18nFields = pi18n.Fields{
		"title": {Max: 200}, "seoDesc": {Max: 300}, "content": {Rich: true},
	}
	postI18nFields = pi18n.Fields{
		"title": {Max: 200}, "summary": {Max: 500}, "seoDesc": {Max: 300}, "content": {Rich: true},
	}
)

// i18nJSON 只用于 seed/backfill 的编译期常量，序列化不会失败。
func i18nJSON(fields map[string]map[string]string) string {
	if len(fields) == 0 {
		return ""
	}
	b, _ := json.Marshal(fields)
	return string(b)
}

// ---- 按语言发布 ----
//
// 判定算法在 pkg/i18n（Translatable / NormalizePublish / Published），官网只提供语言表与字段白名单。
// 闸门必须在服务端：只在前端禁用开关的话，直接写接口就能把「伪装成日语的中文页」放回索引。

// visibleLanguages 返回这条内容已发布的语言，顺序沿用配置顺序。
// 详情页的 hreflang、og:locale:alternate 和语言切换器都遍历它：未发布的语言不能出现在
// 这些位置，否则等于告诉搜索引擎「这个中文页还有个日语版」，而那个地址其实 404。
func visibleLanguages(published bool, publishedJSON string) []Language {
	all := Languages()
	out := make([]Language, 0, len(all))
	for _, l := range all {
		if pi18n.Published(published, publishedJSON, l.Code, siteLocales{}) {
			out = append(out, l)
		}
	}
	return out
}
