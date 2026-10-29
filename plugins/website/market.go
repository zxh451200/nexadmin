package website

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/plugin"
	"nexadmin/core/plugin/market"
)

// 官网插件市场：与后台「插件管理 → 插件市场」共用 config/plugin-market.yaml 这一份目录。
// 官网只做展示、搜索、下载与“去后台一键安装”，不在这里执行安装（安装要管理员权限与构建/重启）。

// marketCategoryOrder 固定分类顺序，避免同一批插件在不同实例上顺序漂移。
var marketCategoryOrder = []string{"ai", "security", "devops", "business", "content"}

// marketCategoryLabel 取分类的本地化名称；字典没有该分类时退回原始 code。
func marketCategoryLabel(lang, category string) string {
	if category == "" {
		return ""
	}
	key := "market.category." + category
	if s := tr(lang, key); s != key {
		return s
	}
	return category
}

// localizeEntry 用目录里的 i18n 覆盖名称/简介；语言缺该条目时退回语言基码。
func localizeEntry(e market.Entry, lang string) market.Entry {
	tr, ok := e.I18n[lang]
	if !ok {
		if base, _, found := strings.Cut(lang, "-"); found {
			tr, ok = e.I18n[base]
		}
	}
	if ok {
		if strings.TrimSpace(tr.Name) != "" {
			e.Name = tr.Name
		}
		if strings.TrimSpace(tr.Description) != "" {
			e.Description = tr.Description
		}
	}
	return e
}

// marketPriceLabel 把目录里的中文占位价翻成当前语言；自由填写的价格原样展示。
func marketPriceLabel(lang, price string) string {
	switch strings.TrimSpace(price) {
	case "待定":
		return tr(lang, "market.priceTbd")
	case "免费":
		return tr(lang, "market.priceFree")
	}
	return price
}

type marketCard struct {
	ID            string
	Name          string
	Version       string
	Description   string
	Category      string
	CategoryLabel string
	Edition       string
	Pro           bool
	Price         string
	Author        string
	Initial       string
	DetailURL     string
	DownloadURL   string
	Cover         string
	Screenshots   []string
	ChangelogURL  string
	Downloads     int64
	DownloadsText string
	RatingAvg     string
	RatingCount   int64
	Versions      []marketVersionView
}

// marketVersionView 是详情页版本历史的一行。
type marketVersionView struct {
	Version      string
	Date         string
	Notes        string
	DownloadURL  string
	ChecksumURL  string
	SignatureURL string
	Current      bool
	Available    bool
}

type marketChip struct {
	Label  string
	Href   string
	Active bool
}

// marketIndexItem 是 /plugins/index.json 的条目（给 CLI/外部工具消费，字段保持稳定）。
type marketIndexItem struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Version        string               `json:"version"`
	Edition        string               `json:"edition"`
	Category       string               `json:"category"`
	CategoryLabel  string               `json:"categoryLabel"`
	Description    string               `json:"description"`
	Author         string               `json:"author"`
	Price          string               `json:"price"`
	Source         string               `json:"source,omitempty"`
	DetailURL      string               `json:"detailUrl"`
	DownloadURL    string               `json:"downloadUrl"`
	ChecksumURL    string               `json:"checksumUrl"`
	SignatureURL   string               `json:"signatureUrl,omitempty"`
	PublicKeyURL   string               `json:"publicKeyUrl,omitempty"`
	InstallCommand string               `json:"installCommand"`
	Downloads      int64                `json:"downloads"`
	Rating         float64              `json:"rating"`
	RatingCount    int64                `json:"ratingCount"`
	Signed         bool                 `json:"signed"`
	Versions       []marketIndexVersion `json:"versions,omitempty"`
}

type marketIndexVersion struct {
	Version      string `json:"version"`
	Date         string `json:"date,omitempty"`
	Notes        string `json:"notes,omitempty"`
	DownloadURL  string `json:"downloadUrl,omitempty"`
	ChecksumURL  string `json:"checksumUrl,omitempty"`
	SignatureURL string `json:"signatureUrl,omitempty"`
	Available    bool   `json:"available"`
}

