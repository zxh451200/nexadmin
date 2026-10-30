package website

import (
	"context"
	"crypto/ed25519"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/plugin/market"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 公开官网页（匿名 SSR）。路由注册在根引擎的 RootGroup 上（见 plugin.go），
// 已带插件授权闸门与本插件自己的 IP 限流。

type siteHandler struct {
	svc *Service
	log *zap.Logger
	pkg *packageCache
	// catalog 允许测试注入市场目录；nil 时读 config/plugin-market.yaml。
	catalog func() []market.Entry
	// marketOn 允许测试注入市场开关；nil 时读站点设置。
	marketOn func() (bool, error)
	// signKey 为空表示不签名（未配置 NEXADMIN_MARKET_SIGN_KEY）。
	signKey ed25519.PrivateKey
	// contactLimit 是在线留言的 IP 限流中间件（每种语言分组共用同一实例，计数不分裂）。
	contactLimit gin.HandlerFunc
}

// baseView 装配站点级数据（设置、导航、特性、最新新闻）与页脚年份。
func (h *siteHandler) baseView(ctx context.Context, lang string, postLimit int) (*siteView, error) {
	data, err := h.svc.SiteData(ctx, postLimit, lang)
	if err != nil {
		return nil, err
	}
	set := data.Setting
	v := &siteView{
		SiteName: set.SiteName, SiteDesc: set.SiteDesc, LogoURL: set.LogoURL,
		HomeTitle: set.HomeTitle, HomeSubtitle: set.HomeSubtitle,
		HomeCTALabel: set.HomeCTALabel, HomeCTAHref: set.HomeCTAHref,
		ContactEmail: set.ContactEmail, ContactPhone: set.ContactPhone,
		QRCodeURL: set.QRCodeURL, QRCodeCaption: set.QRCodeCaption,
		Socials: parseSocials(set.SocialsJSON), FriendLinks: parseFriendLinks(set.FriendLinksJSON),
		ICP: set.ICP, FooterNote: set.FooterNote,
		SeoKeywords: set.SeoKeywords, MarketEnabled: set.MarketEnabled,
		Navs: data.Navs, Features: data.Features, Posts: data.Posts,
		Year: time.Now().Year(),
		Lang: languageOr(lang), Languages: Languages(),
		// 文档随二进制内嵌，始终可用；顶层导航与移动菜单都展示入口。
		DocsEnabled: true,
	}
	if v.SiteName == "" {
		v.SiteName = "NexAdmin"
	}
	return v, nil
}

func (h *siteHandler) render(c *gin.Context, name string, status int, v *siteView) {
	t, ok := pageTmpl[name]
	if !ok {
		c.String(http.StatusInternalServerError, "template %q missing", name)
		return
	}
	v.ensureLang()
	// canonical / og:url 需要绝对且带语言前缀的地址；Route 保留无前缀路径用于导航。
	v.BaseURL = baseURL(c)
	v.Route = stripLangPrefix(c.Request.URL.Path)
	v.Path = v.Lang.Path(v.Route)
	v.Query = c.Request.URL.RawQuery
	var b strings.Builder
	if err := t.ExecuteTemplate(&b, "layout", v); err != nil {
		c.String(http.StatusInternalServerError, "render failed")
		return
	}
	c.Data(status, "text/html; charset=utf-8", []byte(b.String()))
}

func (h *siteHandler) notFound(c *gin.Context, v *siteView) {
	if v == nil {
		var err error
		if v, err = h.baseView(c.Request.Context(), langOf(c).Code, 3); err != nil {
			c.String(http.StatusInternalServerError, "render failed")
			return
		}
	}
	v.Title = v.T("notfound.title") + " - " + v.SiteName
	h.render(c, "notfound", http.StatusNotFound, v)
}

// GET / —— 官网首页。
func (h *siteHandler) home(c *gin.Context) {
	lang := langOf(c)
	v, err := h.baseView(c.Request.Context(), lang.Code, 3)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	// 标题用首页主标题（两行合成一行），比整段站点描述更适合 <title> 与搜索结果。
	v.Title = homePageTitle(v.SiteName, v.HomeTitle, v.SiteDesc)
	v.Desc = v.SiteDesc
	if v.MarketEnabled {
		v.FeaturedPlugins = marketCards(featuredEntries(h.marketEntries(), 3), h.marketStats(c.Request.Context()), lang.Code)
	}
	if l, ok := suggestLanguage(c); ok {
		v.Suggest, v.SuggestLang = true, l
		c.Header("Vary", "Accept-Language")
	}
	h.render(c, "home", http.StatusOK, v)
}

// homePageTitle 生成首页 <title>：优先主标题，其次站点描述，最后只有站名。
func homePageTitle(siteName, homeTitle, siteDesc string) string {
	if t := strings.Join(strings.Split(strings.TrimSpace(homeTitle), "\n"), " "); t != "" {
		return siteName + " — " + t
	}
	if siteDesc != "" {
		return siteName + " — " + siteDesc
	}
	return siteName
}

// GET /news —— 新闻列表。
func (h *siteHandler) newsList(c *gin.Context) {
	lang := langOf(c)
	v, err := h.baseView(c.Request.Context(), lang.Code, 20)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.Title = v.T("news.pageTitleTag") + " - " + v.SiteName
	v.Desc = v.TF("news.pageDesc", v.SiteName)
	h.render(c, "news", http.StatusOK, v)
}

// GET /news/:slug —— 新闻详情。
func (h *siteHandler) postDetail(c *gin.Context) {
	lang := langOf(c)
	p, err := h.svc.PublishedPostBy(c.Request.Context(), c.Param("slug"), lang.Code)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			h.notFound(c, nil)
			return
		}
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v, err := h.baseView(c.Request.Context(), lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.Title = p.Title + " - " + v.SiteName
	switch {
	case p.SeoDesc != "":
		v.Desc = p.SeoDesc
	case p.Summary != "":
		v.Desc = p.Summary
	default:
		v.Desc = v.SiteDesc
	}
	v.Post = p
	// og:image 只认绝对 URL；文章设了封面就用封面，否则模板回退站点默认图。
	v.OGImage = absURL(v.BaseURL, p.CoverURL)
	// hreflang、og:locale:alternate 与语言切换器都遍历 v.Languages：未发布的语言不能出现在这里。
	v.Languages = visibleLanguages(p.Published, p.PublishedJSON)
	h.render(c, "post", http.StatusOK, v)
}

// absURL 把站内相对路径补成绝对地址：og:image / twitter:image 只认绝对 URL，
// 相对路径在多数平台上会被忽略。已是 http(s) 或协议相对的地址原样返回。
func absURL(base, u string) string {
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "//") {
		return u
	}
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	return strings.TrimSuffix(base, "/") + u
}

