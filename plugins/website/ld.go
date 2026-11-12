package website

// ld.go 是公开站的结构化数据（schema.org JSON-LD）与 RSS 订阅源。
// 基础标签（canonical/hreflang/og/twitter/sitemap）在 layout.tmpl 与 public.go 已就位，
// 这里补齐蜘蛛侧的富摘要口径：站级 Organization/WebSite、产品详情 Product+Offer、
// 新闻详情 Article、内页 BreadcrumbList，以及 /rss.xml 新闻订阅源。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"html/template"
)

// marshalLD 序列化 JSON-LD；marshal 默认把 < > & 转义成 \uXXXX，
// 注入 <script> 不会被提前闭合标签，插入失败静默缺席（宁可少一个富摘要，不给页面输出坏 HTML）。
func marshalLD(v any) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return template.JS(b)
}

// orgJSONLD 站级 Organization + WebSite：品牌实体与站点实体是所有富摘要的挂靠点。
func orgJSONLD(v *siteView) template.JS {
	org := map[string]any{
		"@type": "Organization",
		"name":  v.SiteName,
		"url":   strings.TrimSuffix(v.BaseURL, "/") + "/",
	}
	if logo := absURL(v.BaseURL, v.LogoURL); logo != "" {
		org["logo"] = logo
	}
	var sameAs []string
	for _, s := range v.Socials {
		if strings.HasPrefix(s.URL, "https://") || strings.HasPrefix(s.URL, "http://") {
			sameAs = append(sameAs, s.URL)
		}
	}
	if len(sameAs) > 0 {
		org["sameAs"] = sameAs
	}
	web := map[string]any{"@type": "WebSite", "name": v.SiteName, "url": strings.TrimSuffix(v.BaseURL, "/") + "/"}
	return marshalLD([]any{org, web})
}

// breadcrumbJSONLD 面包屑：items 是 [名称, 绝对地址] 对，按展示顺序传入。
func breadcrumbJSONLD(items ...[2]string) map[string]any {
	list := make([]any, 0, len(items))
	for i, it := range items {
		list = append(list, map[string]any{"@type": "ListItem", "position": i + 1, "name": it[0], "item": it[1]})
	}
	return map[string]any{"@type": "BreadcrumbList", "itemListElement": list}
}

// publisherOf 是文章/产品共用的发布者实体（站点本身）。
func publisherOf(v *siteView) map[string]any {
	m := map[string]any{"@type": "Organization", "name": v.SiteName}
	if logo := absURL(v.BaseURL, v.LogoURL); logo != "" {
		m["logo"] = map[string]any{"@type": "ImageObject", "url": logo}
	}
	return m
}

// articleJSONLD 新闻详情：headline/首图/发布与更新时间/作者/发布者 + 面包屑，一组两个实体。
func articleJSONLD(v *siteView, p *Post) template.JS {
	m := map[string]any{
		"@type":            "Article",
		"headline":         p.Title,
		"mainEntityOfPage": v.Canonical(),
		"publisher":        publisherOf(v),
	}
	if desc := strings.TrimSpace(v.Desc); desc != "" {
		m["description"] = desc
	}
	if img := v.OGImage; img != "" {
		m["image"] = img
	}
	if p.PublishedAt != nil {
		m["datePublished"] = p.PublishedAt.Format(time.RFC3339)
	}
	if !p.UpdatedAt.IsZero() {
		m["dateModified"] = p.UpdatedAt.Format(time.RFC3339)
	}
	author := strings.TrimSpace(p.Author)
	if author == "" {
		author = v.SiteName
	}
	m["author"] = map[string]any{"@type": "Organization", "name": author}
	return marshalLD([]any{m, breadcrumbJSONLD(
		[2]string{v.T("layout.home"), strings.TrimSuffix(v.BaseURL, "/") + v.Home()},
		[2]string{v.T("layout.news"), strings.TrimSuffix(v.BaseURL, "/") + v.URL("/news")},
		[2]string{p.Title, v.Canonical()},
	)})
}