func marketCardOf(e market.Entry, st MarketStat, lang string) marketCard {
	e = localizeEntry(e, lang)
	initial := "?"
	if r := []rune(strings.TrimSpace(e.Name)); len(r) > 0 {
		initial = strings.ToUpper(string(r[0]))
	}
	c := marketCard{
		ID: e.ID, Name: e.Name, Version: e.Version, Description: e.Description,
		Category: e.Category, CategoryLabel: marketCategoryLabel(lang, e.Category),
		Edition: string(e.Edition), Pro: e.Edition == plugin.Pro,
		Price: marketPriceLabel(lang, e.Price), Author: e.Author, Initial: initial,
		DetailURL:     "/plugins/" + e.ID,
		DownloadURL:   "/plugins/" + e.ID + "/download",
		Screenshots:   e.Screenshots,
		ChangelogURL:  e.ChangelogURL,
		Downloads:     st.Downloads,
		DownloadsText: formatCount(st.Downloads),
		RatingCount:   st.RatingCount,
	}
	if st.RatingCount > 0 {
		c.RatingAvg = fmt.Sprintf("%.1f", float64(st.RatingSum)/float64(st.RatingCount))
	}
	return c
}

func marketCards(entries []market.Entry, stats map[string]MarketStat, lang string) []marketCard {
	out := make([]marketCard, 0, len(entries))
	for _, e := range entries {
		out = append(out, marketCardOf(e, stats[e.ID], lang))
	}
	return out
}

// featuredEntries 让首页优先展示带截图的插件，再补足其余条目。
func featuredEntries(entries []market.Entry, limit int) []market.Entry {
	out := make([]market.Entry, 0, limit)
	for _, e := range entries {
		if len(e.Screenshots) > 0 {
			out = append(out, e)
			if len(out) == limit {
				return out
			}
		}
	}
	for _, e := range entries {
		if len(e.Screenshots) == 0 {
			out = append(out, e)
			if len(out) == limit {
				return out
			}
		}
	}
	return out
}

// formatCount 给下载量加千分位。
func formatCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// marketVersionViews 生成详情页的版本历史：当前版本永远在第一条。
func marketVersionViews(e market.Entry, signed bool) []marketVersionView {
	id := e.ID
	views := []marketVersionView{{
		Version: e.Version, Current: true, Available: true,
		DownloadURL: "/plugins/" + id + "/download",
		ChecksumURL: "/plugins/" + id + "/download.sha256",
	}}
	if signed {
		views[0].SignatureURL = "/plugins/" + id + "/download.sig"
	}
	for _, v := range e.Versions {
		if v.Version == e.Version {
			continue
		}
		view := marketVersionView{Version: v.Version, Date: v.Date, Notes: v.Notes, Available: v.Ref != ""}
		if view.Available {
			q := url.Values{"version": {v.Version}}.Encode()
			view.DownloadURL = "/plugins/" + id + "/download?" + q
			view.ChecksumURL = "/plugins/" + id + "/download.sha256?" + q
			if signed {
				view.SignatureURL = "/plugins/" + id + "/download.sig?" + q
			}
		}
		views = append(views, view)
	}
	return views
}

// marketStats 读取公开统计；失败只影响展示，不阻塞页面。
func (h *siteHandler) marketStats(ctx context.Context) map[string]MarketStat {
	if h.svc == nil {
		return map[string]MarketStat{}
	}
	stats, err := h.svc.MarketStats(ctx)
	if err != nil {
		if h.log != nil {
			h.log.Warn("website: 读取市场统计失败", zap.Error(err))
		}
		return map[string]MarketStat{}
	}
	return stats
}

func marketSourceURL(e market.Entry) string {
	switch {
	case e.Homepage != "":
		return e.Homepage
	case e.Repo != "":
		return "https://github.com/" + e.Repo
	default:
		return ""
	}
}

// marketRoot 返回项目根：市场目录在 <root>/config/plugin-market.yaml。
func marketRoot() string {
	if root := plugin.Default().Root(); root != "" {
		return root
	}
	return "."
}

