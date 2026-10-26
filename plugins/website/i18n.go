package website

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"nexadmin/pkg/htmlsafe"
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
	if err := json.Unmarshal(raw, &languages); err != nil {
		panic("website: locales/languages.json 解析失败: " + err.Error())
	}
	if len(languages) == 0 {
		panic("website: 至少需要一种语言")
	}
	seenCode := map[string]bool{}
	seenPrefix := map[string]bool{}
	for _, l := range languages {
		if l.Code == "" || l.HTMLLang == "" {
			panic("website: 语言缺少 code/htmlLang")
		}
		if seenCode[l.Code] {
			panic("website: 语言 code 重复: " + l.Code)
		}
		seenCode[l.Code] = true
		if l.Prefix != "" {
			if seenPrefix[l.Prefix] || strings.ContainsAny(l.Prefix, "/?#") {
				panic("website: 语言 prefix 非法或重复: " + l.Prefix)
			}
			seenPrefix[l.Prefix] = true
		} else if !l.Default {
			panic("website: 非默认语言必须配置 prefix: " + l.Code)
		}
		if l.Default {
			if defaultLang.Code != "" {
				panic("website: 默认语言只能有一个")
			}
			defaultLang = l
		}
		if l.DateFormat == "" {
			l.DateFormat = "2006-01-02"
		}
		langByCode[l.Code] = l
		langByAlias[strings.ToLower(l.Code)] = l
		if l.Prefix != "" {
			langByAlias[strings.ToLower(l.Prefix)] = l
		}
		langByAlias[strings.ToLower(l.HTMLLang)] = l
		if base, _, ok := strings.Cut(l.Code, "-"); ok {
			if _, exists := langByAlias[strings.ToLower(base)]; !exists {
				langByAlias[strings.ToLower(base)] = l
			}
		}

		dictRaw, err := localeFS.ReadFile("locales/" + l.Code + ".json")
		if err != nil {
			panic("website: 缺少语言字典 locales/" + l.Code + ".json")
		}
		var d map[string]string
		if err := json.Unmarshal(dictRaw, &d); err != nil {
			panic("website: 语言字典解析失败 " + l.Code + ": " + err.Error())
		}
		dicts[l.Code] = d
	}
	if defaultLang.Code == "" {
		panic("website: 必须有一个 default 语言")
	}
	localeLoaded = true
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
			vals := c.Request.URL.Query()
			vals.Del("lang")
			target := l.Path(stripLangPrefix(c.Request.URL.Path))
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
	for _, pre := range []string{"/website-assets/", "/api/"} {
		if strings.HasPrefix(path, pre) {
			return true
		}
	}
	switch path {
	case "/login", "/healthz", "/sitemap.xml", "/robots.txt", "/plugins/index.json", "/plugins/signing-key.pub":
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

// translationFor 取某语言的字段表；为空或损坏时返回 nil（调用方保留原文）。
func translationFor(raw, code string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var all map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &all); err != nil {
		return nil
	}
	if tr, ok := all[code]; ok {
		return tr
	}
	if base, _, found := strings.Cut(code, "-"); found {
		if tr, ok := all[base]; ok {
			return tr
		}
	}
	return nil
}

func pick(tr map[string]string, field, base string) string {
	if v, ok := tr[field]; ok && v != "" {
		return v
	}
	return base
}

func (set *Setting) localize(code string) {
	tr := translationFor(set.I18nJSON, code)
	set.SiteDesc = pick(tr, "siteDesc", set.SiteDesc)
	set.HomeTitle = pick(tr, "homeTitle", set.HomeTitle)
	set.HomeSubtitle = pick(tr, "homeSubtitle", set.HomeSubtitle)
	set.HomeCTALabel = pick(tr, "homeCtaLabel", set.HomeCTALabel)
	set.SeoKeywords = pick(tr, "seoKeywords", set.SeoKeywords)
	set.FooterNote = pick(tr, "footerNote", set.FooterNote)
}

func (n *Nav) localize(code string) {
	n.Label = pick(translationFor(n.I18nJSON, code), "label", n.Label)
}

func (f *Feature) localize(code string) {
	tr := translationFor(f.I18nJSON, code)
	f.Title = pick(tr, "title", f.Title)
	f.Description = pick(tr, "description", f.Description)
}

func (p *Post) localize(code string) {
	tr := translationFor(p.I18nJSON, code)
	p.Title = pick(tr, "title", p.Title)
	p.Summary = pick(tr, "summary", p.Summary)
	p.Content = pick(tr, "content", p.Content)
}

func (p *Page) localize(code string) {
	tr := translationFor(p.I18nJSON, code)
	p.Title = pick(tr, "title", p.Title)
	p.SeoDesc = pick(tr, "seoDesc", p.SeoDesc)
	p.Content = pick(tr, "content", p.Content)
}

// i18nField 是某种内容允许翻译的字段：Max=0 表示不限长度，Sanitize 表示富文本。
type i18nField struct {
	Max      int
	Sanitize bool
}

var (
	settingI18nFields = map[string]i18nField{
		"siteDesc": {Max: 300}, "homeTitle": {Max: 200}, "homeSubtitle": {Max: 500},
		"homeCtaLabel": {Max: 64}, "seoKeywords": {Max: 300}, "footerNote": {Max: 300},
	}
	navI18nFields     = map[string]i18nField{"label": {Max: 64}}
	featureI18nFields = map[string]i18nField{
		"title": {Max: 128}, "description": {Max: 500},
	}
	pageI18nFields = map[string]i18nField{
		"title": {Max: 200}, "seoDesc": {Max: 300}, "content": {Sanitize: true},
	}
	postI18nFields = map[string]i18nField{
		"title": {Max: 200}, "summary": {Max: 500}, "content": {Sanitize: true},
	}
)

// normalizeI18nJSON 校验并收窄翻译 JSON：只保留已配置语言与白名单字段，
// 统一裁剪长度、清洗富文本；未知语言/字段直接丢弃，避免脏数据进库。
func normalizeI18nJSON(raw string, fields map[string]i18nField) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 512<<10 {
		return "", fmt.Errorf("翻译内容过大")
	}
	var in map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("翻译 JSON 格式不正确")
	}
	out := map[string]map[string]string{}
	for code, vals := range in {
		l, ok := LanguageByCode(code)
		if !ok || l.Default {
			// 默认语言的正文就是原文，不必重复存译文。
			continue
		}
		dst := map[string]string{}
		for key, spec := range fields {
			v, ok := vals[key]
			if !ok {
				continue
			}
			if spec.Sanitize {
				v = htmlsafe.Sanitize(v)
			} else {
				v = strings.TrimSpace(v)
			}
			if spec.Max > 0 && utf8.RuneCountInString(v) > spec.Max {
				v = string([]rune(v)[:spec.Max])
			}
			if v != "" {
				dst[key] = v
			}
		}
		if len(dst) > 0 {
			out[l.Code] = dst
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// i18nJSON 只用于 seed/backfill 的编译期常量，序列化不会失败。
func i18nJSON(fields map[string]map[string]string) string {
	if len(fields) == 0 {
		return ""
	}
	b, _ := json.Marshal(fields)
	return string(b)
}
