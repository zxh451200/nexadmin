package website

// Atom 订阅源（/atom.xml）：部分阅读器与聚合平台只认 Atom，与 RSS 2.0（ld.go 的 rss）
// 并行提供。条目口径与 rss 一致：站点默认语言最新 20 篇；updated 取内容实际变更时间
// 而不是请求时间——内容没动时间戳就不动，订阅器不会误判 feed 有更新。

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *siteHandler) atom(c *gin.Context) {
	ctx := c.Request.Context()
	lang := SiteDefaultLanguage()
	posts, _, err := h.svc.PublishedPostsPage(ctx, lang.Code, termFilter{}, "", 1, 20)
	if err != nil {
		c.String(http.StatusInternalServerError, "atom unavailable")
		return
	}
	set, err := h.svc.Settings(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, "atom unavailable")
		return
	}
	base := strings.TrimSuffix(siteBase(c, set.SiteOrigin), "/")
	siteName := set.SiteName
	if siteName == "" {
		siteName = "NexAdmin"
	}
	build := set.UpdatedAt
	for _, p := range posts {
		if p.PublishedAt != nil && p.PublishedAt.After(build) {
			build = *p.PublishedAt
		}
	}
	if build.IsZero() {
		build = time.Now()
	}
	stamp := build.UTC().Format(time.RFC3339)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	// 与 rss.xml 同一张 XSL：浏览器里是人能读的说明页，阅读器照常解析 Atom 结构。
	b.WriteString(`<?xml-stylesheet type="text/xsl" href="/website-assets/feed.xsl"?>` + "\n")
	fmt.Fprintf(&b, `<feed xmlns="http://www.w3.org/2005/Atom"><title>%s</title><link href="%s/"/><link href="%s/atom.xml" rel="self" type="application/atom+xml"/><id>%s/</id><updated>%s</updated><generator>NexAdmin</generator>`+"\n",
		rssEsc.Replace(siteName), base, base, base, stamp)
	for _, p := range posts {
		desc := p.Summary
		if desc == "" {
			desc = summaryFromHTML(p.Content, 300)
		}
		link := base + lang.Path("/news/"+p.Slug)
		pub := p.UpdatedAt
		if p.PublishedAt != nil {
			pub = *p.PublishedAt
		}
		fmt.Fprintf(&b, "<entry><title>%s</title><link href=\"%s\"/><id>%s</id><updated>%s</updated><summary>%s</summary></entry>\n",
			rssEsc.Replace(p.Title), link, link, pub.UTC().Format(time.RFC3339), rssEsc.Replace(rssPlain(desc)))
	}
	b.WriteString(`</feed>`)
	c.Header("Cache-Control", "public, max-age=3600")
	// MIME 口径与 rss 相同：application/xml 才能让 Chrome 应用 feed.xsl 样式表。
	c.Data(http.StatusOK, "application/xml; charset=utf-8", []byte(b.String()))
}