// marketEntries 每次请求读取目录（文件很小），保证后台改完目录官网即时生效。
func (h *siteHandler) marketEntries() []market.Entry {
	if h.catalog != nil {
		return h.catalog()
	}
	entries, err := market.LoadCatalog(marketRoot())
	if err != nil {
		if h.log != nil {
			h.log.Warn("website: 读取插件市场目录失败", zap.Error(err))
		}
		return nil
	}
	return entries
}

// marketEnabled 供不经过 baseView 的公开路由（下载、校验和、索引）判断市场开关。
func (h *siteHandler) marketEnabled(ctx context.Context) (bool, error) {
	if h.marketOn != nil {
		return h.marketOn()
	}
	set, err := h.svc.Settings(ctx)
	if err != nil {
		return false, err
	}
	return set.MarketEnabled, nil
}

// filterMarket 处理关键词、分类与版本筛选；关键词覆盖名称、id、简介、分类与作者。
func filterMarket(entries []market.Entry, q, category, edition, lang string) []market.Entry {
	kw := strings.ToLower(strings.TrimSpace(q))
	out := make([]market.Entry, 0, len(entries))
	for _, e := range entries {
		if category != "" && e.Category != category {
			continue
		}
		if edition != "" && string(e.Edition) != edition {
			continue
		}
		if kw != "" {
			le := localizeEntry(e, lang)
			// 同时收录原文与译文，中英文关键词在任一语言页都能搜到。
			hay := strings.ToLower(strings.Join([]string{
				e.Name, le.Name, e.ID, e.Description, le.Description, e.Category,
				marketCategoryLabel(lang, e.Category),
				marketCategoryLabel(DefaultLanguage().Code, e.Category), e.Author,
			}, " "))
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

func marketHref(q, category, edition string) string {
	vals := url.Values{}
	if q != "" {
		vals.Set("q", q)
	}
	if category != "" {
		vals.Set("category", category)
	}
	if edition != "" {
		vals.Set("edition", edition)
	}
	if len(vals) == 0 {
		return "/plugins"
	}
	return "/plugins?" + vals.Encode()
}

// marketChips 生成分类与版本筛选片；分类只展示目录里实际出现的值。
func marketChips(entries []market.Entry, q, category, edition, lang string) (cats, eds []marketChip) {
	present := map[string]bool{}
	for _, e := range entries {
		if e.Category != "" {
			present[e.Category] = true
		}
	}
	cats = append(cats, marketChip{Label: tr(lang, "market.all"), Href: marketHref(q, "", edition), Active: category == ""})
	seen := map[string]bool{}
	for _, c := range marketCategoryOrder {
		if present[c] {
			cats = append(cats, marketChip{Label: marketCategoryLabel(lang, c), Href: marketHref(q, c, edition), Active: category == c})
			seen[c] = true
		}
	}
	var rest []string
	for c := range present {
		if !seen[c] {
			rest = append(rest, c)
		}
	}
	sort.Strings(rest)
	for _, c := range rest {
		cats = append(cats, marketChip{Label: marketCategoryLabel(lang, c), Href: marketHref(q, c, edition), Active: category == c})
	}
	eds = []marketChip{
		{Label: tr(lang, "market.all"), Href: marketHref(q, category, ""), Active: edition == ""},
		{Label: tr(lang, "market.free"), Href: marketHref(q, category, "free"), Active: edition == "free"},
		{Label: tr(lang, "market.pro"), Href: marketHref(q, category, "pro"), Active: edition == "pro"},
	}
	return cats, eds
}

func relatedMarketCards(entries []market.Entry, self market.Entry, limit int, stats map[string]MarketStat, lang string) []marketCard {
	out := make([]marketCard, 0, limit)
	for _, e := range entries {
		if e.ID == self.ID || e.Category != self.Category {
			continue
		}
		out = append(out, marketCardOf(e, stats[e.ID], lang))
		if len(out) >= limit {
			break
		}
	}
	return out
}

var marketVersionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,31}$`)

// marketPackageVersion 只允许安全字符进文件名；版本号异常时退回 latest。
func marketPackageVersion(v string) string {
	if marketVersionRe.MatchString(v) {
		return v
	}
	return "latest"
}

func marketPackageName(e market.Entry) string {
	return e.ID + "-" + marketPackageVersion(e.Version) + ".zip"
}

func marketInstallCommand(e market.Entry) string {
	return "nexadmin plugin install " + marketPackageName(e)
}

// pluginJSONLD 生成插件详情页的结构化数据。json.Marshal 会转义 <、>、&，
// 因此作为 template.JS 直接嵌入 <script type="application/ld+json"> 是安全的。
func pluginJSONLD(e market.Entry, base, path string) template.JS {
	var author map[string]string
	if e.Author != "" {
		author = map[string]string{"@type": "Organization", "name": e.Author}
	}
	ld := struct {
		Context             string            `json:"@context"`
		Type                string            `json:"@type"`
		Name                string            `json:"name"`
		Description         string            `json:"description,omitempty"`
		SoftwareVersion     string            `json:"softwareVersion,omitempty"`
		ApplicationCategory string            `json:"applicationCategory,omitempty"`
		OperatingSystem     string            `json:"operatingSystem,omitempty"`
		URL                 string            `json:"url,omitempty"`
		DownloadURL         string            `json:"downloadUrl,omitempty"`
		Author              map[string]string `json:"author,omitempty"`
	}{
		Context: "https://schema.org", Type: "SoftwareApplication",
		Name: e.Name, Description: e.Description, SoftwareVersion: e.Version,
		ApplicationCategory: "BusinessApplication", OperatingSystem: "Web",
		URL:         base + path,
		DownloadURL: base + "/plugins/" + e.ID + "/download",
		Author:      author,
	}
	raw, err := json.Marshal(ld)
	if err != nil {
		return ""
	}
	return template.JS(raw)
}

// GET /plugins —— 市场列表。
func (h *siteHandler) marketList(c *gin.Context) {
	lang := langOf(c)
	v, err := h.baseView(c.Request.Context(), lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	if !v.MarketEnabled {
		h.notFound(c, v)
		return
	}
	all := h.marketEntries()
	q := strings.TrimSpace(c.Query("q"))
	category := strings.TrimSpace(c.Query("category"))
	edition := strings.TrimSpace(c.Query("edition"))
	if edition != "free" && edition != "pro" {
		edition = ""
	}
	filtered := filterMarket(all, q, category, edition, lang.Code)

	v.MarketAvailable = len(all) > 0
	v.PluginCount = len(all)
	v.MarketFiltered = len(filtered)
	v.MarketQuery, v.MarketCategory, v.MarketEdition = q, category, edition
	v.MarketCategories, v.MarketEditions = marketChips(all, q, category, edition, lang.Code)
	v.Plugins = marketCards(filtered, h.marketStats(c.Request.Context()), lang.Code)
	v.Title = v.T("market.titleTag") + " - " + v.SiteName
	v.Desc = v.T("market.metaDesc")
	h.render(c, "plugins", http.StatusOK, v)
}

// GET /plugins/:id —— 插件详情。
func (h *siteHandler) marketDetail(c *gin.Context) {
	lang := langOf(c)
	v, err := h.baseView(c.Request.Context(), lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	if !v.MarketEnabled {
		h.notFound(c, v)
		return
	}
	entries := h.marketEntries()
	e, ok := market.FindEntry(entries, c.Param("id"))
	if !ok {
		h.notFound(c, v)
		return
	}
	le := localizeEntry(e, lang.Code)
	stats := h.marketStats(c.Request.Context())
	card := marketCardOf(e, stats[e.ID], lang.Code)
	card.Versions = marketVersionViews(e, h.signKey != nil)
	v.Plugin = &card
	v.Related = relatedMarketCards(entries, e, 3, stats, lang.Code)
	v.MarketAvailable = true
	v.MyScore = h.svc.MyRating(c.Request.Context(), e.ID, c.ClientIP())
	v.Signed = h.signKey != nil
	v.SignatureURL = "/plugins/" + e.ID + "/download.sig"
	v.PublicKeyURL = "/plugins/signing-key.pub"
	v.DownloadURL = card.DownloadURL
	v.DownloadFilename = marketPackageName(e)
	v.InstallCommand = marketInstallCommand(e)
	v.MarketSourceURL = marketSourceURL(e)
	v.AdminInstallURL = "/admin/login?redirect=" + url.QueryEscape("/system/plugin?tab=market&plugin="+e.ID)
	v.PluginLD = pluginJSONLD(le, baseURL(c), c.Request.URL.Path)
	v.Title = le.Name + " - " + v.T("market.titleTag") + " - " + v.SiteName
	v.Desc = le.Description
	h.render(c, "plugin", http.StatusOK, v)
}

// GET /plugins/index.json —— 机器可读目录（与页面同一份数据）。
func (h *siteHandler) marketIndexJSON(c *gin.Context) {
	ok, err := h.marketEnabled(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	if !ok {
		c.String(http.StatusNotFound, "not found")
		return
	}
	entries := h.marketEntries()
	base := baseURL(c)
	stats := h.marketStats(c.Request.Context())
	signed := h.signKey != nil
	out := make([]marketIndexItem, 0, len(entries))
	for _, e := range entries {
		st := stats[e.ID]
		rating := 0.0
		if st.RatingCount > 0 {
			rating = math.Round(float64(st.RatingSum)/float64(st.RatingCount)*10) / 10
		}
		item := marketIndexItem{
			ID: e.ID, Name: e.Name, Version: e.Version, Edition: string(e.Edition),
			Category: e.Category, CategoryLabel: marketCategoryLabel(DefaultLanguage().Code, e.Category),
			Description: e.Description, Author: e.Author, Price: e.Price,
			Source:         marketSourceURL(e),
			DetailURL:      base + "/plugins/" + e.ID,
			DownloadURL:    base + "/plugins/" + e.ID + "/download",
			ChecksumURL:    base + "/plugins/" + e.ID + "/download.sha256",
			InstallCommand: marketInstallCommand(e),
			Downloads:      st.Downloads, Rating: rating, RatingCount: st.RatingCount,
			Signed: signed,
		}
		if signed {
			item.SignatureURL = base + "/plugins/" + e.ID + "/download.sig"
			item.PublicKeyURL = base + "/plugins/signing-key.pub"
		}
		for _, v := range marketVersionViews(e, signed) {
			iv := marketIndexVersion{Version: v.Version, Date: v.Date, Notes: v.Notes, Available: v.Available}
			if v.Available {
				iv.DownloadURL = base + v.DownloadURL
				iv.ChecksumURL = base + v.ChecksumURL
				if v.SignatureURL != "" {
					iv.SignatureURL = base + v.SignatureURL
				}
			}
			item.Versions = append(item.Versions, iv)
		}
		out = append(out, item)
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, out)
}

// GET /plugins/:id/download —— 现场把市场源码重打成安装包再下载（带 6 小时磁盘缓存）。
func (h *siteHandler) marketDownload(c *gin.Context) {
	ok, err := h.marketEnabled(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	if !ok {
		h.notFound(c, nil)
		return
	}
	entries := h.marketEntries()
	e, ok := market.FindEntry(entries, c.Param("id"))
	if !ok {
		h.notFound(c, nil)
		return
	}
	e, ok = market.EntryForVersion(e, c.Query("version"))
	if !ok {
		h.notFound(c, nil)
		return
	}
	// 与访客连接解耦：访客断线也应把包打完写进缓存，避免下次重复拉源码。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	path, err := h.pkg.get(ctx, e)
	if err != nil {
		if h.log != nil {
			h.log.Warn("website: 插件包生成失败", zap.String("plugin", e.ID), zap.Error(err))
		}
		lang := preferredLang(c)
		v, verr := h.baseView(c.Request.Context(), lang.Code, 0)
		if verr != nil {
			c.String(http.StatusBadGateway, "plugin package unavailable")
			return
		}
		c.Header("Content-Language", lang.HTMLLang)
		card := marketCardOf(e, MarketStat{}, lang.Code)
		v.Plugin = &card
		v.MarketError = v.T("error.msg")
		v.Title = v.T("error.titleTag") + " - " + v.SiteName
		v.Noindex = true
		h.render(c, "error", http.StatusBadGateway, v)
		return
	}
	if sum, err := fileSHA256(path); err == nil {
		c.Header("X-Checksum-SHA256", sum)
	}
	if h.signKey != nil {
		c.Header("X-Signed", "ed25519")
	}
	// 显式指定，避免不同操作系统的 mime 注册表把 .zip 报成 x-zip-compressed。
	c.Header("Content-Type", "application/zip")
	c.Header("Cache-Control", "public, max-age=300")
	c.FileAttachment(path, marketPackageName(e))

	// 文件已经发出；计数失败只影响统计。
	if h.svc != nil {
		ictx, icancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer icancel()
		if err := h.svc.IncrementDownload(ictx, e.ID); err != nil && h.log != nil {
			h.log.Warn("website: 记录下载次数失败", zap.String("plugin", e.ID), zap.Error(err))
		}
	}
}

// GET /plugins/:id/download.sha256 —— sha256sum 格式的校验文件（与下载同一份缓存产物）。
func (h *siteHandler) marketChecksum(c *gin.Context) {
	ok, err := h.marketEnabled(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	if !ok {
		h.notFound(c, nil)
		return
	}
	entries := h.marketEntries()
	e, ok := market.FindEntry(entries, c.Param("id"))
	if !ok {
		h.notFound(c, nil)
		return
	}
	e, ok = market.EntryForVersion(e, c.Query("version"))
	if !ok {
		h.notFound(c, nil)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	path, err := h.pkg.get(ctx, e)
	if err != nil {
		if h.log != nil {
			h.log.Warn("website: 插件包生成失败", zap.String("plugin", e.ID), zap.Error(err))
		}
		c.String(http.StatusBadGateway, "plugin package unavailable")
		return
	}
	sum, err := fileSHA256(path)
	if err != nil {
		c.String(http.StatusInternalServerError, "checksum unavailable")
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(sum+"  "+marketPackageName(e)+"\n"))
}

// GET /plugins/:id/download.sig —— detached Ed25519 签名（base64）。
func (h *siteHandler) marketSignature(c *gin.Context) {
	if h.signKey == nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	ok, err := h.marketEnabled(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	if !ok {
		h.notFound(c, nil)
		return
	}
	entries := h.marketEntries()
	e, found := market.FindEntry(entries, c.Param("id"))
	if !found {
		h.notFound(c, nil)
		return
	}
	e, found = market.EntryForVersion(e, c.Query("version"))
	if !found {
		h.notFound(c, nil)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	path, err := h.pkg.get(ctx, e)
	if err != nil {
		if h.log != nil {
			h.log.Warn("website: 插件包生成失败", zap.String("plugin", e.ID), zap.Error(err))
		}
		c.String(http.StatusBadGateway, "plugin package unavailable")
		return
	}
	sig, err := market.SignFile(path, h.signKey)
	if err != nil {
		c.String(http.StatusInternalServerError, "signature unavailable")
		return
	}
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+marketPackageName(e)+`.sig"`)
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(market.EncodeSignature(sig)+"\n"))
}