// GET /p/:slug —— 自定义页。
func (h *siteHandler) pageDetail(c *gin.Context) {
	lang := langOf(c)
	p, err := h.svc.PublishedPageBy(c.Request.Context(), c.Param("slug"), lang.Code)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			h.notFound(c, nil)
			return
		}
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v, err := h.baseView(c.Request.Context(), lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.Title = p.Title + " - " + v.SiteName
	if p.SeoDesc != "" {
		v.Desc = p.SeoDesc
	} else {
		v.Desc = v.SiteDesc
	}
	v.Page = p
	v.Languages = visibleLanguages(p.Published, p.PublishedJSON)
	h.render(c, "page", http.StatusOK, v)
}

// GET /contact —— 联系页：联系方式 + 在线留言。
func (h *siteHandler) contact(c *gin.Context) {
	lang := langOf(c)
	v, err := h.baseView(c.Request.Context(), lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.Title = v.T("contact.titleTag") + " - " + v.SiteName
	v.Desc = v.TF("contact.desc", v.SiteName)
	v.ContactSent = c.Query("sent") == "1"
	v.ContactError = contactErrorText(v, c.Query("error"))
	h.render(c, "contact", http.StatusOK, v)
}

// contactErrorText 把无 JS 整页提交回跳的错误码翻成站点文案；未知码给通用失败。
func contactErrorText(v *siteView, code string) string {
	switch code {
	case "":
		return ""
	case "name":
		return v.T("contact.err.name")
	case "email":
		return v.T("contact.err.email")
	case "content":
		return v.T("contact.err.content")
	case "rate":
		return v.T("contact.err.rate")
	}
	return v.T("contact.err.fail")
}

// POST /contact —— 在线留言。fetch 提交回 JSON；无 JS 的整页表单提交 303 回联系页带结果。
func (h *siteHandler) contactSubmit(c *gin.Context) {
	var f ContactForm
	if err := c.ShouldBind(&f); err != nil {
		h.contactFail(c, http.StatusBadRequest, "fail")
		return
	}
	// 蜜罐命中：静默成功，不给机器人任何反馈。
	if strings.TrimSpace(f.Company) != "" {
		h.contactOK(c)
		return
	}
	if _, err := h.svc.CreateMessage(c.Request.Context(), f, c.ClientIP(), c.Request.UserAgent()); err != nil {
		var be *response.Error
		if errors.As(err, &be) {
			h.contactFail(c, be.Status, contactErrorCode(be.Key))
			return
		}
		if h.log != nil {
			h.log.Warn("website: 留言保存失败", zap.Error(err))
		}
		h.contactFail(c, http.StatusInternalServerError, "fail")
		return
	}
	h.contactOK(c)
}

func contactErrorCode(key string) string {
	switch key {
	case "website.contact_name_required":
		return "name"
	case "website.contact_email_invalid":
		return "email"
	case "website.contact_content_short":
		return "content"
	}
	return "fail"
}

func wantsJSON(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "application/json")
}

