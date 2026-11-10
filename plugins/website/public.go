package website

import (
	"context"
	"crypto/ed25519"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/middleware"
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
	// previewKey 是草稿预览令牌的签名密钥（复用框架 JWT 密钥）；nil = 预览关闭。
	previewKey []byte
	// contactHits 是在线留言的 IP 投稿计数（共享 cache，多实例一致）。
	contactHits *middleware.Limiter
	// contactLocal 是 cache 未装配时的进程内兜底计数器。
	contactLocal *ipLimiter
}

// contactAllowed 报告这个 IP 还能不能投稿；超限已经写好 Retry-After。
func (h *siteHandler) contactAllowed(c *gin.Context) bool {
	if h.contactHits != nil {
		ok, retry := h.contactHits.Hit(c.Request.Context(), "ip:"+c.ClientIP())
		if ok {
			return true
		}
		secs := int(retry.Seconds())
		if secs < 1 {
			secs = 1
		}
		c.Header("Retry-After", strconv.Itoa(secs))
		return false
	}
	if h.contactLocal != nil {
		return h.contactLocal.allow(c.ClientIP())
	}
	return true
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
		FaviconURL: set.FaviconURL, OgImageURL: set.OgImageURL,
		HeadCode:    template.HTML(set.HeadCode),
		TitleSuffix: set.TitleSuffix, HomeSlug: set.HomeSlug,
		HomeTitle:   set.HomeTitle, HomeSubtitle: set.HomeSubtitle,
		HomeCTALabel: set.HomeCTALabel, HomeCTAHref: set.HomeCTAHref,
		ContactEmail: set.ContactEmail, ContactPhone: set.ContactPhone,
		QRCodeURL: set.QRCodeURL, QRCodeCaption: set.QRCodeCaption,
		Socials: parseSocials(set.SocialsJSON), FriendLinks: parseFriendLinks(set.FriendLinksJSON),
		ICP: set.ICP, FooterNote: set.FooterNote,
		SeoKeywords: set.SeoKeywords, MarketEnabled: set.MarketEnabled,
		Consent: set.ContactConsent, ConsentURL: set.ContactConsentURL,
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

// composeTitle 组合 <title> 的「标题」段：`{标题} - {后缀}`。后缀取站点设置的
// title_suffix，空回退站点名——「关于我们 - NexAdmin」与「关于我们 - 某品牌」只差一个配置。
func (v *siteView) composeTitle(pageTitle string) string {
	suffix := strings.TrimSpace(v.TitleSuffix)
	if suffix == "" {
		suffix = v.SiteName
	}
	return pageTitle + " - " + suffix
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
	// 站点默认分享图兜底：内容级 OGImage 为空时用站点设置（absURL 补协议），仍空由模板回退内置 og.png。
	if v.OGImage == "" && v.OgImageURL != "" {
		v.OGImage = absURL(v.BaseURL, v.OgImageURL)
	}
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
	v.Title = v.composeTitle(v.T("notfound.title"))
	h.render(c, "notfound", http.StatusNotFound, v)
}

// GET / —— 官网首页。站点设置把某张单页设为首页时渲染该页内容（替换内置 Hero 页），
// 与 /p/:slug 同一套发布判定；页面被删或（在当前语言）下线时静默回退内置首页——
// 首页永远可访问，比 404 稳，也省得管理员每次改版还要回头动首页设置。
func (h *siteHandler) home(c *gin.Context) {
	lang := langOf(c)
	v, err := h.baseView(c.Request.Context(), lang.Code, 3)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	if slug := strings.TrimSpace(v.HomeSlug); slug != "" {
		if p, perr := h.svc.PublishedPageBy(c.Request.Context(), slug, lang.Code); perr == nil {
			v.Page = p
			v.Title = v.composeTitle(p.Title)
			if p.SeoTitle != "" {
				v.SeoTitle = v.composeTitle(p.SeoTitle)
			}
			if p.SeoDesc != "" {
				v.Desc = p.SeoDesc
			} else {
				v.Desc = v.SiteDesc
			}
			v.OGImage = absURL(v.BaseURL, p.OgImage)
			v.Noindex = p.Noindex
			// 语言切换器保持站点全局清单：切到该页未发布的语言时回退内置首页，而不是没有入口。
			h.render(c, "page", http.StatusOK, v)
			return
		}
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

// newsPageSize 是公开新闻列表每页条数。
const newsPageSize = 20

// GET /news —— 新闻列表：?category=栏目 / ?tag=标签 收窄，?page=N 翻页。
// 归档与全部共用这一条路由：查询式归档不占 /news/:slug 的下一段，
// 不会和文章的 slug 抢路由（Gin 在同一层同时挂静态段和通配段会直接 panic）。
func (h *siteHandler) newsList(c *gin.Context) {
	ctx := c.Request.Context()
	lang := langOf(c)
	v, err := h.baseView(ctx, lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	term := termFilter{}
	// 栏目/标签不存在就是 404，而不是「这个筛选下暂时没内容」：前者是链接写错了，
	// 后者是编辑部还没发，两者给访客的信息完全不同。
	if slug := strings.TrimSpace(c.Query("category")); slug != "" {
		row, cerr := h.svc.CategoryBySlug(ctx, slug)
		if cerr != nil {
			h.notFound(c, nil)
			return
		}
		term = termFilter{Kind: "category", Slug: row.Slug}
		v.NewsKind, v.NewsTermName = "category", row.Title
		v.NewsTermQuery = url.Values{"category": {row.Slug}}.Encode()
		v.Title = v.composeTitle(v.TF("news.categoryTitleTag", row.Title))
	} else if slug := strings.TrimSpace(c.Query("tag")); slug != "" {
		row, terr := h.svc.TagBySlug(ctx, slug)
		if terr != nil {
			h.notFound(c, nil)
			return
		}
		term = termFilter{Kind: "tag", Slug: row.Slug}
		v.NewsKind, v.NewsTermName = "tag", row.Name
		v.NewsTermQuery = url.Values{"tag": {row.Slug}}.Encode()
		v.Title = v.composeTitle(v.TF("news.tagTitleTag", row.Name))
	}
	posts, total, err := h.svc.PublishedPostsPage(ctx, lang.Code, term, page, newsPageSize)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	cats, err := h.svc.ListCategories(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	active := ""
	if term.Kind == "category" {
		active = term.Slug
	}
	v.NewsCategories = CategoryLinks(cats, active)
	v.Posts = posts
	v.NewsPage, v.NewsTotalPages = pageOf(page), pagesOf(total, newsPageSize)
	// 第 1 页才是规范地址：?page=2 不进 canonical，避免搜索引擎把每页当独立页面收录。
	v.Noindex = v.NewsPage > 1
	if v.NewsTermName == "" {
		v.Title = v.composeTitle(v.T("news.pageTitleTag"))
	}
	v.Desc = v.TF("news.pageDesc", v.SiteName)
	h.render(c, "news", http.StatusOK, v)
}

func pageOf(p int) int {
	if p < 1 {
		return 1
	}
	return p
}

func pagesOf(total int64, size int) int {
	if total <= 0 {
		return 1
	}
	n := int((total + int64(size) - 1) / int64(size))
	if n < 1 {
		return 1
	}
	return n
}

// GET /news/:slug —— 新闻详情。
func (h *siteHandler) postDetail(c *gin.Context) {
	lang := langOf(c)
	slug := c.Param("slug")
	p, err := h.svc.PublishedPostBy(c.Request.Context(), slug, lang.Code)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			c.String(http.StatusInternalServerError, "site unavailable")
			return
		}
		// 未发布（不存在、整篇草稿或定时未到点）都按 404 走；唯一例外是
		// 管理端签发的预览链接：令牌有效就照常渲染，发布前能看到真实版式。
		draft, derr := h.svc.PostBySlug(c.Request.Context(), slug, lang.Code)
		if derr != nil || !verifyPostPreview(h.previewKey, slug, c.Query("preview"), time.Now()) {
			h.notFound(c, nil)
			return
		}
		p = draft
	}
	v, err := h.baseView(c.Request.Context(), lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.Title = v.composeTitle(p.Title)
	// SEO 标题是完整标题的覆盖：填了就替换「文章标题」，站点名后缀保持一致的组合规律。
	if p.SeoTitle != "" {
		v.SeoTitle = v.composeTitle(p.SeoTitle)
	}
	switch {
	case p.SeoDesc != "":
		v.Desc = p.SeoDesc
	case p.Summary != "":
		v.Desc = p.Summary
	default:
		v.Desc = v.SiteDesc
	}
	// 新闻级关键词优先：留空才回退站点设置，与 meta description 的取舍一致。
	if p.SeoKeywords != "" {
		v.SeoKeywords = p.SeoKeywords
	}
	v.Post = p
	// og:image 只认绝对 URL；文章设了封面就用封面，否则模板回退站点默认图。
	v.OGImage = absURL(v.BaseURL, p.CoverURL)
	v.Noindex = p.Noindex
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

// GET /products —— 产品列表：?category=类目 收窄，?page=N 翻页。
// 与新闻列表同构：查询式归档不占 /products/:slug 的下一段，不会和商品 slug 抢路由。
func (h *siteHandler) productsList(c *gin.Context) {
	ctx := c.Request.Context()
	lang := langOf(c)
	v, err := h.baseView(ctx, lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	term := termFilter{}
	// 类目不存在就是 404，而不是「这个筛选下暂时没内容」：前者是链接写错了，
	// 后者是还没上商品，两者给访客的信息完全不同。
	if slug := strings.TrimSpace(c.Query("category")); slug != "" {
		row, cerr := h.svc.ProductCategoryBySlug(ctx, slug)
		if cerr != nil {
			h.notFound(c, nil)
			return
		}
		term = termFilter{Kind: "category", Slug: row.Slug}
		v.ProductTermName = row.Title
		v.ProductTermQuery = url.Values{"category": {row.Slug}}.Encode()
		v.Title = v.composeTitle(v.TF("products.categoryTitleTag", row.Title))
	}
	products, total, err := h.svc.PublishedProductsPage(ctx, lang.Code, term, page, productsPageSize)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	cats, err := h.svc.ListProductCategories(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	active := ""
	if term.Kind == "category" {
		active = term.Slug
	}
	v.ProductCategories = ProductCategoryLinks(cats, active)
	v.Products = products
	v.ProductsPage, v.ProductsTotalPages = pageOf(page), pagesOf(total, productsPageSize)
	// 第 1 页才是规范地址：?page=2 不进 canonical，避免搜索引擎把每页当独立页面收录。
	v.Noindex = v.ProductsPage > 1
	if v.ProductTermName == "" {
		v.Title = v.composeTitle(v.T("products.pageTitleTag"))
	}
	v.Desc = v.TF("products.pageDesc", v.SiteName)
	h.render(c, "products", http.StatusOK, v)
}

// GET /products/:slug —— 产品详情（对标阿里商详：图集 / 价格 / 起订量 / 属性 / 富文本详情）。
func (h *siteHandler) productDetail(c *gin.Context) {
	lang := langOf(c)
	slug := c.Param("slug")
	p, err := h.svc.PublishedProductBy(c.Request.Context(), slug, lang.Code)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			c.String(http.StatusInternalServerError, "site unavailable")
			return
		}
		// 未上架（不存在、草稿或该语言未发布）按 404 走；唯一例外是
		// 管理端签发的预览链接：令牌有效就照常渲染，发布前能看到真实版式。
		draft, derr := h.svc.ProductBySlug(c.Request.Context(), slug, lang.Code)
		if derr != nil || !verifyProductPreview(h.previewKey, slug, c.Query("preview"), time.Now()) {
			h.notFound(c, nil)
			return
		}
		p = draft
	}
	v, err := h.baseView(c.Request.Context(), lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.Title = v.composeTitle(p.Title)
	// SEO 标题是完整标题的覆盖：填了就替换「商品标题」，站点名后缀保持一致的组合规律。
	if p.SeoTitle != "" {
		v.SeoTitle = v.composeTitle(p.SeoTitle)
	}
	switch {
	case p.SeoDesc != "":
		v.Desc = p.SeoDesc
	case p.Summary != "":
		v.Desc = p.Summary
	default:
		v.Desc = v.SiteDesc
	}
	// 商品级关键词优先：留空才回退站点设置，与新闻详情的取舍一致。
	if p.SeoKeywords != "" {
		v.SeoKeywords = p.SeoKeywords
	}
	v.Product = p
	// 同类产品推荐：同类目已上架商品（不含自身）；加载失败不挡详情，推荐区静默缺席。
	if related, rerr := h.svc.PublishedRelatedProducts(c.Request.Context(), lang.Code, p); rerr == nil {
		v.ProductsRelated = related
	} else if h.log != nil {
		h.log.Warn("website: 产品同类推荐加载失败", zap.Error(rerr), zap.String("slug", slug))
	}
	// og:image 只认绝对 URL；商品设了主图就用主图，否则模板回退站点默认图。
	v.OGImage = absURL(v.BaseURL, p.CoverURL)
	v.Noindex = p.Noindex
	// hreflang、og:locale:alternate 与语言切换器都遍历 v.Languages：未发布的语言不能出现在这里。
	v.Languages = visibleLanguages(p.Published(), p.PublishedJSON)
	h.render(c, "product", http.StatusOK, v)
}

// GET /p/:slug —— 自定义页。
func (h *siteHandler) pageDetail(c *gin.Context) {	lang := langOf(c)
	slug := c.Param("slug")
	p, err := h.svc.PublishedPageBy(c.Request.Context(), slug, lang.Code)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			c.String(http.StatusInternalServerError, "site unavailable")
			return
		}
		// 未发布（不存在、整篇草稿或该语言未发布）按 404 走；唯一例外是
		// 管理端签发的预览链接：令牌有效就照常渲染，发布前能看到真实版式。
		draft, derr := h.svc.PageBySlug(c.Request.Context(), slug, lang.Code)
		if derr != nil || !verifyPagePreview(h.previewKey, slug, c.Query("preview"), time.Now()) {
			h.notFound(c, nil)
			return
		}
		p = draft
	}
	v, err := h.baseView(c.Request.Context(), lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.Title = v.composeTitle(p.Title)
	// SEO 标题是完整标题的覆盖：填了就替换「页面标题」，站点名后缀保持一致的组合规律。
	if p.SeoTitle != "" {
		v.SeoTitle = v.composeTitle(p.SeoTitle)
	}
	if p.SeoDesc != "" {
		v.Desc = p.SeoDesc
	} else {
		v.Desc = v.SiteDesc
	}
	v.Page = p
	// og:image 只认绝对 URL；页面设了分享图就用它，否则模板回退站点默认图。
	v.OGImage = absURL(v.BaseURL, p.OgImage)
	v.Noindex = p.Noindex
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
	v.Title = v.composeTitle(v.T("contact.titleTag"))
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
	case "consent":
		return v.T("contact.err.consent")
	}
	return v.T("contact.err.fail")
}

// POST /contact —— 在线留言。fetch 提交回 JSON；无 JS 的整页表单提交 303 回联系页带结果。
func (h *siteHandler) contactSubmit(c *gin.Context) {
	// 先计数再解析：绑定失败也是一次投稿尝试，不能靠反复触发校验绕过配额。
	if !h.contactAllowed(c) {
		h.contactFail(c, http.StatusTooManyRequests, "rate")
		return
	}
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
	// 隐私同意：站点要求而未勾选就不入库。读设置失败同样拒绝——
	// 把「查不到」当成「没开启」，等于这道合规要求可以被数据库抖动绕过。
	set, err := h.svc.Settings(c.Request.Context())
	if err != nil {
		if h.log != nil {
			h.log.Warn("website: 留言读取站点设置失败", zap.Error(err))
		}
		h.contactFail(c, http.StatusInternalServerError, "fail")
		return
	}
	if set.ContactConsent && !f.Consent {
		h.contactFail(c, http.StatusBadRequest, "consent")
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