// GET /plugins/signing-key.pub —— base64 Ed25519 公钥（供 `nexadmin plugin verify`）。
func (h *siteHandler) marketPublicKey(c *gin.Context) {
	if h.signKey == nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	pub := h.signKey.Public().(ed25519.PublicKey)
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(market.EncodePublicKey(pub)+"\n"))
}

// POST /plugins/:id/rate —— 匿名评分（1-5），同一 IP 对同一插件只保留一条。
func (h *siteHandler) marketRate(c *gin.Context) {
	ok, err := h.marketEnabled(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	if !ok {
		h.notFound(c, nil)
		return
	}
	entries := h.marketEntries()
	e, found := market.FindEntry(entries, c.Param("id"))
	if !found {
		h.notFound(c, nil)
		return
	}
	lang, _ := LanguageByCode(strings.TrimSpace(c.PostForm("lang")))
	if lang.Code == "" {
		lang = preferredLang(c)
	}
	score, err := strconv.Atoi(c.PostForm("score"))
	if err != nil || score < 1 || score > 5 {
		c.String(http.StatusBadRequest, tr(lang.Code, "plugin.rateInvalid"))
		return
	}
	if err := h.svc.Rate(c.Request.Context(), e.ID, c.ClientIP(), score); err != nil {
		if h.log != nil {
			h.log.Warn("website: 保存评分失败", zap.String("plugin", e.ID), zap.Error(err))
		}
		c.String(http.StatusInternalServerError, tr(lang.Code, "plugin.rateFailed"))
		return
	}
	c.Redirect(http.StatusSeeOther, lang.Path("/plugins/"+e.ID)+"#rating")
}

// packageCache 负责“下载 → 重打包”的磁盘缓存与并发合并。
// 缓存放在应用自己的 data/cache/market 下（0700），不落系统临时目录，避免同机其他用户投毒。
type packageCache struct {
	dir   string // 测试注入；空 = <root>/data/cache/market
	sem   chan struct{}
	mu    sync.Mutex
	locks map[string]*sync.Mutex
	fails map[string]packageFail
}

type packageFail struct {
	until time.Time
	msg   string
}

func newPackageCache() *packageCache {
	return &packageCache{
		sem:   make(chan struct{}, 2), // 全局最多两个源站拉取，保护 GitHub 匿名配额
		locks: map[string]*sync.Mutex{},
		fails: map[string]packageFail{},
	}
}

func (p *packageCache) cacheDir() string {
	if p.dir != "" {
		return p.dir
	}
	return filepath.Join(marketRoot(), "data", "cache", "market")
}

func (p *packageCache) failure(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f, ok := p.fails[name]; ok && time.Now().Before(f.until) {
		return errors.New(f.msg)
	}
	return nil
}

func (p *packageCache) markFailure(name string, err error) {
	p.mu.Lock()
	p.fails[name] = packageFail{until: time.Now().Add(time.Minute), msg: err.Error()}
	p.mu.Unlock()
}

func (p *packageCache) clearFailure(name string) {
	p.mu.Lock()
	delete(p.fails, name)
	p.mu.Unlock()
}

func (p *packageCache) lock(key string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.locks[key] == nil {
		p.locks[key] = &sync.Mutex{}
	}
	return p.locks[key]
}

func (p *packageCache) get(ctx context.Context, e market.Entry) (string, error) {
	name := marketPackageName(e)
	dir := p.cacheDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name)
	if freshPackage(dst) {
		return dst, nil
	}
	// 失败后 1 分钟内不重复拉取（源站 404/限流时保护自己，也避免访客狂点）。
	if err := p.failure(name); err != nil {
		return "", err
	}

	mu := p.lock(name)
	mu.Lock()
	defer mu.Unlock()
	if freshPackage(dst) {
		return dst, nil
	}
	// 锁内复查：排队期间别的请求已经失败，不要把它放大成 N 次全量拉取。
	if err := p.failure(name); err != nil {
		return "", err
	}

	// 全局拉取并发上限；排队时尊重请求上下文取消。
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// 唯一临时名 + 发布时原子改名：多实例/多请求不会互相截断，也不删除别人刚写好的包。
	tmpFile, err := os.CreateTemp(dir, ".pkg-*.part")
	if err != nil {
		return "", err
	}
	tmp := tmpFile.Name()
	_ = tmpFile.Close()
	_ = os.Remove(tmp) // 交给 Package 创建；名字仍唯一
	if err := market.Package(ctx, e, tmp); err != nil {
		_ = os.Remove(tmp)
		p.markFailure(name, err)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		if freshPackage(dst) { // 另一实例已发布同一版本，直接复用
			p.clearFailure(name)
			return dst, nil
		}
		p.markFailure(name, err)
		return "", err
	}
	p.clearFailure(name)
	sweepPackages(dir)
	return dst, nil
}

func freshPackage(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && time.Since(st.ModTime()) < 6*time.Hour
}

// fileSHA256 返回小写十六进制摘要，用于下载响应头与 sha256sum 校验文件。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sweepPackages 清理一天前的缓存包与一小时前的残留临时文件；失败不影响下载。
func sweepPackages(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, ent := range ents {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		info, err := ent.Info()
		if err != nil {
			continue
		}
		switch {
		case strings.HasSuffix(name, ".zip") && info.ModTime().Before(now.Add(-24*time.Hour)):
			_ = os.Remove(filepath.Join(dir, name))
		case strings.HasPrefix(name, ".pkg-") && info.ModTime().Before(now.Add(-time.Hour)):
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}
