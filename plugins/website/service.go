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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/response"
	"nexadmin/pkg/htmlsafe"
	pi18n "nexadmin/pkg/i18n"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/slug"
)

// Service 是官网内容业务层：站点设置（单行）、导航、特性、页面、新闻的 CRUD 与公开页数据装配。
type Service struct {
	db *gorm.DB
	// cfgImageMaxMB 是 config.yaml 的 upload.max_size_image_mb（<=0 = 未配），由模块注册时登记，
	// 系统参数「上传限制」没设时用它兜底。
	cfgImageMaxMB float64
	// cache 是共享缓存（阿里类目树等外部数据）；nil 时直连不缓存。
	cache cache.Cache
}

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// SetCache 登记共享缓存（可选）。
func (s *Service) SetCache(c cache.Cache) { s.cache = c }

// SetConfigImageMaxMB 登记 config.yaml 的图片大小上限（MB）作为媒体上传的兜底值。
func (s *Service) SetConfigImageMaxMB(mb int) { s.cfgImageMaxMB = float64(mb) }

// slugReg 允许小写字母、数字、连字符。
var slugReg = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// reservedSlugs 只保留插件自己的路由段（/news、/products、/p）。页面、新闻与产品分别挂在
// /p/:slug、/news/:slug 与 /products/:slug 下，天然带命名空间，不会与后台 SPA 路径
//（/about、/system 等）冲突，因此后台路径不必保留——about、login-guide 这类自然 slug 都允许。
var reservedSlugs = map[string]bool{"news": true, "p": true, "products": true}

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
var socialPlatforms = []string{
	"github", "x", "linkedin", "youtube", "instagram", "facebook", "discord", "telegram",
	"whatsapp", "reddit", "tiktok", "twitch", "mastodon", "wechat", "weibo", "bilibili", "rss", "mail",
}

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
	row.FaviconURL = trimLen(f.FaviconURL, 255)
	row.OgImageURL = trimLen(f.OgImageURL, 255)
	if row.FaviconURL != "" && !footerURLValid(row.FaviconURL, false) {
		return nil, response.BadRequest("website.url_invalid")
	}
	if row.OgImageURL != "" && !footerURLValid(row.OgImageURL, false) {
		return nil, response.BadRequest("website.url_invalid")
	}
	// head 注入限长：统计脚本几十行足够，超长多半是误把整页代码粘进来
	row.HeadCode = trimLen(f.HeadCode, 5000)
	row.TitleSuffix = trimLen(f.TitleSuffix, 100)
	// 设为首页只存 slug 不存 ID：页面被删时首页自动回退内置首页，不留悬空引用。
	// 校验只要求页面存在（草稿也行——发布后首页即生效）；下线后在首页自动回退，不用改设置。
	row.HomeSlug = trimLen(f.HomeSlug, 64)
	if row.HomeSlug != "" {
		var n int64
		if err := s.db.WithContext(ctx).Model(&Page{}).Where("slug = ?", row.HomeSlug).Count(&n).Error; err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, response.BadRequest("website.home_slug_missing")
		}
	}
	// 主题只存 id：主题插件被卸载时渲染自动回退 default（站点不断线），
	// 保存时校验当前进程里已注册，防止手滑把名字敲错导致设置「看起来生效其实没换」。
	row.Theme = trimLen(f.Theme, 64)
	if !themeRegistered(row.Theme) {
		return nil, response.BadRequest("website.theme_not_found")
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
	if f.MessageNotifyEmail != nil {
		row.MessageNotifyEmail = *f.MessageNotifyEmail
	}
	if f.ContactConsent != nil {
		row.ContactConsent = *f.ContactConsent
	}
	row.ContactConsentURL = trimLen(f.ContactConsentURL, 255)
	// 开关开着就必须有可点的政策地址：勾选项没有正文可读，等于让人盲签，合规上站不住。
	if row.ContactConsent && !footerURLValid(row.ContactConsentURL, false) {
		return nil, response.BadRequest("website.contact_consent_url_invalid")
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
	for _, prefix := range []string{"/news", "/products", "/plugins", "/docs", "/contact"} {
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

// DeleteNavs 批量删除导航：每一项连同其全部后代一起删（与单条删除同口径），返回实际删除条数。
func (s *Service) DeleteNavs(ctx context.Context, ids []idgen.ID) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []Nav
		if err := tx.Select("id", "parent_id").Find(&rows).Error; err != nil {
			return err
		}
		parents := navParentMap(rows)
		set := make(map[uint64]bool, len(ids))
		for _, id := range ids {
			for _, v := range navSubtreeIDs(parents, uint64(id)) {
				set[v] = true
			}
		}
		if len(set) == 0 {
			return response.NotFound("website.nav_not_found")
		}
		list := make([]int64, 0, len(set))
		for v := range set {
			list = append(list, int64(v))
		}
		res := tx.Delete(&Nav{}, "id IN ?", list)
		if res.Error != nil {
			return res.Error
		}
		n = res.RowsAffected
		return nil
	})
	return n, err
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
		// 孤儿行（父级已被并发删除/历史脏数据）会卡死整树校验：任何一次拖拽都报
		// nav_parent_missing。先把它们静默洗回顶级，再继续——这与前端 buildNavTree
		// 对孤儿按顶级兜底是同一口径，管理员在列表里也能看见并手动归位。
		known := make(map[uint64]bool, len(rows))
		for _, r := range rows {
			known[uint64(r.ID)] = true
		}
		for _, r := range rows {
			pid := uint64(r.ParentID)
			if pid == 0 || known[pid] {
				continue
			}
			if err := tx.Model(&Nav{}).Where("id = ?", int64(r.ID)).Update("parent_id", 0).Error; err != nil {
				return err
			}
			r.ParentID = 0
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
	if _, ok := iconPaths[f.Icon]; !ok {
		// 图标名只收模板内置集合；未知名（含历史残留）退回空串，官网渲染有 defaultIcon 兜底。
		f.Icon = ""
	}
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

// DeleteFeatures 批量删除特性卡片，返回实际删除条数（语义同 DeletePages）。
func (s *Service) DeleteFeatures(ctx context.Context, ids []idgen.ID) (int64, error) {
	res := s.db.WithContext(ctx).Delete(&Feature{}, "id IN ?", int64IDs(ids))
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, response.NotFound("website.feature_not_found")
	}
	return res.RowsAffected, nil
}

// ---- 页面 / 新闻共用的列表查询 ----

// ContentQuery 是页面/新闻的列表查询。
// 枚举/类目筛选是逗号拼接多值（?status=published,draft），空切片 = 不过滤。
type ContentQuery struct {
	Keyword string
	// Statuses 多值："published" | "draft" | "scheduled"（已发布但 published_at 在未来，到点才公开）。
	Statuses []string
	// Missings 是语言码集合：筛「缺任一所选语言译文」的条目（OR）。不是「缺某个字段」，
	// 那种细粒度要靠译文 JSON 里的键判断，跨引擎不通用，交给界面上的 done/total 徽标。
	Missings []string
	// CategoryIDs 是新闻分类筛选（多选取并集）；页面列表不消费这个字段。
	CategoryIDs []idgen.ID
	Page        int
	PageSize    int
}

// contentStatusTx 追加页面/新闻的状态筛选：三种状态 SQL 语义各不同，多选时按 OR 组合。
func contentStatusTx(tx *gorm.DB, statuses []string) *gorm.DB {
	conds, args := []string{}, []any{}
	seen := map[string]bool{}
	for _, st := range statuses {
		if seen[st] {
			continue
		}
		seen[st] = true
		switch st {
		case "published":
			conds = append(conds, "published = ?")
			args = append(args, true)
		case "draft":
			conds = append(conds, "published = ?")
			args = append(args, false)
		case "scheduled":
			// 定时发布：整篇已置发布、但发布时间还在未来。
			conds = append(conds, "(published = ? AND published_at IS NOT NULL AND published_at > ?)")
			args = append(args, true, time.Now().UTC())
		}
	}
	if len(conds) > 0 {
		tx = tx.Where("("+strings.Join(conds, " OR ")+")", args...)
	}
	return tx
}

// missingTx 追加「缺译文语言」筛选：只接受已配置的语言，并用规范 code 匹配。拼出来的是
// LIKE 的**参数值**，走占位符绑定，不进 SQL 文本；未知 code 就当没有这个取值。
func missingTx(tx *gorm.DB, codes []string) *gorm.DB {
	conds, args := []string{}, []any{}
	for _, code := range codes {
		l, ok := LanguageByCode(code)
		if !ok {
			continue
		}
		// nexadmin:like-exempt 非关键词筛选：匹配的是配置里的语言 code，两侧同源、
		// 大小写天然一致（不是人随手打的大写），折叠没有意义。见 plugins/like_case_insensitive_test.go。
		conds = append(conds, "i18n_json IS NULL OR i18n_json = '' OR i18n_json NOT LIKE ?")
		args = append(args, "%\""+l.Code+":\"%")
	}
	if len(conds) > 0 {
		tx = tx.Where("("+strings.Join(conds, " OR ")+")", args...)
	}
	return tx
}

func contentTx(tx *gorm.DB, q ContentQuery) *gorm.DB {
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		tx = tx.Where("LOWER(title) LIKE LOWER(?) OR LOWER(slug) LIKE LOWER(?)", "%"+kw+"%", "%"+kw+"%")
	}
	tx = contentStatusTx(tx, q.Statuses)
	// 分类筛选只对新闻生效（关联表固定，无用户输入拼接）；多类目取并集。
	if len(q.CategoryIDs) > 0 {
		ids := make([]int64, 0, len(q.CategoryIDs))
		for _, id := range q.CategoryIDs {
			ids = append(ids, int64(id))
		}
		tx = tx.Where("id IN (SELECT post_id FROM plg_website_post_category WHERE category_id IN ?)", ids)
	}
	tx = missingTx(tx, q.Missings)
	return tx
}

