package website

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	"nexadmin/pkg/htmlsafe"
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
		return response.BadRequest("error.bad_request").WithMsg("slug 只能是小写字母、数字与连字符，长度 1-64")
	}
	if reservedSlugs[s] || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return response.BadRequest("error.bad_request").WithMsg("slug 不可用：" + s)
	}
	return nil
}

func trimLen(s string, max int) string {
	s = strings.TrimSpace(s)
	return s[:min(len(s), max)]
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
	row.ICP = trimLen(f.ICP, 128)
	row.FooterNote = trimLen(f.FooterNote, 300)
	i18n, err := normalizeI18nJSON(f.I18nJSON, settingI18nFields)
	if err != nil {
		return nil, response.BadRequest("error.bad_request").WithMsg(err.Error())
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

func (s *Service) SaveNav(ctx context.Context, id idgen.ID, f ItemForm) (*Nav, error) {
	f.Label = trimLen(f.Label, 64)
	f.Href = trimLen(f.Href, 255)
	if f.Label == "" || f.Href == "" {
		return nil, response.BadRequest("error.bad_request").WithMsg("导航需要名称与链接")
	}
	var row Nav
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
	} else {
		row = Nav{}
	}
	i18n, err := normalizeI18nJSON(f.I18nJSON, navI18nFields)
	if err != nil {
		return nil, response.BadRequest("error.bad_request").WithMsg(err.Error())
	}
	row.Label, row.Href, row.Sort, row.Enabled, row.I18nJSON = f.Label, f.Href, f.Sort, f.Enabled, i18n
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Service) DeleteNav(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Delete(&Nav{}, "id = ?", int64(id)).Error
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
		return nil, response.BadRequest("error.bad_request").WithMsg("特性需要标题")
	}
	var row Feature
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
	} else {
		row = Feature{}
	}
	i18n, err := normalizeI18nJSON(f.I18nJSON, featureI18nFields)
	if err != nil {
		return nil, response.BadRequest("error.bad_request").WithMsg(err.Error())
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
	Keyword  string
	Status   string // "" | "published" | "draft"
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
		return nil, response.BadRequest("error.bad_request").WithMsg("页面需要标题")
	}
	var row Page
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
	} else {
		row = Page{}
	}
	i18n, err := normalizeI18nJSON(f.I18nJSON, pageI18nFields)
	if err != nil {
		return nil, response.BadRequest("error.bad_request").WithMsg(err.Error())
	}
	row.Slug, row.Title, row.SeoDesc = f.Slug, f.Title, f.SeoDesc
	row.Content, row.Published, row.I18nJSON = htmlsafe.Sanitize(f.Content), f.Published, i18n
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
	f.CoverURL = trimLen(f.CoverURL, 255)
	if err := ValidateSlug(f.Slug); err != nil {
		return nil, err
	}
	if f.Title == "" {
		return nil, response.BadRequest("error.bad_request").WithMsg("新闻需要标题")
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
	i18n, err := normalizeI18nJSON(f.I18nJSON, postI18nFields)
	if err != nil {
		return nil, response.BadRequest("error.bad_request").WithMsg(err.Error())
	}
	row.Slug, row.Title, row.CoverURL, row.Summary = f.Slug, f.Title, f.CoverURL, f.Summary
	row.Content, row.Published, row.I18nJSON = htmlsafe.Sanitize(f.Content), f.Published, i18n
	if f.Published && (!wasPublished || row.PublishedAt == nil) {
		now := time.Now().UTC()
		row.PublishedAt = &now
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
	Navs     []Nav
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
	for i := range navs {
		navs[i].localize(lang)
	}
	var features []Feature
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("sort, id").Find(&features).Error; err != nil {
		return nil, err
	}
	for i := range features {
		features[i].localize(lang)
	}
	var posts []Post
	if err := s.db.WithContext(ctx).Where("published = ?", true).
		Order("published_at DESC").Limit(postLimit).Find(&posts).Error; err != nil {
		return nil, err
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
	row.localize(lang)
	return &row, err
}

// PublishedPageBy 取一篇已发布的自定义页。
func (s *Service) PublishedPageBy(ctx context.Context, slug, lang string) (*Page, error) {
	var row Page
	err := s.db.WithContext(ctx).Where("slug = ? AND published = ?", slug, true).First(&row).Error
	row.localize(lang)
	return &row, err
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
		return nil, response.BadRequest("error.bad_request").WithMsg("文件为空")
	}
	if len(data) > mediaMaxBytes {
		return nil, response.BadRequest("error.bad_request").WithMsg("图片不能超过 2MB")
	}
	mime := http.DetectContentType(data)
	if !mediaMimes[mime] {
		return nil, response.BadRequest("error.bad_request").WithMsg("只支持 PNG / JPEG / WebP / GIF 图片")
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
}

// SitemapEntries 汇总官网全部已发布页面（首页、新闻列表、新闻、自定义页）。
func (s *Service) SitemapEntries(ctx context.Context) ([]SitemapEntry, error) {
	out := []SitemapEntry{{Loc: "/"}, {Loc: "/news"}}
	var posts []Post
	if err := s.db.WithContext(ctx).Where("published = ?", true).
		Select("slug", "published_at", "updated_at").Order("published_at DESC").Limit(1000).Find(&posts).Error; err != nil {
		return nil, err
	}
	for _, p := range posts {
		e := SitemapEntry{Loc: "/news/" + p.Slug, Lastmod: p.PublishedAt}
		if e.Lastmod == nil {
			e.Lastmod = &p.UpdatedAt
		}
		out = append(out, e)
	}
	var pages []Page
	if err := s.db.WithContext(ctx).Where("published = ?", true).
		Select("slug", "updated_at").Order("updated_at DESC").Limit(1000).Find(&pages).Error; err != nil {
		return nil, err
	}
	for _, p := range pages {
		out = append(out, SitemapEntry{Loc: "/p/" + p.Slug, Lastmod: &p.UpdatedAt})
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