func (h *siteHandler) contactOK(c *gin.Context) {
	if wantsJSON(c) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	c.Redirect(http.StatusSeeOther, langOf(c).Path("/contact")+"?sent=1")
}

func (h *siteHandler) contactFail(c *gin.Context, status int, code string) {
	if wantsJSON(c) {
		c.JSON(status, gin.H{"ok": false, "error": code})
		return
	}
	c.Redirect(http.StatusSeeOther, langOf(c).Path("/contact")+"?error="+code)
}

var assetTypes = map[string]string{
	".css":   "text/css; charset=utf-8",
	".js":    "application/javascript; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
}

// GET /website-assets/*path —— 服务 go:embed 的静态资源（site.css / site.js 等）；
// media/<id> 段落转到数据库里的上传图片（logo / 封面）。
func (h *siteHandler) asset(c *gin.Context) {
	p := strings.TrimPrefix(c.Param("path"), "/")
	if p == "" || strings.Contains(p, "..") {
		c.String(http.StatusNotFound, "not found")
		return
	}
	if rest, ok := strings.CutPrefix(p, "media/"); ok {
		h.mediaAsset(c, rest)
		return
	}
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		c.String(http.StatusInternalServerError, "asset unavailable")
		return
	}
	b, err := fs.ReadFile(sub, p)
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	ctype := assetTypes[strings.ToLower(path.Ext(p))]
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	// 文件名带内容版本号（site.css?v=0.6.0），可安全长缓存；插件升级会换版本号。
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, ctype, b)
}

// mediaAsset 服务上传的图片：id 是雪花号不可变（重传生成新 id），可以 immutable。
func (h *siteHandler) mediaAsset(c *gin.Context, idStr string) {
	id, err := idgen.Parse(idStr)
	if err != nil || id <= 0 {
		c.String(http.StatusNotFound, "not found")
		return
	}
	m, err := h.svc.MediaByID(c.Request.Context(), id)
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, m.Mime, m.Data)
}