func (s *Service) ListPages(ctx context.Context, q ContentQuery) ([]Page, int64, error) {
	tx := contentTx(s.db.WithContext(ctx).Model(&Page{}), q)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Page
	// 排序字段在前（0 = 未设置排在最后），同序号内按更新时间倒序。
	if err := tx.Order("CASE WHEN sort > 0 THEN 0 ELSE 1 END, sort ASC, updated_at DESC, id DESC").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *Service) SavePage(ctx context.Context, id idgen.ID, f PageForm) (*Page, error) {
	f.Slug = strings.TrimSpace(f.Slug)
	f.Title = trimLen(f.Title, 200)
	f.SeoTitle = trimLen(f.SeoTitle, 200)
	f.SeoDesc = trimLen(f.SeoDesc, 300)
	f.OgImage = trimLen(f.OgImage, 255)
	if f.Title == "" {
		return nil, response.BadRequest("website.page_title_required")
	}
	slugOut, err := s.resolveSlug(ctx, &Page{}, f.Slug, f.Title, id)
	if err != nil {
		return nil, err
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
	if slugOut != "" {
		row.Slug = slugOut
	}
	row.Title, row.SeoTitle, row.SeoDesc = f.Title, f.SeoTitle, f.SeoDesc
	row.OgImage, row.Noindex, row.Sort = f.OgImage, f.NoIndex, f.Sort
	row.Content, row.Published, row.I18nJSON = htmlsafe.Sanitize(f.Content), f.Published, i18n
	pub, err := pi18n.NormalizePublish(f.PublishedJSON, f.Published, pi18n.ParseDoc(i18n),
		map[string]string{"title": row.Title, "seoTitle": row.SeoTitle, "seoDesc": row.SeoDesc, "content": row.Content}, siteLocales{}, pageI18nFields)
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

// SetPagesStatus 批量发布或下线单页，返回实际改动条数。语义与新闻批量一致：
// 只动原文发布位，各语言的发布开关（published_json）不碰。
func (s *Service) SetPagesStatus(ctx context.Context, ids []idgen.ID, status string) (int64, error) {
	if len(ids) == 0 {
		return 0, response.BadRequest("website.page_ids_empty")
	}
	tx := s.db.WithContext(ctx).Model(&Page{}).Where("id IN ?", int64IDs(ids))
	switch status {
	case "published":
		tx = tx.Where("published = ?", false).Update("published", true)
	case "draft":
		tx = tx.Where("published = ?", true).Update("published", false)
	default:
		return 0, response.BadRequest("website.page_status_invalid")
	}
	return tx.RowsAffected, tx.Error
}

// DeletePages 批量删除单页，返回实际删除条数。
func (s *Service) DeletePages(ctx context.Context, ids []idgen.ID) (int64, error) {
	res := s.db.WithContext(ctx).Delete(&Page{}, "id IN ?", int64IDs(ids))
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, response.NotFound("website.page_not_found")
	}
	return res.RowsAffected, nil
}

// PageByID 按主键取页面（含草稿）；管理端签发预览链接用。
func (s *Service) PageByID(ctx context.Context, id idgen.ID) (*Page, error) {
	var row Page
	if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// PageBySlug 按 slug 取页面（不看发布态）并按语言落译文。预览链路用：
// 发布校验由调用方决定（PublishedPageBy 管公开访问，预览令牌管例外放行）。
func (s *Service) PageBySlug(ctx context.Context, slug, lang string) (*Page, error) {
	var row Page
	if err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&row).Error; err != nil {
		return nil, err
	}
	row.localize(lang)
	return &row, nil
}

// CopyPage 复制一篇页面：内容与译文原样带走，发布态归零（副本一律是草稿，
// 不会顺手把整份多语言内容顶上线），slug 取 <slug>-copy，撞车递增为 -copy-2、-copy-3…。
func (s *Service) CopyPage(ctx context.Context, id idgen.ID) (*Page, error) {
	var src Page
	if err := s.db.WithContext(ctx).First(&src, "id = ?", int64(id)).Error; err != nil {
		return nil, err
	}
	slug, err := s.freePageSlug(ctx, src.Slug)
	if err != nil {
		return nil, err
	}
	row := Page{
		Slug: slug, Title: src.Title, SeoTitle: src.SeoTitle, SeoDesc: src.SeoDesc, Content: src.Content,
		OgImage: src.OgImage, I18nJSON: src.I18nJSON,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// freePageSlug 给副本挑一个没被占用的 slug；slug 列上限 64，放不下后缀就先截短原文
// （base 本身只含合法字符，截短不破坏 slug 规则）。
func (s *Service) freePageSlug(ctx context.Context, base string) (string, error) {
	candidate := truncateSlug(base+"-copy", "page")
	for i := 2; ; i++ {
		var n int64
		if err := s.db.WithContext(ctx).Model(&Page{}).Where("slug = ?", candidate).Count(&n).Error; err != nil {
			return "", err
		}
		if n == 0 {
			return candidate, nil
		}
		candidate = truncateSlug(fmt.Sprintf("%s-copy-%d", base, i), "page")
	}
}

// truncateSlug 把 slug 收进列宽（64）内；截到空串就回退 fallback，保证仍是合法 slug。
// 规则统一收敛在 pkg/slug（行业标准：限长、去尾部悬空连字符），这里只是旧调用点的别名。
func truncateSlug(s, fallback string) string {
	return slug.Clamp(s, slug.MaxLen, fallback)
}

func (s *Service) ListPosts(ctx context.Context, q ContentQuery) ([]Post, int64, error) {
	tx := contentTx(s.db.WithContext(ctx).Model(&Post{}), q)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Post
	// 列表要展示分类徽标，整页一次带出；关联行数远小于页面行数，不构成 N+1。
	if err := tx.Preload("Categories").Preload("Tags").Order("updated_at DESC").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *Service) SavePost(ctx context.Context, id idgen.ID, f PostForm) (*Post, error) {
	f.Slug = strings.TrimSpace(f.Slug)
	f.Title = trimLen(f.Title, 200)
	f.Summary = trimLen(f.Summary, 500)
	f.SeoTitle = trimLen(f.SeoTitle, 200)
	f.SeoDesc = trimLen(f.SeoDesc, 300)
	f.SeoKeywords = trimLen(f.SeoKeywords, 300)
	f.CoverURL = trimLen(f.CoverURL, 255)
	if f.Title == "" {
		return nil, response.BadRequest("website.post_title_required")
	}
	slugOut, err := s.resolveSlug(ctx, &Post{}, f.Slug, f.Title, id)
	if err != nil {
		return nil, err
	}
	// 标签上限先挡住：超出的部分静默丢弃会让人以为都打上了。
	if len(f.Tags) > maxTagsPerPost {
		return nil, response.BadRequest("website.tag_too_many", maxTagsPerPost)
	}
	// 分类只认已存在的：并发下被删掉的分类静默剔除，保存后回读的真实关联就是界面的下一帧。
	var cats []Category
	if len(f.CategoryIDs) > 0 {
		ids := make([]int64, 0, len(f.CategoryIDs))
		for _, c := range f.CategoryIDs {
			if c > 0 {
				ids = append(ids, int64(c))
			}
		}
		if len(ids) > 0 {
			if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&cats).Error; err != nil {
				return nil, err
			}
		}
	}
	var row Post
	updating := id != 0
	var prev Post // 覆盖前的旧行快照：进事务后先归档再覆盖，改坏了能回滚
	if updating {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
		prev = row
	} else {
		row = Post{}
	}
	wasPublished := row.Published
	i18n, err := pi18n.Normalize(f.I18nJSON, siteLocales{}, postI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	if slugOut != "" {
		row.Slug = slugOut
	}
	row.Title, row.CoverURL, row.SeoDesc = f.Title, f.CoverURL, f.SeoDesc
	row.SeoKeywords, row.SeoTitle, row.Noindex = f.SeoKeywords, f.SeoTitle, f.NoIndex
	row.Author, row.Pinned = trimLen(f.Author, 64), f.Pinned
	row.Content, row.Published, row.I18nJSON = htmlsafe.Sanitize(f.Content), f.Published, i18n
	// 摘要留空时从正文截一段：竞品的入门体验，别让每篇文章都得手写摘要。
	if f.Summary == "" && row.Content != "" {
		f.Summary = summaryFromHTML(row.Content, summaryMaxRunes)
	}
	row.Summary = f.Summary
	pub, err := pi18n.NormalizePublish(f.PublishedJSON, f.Published, pi18n.ParseDoc(i18n),
		map[string]string{"title": row.Title, "summary": row.Summary, "seoTitle": row.SeoTitle, "seoDesc": row.SeoDesc, "content": row.Content}, siteLocales{}, postI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	row.PublishedJSON = pub
	if f.Published {
		switch {
		case f.PublishedAt != nil:
			// 手动指定：回填历史文章的真实发布时间、修正误发，或设未来时间做定时发布。
			t := f.PublishedAt.UTC()
			row.PublishedAt = &t
		case !wasPublished || row.PublishedAt == nil:
			now := time.Now().UTC()
			row.PublishedAt = &now
		}
	}
	// 正文与关联一起写：分类挂一半算保存失败，界面上重新来一遍比解释一半成功容易。
	// 标签也在事务里找或建：新闻没存上却长出一个新标签，是没人会来清理的脏数据。
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 覆盖前留档（同 workflow 定义版本）：旧行进版本历史表，正文没变不重复存。
		if updating {
			if _, err := archivePostVersion(ctx, tx, &prev, row.Content); err != nil {
				return err
			}
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := tx.Model(&row).Association("Categories").Replace(cats); err != nil {
			return err
		}
		tags, terr := resolveTagsTx(tx, f.Tags)
		if terr != nil {
			return terr
		}
		if err := tx.Model(&row).Association("Tags").Replace(tags); err != nil {
			return err
		}
		row.Tags = tags
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// summaryMaxRunes 是自动摘要的截取长度（rune 数）：列表页一行简介的量级。
const summaryMaxRunes = 120

// plainFromHTML 从消毒后的富文本里剥出纯文本、压掉空白并还原转义实体。
// 输入已经过 htmlsafe.Sanitize，剩下的实体只剩转义文本（&amp; 之类），顺手还原。
// 自动摘要与版本历史的字数统计共用这一份剥离逻辑，两边口径不漂移。
func plainFromHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
			b.WriteRune(' ') // 块级标签的边界退化为一个空格，词与词不粘连
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	plain := strings.Join(strings.Fields(b.String()), " ")
	return strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(plain)
}

// summaryFromHTML 从消毒后的富文本里剥纯文本，截 max 个 rune 当摘要。
func summaryFromHTML(s string, max int) string {
	plain := plainFromHTML(s)
	runes := []rune(plain)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return plain
}

// DeletePost 删除新闻并级联清掉它的版本历史与分类/标签关联：彻底删除不留孤儿。
// 关联表不清的话，分类/标签的「新闻数」计数（join 行 COUNT）会永久虚高。
func (s *Service) DeletePost(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&Post{}, "id = ?", int64(id)).Error; err != nil {
			return err
		}
		if err := tx.Where("post_id = ?", int64(id)).Delete(&PostVersion{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM plg_website_post_category WHERE post_id = ?", int64(id)).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM plg_website_post_tag WHERE post_id = ?", int64(id)).Error
	})
}

// ---- 新闻分类 ----

// ListCategories 带每个栏目当前的新闻数：界面要按「这个栏目下有多少篇」排栏目，
// 删栏目之前也得让人先看见影响面。
func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
	var rows []Category
	if err := s.db.WithContext(ctx).Order("sort, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	counts, err := categoryPostCounts(s.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].PostCount = counts[int64(rows[i].ID)]
	}
	return rows, nil
}

func (s *Service) SaveCategory(ctx context.Context, id idgen.ID, f CategoryForm) (*Category, error) {
	f.Title = trimLen(f.Title, 100)
	f.Slug = strings.TrimSpace(f.Slug)
	if f.Title == "" {
		return nil, response.BadRequest("website.category_title_required")
	}
	slugOut, err := s.resolveSlug(ctx, &Category{}, f.Slug, f.Title, id)
	if err != nil {
		return nil, err
	}
	var row Category
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
	} else {
		row = Category{}
	}
	row.Title, row.Sort = f.Title, f.Sort
	if slugOut != "" {
		row.Slug = slugOut
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteCategory 连同新闻-分类关联一起删：留着悬空关联会让列表的分类徽标显示幽灵分类。
func (s *Service) DeleteCategory(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&Category{}, "id = ?", int64(id)).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM plg_website_post_category WHERE category_id = ?", int64(id)).Error
	})
}