// productJSONLD 产品详情：名称/描述/图集/品牌/类目 + 有价时的 Offer（询盘品无价不给 offers，
// 空报价会被谷歌商品摘要判无效）+ 面包屑，一组两个实体。
func productJSONLD(v *siteView, p *Product) template.JS {
	m := map[string]any{
		"@type": "Product",
		"name":  p.Title,
		"sku":   p.Slug,
		"brand": map[string]any{"@type": "Brand", "name": v.SiteName},
	}
	switch {
	case strings.TrimSpace(p.SeoDesc) != "":
		m["description"] = p.SeoDesc
	case strings.TrimSpace(p.Summary) != "":
		m["description"] = p.Summary
	}
	var images []string
	if img := absURL(v.BaseURL, p.CoverURL); img != "" {
		images = append(images, img)
	}
	for _, img := range safeImageList(p.ImagesJSON) {
		if abs := absURL(v.BaseURL, img); abs != "" {
			images = append(images, abs)
		}
	}
	if len(images) > 0 {
		m["image"] = images
	}
	var cats []string
	for _, c := range p.Categories {
		cats = append(cats, c.Title)
	}
	if len(cats) > 0 {
		m["category"] = strings.Join(cats, " > ")
	}
	if p.PriceMin > 0 {
		m["offers"] = map[string]any{
			"@type":         "Offer",
			"url":           v.Canonical(),
			"price":         p.PriceMin,
			"priceCurrency": p.Currency,
			"itemCondition": "https://schema.org/NewCondition",
			"availability":  "https://schema.org/InStock",
		}
	}
	return marshalLD([]any{m, breadcrumbJSONLD(
		[2]string{v.T("layout.home"), strings.TrimSuffix(v.BaseURL, "/") + v.Home()},
		[2]string{v.T("layout.product"), strings.TrimSuffix(v.BaseURL, "/") + v.URL("/products")},
		[2]string{p.Title, v.Canonical()},
	)})
}

// xmlEscape 是 RSS 文本的最小转义：正文里的 & < > ' " 都会破坏 XML 结构。
var rssEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")

// GET /rss.xml —— 新闻订阅源（默认语言最近 20 篇）；蜘蛛与订阅器借此发现内容更新。
func (h *siteHandler) rss(c *gin.Context) {
	ctx := c.Request.Context()
	lang := DefaultLanguage()
	posts, _, err := h.svc.PublishedPostsPage(ctx, lang.Code, termFilter{}, 1, 20)
	if err != nil {
		c.String(http.StatusInternalServerError, "rss unavailable")
		return
	}
	set, err := h.svc.Settings(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, "rss unavailable")
		return
	}
	base := strings.TrimSuffix(baseURL(c), "/")
	siteName := set.SiteName
	if siteName == "" {
		siteName = "NexAdmin"
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&b, `<rss version="2.0"><channel><title>%s</title><link>%s/</link><description>%s</description><language>%s</language><generator>NexAdmin</generator>`+"\n",
		rssEsc.Replace(siteName), base, rssEsc.Replace(set.SiteDesc), lang.Code)
	for _, p := range posts {
		desc := p.Summary
		if desc == "" {
			desc = summaryFromHTML(p.Content, 300)
		}
		link := base + lang.Path("/news/" + p.Slug)
		pub := time.Now()
		if p.PublishedAt != nil {
			pub = *p.PublishedAt
		}
		fmt.Fprintf(&b, "<item><title>%s</title><link>%s</link><guid isPermaLink=\"true\">%s</guid><pubDate>%s</pubDate><description>%s</description></item>\n",
			rssEsc.Replace(p.Title), link, link, pub.Format(time.RFC1123Z), rssEsc.Replace(desc))
	}
	b.WriteString(`</channel></rss>`)
	c.Data(http.StatusOK, "application/rss+xml; charset=utf-8", []byte(b.String()))
}
