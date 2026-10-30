package website

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/pkg/htmlsafe"
	pi18n "nexadmin/pkg/i18n"
	"nexadmin/pkg/idgen"
)

// Service 是官网内容业务层：站点设置（单行）、导航、特性、页面、新闻的 CRUD 与公开页数据装配。
type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// slugReg 允许小写字母、数字、连字符。
var slugReg = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// reservedSlugs 只保留插件自己的路由段（/news、/p）。页面与新闻分别挂在 /p/:slug 与
// /news/:slug 下，天然带命名空间，不会与后台 SPA 路径（/about、/system 等）冲突，
// 因此后台路径不必保留——about、login-guide 这类自然 slug 都允许。
var reservedSlugs = map[string]bool{"news": true, "p": true}

// ValidateSlug 校验 slug 合法且不占用本插件的路由段。
func ValidateSlug(s string) error {
	s = strings.TrimSpace(s)
	if !slugReg.MatchString(s) {
		return response.BadRequest("website.slug_invalid")
	}
	if reservedSlugs[s] || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return response.BadRequest("website.slug_reserved", s)
	}
	return nil
}

func trimLen(s string, max int) string {
	s = strings.TrimSpace(s)
	return s[:min(len(s), max)]
}

// trimRunes 按字符限长：trimLen 按字节切会把中文切出半个码点，用户投稿必须按字符截断。
func trimRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// ---- 页脚列表（社交图标 / 友情链接） ----

const (
	maxSocialLinks = 12
	maxFriendLinks = 20
)

// socialPlatforms 是页脚社交图标的可选值，必须与 render.go 的 socialIconPaths 键一致（有测试兜底）。
var socialPlatforms = []string{"github", "x", "linkedin", "youtube", "wechat", "weibo", "bilibili", "rss", "mail"}

func validSocialPlatform(p string) bool {
	for _, v := range socialPlatforms {
		if v == p {
			return true
		}
	}
	return false
}

// footerURLValid：页脚链接只能是站内路径/锚点或 http(s) 外部地址；社交还允许 mailto。
func footerURLValid(raw string, allowMailto bool) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "mailto":
		return allowMailto && (u.Opaque != "" || u.Path != "")
	case "":
		return strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "#")
	}
	return false
}