// ---- 新闻批量操作 / 导出 ----

// SetPostsStatus 批量发布或下线新闻，返回实际改动条数。
// 发布时只补齐空的发布时间：定时文章的未来时间不动（批量发布不是「立即上线」的强制令），
// 各语言的发布开关（published_json）也不动——那是对编辑工作成果的按语言裁剪，批量动作不该覆盖。
func (s *Service) SetPostsStatus(ctx context.Context, ids []idgen.ID, status string) (int64, error) {
	if len(ids) == 0 {
		return 0, response.BadRequest("website.post_ids_empty")
	}
	tx := s.db.WithContext(ctx).Model(&Post{}).Where("id IN ?", int64IDs(ids))
	switch status {
	case "published":
		tx = tx.Where("published = ?", false).Updates(map[string]any{
			"published":    true,
			"published_at": gorm.Expr("COALESCE(published_at, ?)", time.Now().UTC()),
		})
	case "draft":
		// 原文下线 = 整篇下线（含全部语言），语义与单条保存一致。
		tx = tx.Where("published = ?", true).Update("published", false)
	default:
		return 0, response.BadRequest("website.post_status_invalid")
	}
	return tx.RowsAffected, tx.Error
}

// DeletePosts 批量删除新闻，返回实际删除条数；部分没命中是并发下的常态，如实返回。
// 与单条删除同口径：版本历史与分类/标签关联级联清掉，不留孤儿。
func (s *Service) DeletePosts(ctx context.Context, ids []idgen.ID) (int64, error) {
	if len(ids) == 0 {
		return 0, response.BadRequest("website.post_ids_empty")
	}
	var deleted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&Post{}, "id IN ?", int64IDs(ids))
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		if deleted == 0 {
			return nil
		}
		if err := tx.Where("post_id IN ?", int64IDs(ids)).Delete(&PostVersion{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM plg_website_post_category WHERE post_id IN ?", int64IDs(ids)).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM plg_website_post_tag WHERE post_id IN ?", int64IDs(ids)).Error
	})
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		return 0, response.NotFound("website.post_not_found")
	}
	return deleted, nil
}