// baseURL 从请求推断站点绝对地址（反代场景尊重 X-Forwarded-Proto）。
func baseURL(c *gin.Context) string {
	scheme := "http"
	if requestHTTPS(c) {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

// notFoundPage 供核心在未注册路径上渲染官网 404（后台只占 /admin）。
func (h *siteHandler) notFoundPage(c *gin.Context) bool {
	m := c.Request.Method
	if m != http.MethodGet && m != http.MethodHead {
		return false
	}
	v, err := h.baseView(c.Request.Context(), languageForPath(c.Request.URL.Path).Code, 0)
	if err != nil {
		return false
	}
	h.notFound(c, v)
	return true
}

// languageForPath 按路径前缀识别语言；未注册的 /en/... 也能渲染英文 404。
func languageForPath(p string) Language {
	for _, l := range languages {
		if l.Prefix != "" && (p == "/"+l.Prefix || strings.HasPrefix(p, "/"+l.Prefix+"/")) {
			return l
		}
	}
	return DefaultLanguage()
}

// GET /robots.txt —— 官网开放收录，后台路径不允许抓取。
func (h *siteHandler) robots(c *gin.Context) {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	b.WriteString("Allow: /\n")
	for _, p := range []string{"/admin", "/api", "/mcp", "/healthz"} {
		b.WriteString("Disallow: " + p + "\n")
	}
	fmt.Fprintf(&b, "Sitemap: %s/sitemap.xml\n", baseURL(c))
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(b.String()))
}

// GET /sitemap.xml —— 首页 + 新闻列表 + 已发布新闻/自定义页；每个 URL 带全语言 hreflang。
func (h *siteHandler) sitemap(c *gin.Context) {
	entries, err := h.svc.SitemapEntries(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "sitemap unavailable")
		return
	}
	set, err := h.svc.Settings(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "sitemap unavailable")
		return
	}
	if set.MarketEnabled {
		entries = append(entries, SitemapEntry{Loc: "/plugins"})
		for _, e := range h.marketEntries() {
			entries = append(entries, SitemapEntry{Loc: "/plugins/" + e.ID})
		}
	}
	// 文档区也进 sitemap（内容由框架 docs/*.md 渲染，随版本更新）。
	entries = append(entries, SitemapEntry{Loc: "/docs"}, SitemapEntry{Loc: "/contact"})
	for _, g := range docGroups {
		for _, it := range g.Items {
			entries = append(entries, SitemapEntry{Loc: "/docs/" + it.Slug})
		}
	}
	base := baseURL(c)
	langs := Languages()
	def := DefaultLanguage()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n")
	for _, e := range entries {
		b.WriteString("  <url>\n    <loc>" + xmlEscape(base+def.Path(e.Loc)) + "</loc>\n")
		// 只为该 URL 真正已发布的语言写 alternate：挂一个会 404 的地址等于自己制造索引错误。
		for _, l := range e.visibleLocales(langs) {
			b.WriteString(`    <xhtml:link rel="alternate" hreflang="` + xmlEscape(l.HTMLLang) + `" href="` +
				xmlEscape(base+l.Path(e.Loc)) + `"/>` + "\n")
		}
		b.WriteString(`    <xhtml:link rel="alternate" hreflang="x-default" href="` + xmlEscape(base+def.Path(e.Loc)) + `"/>` + "\n")
		if e.Lastmod != nil {
			b.WriteString("    <lastmod>" + e.Lastmod.UTC().Format("2006-01-02") + "</lastmod>\n")
		}
		b.WriteString("  </url>\n")
	}
	b.WriteString("</urlset>\n")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", []byte(b.String()))
}

// xmlEscape 转义 XML 文本（路径是 slug 安全字符，host 可能带特殊字符）。
func xmlEscape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}