// parseSocials / parseFriendLinks 读库里的 JSON 文本；脏数据当作空列表，不能让页脚整页挂掉。
func parseSocials(raw string) []SocialLink {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []SocialLink
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func parseFriendLinks(raw string) []FriendLink {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []FriendLink
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func normalizeSocials(in []SocialLink) (string, error) {
	out := make([]SocialLink, 0, len(in))
	for _, s := range in {
		p, u := strings.TrimSpace(s.Platform), strings.TrimSpace(s.URL)
		if p == "" && u == "" {
			continue
		}
		if !validSocialPlatform(p) || !footerURLValid(u, true) {
			return "", response.BadRequest("website.social_invalid")
		}
		out = append(out, SocialLink{Platform: p, URL: u})
	}
	if len(out) > maxSocialLinks {
		return "", response.BadRequest("website.social_too_many", maxSocialLinks)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func normalizeFriendLinks(in []FriendLink) (string, error) {
	out := make([]FriendLink, 0, len(in))
	for _, l := range in {
		name, u := trimRunes(l.Name, 64), strings.TrimSpace(l.URL)
		if name == "" && u == "" {
			continue
		}
		if name == "" || !footerURLValid(u, false) {
			return "", response.BadRequest("website.friend_link_invalid")
		}
		out = append(out, FriendLink{Name: name, URL: u})
	}
	if len(out) > maxFriendLinks {
		return "", response.BadRequest("website.friend_link_too_many", maxFriendLinks)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// settingView 是管理端设置响应：两个 JSON 文本列展开成数组，前端不用二次解析。
type settingView struct {
	Setting
	Socials     []SocialLink `json:"socials"`
	FriendLinks []FriendLink `json:"friendLinks"`
}

func newSettingView(row *Setting) settingView {
	return settingView{
		Setting:     *row,
		Socials:     parseSocials(row.SocialsJSON),
		FriendLinks: parseFriendLinks(row.FriendLinksJSON),
	}
}

// ---- 站点设置（单行） ----

// Settings 读取站点设置；行不存在时（如迁移后未 seed）现场补一行默认值。
func (s *Service) Settings(ctx context.Context) (*Setting, error) {
	var row Setting
	err := s.db.WithContext(ctx).Order("id").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = defaultSetting()
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, err
		}
		return &row, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// SaveSettings 更新单行设置。
func (s *Service) SaveSettings(ctx context.Context, f SettingForm) (*Setting, error) {
	row, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	row.SiteName = trimLen(f.SiteName, 64)
	row.SiteDesc = trimLen(f.SiteDesc, 300)
	row.LogoURL = trimLen(f.LogoURL, 255)
	row.HomeTitle = trimLen(f.HomeTitle, 200)
	row.HomeSubtitle = trimLen(f.HomeSubtitle, 500)
	row.HomeCTALabel = trimLen(f.HomeCTALabel, 64)
	row.HomeCTAHref = trimLen(f.HomeCTAHref, 255)
	row.SeoKeywords = trimLen(f.SeoKeywords, 300)
	row.ContactEmail = trimLen(f.ContactEmail, 128)
	row.ContactPhone = trimLen(f.ContactPhone, 64)
	row.QRCodeURL = trimLen(f.QRCodeURL, 255)
	row.QRCodeCaption = trimLen(f.QRCodeCaption, 128)
	row.ICP = trimLen(f.ICP, 128)
	row.FooterNote = trimLen(f.FooterNote, 300)
	if row.QRCodeURL != "" && !footerURLValid(row.QRCodeURL, false) {
		return nil, response.BadRequest("website.qr_url_invalid")
	}
	socials, err := normalizeSocials(f.Socials)
	if err != nil {
		return nil, err
	}
	friends, err := normalizeFriendLinks(f.FriendLinks)
	if err != nil {
		return nil, err
	}
	row.SocialsJSON, row.FriendLinksJSON = socials, friends
	i18n, err := pi18n.Normalize(f.I18nJSON, siteLocales{}, settingI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	row.I18nJSON = i18n
	if f.MarketEnabled != nil {
		row.MarketEnabled = *f.MarketEnabled
	}
	if err := s.db.WithContext(ctx).Save(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// ---- 导航 ----

func (s *Service) ListNavs(ctx context.Context) ([]Nav, error) {
	var rows []Nav
	err := s.db.WithContext(ctx).Order("sort, id").Find(&rows).Error
	return rows, err
}

// navHrefValid 判断导航链接是否落在官网能服务的地址上：站内锚点、绝对外部链接，
// 或官网注册的页面路由（可带语言前缀、查询串与锚点）。
// 裸路径（"xx"）与后台客户端路由（/users 等）都不合法——后者会让访客落进后台 SPA 外壳。
func navHrefValid(href string) bool {
	href = strings.TrimSpace(href)
	if href == "" {
		return false
	}
	if strings.HasPrefix(href, "#") {
		return true
	}
	lower := strings.ToLower(href)
	for _, scheme := range []string{"http://", "https://", "mailto:", "tel:"} {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	if !strings.HasPrefix(href, "/") {
		return false
	}
	p := href
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = stripLangPrefix(p)
	if p == "" || p == "/" {
		return true
	}
	for _, prefix := range []string{"/news", "/plugins", "/docs", "/contact"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return strings.HasPrefix(p, "/p/") && len(p) > len("/p/")
}

// navPageSlug 取出站内页面链接里的 slug（/p/about、/en/p/about?x=1 都可）。
func navPageSlug(href string) (string, bool) {
	p := strings.TrimSpace(href)
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	slug, ok := strings.CutPrefix(stripLangPrefix(p), "/p/")
	if !ok || slug == "" {
		return "", false
	}
	return slug, true
}

func (s *Service) SaveNav(ctx context.Context, id idgen.ID, f ItemForm) (*Nav, error) {
	f.Label = trimLen(f.Label, 64)
	f.Href = trimLen(f.Href, 255)
	if f.Label == "" || f.Href == "" {
		return nil, response.BadRequest("website.nav_required")
	}
	if !navHrefValid(f.Href) {
		return nil, response.BadRequest("website.nav_href_invalid", f.Href)
	}
	if slug, ok := navPageSlug(f.Href); ok {
		var n int64
		if err := s.db.WithContext(ctx).Model(&Page{}).Where("slug = ?", slug).Count(&n).Error; err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, response.BadRequest("website.nav_page_missing", slug)
		}
	}
	var row Nav
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
	} else {
		row = Nav{}
	}
	var all []Nav
	if err := s.db.WithContext(ctx).Select("id", "parent_id").Find(&all).Error; err != nil {
		return nil, err
	}
	// 官网导航是平台级数据（无 tenant_id），父级存在性校验即等价于「同一租户」。
	parents := navParentMap(all)
	level, err := navDepth(parents, uint64(row.ID), uint64(f.ParentID))
	if err != nil {
		return nil, err
	}
	// 挂到新父级后整棵子树会整体下移，深度上限要按子树层数一起判断。
	if id != 0 && level+navSubtreeHeight(parents, uint64(id))-1 > navMaxDepth {
		return nil, navBadRequest("website.nav_depth_exceeded", navMaxDepth)
	}
	i18n, err := pi18n.Normalize(f.I18nJSON, siteLocales{}, navI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	row.ParentID, row.Label, row.Href = f.ParentID, f.Label, f.Href
	row.Sort, row.Enabled, row.I18nJSON = f.Sort, f.Enabled, i18n
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteNav 级联删除自身与全部后代，避免留下孤儿节点。
func (s *Service) DeleteNav(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []Nav
		if err := tx.Select("id", "parent_id").Find(&rows).Error; err != nil {
			return err
		}
		ids := navSubtreeIDs(navParentMap(rows), uint64(id))
		if len(ids) == 0 {
			return nil
		}
		list := make([]int64, len(ids))
		for i, v := range ids {
			list[i] = int64(v)
		}
		return tx.Delete(&Nav{}, "id IN ?", list).Error
	})
}

// ReorderNavs 在一个事务内批量写入 parentId / sort（拖拽排序与改上级共用）。
// sort 为 0 时按 items 顺序补 10、20…，兼容只传 id 的调用方。
func (s *Service) ReorderNavs(ctx context.Context, items []NavOrderItem) error {
	if len(items) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []Nav
		if err := tx.Select("id", "parent_id").Find(&rows).Error; err != nil {
			return err
		}
		parents := navParentMap(rows)
		for i := range items {
			if _, ok := parents[uint64(items[i].ID)]; !ok {
				return navBadRequest("website.nav_not_found")
			}
			if items[i].Sort == 0 {
				items[i].Sort = (i + 1) * 10
			}
			parents[uint64(items[i].ID)] = uint64(items[i].ParentID)
		}
		if err := validateNavParents(parents); err != nil {
			return err
		}
		for _, it := range items {
			if err := tx.Model(&Nav{}).Where("id = ?", int64(it.ID)).
				Updates(map[string]any{"parent_id": it.ParentID, "sort": it.Sort}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- 特性卡片 ----

func (s *Service) ListFeatures(ctx context.Context) ([]Feature, error) {
	var rows []Feature
	err := s.db.WithContext(ctx).Order("sort, id").Find(&rows).Error
	return rows, err
}

func (s *Service) SaveFeature(ctx context.Context, id idgen.ID, f FeatureForm) (*Feature, error) {
	f.Title = trimLen(f.Title, 128)
	f.Description = trimLen(f.Description, 500)
	f.Icon = trimLen(f.Icon, 64)
	if f.Title == "" {
		return nil, response.BadRequest("website.feature_title_required")
	}
	var row Feature
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
	} else {
		row = Feature{}
	}
	i18n, err := pi18n.Normalize(f.I18nJSON, siteLocales{}, featureI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	row.Icon, row.Title, row.Description, row.Sort, row.Enabled, row.I18nJSON = f.Icon, f.Title, f.Description, f.Sort, f.Enabled, i18n
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Service) DeleteFeature(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Delete(&Feature{}, "id = ?", int64(id)).Error
}

// ---- 页面 / 新闻共用的列表查询 ----

// ContentQuery 是页面/新闻的列表查询。
type ContentQuery struct {
	Keyword string
	Status  string // "" | "published" | "draft"
	// Missing 是语言码：筛「该语言完全没有译文」的条目。不是「缺某个字段」，
	// 那种细粒度要靠译文 JSON 里的键判断，跨引擎不通用，交给界面上的 done/total 徽标。
	Missing  string
	Page     int
	PageSize int
}

func contentTx(tx *gorm.DB, q ContentQuery) *gorm.DB {
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		tx = tx.Where("title LIKE ? OR slug LIKE ?", "%"+kw+"%", "%"+kw+"%")
	}
	switch q.Status {
	case "published":
		tx = tx.Where("published = ?", true)
	case "draft":
		tx = tx.Where("published = ?", false)
	}
	if code := strings.TrimSpace(q.Missing); code != "" {
		// 只接受已配置的语言，并且用规范 code 去匹配：拼出来的是 LIKE 的**参数值**，
		// 走占位符绑定，不进 SQL 文本；未知 code 就当没有这个筛选。
		if l, ok := LanguageByCode(code); ok {
			tx = tx.Where("i18n_json IS NULL OR i18n_json = '' OR i18n_json NOT LIKE ?", "%\""+l.Code+"\":%")
		}
	}
	return tx
}

func (s *Service) ListPages(ctx context.Context, q ContentQuery) ([]Page, int64, error) {
	tx := contentTx(s.db.WithContext(ctx).Model(&Page{}), q)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Page
	if err := tx.Order("updated_at DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *Service) SavePage(ctx context.Context, id idgen.ID, f PageForm) (*Page, error) {
	f.Slug = strings.TrimSpace(f.Slug)
	f.Title = trimLen(f.Title, 200)
	f.SeoDesc = trimLen(f.SeoDesc, 300)
	if err := ValidateSlug(f.Slug); err != nil {
		return nil, err
	}
	if f.Title == "" {
		return nil, response.BadRequest("website.page_title_required")
	}
	var row Page
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
	} else {
		row = Page{}
	}
	i18n, err := pi18n.Normalize(f.I18nJSON, siteLocales{}, pageI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	row.Slug, row.Title, row.SeoDesc = f.Slug, f.Title, f.SeoDesc
	row.Content, row.Published, row.I18nJSON = htmlsafe.Sanitize(f.Content), f.Published, i18n
	pub, err := pi18n.NormalizePublish(f.PublishedJSON, f.Published, pi18n.ParseDoc(i18n),
		map[string]string{"title": row.Title, "seoDesc": row.SeoDesc, "content": row.Content}, siteLocales{}, pageI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	row.PublishedJSON = pub
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Service) DeletePage(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Delete(&Page{}, "id = ?", int64(id)).Error
}

func (s *Service) ListPosts(ctx context.Context, q ContentQuery) ([]Post, int64, error) {
	tx := contentTx(s.db.WithContext(ctx).Model(&Post{}), q)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Post
	if err := tx.Order("updated_at DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *Service) SavePost(ctx context.Context, id idgen.ID, f PostForm) (*Post, error) {
	f.Slug = strings.TrimSpace(f.Slug)
	f.Title = trimLen(f.Title, 200)
	f.Summary = trimLen(f.Summary, 500)
	f.SeoDesc = trimLen(f.SeoDesc, 300)
	f.CoverURL = trimLen(f.CoverURL, 255)
	if err := ValidateSlug(f.Slug); err != nil {
		return nil, err
	}
	if f.Title == "" {
		return nil, response.BadRequest("website.post_title_required")
	}
	var row Post
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
	} else {
		row = Post{}
	}
	wasPublished := row.Published
	i18n, err := pi18n.Normalize(f.I18nJSON, siteLocales{}, postI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	row.Slug, row.Title, row.CoverURL, row.Summary, row.SeoDesc = f.Slug, f.Title, f.CoverURL, f.Summary, f.SeoDesc
	row.Content, row.Published, row.I18nJSON = htmlsafe.Sanitize(f.Content), f.Published, i18n
	pub, err := pi18n.NormalizePublish(f.PublishedJSON, f.Published, pi18n.ParseDoc(i18n),
		map[string]string{"title": row.Title, "summary": row.Summary, "seoDesc": row.SeoDesc, "content": row.Content}, siteLocales{}, postI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	row.PublishedJSON = pub
	if f.Published {
		switch {
		case f.PublishedAt != nil:
			// 手动指定：回填历史文章的真实发布时间，或修正误发时间。
			t := f.PublishedAt.UTC()
			row.PublishedAt = &t
		case !wasPublished || row.PublishedAt == nil:
			now := time.Now().UTC()
			row.PublishedAt = &now
		}
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Service) DeletePost(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Delete(&Post{}, "id = ?", int64(id)).Error
}

// ---- 公开页数据装配 ----

// SiteData 汇总渲染官网所需的全部启用内容。
type SiteData struct {
	Setting  *Setting
	Navs     []Nav // 已组好的导航树：父级不可见的节点连同子树已丢弃
	Features []Feature
	Posts    []Post // 最新已发布新闻（首页取 3 条，列表页取 20 条）
}

func (s *Service) SiteData(ctx context.Context, postLimit int, lang string) (*SiteData, error) {
	setting, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	setting.localize(lang)
	var navs []Nav
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("sort, id").Find(&navs).Error; err != nil {
		return nil, err
	}
	// 历史数据里可能有裸路径或后台路由（保存早于校验），公开站不渲染官网服务不了的链接。
	kept := navs[:0]
	for _, n := range navs {
		if navHrefValid(n.Href) {
			kept = append(kept, n)
		}
	}
	navs = kept
	for i := range navs {
		navs[i].localize(lang)
	}
	// Navs 是树：父级不可见的节点整棵丢弃，模板只按一级遍历。
	navs = buildNavTree(navs)
	var features []Feature
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("sort, id").Find(&features).Error; err != nil {
		return nil, err
	}
	for i := range features {
		features[i].localize(lang)
	}
	var posts []Post
	// 按语言发布不是 SQL 谓词，只能先取一个有界窗口再在 Go 里筛。
	// 多取一些，保证筛完仍够 postLimit 条（数据充足时），也不会漏掉整页。
	window := postLimit
	if len(Languages()) > 1 {
		window = postLimit * 4
		if window < 60 {
			window = 60
		}
	}
	if err := s.db.WithContext(ctx).Where("published = ?", true).
		Order("published_at DESC").Limit(window).Find(&posts).Error; err != nil {
		return nil, err
	}
	if len(Languages()) > 1 {
		kept := make([]Post, 0, postLimit)
		for _, p := range posts {
			if !pi18n.Published(p.Published, p.PublishedJSON, lang, siteLocales{}) {
				continue
			}
			kept = append(kept, p)
			if len(kept) == postLimit {
				break
			}
		}
		posts = kept
	}
	for i := range posts {
		posts[i].localize(lang)
	}
	return &SiteData{Setting: setting, Navs: navs, Features: features, Posts: posts}, nil
}

// PublishedPostBy 取一篇已发布的新闻；未发布或不存在返回 gorm.ErrRecordNotFound。
func (s *Service) PublishedPostBy(ctx context.Context, slug, lang string) (*Post, error) {
	var row Post
	err := s.db.WithContext(ctx).Where("slug = ? AND published = ?", slug, true).First(&row).Error
	if err != nil {
		return &row, err
	}
	// 整行已发布不等于每种语言都已发布：该语言未发布就当作不存在，让它走 404，
	// 而不是把一篇中文页挂在 /ja/ 下面。
	if !pi18n.Published(row.Published, row.PublishedJSON, lang, siteLocales{}) {
		return nil, gorm.ErrRecordNotFound
	}
	row.localize(lang)
	return &row, nil
}

// PublishedPageBy 取一篇已发布的自定义页。
func (s *Service) PublishedPageBy(ctx context.Context, slug, lang string) (*Page, error) {
	var row Page
	err := s.db.WithContext(ctx).Where("slug = ? AND published = ?", slug, true).First(&row).Error
	if err != nil {
		return &row, err
	}
	if !pi18n.Published(row.Published, row.PublishedJSON, lang, siteLocales{}) {
		return nil, gorm.ErrRecordNotFound
	}
	row.localize(lang)
	return &row, nil
}

// ---- 媒体（logo / 封面小图） ----

// 上传限制：只收位图（svg 可携带脚本，不当图片存储）；单文件上限 2MB。
const mediaMaxBytes = 2 << 20

var mediaMimes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true,
}

// SaveMedia 校验并保存一张小图，返回可公开访问的 URL 路径。
// 类型按内容嗅探判定（http.DetectContentType），不信任扩展名。
func (s *Service) SaveMedia(ctx context.Context, data []byte) (*Media, error) {
	if len(data) == 0 {
		return nil, response.BadRequest("website.file_empty")
	}
	if len(data) > mediaMaxBytes {
		return nil, response.BadRequest("website.image_too_large")
	}
	mime := http.DetectContentType(data)
	if !mediaMimes[mime] {
		return nil, response.BadRequest("website.image_type_unsupported")
	}
	m := Media{Mime: mime, Size: len(data), Data: data}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// MediaByID 取一个媒体文件（含内容）。
func (s *Service) MediaByID(ctx context.Context, id idgen.ID) (*Media, error) {
	var m Media
	if err := s.db.WithContext(ctx).First(&m, "id = ?", int64(id)).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ---- sitemap ----

// SitemapEntry 是一条 sitemap 记录；Loc 是站内路径，Lastmod 可空。
type SitemapEntry struct {
	Loc     string
	Lastmod *time.Time
	// Locales 是该 URL 已发布的语言码；nil = 语言完备（首页、列表、文档这类不随内容缺译的页）。
	// 内容页必须给出，否则 sitemap 会为未发布的语言生成一条其实 404 的 alternate。
	Locales []string
}

// visibleLocales 把 Locales 还原成配置顺序的 Language 列表；nil 表示不过滤。
func (e SitemapEntry) visibleLocales(all []Language) []Language {
	if e.Locales == nil {
		return all
	}
	set := make(map[string]bool, len(e.Locales))
	for _, c := range e.Locales {
		set[c] = true
	}
	out := make([]Language, 0, len(e.Locales))
	for _, l := range all {
		if set[l.Code] {
			out = append(out, l)
		}
	}
	return out
}

// publishedLocales 汇总这条内容实际已发布的语言码。
func publishedLocales(published bool, publishedJSON string) []string {
	vis := visibleLanguages(published, publishedJSON)
	codes := make([]string, 0, len(vis))
	for _, l := range vis {
		codes = append(codes, l.Code)
	}
	return codes
}

// SitemapEntries 汇总官网全部已发布页面（首页、新闻列表、新闻、自定义页）。
func (s *Service) SitemapEntries(ctx context.Context) ([]SitemapEntry, error) {
	out := []SitemapEntry{{Loc: "/"}, {Loc: "/news"}}
	var posts []Post
	if err := s.db.WithContext(ctx).Where("published = ?", true).
		Select("slug", "published_at", "updated_at", "published_json").Order("published_at DESC").Limit(1000).Find(&posts).Error; err != nil {
		return nil, err
	}
	for _, p := range posts {
		e := SitemapEntry{Loc: "/news/" + p.Slug, Lastmod: p.PublishedAt, Locales: publishedLocales(p.Published, p.PublishedJSON)}
		if e.Lastmod == nil {
			e.Lastmod = &p.UpdatedAt
		}
		out = append(out, e)
	}
	var pages []Page
	if err := s.db.WithContext(ctx).Where("published = ?", true).
		Select("slug", "updated_at", "published_json").Order("updated_at DESC").Limit(1000).Find(&pages).Error; err != nil {
		return nil, err
	}
	for _, p := range pages {
		out = append(out, SitemapEntry{
			Loc: "/p/" + p.Slug, Lastmod: &p.UpdatedAt,
			Locales: publishedLocales(p.Published, p.PublishedJSON),
		})
	}
	return out, nil
}

// ---- 市场统计与评分 ----

func ratingVoter(pluginID, ip string) string {
	sum := sha256.Sum256([]byte(pluginID + "|" + ip))
	return hex.EncodeToString(sum[:])
}

// MarketStats 返回所有插件的下载与评分汇总。
func (s *Service) MarketStats(ctx context.Context) (map[string]MarketStat, error) {
	var rows []MarketStat
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]MarketStat, len(rows))
	for _, r := range rows {
		out[r.PluginID] = r
	}
	return out, nil
}

// IncrementDownload 记录一次公开下载。失败只影响统计，不影响下载本身。
func (s *Service) IncrementDownload(ctx context.Context, pluginID string) error {
	return bumpMarketStat(s.db.WithContext(ctx), pluginID, 1, 0, 0)
}

// Rate 记录一次匿名评分：同一 IP 对同一插件只有一行，重复评分会替换旧分。
func (s *Service) Rate(ctx context.Context, pluginID, ip string, score int) error {
	if score < 1 || score > 5 {
		return fmt.Errorf("评分必须在 1-5 之间")
	}
	voter := ratingVoter(pluginID, ip)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old MarketRating
		err := tx.Where("plugin_id = ? AND voter = ?", pluginID, voter).First(&old).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			row := MarketRating{PluginID: pluginID, Voter: voter, Score: score}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			return bumpMarketStat(tx, pluginID, 0, int64(score), 1)
		case err != nil:
			return err
		case old.Score == score:
			return nil
		default:
			// 先算增量：GORM 的 Update 会同步改写 old.Score，之后再算会得到 0。
			delta := score - old.Score
			if err := tx.Model(&old).Update("score", score).Error; err != nil {
				return err
			}
			return bumpMarketStat(tx, pluginID, 0, int64(delta), 0)
		}
	})
}

// MyRating 返回该 IP 对插件的评分，0 表示未评分。
func (s *Service) MyRating(ctx context.Context, pluginID, ip string) int {
	var row MarketRating
	err := s.db.WithContext(ctx).Select("score").
		Where("plugin_id = ? AND voter = ?", pluginID, ratingVoter(pluginID, ip)).First(&row).Error
	if err != nil {
		return 0
	}
	return row.Score
}

// bumpMarketStat 原子累加统计；不存在时插入一行。
func bumpMarketStat(db *gorm.DB, pluginID string, downloads, sum, count int64) error {
	if downloads == 0 && sum == 0 && count == 0 {
		return nil
	}
	row := MarketStat{PluginID: pluginID, Downloads: downloads, RatingSum: sum, RatingCount: count}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "plugin_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"downloads":    gorm.Expr("downloads + ?", downloads),
			"rating_sum":   gorm.Expr("rating_sum + ?", sum),
			"rating_count": gorm.Expr("rating_count + ?", count),
			"updated_at":   time.Now(),
		}),
	}).Create(&row).Error
}

// ---- 在线留言 ----

// emailReg 比 net/mail 严格一档：ParseAddress 接受 a@b 这种无域名后缀的地址。
var emailReg = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// CreateMessage 保存一条匿名留言。限流与蜜罐在 HTTP 层，这里只校验内容。
func (s *Service) CreateMessage(ctx context.Context, f ContactForm, ip, ua string) (*Message, error) {
	name := trimRunes(f.Name, 64)
	email := trimRunes(f.Email, 128)
	phone := trimRunes(f.Phone, 32)
	content := trimRunes(f.Content, 2000)
	if name == "" {
		return nil, response.BadRequest("website.contact_name_required")
	}
	if _, err := mail.ParseAddress(email); err != nil || !emailReg.MatchString(email) {
		return nil, response.BadRequest("website.contact_email_invalid")
	}
	if utf8.RuneCountInString(content) < 5 {
		return nil, response.BadRequest("website.contact_content_short")
	}
	row := &Message{
		Name: name, Email: email, Phone: phone, Content: content,
		IP: trimLen(ip, 64), UA: trimLen(ua, 255),
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// ListMessages 是后台留言列表：按时间倒序，可按未读与关键字筛。
func (s *Service) ListMessages(ctx context.Context, q MessageQuery) ([]Message, int64, error) {
	tx := s.db.WithContext(ctx).Model(&Message{})
	if q.Unread {
		tx = tx.Where("is_read = ?", false)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("name LIKE ? OR email LIKE ? OR content LIKE ?", like, like, like)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Message
	if err := tx.Order("created_at DESC, id DESC").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// UnreadMessages 返回未读数（后台页签角标用）。
func (s *Service) UnreadMessages(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Message{}).Where("is_read = ?", false).Count(&n).Error
	return n, err
}

// SetMessageRead 标记已读/未读；留言不存在时返回 404（而不是静默成功）。
func (s *Service) SetMessageRead(ctx context.Context, id idgen.ID, read bool) error {
	res := s.db.WithContext(ctx).Model(&Message{}).Where("id = ?", int64(id)).Update("is_read", read)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return response.NotFound("website.message_not_found")
	}
	return nil
}

// DeleteMessage 删除一条留言。
func (s *Service) DeleteMessage(ctx context.Context, id idgen.ID) error {
	res := s.db.WithContext(ctx).Delete(&Message{}, "id = ?", int64(id))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return response.NotFound("website.message_not_found")
	}
	return nil
}