// ExportPosts 取当前筛选下的全部新闻（不带分页，上限 exportMax），前端据此生成 CSV；
// 筛选与 ListPosts 同源，导出文件和页面上看到的列表对得上。
func (s *Service) ExportPosts(ctx context.Context, q ContentQuery) ([]Post, error) {
	var rows []Post
	err := contentTx(s.db.WithContext(ctx).Model(&Post{}), q).Preload("Categories").Preload("Tags").
		Order("updated_at DESC, id DESC").Limit(exportMax).Find(&rows).Error
	return rows, err
}

// PostByID 按主键取新闻（含草稿）；管理端签发预览链接用。
func (s *Service) PostByID(ctx context.Context, id idgen.ID) (*Post, error) {
	var row Post
	if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// PostBySlug 按 slug 取新闻（不看发布态）并按语言落译文。预览链路用：
// 发布校验由调用方决定（PublishedPostBy 管公开访问，预览令牌管例外放行）。
func (s *Service) PostBySlug(ctx context.Context, slug, lang string) (*Post, error) {
	var row Post
	if err := s.db.WithContext(ctx).Preload("Categories").Preload("Tags").Where("slug = ?", slug).First(&row).Error; err != nil {
		return nil, err
	}
	row.localize(lang)
	return &row, nil
}

// CopyPost 复制一篇新闻：内容、译文、分类与标签原样带走，发布态归零（副本一律是草稿，
// 不会顺手把整份多语言内容顶上线），slug 取 <slug>-copy，撞车递增为 -copy-2、-copy-3…。
func (s *Service) CopyPost(ctx context.Context, id idgen.ID) (*Post, error) {
	var src Post
	if err := s.db.WithContext(ctx).Preload("Categories").Preload("Tags").First(&src, "id = ?", int64(id)).Error; err != nil {
		return nil, err
	}
	slug, err := s.freePostSlug(ctx, src.Slug)
	if err != nil {
		return nil, err
	}
	row := Post{
		Slug: slug, Title: src.Title, CoverURL: src.CoverURL, Summary: src.Summary,
		SeoDesc: src.SeoDesc, SeoKeywords: src.SeoKeywords, Content: src.Content,
		I18nJSON: src.I18nJSON,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Model(&row).Association("Categories").Replace(src.Categories); err != nil {
			return err
		}
		return tx.Model(&row).Association("Tags").Replace(src.Tags)
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// freePostSlug 给副本挑一个没被占用的 slug；slug 列上限 64，放不下后缀就先截短原文
// （base 本身只含合法字符，截短不破坏 slug 规则）。
func (s *Service) freePostSlug(ctx context.Context, base string) (string, error) {
	candidate := truncateSlug(base+"-copy", "post")
	for i := 2; ; i++ {
		var n int64
		if err := s.db.WithContext(ctx).Model(&Post{}).Where("slug = ?", candidate).Count(&n).Error; err != nil {
			return "", err
		}
		if n == 0 {
			return candidate, nil
		}
		candidate = truncateSlug(fmt.Sprintf("%s-copy-%d", base, i), "post")
	}
}

// ---- 新闻版本历史 ----

// maxPostVersions 是单条新闻的版本保留上限：官网新闻编辑频繁，超限若按 formbuilder
// 的做法报 conflict 会把正常保存整个挡住，编辑寸步难行；这里选滚动丢弃最旧的，
// 恒定保留最近 maxPostVersions 条。归档与裁剪都在覆盖保存的同一事务里完成。
const maxPostVersions = 50

// contentFingerprint 是快照守卫的正文指纹：content 是 mediumtext，先哈希再比
// 比整串逐字节比较便宜，也让「内容相同不重复存」这条守卫在测试里可断言。
func contentFingerprint(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// editorName 取当前登录用户名作为快照归属（归档时正在保存的那位编辑）；
// 用户名空的老令牌回退用户 ID 串，无登录态（种子、后台任务）返回空。
func editorName(ctx context.Context) string {
	cl := auth.ClaimsFromContext(ctx)
	if cl == nil {
		return ""
	}
	if name := strings.TrimSpace(cl.Username); name != "" {
		return trimLen(name, 64)
	}
	return strconv.FormatInt(int64(cl.UserID), 10)
}

// archivePostVersion 把覆盖前的旧行存进版本历史表（同 workflow 定义版本的范式）：
// 正文指纹没变就不重复存——只挪标题/分类的历史没有回看价值，还会白占保留额度；
// 版本号 = 该新闻现有最大 version+1（滚动丢弃后表行数不等于最大版本号，不能拿行号或下标发号）。
// 超过上限时删掉最旧的整段，保留最近 maxPostVersions 条。返回本次落的版本号（没存给 0）。
func archivePostVersion(ctx context.Context, tx *gorm.DB, prev *Post, newContent string) (int, error) {
	if contentFingerprint(prev.Content) == contentFingerprint(newContent) {
		return 0, nil
	}
	var maxVer int
	if err := tx.Model(&PostVersion{}).Where("post_id = ?", int64(prev.ID)).
		Select("COALESCE(MAX(version), 0)").Scan(&maxVer).Error; err != nil {
		return 0, err
	}
	next := maxVer + 1
	snap := PostVersion{
		PostID: prev.ID, Version: next, Title: prev.Title, Summary: prev.Summary,
		CoverURL: prev.CoverURL, Content: prev.Content, I18nJSON: prev.I18nJSON,
		SeoTitle: prev.SeoTitle, SeoDesc: prev.SeoDesc, Noindex: prev.Noindex,
		Published: prev.Published, PublishedAt: prev.PublishedAt, CreatedBy: editorName(ctx),
	}
	if err := tx.Create(&snap).Error; err != nil {
		return 0, err
	}
	// 版本号逐次 +1 连续递增，所以 version <= next-上限 恰好是超额的最旧段。
	if next > maxPostVersions {
		if err := tx.Where("post_id = ? AND version <= ?", int64(prev.ID), next-maxPostVersions).
			Delete(&PostVersion{}).Error; err != nil {
			return 0, err
		}
	}
	return next, nil
}

// PostVersionView 是版本历史的一条展示行：不含正文（mediumtext 不进列表响应），
// 要看内容走 GetPostVersion 单版本详情。
type PostVersionView struct {
	Version   int       `json:"version"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// PostVersionDetail 是单个历史版本的详情：唯一回传快照正文的接口，
// Words 是剥掉 HTML 后的字符数——界面按「标题/摘要 + 字数」预览，不把整篇 HTML 上屏。
type PostVersionDetail struct {
	PostVersionView
	CoverURL    string     `json:"coverUrl"`
	Content     string     `json:"content"`
	Words       int        `json:"words"`
	Published   bool       `json:"published"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

func (r *PostVersion) versionView() PostVersionView {
	return PostVersionView{
		Version: r.Version, Title: r.Title, Summary: r.Summary,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
	}
}

// ListPostVersions 列出一条新闻的版本历史（不含正文），新→旧，最多 maxPostVersions 条。
// 新闻不存在给 404：对着已删新闻的历史列表静默回空，界面会把「没权限看」误报成「没历史」。
func (s *Service) ListPostVersions(ctx context.Context, postID idgen.ID) ([]PostVersionView, error) {
	if _, err := s.PostByID(ctx, postID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("website.post_not_found")
		}
		return nil, err
	}
	var rows []PostVersion
	// Select 只取展示列：列表绝不把 mediumtext 正文带出来。
	err := s.db.WithContext(ctx).Model(&PostVersion{}).
		Where("post_id = ?", int64(postID)).
		Select("version", "title", "summary", "created_by", "created_at").
		Order("version DESC").Limit(maxPostVersions).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]PostVersionView, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].versionView())
	}
	return out, nil
}

func (s *Service) postVersionRow(ctx context.Context, postID idgen.ID, version int) (*PostVersion, error) {
	var row PostVersion
	err := s.db.WithContext(ctx).Where("post_id = ? AND version = ?", int64(postID), version).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.NotFound("website.post_not_found")
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetPostVersion 取单个历史版本的详情（正文只在这里回传）。
func (s *Service) GetPostVersion(ctx context.Context, postID idgen.ID, version int) (*PostVersionDetail, error) {
	row, err := s.postVersionRow(ctx, postID, version)
	if err != nil {
		return nil, err
	}
	detail := &PostVersionDetail{
		PostVersionView: row.versionView(),
		CoverURL:        row.CoverURL,
		Content:         row.Content,
		Words:           utf8.RuneCountInString(plainFromHTML(row.Content)),
		Published:       row.Published,
		PublishedAt:     row.PublishedAt,
	}
	return detail, nil
}

// postWithTaxonomy 取当前行并带上分类/标签：回滚要在「不改这些快照外字段」的前提下构造表单，
// PostByID 不带关联（列表路径不预载），拿它当底会把分类清空——回滚恢复内容，不该拆栏目。
func (s *Service) postWithTaxonomy(ctx context.Context, postID idgen.ID) (*Post, error) {
	var row Post
	err := s.db.WithContext(ctx).Preload("Categories").Preload("Tags").First(&row, "id = ?", int64(postID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, response.NotFound("website.post_not_found")
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// RollbackPostVersion 回滚到历史版本：拿那份快照走一次正常 SavePost——覆盖前先归档当前内容，
// 版本继续递增、回滚本身也可被再回滚（同 workflow 定义版本的范式，不删不改历史行）。
// 快照没有的字段沿用当前行：slug 与分类/标签不动（回滚恢复的是内容，不是 URL 与视图），
// 按语言发布态由 SavePost 依恢复出的译文重新校验，缺译语言照旧被服务端降级。
func (s *Service) RollbackPostVersion(ctx context.Context, postID idgen.ID, version int) (*Post, error) {
	snap, err := s.postVersionRow(ctx, postID, version)
	if err != nil {
		return nil, err
	}
	cur, err := s.postWithTaxonomy(ctx, postID)
	if err != nil {
		return nil, err
	}
	f := PostForm{
		Slug: cur.Slug, Title: snap.Title, CoverURL: snap.CoverURL, Summary: snap.Summary,
		SeoTitle: snap.SeoTitle, SeoDesc: snap.SeoDesc, SeoKeywords: cur.SeoKeywords,
		Content: snap.Content, Published: snap.Published, PublishedAt: snap.PublishedAt,
		I18nJSON: snap.I18nJSON, PublishedJSON: cur.PublishedJSON,
		NoIndex: snap.Noindex, Author: cur.Author, Pinned: cur.Pinned,
	}
	for _, c := range cur.Categories {
		f.CategoryIDs = append(f.CategoryIDs, c.ID)
	}
	for _, tg := range cur.Tags {
		f.Tags = append(f.Tags, tg.Name)
	}
	return s.SavePost(ctx, postID, f)
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
	if err := livePostTx(s.db.WithContext(ctx)).Order("pinned DESC, published_at DESC").Limit(window).Find(&posts).Error; err != nil {
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
	err := livePostTx(s.db.WithContext(ctx)).Preload("Categories").Preload("Tags").Where("slug = ?", slug).First(&row).Error
	if err != nil {
		return &row, err
	}
	// 整行已发布不等于每种语言都已发布：该语言未发布就当作不存在，让它走 404，
	// 而不是把一篇中文页挂在 /ja/ 下面。
	if !pi18n.Published(row.Published, row.PublishedJSON, lang, siteLocales{}) {
		return nil, gorm.ErrRecordNotFound
	}
	row.localize(lang)
	// 浏览量自增：非致命，计数失败不影响渲染（预览令牌的草稿访问不计）。
	_ = s.db.WithContext(ctx).Model(&Post{}).Where("id = ?", int64(row.ID)).
		UpdateColumn("views", gorm.Expr("views + 1")).Error
	return &row, nil
}

// livePostTx 是公开端新闻的上线条件：整篇已发布，且发布时间不在未来。
// 定时发布（published_at 设为未来时间）到点才对访客可见，管理端不受影响。
func livePostTx(tx *gorm.DB) *gorm.DB {
	return tx.Where("published = ? AND (published_at IS NULL OR published_at <= ?)", true, time.Now().UTC())
}

// newsPageCap 是公开新闻列表一次参与排序的行数上限：按语言发布不是 SQL 谓词，
// 只能先取有界窗口再在 Go 里筛，窗口给到两千行对官网新闻足够，也不会全表捞。
const newsPageCap = 2000

// PublishedPostsPage 是公开新闻列表的分页查询：返回该语言已上线的新闻与总数。
// 窗口内先按语言与发布时间筛出真实集合再切片取页，总数是筛完的准确值。
// term 非空时先按栏目/标签收窄，归档页与全部新闻共用这一条查询。
func (s *Service) PublishedPostsPage(ctx context.Context, lang string, term termFilter, page, pageSize int) ([]Post, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	windowTx := livePostTx(s.db.WithContext(ctx))
	switch term.Kind {
	case "category":
		windowTx = windowTx.Where("id IN (SELECT pc.post_id FROM plg_website_post_category pc JOIN plg_website_category wc ON wc.id = pc.category_id WHERE wc.slug = ?)", term.Slug)
	case "tag":
		windowTx = windowTx.Where("id IN (SELECT pt.post_id FROM plg_website_post_tag pt JOIN plg_website_tag wt ON wt.id = pt.tag_id WHERE wt.slug = ?)", term.Slug)
	}
	var window []Post
	if err := windowTx.
		Select("id", "published_json").Order("pinned DESC, published_at DESC, id DESC").
		Limit(newsPageCap).Find(&window).Error; err != nil {
		return nil, 0, err
	}
	ids := make([]int64, 0, len(window))
	for _, p := range window {
		if len(Languages()) > 1 && !pi18n.Published(true, p.PublishedJSON, lang, siteLocales{}) {
			continue
		}
		ids = append(ids, int64(p.ID))
	}
	total := int64(len(ids))
	start := (page - 1) * pageSize
	if start >= len(ids) {
		return nil, total, nil
	}
	end := start + pageSize
	if end > len(ids) {
		end = len(ids)
	}
	pageIDs := ids[start:end]
	var rows []Post
	if err := s.db.WithContext(ctx).Preload("Categories").Preload("Tags").Where("id IN ?", pageIDs).
		Order("published_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	for i := range rows {
		rows[i].localize(lang)
	}
	return rows, total, nil
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

// 媒体图片以字节直接存进 plg_website_media 表，所以 mediaHardMaxMB 是硬顶：
// 系统参数与 config.yaml 只能把上限调到它之下，不能把数据库当对象存储。
const (
	mediaDefaultMaxMB = 2.0
	mediaHardMaxMB    = 5.0
)

// mediaImageTypes 是内置位图白名单：svg 可携带脚本，不当图片存储。
// 清单由代码持有并下发给前端，提示文案与文件选择器都跟着它走，不再各处手抄。
var mediaImageTypes = []struct {
	mime, label string
}{
	{"image/png", "PNG"},
	{"image/jpeg", "JPEG"},
	{"image/webp", "WebP"},
	{"image/gif", "GIF"},
}

// MediaLimits 是媒体上传的生效限制，前端按它渲染 accept 与提示文案。
type MediaLimits struct {
	MaxMB    float64  `json:"maxMb"`
	MaxBytes int      `json:"maxBytes"`
	Mimes    []string `json:"mimes"`
	Labels   []string `json:"labels"`
}

// Text 是上限的人话写法：2MB / 2.5MB（去掉小数尾巴）。
func (l MediaLimits) Text() string {
	return strconv.FormatFloat(l.MaxMB, 'f', -1, 64) + "MB"
}

// TypesText 是类型清单的人话写法：PNG / JPEG / WebP / GIF。
func (l MediaLimits) TypesText() string { return strings.Join(l.Labels, " / ") }

func (l MediaLimits) allows(mime string) bool {
	for _, m := range l.Mimes {
		if m == mime {
			return true
		}
	}
	return false
}

// MediaLimits 返回当前生效的媒体上传限制。大小：系统参数「上传限制」> config.yaml 的
// upload.max_size_image_mb > 内置默认，最后截到 mediaHardMaxMB；类型：位图白名单，由代码持有。
func (s *Service) MediaLimits(ctx context.Context) MediaLimits {
	mb := s.cfgImageMaxMB
	if l, ok := core.UploadLimitsFromSettings(ctx); ok && l.MaxImageMB > 0 {
		mb = l.MaxImageMB
	}
	if mb <= 0 {
		mb = mediaDefaultMaxMB
	}
	lim := MediaLimits{MaxMB: min(mb, mediaHardMaxMB)}
	lim.MaxBytes = int(lim.MaxMB * 1024 * 1024)
	for _, t := range mediaImageTypes {
		lim.Mimes = append(lim.Mimes, t.mime)
		lim.Labels = append(lim.Labels, t.label)
	}
	return lim
}

// WebsiteLimits 是官网管理页要用到的全部生效上限：图片上传、页脚两个列表、导航层级、新闻标签数。
// 这些数字原本在 Go、前端常量与语言包里各写一份，改一处就漂；这里收成一份，前端只消费。
type WebsiteLimits struct {
	Media       MediaLimits `json:"media"`
	SocialMax   int         `json:"socialMax"`
	FriendMax   int         `json:"friendMax"`
	NavMaxDepth int         `json:"navMaxDepth"`
	PostTagsMax int         `json:"postTagsMax"`
}

// WebsiteLimits 汇总生效上限：图片大小随系统参数变化，其余取代码内的常量。
func (s *Service) WebsiteLimits(ctx context.Context) WebsiteLimits {
	return WebsiteLimits{
		Media: s.MediaLimits(ctx), SocialMax: maxSocialLinks, FriendMax: maxFriendLinks,
		NavMaxDepth: navMaxDepth, PostTagsMax: maxTagsPerPost,
	}
}

// SaveMedia 校验并保存一张小图，返回可公开访问的 URL 路径。
// 类型按内容嗅探判定（http.DetectContentType），不信任扩展名。
func (s *Service) SaveMedia(ctx context.Context, data []byte) (*Media, error) {
	lim := s.MediaLimits(ctx)
	if len(data) == 0 {
		return nil, response.BadRequest("website.file_empty")
	}
	if len(data) > lim.MaxBytes {
		return nil, response.BadRequest("website.image_too_large", lim.Text())
	}
	mime := http.DetectContentType(data)
	if !lim.allows(mime) {
		return nil, response.BadRequest("website.image_type_unsupported", lim.TypesText())
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

// SitemapEntries 汇总官网全部已发布页面（首页、新闻列表、新闻、产品列表、产品、自定义页）。
func (s *Service) SitemapEntries(ctx context.Context) ([]SitemapEntry, error) {
	out := []SitemapEntry{{Loc: "/"}, {Loc: "/news"}, {Loc: "/products"}}
	var posts []Post
	// noindex 的新闻不进 sitemap（<meta robots> 已拒绝收录，list 里再给条目只会浪费抓取配额）。
	if err := livePostTx(s.db.WithContext(ctx)).Where("noindex = ?", false).
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
	// noindex 的页面同理：不进 sitemap，避免与 meta robots 打架。
	if err := s.db.WithContext(ctx).Where("published = ? AND noindex = ?", true, false).
		Select("slug", "updated_at", "published_json").Order("updated_at DESC").Limit(1000).Find(&pages).Error; err != nil {
		return nil, err
	}
	for _, p := range pages {
		out = append(out, SitemapEntry{
			Loc: "/p/" + p.Slug, Lastmod: &p.UpdatedAt,
			Locales: publishedLocales(p.Published, p.PublishedJSON),
		})
	}
	// 产品与新闻同口径：noindex 的不进 sitemap，避免与 meta robots 打架。
	products, err := s.publishedProductSitemapEntries(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range products {
		lastmod := p.UpdatedAt
		out = append(out, SitemapEntry{
			Loc: "/products/" + p.Slug, Lastmod: &lastmod,
			Locales: publishedLocales(p.Published(), p.PublishedJSON),
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
		// 与 handler 的预校验同一口径走 i18n 键：这里兜的是绕过 handler 的直接调用
		return response.BadRequest("website.rating_invalid")
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
// 落库成功后发 website.message.created；通知由订阅者异步做（见 notify.go），
// 请求路径不等发送结果——宁可丢事件，也不拖慢访客投稿。
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
		Status: MsgNew,
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	s.publishCreated(ctx, row)
	return row, nil
}

// messageFilter 是列表与导出共用的筛选条件：保证导出的文件和页面上看到的列表是同一批数据。
func messageFilter(tx *gorm.DB, q MessageQuery) *gorm.DB {
	if len(q.Status) > 0 {
		tx = tx.Where("status IN ?", q.Status)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("LOWER(name) LIKE LOWER(?) OR LOWER(email) LIKE LOWER(?) OR LOWER(content) LIKE LOWER(?)", like, like, like)
	}
	return tx
}

// ListMessages 是后台留言列表：按时间倒序，可按状态与关键字筛。
func (s *Service) ListMessages(ctx context.Context, q MessageQuery) ([]Message, int64, error) {
	tx := messageFilter(s.db.WithContext(ctx).Model(&Message{}), q)
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

// CountNewMessages 返回未读（new）留言数：侧边栏徽标与收件箱入口的数据源。
func (s *Service) CountNewMessages(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Message{}).Where("status = ?", MsgNew).Count(&n).Error
	return n, err
}

// exportMax 是单次导出的行数上限。留言是人工跟进的低频数据，超过这个量级说明
// 该接入导出中心走异步任务了，同步一把梭反而拖垮请求。
const exportMax = 10000

// ExportMessages 取当前筛选下的全部留言（不带分页），前端据此生成 CSV；
// 筛选与 ListMessages 同源，导出文件和页面上看到的列表对得上。
func (s *Service) ExportMessages(ctx context.Context, q MessageQuery) ([]Message, error) {
	var rows []Message
	err := messageFilter(s.db.WithContext(ctx).Model(&Message{}), q).
		Order("created_at DESC, id DESC").Limit(exportMax).Find(&rows).Error
	return rows, err
}

// SetMessagesStatus 批量把 ids 里的留言置为某个状态，返回实际改动条数。
// 全部没命中（被别人删了）才算 404；部分命中是并发下的常态，如实返回条数让界面刷新。
func (s *Service) SetMessagesStatus(ctx context.Context, ids []idgen.ID, status string) (int64, error) {
	if len(ids) == 0 {
		return 0, response.BadRequest("website.message_ids_empty")
	}
	if !ValidMessageStatus(status) {
		return 0, response.BadRequest("website.message_status_invalid")
	}
	res := s.db.WithContext(ctx).Model(&Message{}).
		Where("id IN ?", int64IDs(ids)).Update("status", status)
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, response.NotFound("website.message_not_found")
	}
	return res.RowsAffected, nil
}

// DeleteMessages 批量删除留言，返回实际删除条数（语义同 SetMessagesStatus）。
func (s *Service) DeleteMessages(ctx context.Context, ids []idgen.ID) (int64, error) {
	if len(ids) == 0 {
		return 0, response.BadRequest("website.message_ids_empty")
	}
	res := s.db.WithContext(ctx).Delete(&Message{}, "id IN ?", int64IDs(ids))
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, response.NotFound("website.message_not_found")
	}
	return res.RowsAffected, nil
}

// int64IDs 把 ID 列表转成 IN 子句认得的类型：[]idgen.ID 走反射取值，
// 不同驱动对自定义整数别名的处理不一致，转 int64 是三库都稳的写法。
func int64IDs(ids []idgen.ID) []int64 {
	out := make([]int64, len(ids))
	for i, id := range ids {
		out[i] = int64(id)
	}
	return out
}
